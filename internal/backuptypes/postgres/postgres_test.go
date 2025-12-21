package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shyim/docker-backup/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresBackup_Name(t *testing.T) {
	p := &PostgresBackup{}
	assert.Equal(t, "postgres", p.Name())
}

func TestPostgresBackup_FileExtension(t *testing.T) {
	p := &PostgresBackup{}
	assert.Equal(t, ".tar.zst", p.FileExtension())
}

func TestPostgresBackup_Validate(t *testing.T) {
	p := &PostgresBackup{}

	tests := []struct {
		name        string
		container   *docker.ContainerInfo
		expectError bool
	}{
		{
			name: "valid with POSTGRES_USER",
			container: &docker.ContainerInfo{
				Name: "test",
				Env: map[string]string{
					"POSTGRES_USER": "testuser",
				},
			},
			expectError: false,
		},
		{
			name: "valid with PGUSER",
			container: &docker.ContainerInfo{
				Name: "test",
				Env: map[string]string{
					"PGUSER": "testuser",
				},
			},
			expectError: false,
		},
		{
			name: "invalid missing user",
			container: &docker.ContainerInfo{
				Name: "test",
				Env:  map[string]string{},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.Validate(tt.container)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPostgresBackup_Integration tests the full backup and restore cycle
// using a real PostgreSQL container via testcontainers.
func TestPostgresBackup_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	// Get container ID
	containerID := pgContainer.GetContainerID()

	// Create Docker client
	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer dockerClient.Close()

	// Get container info
	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Get connection string for direct database access
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Create test data
	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	defer db.Close()

	// Wait for database to be ready
	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 10*time.Second, 100*time.Millisecond)

	// Create test table and insert data
	_, err = db.Exec(`
		CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			email VARCHAR(100) NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO users (name, email) VALUES
		('Alice', 'alice@example.com'),
		('Bob', 'bob@example.com'),
		('Charlie', 'charlie@example.com')
	`)
	require.NoError(t, err)

	// Create another database to test multi-database backup
	_, err = db.Exec(`CREATE DATABASE seconddb`)
	require.NoError(t, err)

	// Connect to second database and create data
	connStr2, err := pgContainer.ConnectionString(ctx, "sslmode=disable", "dbname=seconddb")
	require.NoError(t, err)

	db2, err := sql.Open("pgx", connStr2)
	require.NoError(t, err)
	defer db2.Close()

	require.Eventually(t, func() bool {
		return db2.Ping() == nil
	}, 10*time.Second, 100*time.Millisecond)

	_, err = db2.Exec(`
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			price DECIMAL(10,2) NOT NULL
		)
	`)
	require.NoError(t, err)

	_, err = db2.Exec(`
		INSERT INTO products (name, price) VALUES
		('Widget', 9.99),
		('Gadget', 19.99)
	`)
	require.NoError(t, err)

	// Perform backup
	p := &PostgresBackup{}
	var backupBuffer bytes.Buffer
	err = p.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)
	assert.Greater(t, backupBuffer.Len(), 0, "backup should not be empty")

	t.Logf("Backup size: %d bytes", backupBuffer.Len())

	// Drop the databases to simulate data loss
	db2.Close()
	_, err = db.Exec(`DROP DATABASE seconddb`)
	require.NoError(t, err)

	_, err = db.Exec(`DROP TABLE users`)
	require.NoError(t, err)

	// Verify data is gone
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'users'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "users table should be dropped")

	// Perform restore
	err = p.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Verify data is restored in first database
	err = db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "should have 3 users restored")

	// Verify specific data
	var name, email string
	err = db.QueryRow(`SELECT name, email FROM users WHERE name = 'Alice'`).Scan(&name, &email)
	require.NoError(t, err)
	assert.Equal(t, "Alice", name)
	assert.Equal(t, "alice@example.com", email)

	// Verify second database is restored
	db2, err = sql.Open("pgx", connStr2)
	require.NoError(t, err)
	defer db2.Close()

	require.Eventually(t, func() bool {
		return db2.Ping() == nil
	}, 10*time.Second, 100*time.Millisecond)

	err = db2.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should have 2 products restored")

	// Verify specific product data
	var productName string
	var price float64
	err = db2.QueryRow(`SELECT name, price FROM products WHERE name = 'Widget'`).Scan(&productName, &price)
	require.NoError(t, err)
	assert.Equal(t, "Widget", productName)
	assert.Equal(t, 9.99, price)
}

// TestPostgresBackup_LargeData tests backup/restore with larger datasets
func TestPostgresBackup_LargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := pgContainer.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer dockerClient.Close()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	defer db.Close()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 10*time.Second, 100*time.Millisecond)

	// Create table with larger data
	_, err = db.Exec(`
		CREATE TABLE large_data (
			id SERIAL PRIMARY KEY,
			data TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	require.NoError(t, err)

	// Insert 1000 rows with varied data
	for i := 0; i < 1000; i++ {
		_, err = db.Exec(`INSERT INTO large_data (data) VALUES ($1)`,
			fmt.Sprintf("This is test data row %d with some additional content to make it larger", i))
		require.NoError(t, err)
	}

	// Perform backup
	p := &PostgresBackup{}
	var backupBuffer bytes.Buffer
	err = p.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	t.Logf("Large data backup size: %d bytes", backupBuffer.Len())

	// Drop table
	_, err = db.Exec(`DROP TABLE large_data`)
	require.NoError(t, err)

	// Restore
	err = p.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Verify all rows are restored
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM large_data`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1000, count, "should have 1000 rows restored")

	// Verify specific row
	var data string
	err = db.QueryRow(`SELECT data FROM large_data WHERE id = 500`).Scan(&data)
	require.NoError(t, err)
	assert.Contains(t, data, "row 499") // id 500 = row 499 (0-indexed insert)
}

// TestPostgresBackup_SpecialCharacters tests backup/restore with special characters
func TestPostgresBackup_SpecialCharacters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := pgContainer.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer dockerClient.Close()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	defer db.Close()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 10*time.Second, 100*time.Millisecond)

	// Create table
	_, err = db.Exec(`
		CREATE TABLE special_chars (
			id SERIAL PRIMARY KEY,
			content TEXT NOT NULL
		)
	`)
	require.NoError(t, err)

	// Insert data with special characters
	specialStrings := []string{
		"Hello 'World'",
		`Quote "test"`,
		"Unicode: äöü ñ 日本語 🎉",
		"Newlines:\nLine1\nLine2",
		"Tabs:\tColumn1\tColumn2",
		`SQL Injection: '; DROP TABLE users; --`,
		"Backslash: C:\\path\\to\\file",
		"Null bytes should be handled",
	}

	for _, s := range specialStrings {
		_, err = db.Exec(`INSERT INTO special_chars (content) VALUES ($1)`, s)
		require.NoError(t, err)
	}

	// Perform backup
	p := &PostgresBackup{}
	var backupBuffer bytes.Buffer
	err = p.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Drop table
	_, err = db.Exec(`DROP TABLE special_chars`)
	require.NoError(t, err)

	// Restore
	err = p.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Verify all special strings are restored correctly
	rows, err := db.Query(`SELECT content FROM special_chars ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	var restored []string
	for rows.Next() {
		var content string
		err = rows.Scan(&content)
		require.NoError(t, err)
		restored = append(restored, content)
	}

	require.Len(t, restored, len(specialStrings))
	for i, expected := range specialStrings {
		assert.Equal(t, expected, restored[i], "special string %d should match", i)
	}
}
