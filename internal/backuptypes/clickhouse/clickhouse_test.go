package clickhouse

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/shyim/docker-backup/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	chmodule "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

func TestClickHouseBackup_Name(t *testing.T) {
	c := &ClickHouseBackup{}
	assert.Equal(t, "clickhouse", c.Name())
}

func TestClickHouseBackup_FileExtension(t *testing.T) {
	c := &ClickHouseBackup{}
	assert.Equal(t, ".tar.zst", c.FileExtension())
}

func TestClickHouseBackup_Validate(t *testing.T) {
	c := &ClickHouseBackup{}

	tests := []struct {
		name      string
		container *docker.ContainerInfo
	}{
		{
			name: "valid with all env vars",
			container: &docker.ContainerInfo{
				Name: "test",
				Env: map[string]string{
					"CLICKHOUSE_USER":     "admin",
					"CLICKHOUSE_PASSWORD": "secret",
					"CLICKHOUSE_DB":       "testdb",
				},
			},
		},
		{
			name: "valid with no env vars (defaults)",
			container: &docker.ContainerInfo{
				Name: "test",
				Env:  map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Validate(tt.container)
			assert.NoError(t, err)
		})
	}
}

func TestClickHouseBackup_GetCredentials(t *testing.T) {
	c := &ClickHouseBackup{}

	tests := []struct {
		name         string
		env          map[string]string
		expectedUser string
		expectedPass string
	}{
		{
			name: "explicit credentials",
			env: map[string]string{
				"CLICKHOUSE_USER":     "admin",
				"CLICKHOUSE_PASSWORD": "secret",
			},
			expectedUser: "admin",
			expectedPass: "secret",
		},
		{
			name:         "defaults when no env vars",
			env:          map[string]string{},
			expectedUser: "default",
			expectedPass: "",
		},
		{
			name: "user without password",
			env: map[string]string{
				"CLICKHOUSE_USER": "admin",
			},
			expectedUser: "admin",
			expectedPass: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, pass := c.getCredentials(tt.env)
			assert.Equal(t, tt.expectedUser, user)
			assert.Equal(t, tt.expectedPass, pass)
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantMajor   int
		wantMinor   int
		expectError bool
	}{
		{
			name:      "standard format",
			output:    "ClickHouse client version 24.3.1.2672 (official build).",
			wantMajor: 24,
			wantMinor: 3,
		},
		{
			name:      "version 22.8",
			output:    "ClickHouse client version 22.8.15.25 (official build).",
			wantMajor: 22,
			wantMinor: 8,
		},
		{
			name:      "old version",
			output:    "ClickHouse client version 21.3.2.5 (official build).",
			wantMajor: 21,
			wantMinor: 3,
		},
		{
			name:      "with trailing newline",
			output:    "ClickHouse client version 24.3.1.2672 (official build).\n",
			wantMajor: 24,
			wantMinor: 3,
		},
		{
			name:        "no version keyword",
			output:      "something unexpected",
			expectError: true,
		},
		{
			name:        "empty output",
			output:      "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, err := parseVersion(tt.output)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantMajor, major)
				assert.Equal(t, tt.wantMinor, minor)
			}
		})
	}
}

func TestClickHouseBackup_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	chContainer, err := chmodule.Run(ctx,
		"clickhouse/clickhouse-server:latest",
		chmodule.WithDatabase("testdb"),
		chmodule.WithUsername("default"),
		chmodule.WithPassword("testpass"),
	)
	require.NoError(t, err)
	defer func() {
		if err := chContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := chContainer.GetContainerID()
	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() { _ = dockerClient.Close() }()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	connStr, err := chContainer.ConnectionString(ctx)
	require.NoError(t, err)

	db, err := sql.Open("clickhouse", connStr)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 30*time.Second, 500*time.Millisecond)

	_, err = db.Exec(`CREATE TABLE testdb.users (id UInt32, name String, email String) ENGINE = MergeTree() ORDER BY id`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO testdb.users (id, name, email) VALUES (1, 'Alice', 'alice@example.com'), (2, 'Bob', 'bob@example.com'), (3, 'Charlie', 'charlie@example.com')`)
	require.NoError(t, err)

	c := &ClickHouseBackup{}
	var backupBuffer bytes.Buffer
	err = c.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)
	assert.Greater(t, backupBuffer.Len(), 0, "backup should not be empty")
	t.Logf("Backup size: %d bytes", backupBuffer.Len())

	_, err = db.Exec(`DROP TABLE testdb.users`)
	require.NoError(t, err)

	var count int
	err = db.QueryRow(`SELECT count() FROM system.tables WHERE database = 'testdb' AND name = 'users'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "users table should be dropped")

	err = c.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	err = db.QueryRow(`SELECT count() FROM testdb.users`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "should have 3 users restored")

	var name, email string
	err = db.QueryRow(`SELECT name, email FROM testdb.users WHERE id = 1`).Scan(&name, &email)
	require.NoError(t, err)
	assert.Equal(t, "Alice", name)
	assert.Equal(t, "alice@example.com", email)
}

func TestClickHouseBackup_SpecificDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	chContainer, err := chmodule.Run(ctx,
		"clickhouse/clickhouse-server:latest",
		chmodule.WithDatabase("myapp"),
		chmodule.WithUsername("default"),
		chmodule.WithPassword("testpass"),
	)
	require.NoError(t, err)
	defer func() {
		if err := chContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := chContainer.GetContainerID()
	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() { _ = dockerClient.Close() }()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	connStr, err := chContainer.ConnectionString(ctx)
	require.NoError(t, err)

	db, err := sql.Open("clickhouse", connStr)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 30*time.Second, 500*time.Millisecond)

	_, err = db.Exec(`CREATE TABLE myapp.events (id UInt32, name String) ENGINE = MergeTree() ORDER BY id`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO myapp.events (id, name) VALUES (1, 'event1'), (2, 'event2'), (3, 'event3')`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE myapp.products (id UInt32, price Float64) ENGINE = MergeTree() ORDER BY id`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO myapp.products (id, price) VALUES (1, 9.99), (2, 19.99)`)
	require.NoError(t, err)

	c := &ClickHouseBackup{}
	var backupBuffer bytes.Buffer
	err = c.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)
	assert.Greater(t, backupBuffer.Len(), 0)
	t.Logf("Backup size: %d bytes", backupBuffer.Len())

	_, err = db.Exec(`DROP TABLE myapp.events`)
	require.NoError(t, err)
	_, err = db.Exec(`DROP TABLE myapp.products`)
	require.NoError(t, err)

	err = c.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	var count int
	err = db.QueryRow(`SELECT count() FROM myapp.events`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "should have 3 events restored")

	err = db.QueryRow(`SELECT count() FROM myapp.products`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should have 2 products restored")

	var price float64
	err = db.QueryRow(`SELECT price FROM myapp.products WHERE id = 1`).Scan(&price)
	require.NoError(t, err)
	assert.Equal(t, 9.99, price)
}

func TestClickHouseBackup_LargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	chContainer, err := chmodule.Run(ctx,
		"clickhouse/clickhouse-server:latest",
		chmodule.WithDatabase("testdb"),
		chmodule.WithUsername("default"),
		chmodule.WithPassword("testpass"),
	)
	require.NoError(t, err)
	defer func() {
		if err := chContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := chContainer.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	connStr, err := chContainer.ConnectionString(ctx)
	require.NoError(t, err)

	db, err := sql.Open("clickhouse", connStr)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 30*time.Second, 500*time.Millisecond)

	_, err = db.Exec(`
		CREATE TABLE testdb.large_data (
			id UInt32,
			data String,
			created_at DateTime DEFAULT now()
		) ENGINE = MergeTree()
		ORDER BY id
	`)
	require.NoError(t, err)

	for i := 0; i < 1000; i++ {
		_, err = db.Exec(`INSERT INTO testdb.large_data (id, data) VALUES (?, ?)`,
			i, fmt.Sprintf("This is test data row %d with some additional content to make it larger", i))
		require.NoError(t, err)
	}

	c := &ClickHouseBackup{}
	var backupBuffer bytes.Buffer
	err = c.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	t.Logf("Large data backup size: %d bytes", backupBuffer.Len())

	_, err = db.Exec(`DROP TABLE testdb.large_data`)
	require.NoError(t, err)

	err = c.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	var count int
	err = db.QueryRow(`SELECT count() FROM testdb.large_data`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1000, count, "should have 1000 rows restored")

	var data string
	err = db.QueryRow(`SELECT data FROM testdb.large_data WHERE id = 500`).Scan(&data)
	require.NoError(t, err)
	assert.Contains(t, data, "row 500")
}
