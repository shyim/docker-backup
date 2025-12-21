package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/shyim/docker-backup/internal/storage"
)

func init() {
	storage.Register(&LocalStorageType{})
}

// LocalStorageType is the factory for local storage
type LocalStorageType struct{}

// Name returns the storage type identifier
func (t *LocalStorageType) Name() string {
	return "local"
}

// Create instantiates a new local storage from options
func (t *LocalStorageType) Create(poolName string, options map[string]string) (storage.Storage, error) {
	path, ok := options["path"]
	if !ok || path == "" {
		return nil, fmt.Errorf("local storage requires 'path' option")
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &LocalStorage{
		basePath: path,
		poolName: poolName,
	}, nil
}

// LocalStorage implements Storage for local filesystem
type LocalStorage struct {
	basePath string
	poolName string
}

// Store saves backup data to the local filesystem
func (l *LocalStorage) Store(ctx context.Context, key string, reader io.Reader) error {
	fullPath := filepath.Join(l.basePath, key)

	// Create parent directories
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Create file
	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy data
	_, err = io.Copy(file, reader)
	if err != nil {
		os.Remove(fullPath) // Clean up on failure
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// List returns all backups matching the prefix
func (l *LocalStorage) List(ctx context.Context, prefix string) ([]storage.BackupFile, error) {
	searchPath := filepath.Join(l.basePath, prefix)
	var files []storage.BackupFile

	err := filepath.Walk(l.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(l.basePath, path)
		if err != nil {
			return err
		}

		// Check prefix match
		if prefix != "" {
			relDir := filepath.Dir(relPath)
			if relDir != prefix && relPath != prefix && !filepath.HasPrefix(relPath, prefix+string(filepath.Separator)) {
				// Try matching the directory structure
				if !matchesPrefix(relPath, prefix) {
					return nil
				}
			}
		}

		files = append(files, storage.BackupFile{
			Key:          relPath,
			Size:         info.Size(),
			LastModified: info.ModTime(),
		})

		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	// Filter files that match the prefix pattern (container/type/)
	if prefix != "" {
		var filtered []storage.BackupFile
		for _, f := range files {
			if matchesPrefix(f.Key, prefix) {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].LastModified.After(files[j].LastModified)
	})

	// Only walk if path might exist
	if _, err := os.Stat(searchPath); os.IsNotExist(err) {
		return files, nil
	}

	return files, nil
}

// matchesPrefix checks if a file key matches the given prefix pattern
func matchesPrefix(key, prefix string) bool {
	// Normalize separators
	key = filepath.ToSlash(key)
	prefix = filepath.ToSlash(prefix)

	// Simple prefix match
	if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
		return true
	}

	return false
}

// Delete removes a backup file
func (l *LocalStorage) Delete(ctx context.Context, key string) error {
	fullPath := filepath.Join(l.basePath, key)

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Try to clean up empty parent directories
	dir := filepath.Dir(fullPath)
	for dir != l.basePath {
		if err := os.Remove(dir); err != nil {
			break // Directory not empty or other error
		}
		dir = filepath.Dir(dir)
	}

	return nil
}

// Get retrieves a backup file for reading
func (l *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(l.basePath, key)

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}
