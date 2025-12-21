package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalStorageType_Name(t *testing.T) {
	st := &LocalStorageType{}
	assert.Equal(t, "local", st.Name())
}

func TestLocalStorageType_Create(t *testing.T) {
	tmpDir := t.TempDir()

	st := &LocalStorageType{}
	storage, err := st.Create("test-pool", map[string]string{
		"path": tmpDir,
	})

	require.NoError(t, err)
	require.NotNil(t, storage)
}

func TestLocalStorageType_Create_MissingPath(t *testing.T) {
	st := &LocalStorageType{}
	_, err := st.Create("test-pool", map[string]string{})

	assert.Error(t, err, "expected error for missing path")
}

func TestLocalStorageType_Create_EmptyPath(t *testing.T) {
	st := &LocalStorageType{}
	_, err := st.Create("test-pool", map[string]string{
		"path": "",
	})

	assert.Error(t, err, "expected error for empty path")
}

func TestLocalStorageType_Create_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "backups", "nested")

	st := &LocalStorageType{}
	_, err := st.Create("test-pool", map[string]string{
		"path": newDir,
	})

	require.NoError(t, err)
	assert.DirExists(t, newDir)
}

func TestLocalStorage_Store(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	ctx := context.Background()
	content := "test backup content"

	err := storage.Store(ctx, "container/db/2024-01-15/backup.sql", strings.NewReader(content))
	require.NoError(t, err)

	// Verify file exists
	fullPath := filepath.Join(tmpDir, "container/db/2024-01-15/backup.sql")
	data, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestLocalStorage_Store_CreatesParentDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	ctx := context.Background()
	err := storage.Store(ctx, "deep/nested/path/file.txt", strings.NewReader("data"))
	require.NoError(t, err)

	fullPath := filepath.Join(tmpDir, "deep/nested/path/file.txt")
	assert.FileExists(t, fullPath)
}

func TestLocalStorage_Get(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	// Create a test file
	content := "backup data"
	filePath := filepath.Join(tmpDir, "test.sql")
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	ctx := context.Background()
	reader, err := storage.Get(ctx, "test.sql")
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestLocalStorage_Get_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	ctx := context.Background()
	_, err := storage.Get(ctx, "nonexistent.sql")
	assert.Error(t, err, "expected error for nonexistent file")
}

func TestLocalStorage_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	// Create a test file
	filePath := filepath.Join(tmpDir, "container/db/backup.sql")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0755))
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	ctx := context.Background()
	err := storage.Delete(ctx, "container/db/backup.sql")
	require.NoError(t, err)
	assert.NoFileExists(t, filePath)
}

func TestLocalStorage_Delete_CleansEmptyDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	// Create nested structure with single file
	filePath := filepath.Join(tmpDir, "container/db/2024/backup.sql")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0755))
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	ctx := context.Background()
	err := storage.Delete(ctx, "container/db/2024/backup.sql")
	require.NoError(t, err)

	// Parent directories should be cleaned up
	assert.NoDirExists(t, filepath.Join(tmpDir, "container/db/2024"))
}

func TestLocalStorage_Delete_PreservesNonEmptyDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	// Create two files in same directory
	dir := filepath.Join(tmpDir, "container/db")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.sql"), []byte("data1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file2.sql"), []byte("data2"), 0644))

	ctx := context.Background()
	err := storage.Delete(ctx, "container/db/file1.sql")
	require.NoError(t, err)

	// Directory should still exist (has file2.sql)
	assert.DirExists(t, dir, "directory should still exist with remaining file")
	assert.FileExists(t, filepath.Join(dir, "file2.sql"), "file2.sql should still exist")
}

func TestLocalStorage_Delete_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	ctx := context.Background()
	err := storage.Delete(ctx, "nonexistent.sql")

	// Should not return error for already-deleted file
	assert.NoError(t, err)
}

func TestLocalStorage_List(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	// Create test files
	files := []string{
		"container1/db/2024-01-15/backup1.sql",
		"container1/db/2024-01-16/backup2.sql",
		"container2/files/2024-01-15/backup.tar",
	}

	for _, f := range files {
		fullPath := filepath.Join(tmpDir, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte("data"), 0644))
		// Add small delay to ensure different modification times
		time.Sleep(10 * time.Millisecond)
	}

	ctx := context.Background()
	results, err := storage.List(ctx, "container1/db")
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Should be sorted by modification time (newest first)
	assert.True(t, results[0].LastModified.After(results[1].LastModified) || results[0].LastModified.Equal(results[1].LastModified),
		"results should be sorted by modification time (newest first)")
}

func TestLocalStorage_List_EmptyPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	// Create test files
	files := []string{
		"container1/backup1.sql",
		"container2/backup2.sql",
	}

	for _, f := range files {
		fullPath := filepath.Join(tmpDir, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte("data"), 0644))
	}

	ctx := context.Background()
	results, err := storage.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestLocalStorage_List_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	// Create a file in different path
	fullPath := filepath.Join(tmpDir, "other/backup.sql")
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
	require.NoError(t, os.WriteFile(fullPath, []byte("data"), 0644))

	ctx := context.Background()
	results, err := storage.List(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestLocalStorage_List_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	ctx := context.Background()
	results, err := storage.List(ctx, "container")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMatchesPrefix(t *testing.T) {
	tests := []struct {
		key      string
		prefix   string
		expected bool
	}{
		{"container/db/backup.sql", "container/db", true},
		{"container/db/2024/backup.sql", "container/db", true},
		{"container/db/backup.sql", "container", true},
		{"container/files/backup.tar", "container/db", false},
		{"other/db/backup.sql", "container/db", false},
		{"container/db/backup.sql", "", true},
		{"backup.sql", "container", false},
	}

	for _, tt := range tests {
		t.Run(tt.key+"_"+tt.prefix, func(t *testing.T) {
			result := matchesPrefix(tt.key, tt.prefix)
			assert.Equal(t, tt.expected, result, "matchesPrefix(%q, %q)", tt.key, tt.prefix)
		})
	}
}

func TestLocalStorage_StoreAndGet_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	ctx := context.Background()
	content := "test backup data with special chars: äöü 日本語 🎉"

	// Store
	err := storage.Store(ctx, "test/backup.sql", strings.NewReader(content))
	require.NoError(t, err)

	// Get
	reader, err := storage.Get(ctx, "test/backup.sql")
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestLocalStorage_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	storage := &LocalStorage{basePath: tmpDir}

	ctx := context.Background()

	// Create 1MB of data
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Store
	err := storage.Store(ctx, "large.bin", strings.NewReader(string(data)))
	require.NoError(t, err)

	// Get
	reader, err := storage.Get(ctx, "large.bin")
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	retrieved, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Len(t, retrieved, len(data))
}
