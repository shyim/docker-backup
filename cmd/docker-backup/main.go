package main

import (
	"os"

	"github.com/shyim/docker-backup/internal/api"
	"github.com/shyim/docker-backup/internal/config"
	"github.com/spf13/cobra"

	// Import backup types for self-registration
	_ "github.com/shyim/docker-backup/internal/backuptypes"

	// Import storage backends for self-registration
	_ "github.com/shyim/docker-backup/internal/storages/local"
	_ "github.com/shyim/docker-backup/internal/storages/s3"
)

var (
	cfg        = config.New()
	socketPath string

	rootCmd = &cobra.Command{
		Use:   "docker-backup",
		Short: "Docker container backup daemon",
		Long:  "A daemon that monitors Docker containers and performs scheduled backups based on container labels.",
	}
)

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfg.DockerHost, "docker-host", "unix:///var/run/docker.sock", "Docker daemon socket")
	rootCmd.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&cfg.LogFormat, "log-format", "text", "Log format (text, json)")
	rootCmd.PersistentFlags().StringVar(&socketPath, "socket", api.DefaultSocketPath, "Unix socket path for API")

	// Add commands
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(backupCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
