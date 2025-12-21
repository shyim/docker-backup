package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/shyim/docker-backup/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMySQLBackup_Name(t *testing.T) {
	m := &MySQLBackup{}
	assert.Equal(t, "mysql", m.Name())
}

func TestMySQLBackup_FileExtension(t *testing.T) {
	m := &MySQLBackup{}
	assert.Equal(t, ".tar.zst", m.FileExtension())
}

func TestMySQLBackup_Validate(t *testing.T) {
	m := &MySQLBackup{}

	tests := []struct {
		name        string
		container   *docker.ContainerInfo
		expectError bool
	}{
		{
			name: "valid with MYSQL_ROOT_PASSWORD",
			container: &docker.ContainerInfo{
				Name: "test",
				Env: map[string]string{
					"MYSQL_ROOT_PASSWORD": "rootpass",
				},
			},
			expectError: false,
		},
		{
			name: "valid with MYSQL_USER and MYSQL_PASSWORD",
			container: &docker.ContainerInfo{
				Name: "test",
				Env: map[string]string{
					"MYSQL_USER":     "testuser",
					"MYSQL_PASSWORD": "testpass",
				},
			},
			expectError: false,
		},
		{
			name: "invalid - MYSQL_PASSWORD without MYSQL_USER",
			container: &docker.ContainerInfo{
				Name: "test",
				Env: map[string]string{
					"MYSQL_PASSWORD": "testpass",
				},
			},
			expectError: true,
		},
		{
			name: "invalid - no password",
			container: &docker.ContainerInfo{
				Name: "test",
				Env:  map[string]string{},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.container)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMySQLBackup_GetCredentials(t *testing.T) {
	m := &MySQLBackup{}

	tests := []struct {
		name         string
		env          map[string]string
		expectedUser string
		expectedPass string
	}{
		{
			name: "root password takes precedence",
			env: map[string]string{
				"MYSQL_ROOT_PASSWORD": "rootpass",
				"MYSQL_USER":          "testuser",
				"MYSQL_PASSWORD":      "testpass",
			},
			expectedUser: "root",
			expectedPass: "rootpass",
		},
		{
			name: "falls back to user credentials",
			env: map[string]string{
				"MYSQL_USER":     "testuser",
				"MYSQL_PASSWORD": "testpass",
			},
			expectedUser: "testuser",
			expectedPass: "testpass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, pass := m.getCredentials(tt.env)
			assert.Equal(t, tt.expectedUser, user)
			assert.Equal(t, tt.expectedPass, pass)
		})
	}
}

// TestMySQLBackup_Integration tests the full backup and restore cycle
// using a real MySQL container via testcontainers.
func TestMySQLBackup_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MySQL container with root password for full access
	mysqlContainer, err := mysql.Run(ctx,
		"mysql:8.0",
		mysql.WithDatabase("testdb"),
		mysql.WithUsername("root"),
		mysql.WithPassword("rootpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("ready for connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() {
		if err := mysqlContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	// Get container ID
	containerID := mysqlContainer.GetContainerID()

	// Create Docker client
	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer dockerClient.Close()

	// Get container info and set MYSQL_ROOT_PASSWORD env for backup
	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)
	containerInfo.Env["MYSQL_ROOT_PASSWORD"] = "rootpass"

	// Get connection string for direct database access
	connStr, err := mysqlContainer.ConnectionString(ctx)
	require.NoError(t, err)

	// Create test data
	db, err := sql.Open("mysql", connStr)
	require.NoError(t, err)
	defer db.Close()

	// Wait for database to be ready
	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 30*time.Second, 500*time.Millisecond)

	// Create test table and insert data
	_, err = db.Exec(`
		CREATE TABLE users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			email VARCHAR(100) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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

	_, err = db.Exec(`USE seconddb`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE products (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			price DECIMAL(10,2) NOT NULL
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO products (name, price) VALUES
		('Widget', 9.99),
		('Gadget', 19.99)
	`)
	require.NoError(t, err)

	// Switch back to testdb for verification later
	_, err = db.Exec(`USE testdb`)
	require.NoError(t, err)

	// Perform backup
	m := &MySQLBackup{}
	var backupBuffer bytes.Buffer
	err = m.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)
	assert.Greater(t, backupBuffer.Len(), 0, "backup should not be empty")

	t.Logf("Backup size: %d bytes", backupBuffer.Len())

	// Drop the databases to simulate data loss
	_, err = db.Exec(`DROP DATABASE seconddb`)
	require.NoError(t, err)

	_, err = db.Exec(`DROP TABLE users`)
	require.NoError(t, err)

	// Verify data is gone
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'testdb' AND table_name = 'users'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "users table should be dropped")

	// Perform restore
	err = m.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
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
	_, err = db.Exec(`USE seconddb`)
	require.NoError(t, err)

	err = db.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should have 2 products restored")

	// Verify specific product data
	var productName string
	var price float64
	err = db.QueryRow(`SELECT name, price FROM products WHERE name = 'Widget'`).Scan(&productName, &price)
	require.NoError(t, err)
	assert.Equal(t, "Widget", productName)
	assert.Equal(t, 9.99, price)
}

// TestMySQLBackup_LargeData tests backup/restore with larger datasets
func TestMySQLBackup_LargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MySQL container with root access
	mysqlContainer, err := mysql.Run(ctx,
		"mysql:8.0",
		mysql.WithDatabase("testdb"),
		mysql.WithUsername("root"),
		mysql.WithPassword("rootpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("ready for connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() {
		if err := mysqlContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := mysqlContainer.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer dockerClient.Close()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)
	containerInfo.Env["MYSQL_ROOT_PASSWORD"] = "rootpass"

	connStr, err := mysqlContainer.ConnectionString(ctx)
	require.NoError(t, err)

	db, err := sql.Open("mysql", connStr)
	require.NoError(t, err)
	defer db.Close()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 30*time.Second, 500*time.Millisecond)

	// Create table with larger data
	_, err = db.Exec(`
		CREATE TABLE large_data (
			id INT AUTO_INCREMENT PRIMARY KEY,
			data TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Insert 1000 rows with varied data
	for i := 0; i < 1000; i++ {
		_, err = db.Exec(`INSERT INTO large_data (data) VALUES (?)`,
			fmt.Sprintf("This is test data row %d with some additional content to make it larger", i))
		require.NoError(t, err)
	}

	// Perform backup
	m := &MySQLBackup{}
	var backupBuffer bytes.Buffer
	err = m.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	t.Logf("Large data backup size: %d bytes", backupBuffer.Len())

	// Drop table
	_, err = db.Exec(`DROP TABLE large_data`)
	require.NoError(t, err)

	// Restore
	err = m.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
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

// TestMySQLBackup_SpecialCharacters tests backup/restore with special characters
func TestMySQLBackup_SpecialCharacters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	mysqlContainer, err := mysql.Run(ctx,
		"mysql:8.0",
		mysql.WithDatabase("testdb"),
		mysql.WithUsername("root"),
		mysql.WithPassword("rootpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("ready for connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() {
		if err := mysqlContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := mysqlContainer.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer dockerClient.Close()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)
	containerInfo.Env["MYSQL_ROOT_PASSWORD"] = "rootpass"

	connStr, err := mysqlContainer.ConnectionString(ctx)
	require.NoError(t, err)

	db, err := sql.Open("mysql", connStr)
	require.NoError(t, err)
	defer db.Close()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 30*time.Second, 500*time.Millisecond)

	// Create table with utf8mb4 charset for full Unicode support
	_, err = db.Exec(`
		CREATE TABLE special_chars (
			id INT AUTO_INCREMENT PRIMARY KEY,
			content TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL
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
		_, err = db.Exec(`INSERT INTO special_chars (content) VALUES (?)`, s)
		require.NoError(t, err)
	}

	// Perform backup
	m := &MySQLBackup{}
	var backupBuffer bytes.Buffer
	err = m.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Drop table
	_, err = db.Exec(`DROP TABLE special_chars`)
	require.NoError(t, err)

	// Restore
	err = m.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
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

// TestMySQLBackup_MariaDB tests that the backup works with MariaDB as well
func TestMySQLBackup_MariaDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MariaDB container (uses same mysql module) with root access
	mariadbContainer, err := mysql.Run(ctx,
		"mariadb:11",
		mysql.WithDatabase("testdb"),
		mysql.WithUsername("root"),
		mysql.WithPassword("rootpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("ready for connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() {
		if err := mariadbContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := mariadbContainer.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer dockerClient.Close()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)
	containerInfo.Env["MYSQL_ROOT_PASSWORD"] = "rootpass"

	connStr, err := mariadbContainer.ConnectionString(ctx)
	require.NoError(t, err)

	db, err := sql.Open("mysql", connStr)
	require.NoError(t, err)
	defer db.Close()

	require.Eventually(t, func() bool {
		return db.Ping() == nil
	}, 30*time.Second, 500*time.Millisecond)

	// Create test table and insert data
	_, err = db.Exec(`
		CREATE TABLE users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			email VARCHAR(100) NOT NULL
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO users (name, email) VALUES
		('Alice', 'alice@example.com'),
		('Bob', 'bob@example.com')
	`)
	require.NoError(t, err)

	// Perform backup
	m := &MySQLBackup{}
	var backupBuffer bytes.Buffer
	err = m.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	t.Logf("MariaDB backup size: %d bytes", backupBuffer.Len())

	// Drop table
	_, err = db.Exec(`DROP TABLE users`)
	require.NoError(t, err)

	// Restore
	err = m.Restore(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Verify data is restored
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should have 2 users restored")
}
