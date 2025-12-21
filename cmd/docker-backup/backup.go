package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/shyim/docker-backup/internal/api"
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup management commands",
	Long:  "Commands for managing backups: run, list, delete, restore.",
}

var backupRunCmd = &cobra.Command{
	Use:   "run <container-name>",
	Short: "Trigger an immediate backup",
	Long:  "Trigger an immediate backup for a container by communicating with the running daemon.",
	Args:  cobra.ExactArgs(1),
	RunE:  runBackupRun,
}

var backupListCmd = &cobra.Command{
	Use:     "list <container-name>",
	Aliases: []string{"ls"},
	Short:   "List backups for a container",
	Long:    "List all backups for a container.",
	Args:    cobra.ExactArgs(1),
	RunE:    runBackupList,
}

var backupDeleteCmd = &cobra.Command{
	Use:   "delete <container-name> <backup-key>",
	Short: "Delete a specific backup",
	Long:  "Delete a specific backup for a container by its key.",
	Args:  cobra.ExactArgs(2),
	RunE:  runBackupDelete,
}

var backupRestoreCmd = &cobra.Command{
	Use:   "restore <container-name> <backup-key>",
	Short: "Restore a backup to a container",
	Long:  "Restore a specific backup to a running container.",
	Args:  cobra.ExactArgs(2),
	RunE:  runBackupRestore,
}

func init() {
	backupCmd.AddCommand(backupRunCmd)
	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupDeleteCmd)
	backupCmd.AddCommand(backupRestoreCmd)
}

func runBackupRun(cmd *cobra.Command, args []string) error {
	containerName := args[0]

	client := createSocketClient()

	url := fmt.Sprintf("http://localhost/backup/run/%s", containerName)
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon at %s: %w", socketPath, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result api.BackupResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("backup failed: %s", result.Error)
	}

	fmt.Printf("Backup completed successfully for container: %s\n", containerName)
	if result.Message != "" {
		fmt.Printf("Message: %s\n", result.Message)
	}

	return nil
}

func runBackupList(cmd *cobra.Command, args []string) error {
	containerName := args[0]

	client := createSocketClient()

	url := fmt.Sprintf("http://localhost/backup/list/%s", containerName)
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon at %s: %w", socketPath, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result api.ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to list backups: %s", result.Error)
	}

	if len(result.Backups) == 0 {
		fmt.Printf("No backups found for container: %s\n", containerName)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "KEY\tSIZE\tDATE")
	_, _ = fmt.Fprintln(w, "---\t----\t----")

	for _, b := range result.Backups {
		size := formatSize(b.Size)
		date := b.LastModified.Format("2006-01-02 15:04:05")
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", b.Key, size, date)
	}
	_ = w.Flush()

	fmt.Printf("\nTotal: %d backup(s)\n", len(result.Backups))

	return nil
}

func runBackupDelete(cmd *cobra.Command, args []string) error {
	containerName := args[0]
	backupKey := args[1]

	client := createSocketClient()

	url := fmt.Sprintf("http://localhost/backup/delete/%s/%s", containerName, backupKey)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon at %s: %w", socketPath, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result api.DeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to delete backup: %s", result.Error)
	}

	fmt.Printf("Backup deleted successfully: %s\n", backupKey)
	return nil
}

func runBackupRestore(cmd *cobra.Command, args []string) error {
	containerName := args[0]
	backupKey := args[1]

	client := createSocketClient()

	url := fmt.Sprintf("http://localhost/backup/restore/%s/%s", containerName, backupKey)
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon at %s: %w", socketPath, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result api.RestoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("restore failed: %s", result.Error)
	}

	fmt.Printf("Backup restored successfully to container: %s\n", containerName)
	if result.Message != "" {
		fmt.Printf("Message: %s\n", result.Message)
	}

	return nil
}
