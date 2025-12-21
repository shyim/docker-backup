---
icon: lucide/hard-drive
---

# Adding Storage Backends

This guide walks you through creating a new storage backend for docker-backup.

## Interfaces

Storage backends implement two interfaces:

### StorageType

Factory for creating storage instances:

```go
type StorageType interface {
    // Name returns the storage type identifier
    Name() string

    // Create creates a storage instance from options
    Create(poolName string, options map[string]string) (Storage, error)
}
```

### Storage

The actual storage operations:

```go
type Storage interface {
    // Store saves data from the reader to the given key
    Store(ctx context.Context, key string, reader io.Reader) error

    // List returns all backup files matching the prefix
    List(ctx context.Context, prefix string) ([]BackupFile, error)

    // Delete removes a backup file
    Delete(ctx context.Context, key string) error

    // Get retrieves a backup file for reading
    Get(ctx context.Context, key string) (io.ReadCloser, error)
}
```

## Example: Azure Blob Storage

### Step 1: Create Package

Create `internal/storages/azure/azure.go`:

```go
package azure

import (
    "context"
    "fmt"
    "io"

    "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
    "github.com/shyim/docker-backup/internal/storage"
)

func init() {
    storage.Register(&AzureStorageType{})
}

type AzureStorageType struct{}

func (t *AzureStorageType) Name() string {
    return "azure"
}
```

### Step 2: Implement Create

Parse options and create client:

```go
func (t *AzureStorageType) Create(poolName string, options map[string]string) (storage.Storage, error) {
    accountName := options["account-name"]
    if accountName == "" {
        return nil, fmt.Errorf("azure storage %q requires 'account-name'", poolName)
    }

    accountKey := options["account-key"]
    if accountKey == "" {
        return nil, fmt.Errorf("azure storage %q requires 'account-key'", poolName)
    }

    containerName := options["container"]
    if containerName == "" {
        return nil, fmt.Errorf("azure storage %q requires 'container'", poolName)
    }

    // Create client
    cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)
    if err != nil {
        return nil, fmt.Errorf("failed to create credentials: %w", err)
    }

    serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
    client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create client: %w", err)
    }

    return &AzureStorage{
        client:    client,
        container: containerName,
        prefix:    options["prefix"],
    }, nil
}
```

### Step 3: Implement Storage Interface

```go
type AzureStorage struct {
    client    *azblob.Client
    container string
    prefix    string
}

func (s *AzureStorage) Store(ctx context.Context, key string, reader io.Reader) error {
    blobName := s.prefix + key

    _, err := s.client.UploadStream(ctx, s.container, blobName, reader, nil)
    if err != nil {
        return fmt.Errorf("failed to upload: %w", err)
    }

    return nil
}

func (s *AzureStorage) List(ctx context.Context, prefix string) ([]storage.BackupFile, error) {
    fullPrefix := s.prefix + prefix
    var files []storage.BackupFile

    pager := s.client.NewListBlobsFlatPager(s.container, &azblob.ListBlobsFlatOptions{
        Prefix: &fullPrefix,
    })

    for pager.More() {
        page, err := pager.NextPage(ctx)
        if err != nil {
            return nil, fmt.Errorf("failed to list blobs: %w", err)
        }

        for _, blob := range page.Segment.BlobItems {
            files = append(files, storage.BackupFile{
                Key:          *blob.Name,
                Size:         *blob.Properties.ContentLength,
                LastModified: *blob.Properties.LastModified,
            })
        }
    }

    return files, nil
}

func (s *AzureStorage) Delete(ctx context.Context, key string) error {
    blobName := s.prefix + key

    _, err := s.client.DeleteBlob(ctx, s.container, blobName, nil)
    if err != nil {
        return fmt.Errorf("failed to delete: %w", err)
    }

    return nil
}

func (s *AzureStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
    blobName := s.prefix + key

    resp, err := s.client.DownloadStream(ctx, s.container, blobName, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to download: %w", err)
    }

    return resp.Body, nil
}
```

### Step 4: Register Plugin

Add import to `internal/storages/registry.go`:

```go
package storages

import (
    _ "github.com/shyim/docker-backup/internal/storages/azure"
    _ "github.com/shyim/docker-backup/internal/storages/local"
    _ "github.com/shyim/docker-backup/internal/storages/s3"
)
```

### Step 5: Add Dependency

```bash
go get github.com/Azure/azure-sdk-for-go/sdk/storage/azblob
```

## Configuration Options

Options are passed as `map[string]string` from CLI flags or environment variables:

```bash
# CLI
--storage=azure.account-name=myaccount
--storage=azure.account-key=secret
--storage=azure.container=backups

# Environment
DOCKER_BACKUP_STORAGE_AZURE_ACCOUNT_NAME=myaccount
DOCKER_BACKUP_STORAGE_AZURE_ACCOUNT_KEY=secret
DOCKER_BACKUP_STORAGE_AZURE_CONTAINER=backups
```

## Best Practices

### Option Validation

Validate all required options in `Create()`:

```go
func (t *AzureStorageType) Create(poolName string, options map[string]string) (storage.Storage, error) {
    required := []string{"account-name", "account-key", "container"}
    for _, opt := range required {
        if options[opt] == "" {
            return nil, fmt.Errorf("azure storage %q requires '%s'", poolName, opt)
        }
    }
    // ...
}
```

### Error Wrapping

Wrap errors with context:

```go
if err != nil {
    return fmt.Errorf("azure: failed to upload %s: %w", key, err)
}
```

### Context Handling

Always pass context to SDK calls:

```go
func (s *AzureStorage) Store(ctx context.Context, key string, reader io.Reader) error {
    _, err := s.client.UploadStream(ctx, s.container, key, reader, nil)
    // ...
}
```

### Prefix Support

Support optional key prefix for organizing backups:

```go
type AzureStorage struct {
    prefix string
}

func (s *AzureStorage) Store(ctx context.Context, key string, reader io.Reader) error {
    fullKey := s.prefix + key
    // ...
}
```

## BackupFile Structure

The `List` method returns `[]storage.BackupFile`:

```go
type BackupFile struct {
    Key          string    // Full path/key
    Size         int64     // Size in bytes
    LastModified time.Time // Last modification time
}
```

## Testing

Create integration tests (requires actual Azure account):

```go
func TestAzureStorage(t *testing.T) {
    if os.Getenv("AZURE_ACCOUNT_NAME") == "" {
        t.Skip("Azure credentials not set")
    }

    storageType := &AzureStorageType{}
    storage, err := storageType.Create("test", map[string]string{
        "account-name": os.Getenv("AZURE_ACCOUNT_NAME"),
        "account-key":  os.Getenv("AZURE_ACCOUNT_KEY"),
        "container":    "test-backups",
    })
    if err != nil {
        t.Fatal(err)
    }

    ctx := context.Background()

    // Test Store
    data := strings.NewReader("test data")
    err = storage.Store(ctx, "test/backup.txt", data)
    if err != nil {
        t.Errorf("Store failed: %v", err)
    }

    // Test List
    files, err := storage.List(ctx, "test/")
    if err != nil {
        t.Errorf("List failed: %v", err)
    }
    if len(files) != 1 {
        t.Errorf("Expected 1 file, got %d", len(files))
    }

    // Test Get
    reader, err := storage.Get(ctx, "test/backup.txt")
    if err != nil {
        t.Errorf("Get failed: %v", err)
    }
    reader.Close()

    // Test Delete
    err = storage.Delete(ctx, "test/backup.txt")
    if err != nil {
        t.Errorf("Delete failed: %v", err)
    }
}
```

## Complete Examples

See existing implementations:

- **Local**: `internal/storages/local/local.go`
- **S3**: `internal/storages/s3/s3.go`
