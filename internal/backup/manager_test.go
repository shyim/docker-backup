package backup

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shyim/docker-backup/internal/config"
	"github.com/shyim/docker-backup/internal/storage"
	_ "github.com/shyim/docker-backup/internal/storages/local"
	"github.com/stretchr/testify/require"
)

func TestFolderBackupLifecycle(t *testing.T) {
	for _, folder := range []string{"", "sw6"} {
		t.Run("folder="+folder, func(t *testing.T) {
			ctx := context.Background()
			pools, err := storage.NewPoolManager(map[string]*config.StoragePool{
				"first":  {Type: "local", Options: map[string]string{"path": t.TempDir()}},
				"second": {Type: "local", Options: map[string]string{"path": t.TempDir()}},
			}, "first")
			require.NoError(t, err)
			cfg := &config.ContainerConfig{ContainerName: "sw6-sql", Folder: folder, Backups: []config.BackupConfig{
				{Name: "files", BackupType: "volume", Storage: "first"},
				{Name: "db", BackupType: "mysql", Storage: "second"},
			}}
			m := &Manager{poolManager: pools, containers: map[string]*config.ContainerConfig{"id": cfg}}
			prefix := "sw6-sql/"
			if folder != "" {
				prefix = folder + "/" + prefix
			}
			require.Equal(t, prefix, cfg.BackupPrefix())
			key := m.generateBackupKey(strings.TrimSuffix(cfg.BackupPrefix(), "/"), "db", ".tar.zst", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			require.Equal(t, prefix+"db/2026-01-02/030405.tar.zst", key)
			store, err := pools.Get("second")
			require.NoError(t, err)
			require.NoError(t, store.Store(ctx, key, strings.NewReader("backup")))
			require.NoError(t, store.Store(ctx, "other/sw6-sql/db/2026-01-02/030405.tar.zst", strings.NewReader("other")))
			files, err := m.ListBackups(ctx, "sw6-sql")
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, key, files[0].Key)
			reader, err := m.GetBackup(ctx, "sw6-sql", key)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			require.Equal(t, "backup", string(data))
			// Old keys still select the correct pool after opting in.
			oldStore, err := m.getStorageForBackupKey(cfg, "sw6-sql/db/2025-01-02/030405.tar.zst")
			require.NoError(t, err)
			require.Same(t, store, oldStore)
			require.NoError(t, m.DeleteBackup(ctx, "sw6-sql", key))
			files, err = m.ListBackups(ctx, "sw6-sql")
			require.NoError(t, err)
			require.Empty(t, files)
		})
	}
}
