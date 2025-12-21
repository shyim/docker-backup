package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/shyim/docker-backup/internal/api"
	"github.com/shyim/docker-backup/internal/backup"
	"github.com/shyim/docker-backup/internal/dashboard"
	"github.com/shyim/docker-backup/internal/docker"
	"github.com/shyim/docker-backup/internal/notification"
	"github.com/shyim/docker-backup/internal/retention"
	"github.com/shyim/docker-backup/internal/scheduler"
	"github.com/shyim/docker-backup/internal/storage"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the backup daemon",
	Long:  "Start the backup daemon that monitors containers and performs scheduled backups.",
	RunE:  runDaemon,
}

func init() {
	daemonCmd.Flags().DurationVar(&cfg.PollInterval, "poll-interval", cfg.PollInterval, "How often to scan for container changes")
	daemonCmd.Flags().StringVar(&cfg.DefaultStorage, "default-storage", "", "Default storage pool name")
	daemonCmd.Flags().StringVar(&cfg.TempDir, "temp-dir", os.TempDir(), "Temporary directory for backup files")
	daemonCmd.Flags().StringArrayVar(&cfg.StorageArgs, "storage", []string{}, "Storage pool configuration (format: pool.option=value)")
	daemonCmd.Flags().StringArrayVar(&cfg.NotifyArgs, "notify", []string{}, "Notification provider configuration (format: provider.option=value)")
	daemonCmd.Flags().StringVar(&cfg.DashboardAddr, "dashboard", "", "Enable dashboard on address (e.g., :8080)")
	daemonCmd.Flags().StringVar(&cfg.DashboardBasicAuth, "dashboard.auth.basic", "", "Dashboard basic auth (htpasswd file path or inline user:hash)")
	daemonCmd.Flags().StringVar(&cfg.DashboardOIDCProvider, "dashboard.auth.oidc.provider", "", "OIDC provider (google, github, or oidc)")
	daemonCmd.Flags().StringVar(&cfg.DashboardOIDCIssuerURL, "dashboard.auth.oidc.issuer-url", "", "OIDC issuer URL (required for generic 'oidc' provider)")
	daemonCmd.Flags().StringVar(&cfg.DashboardOIDCClientID, "dashboard.auth.oidc.client-id", "", "OIDC client ID")
	daemonCmd.Flags().StringVar(&cfg.DashboardOIDCClientSecret, "dashboard.auth.oidc.client-secret", "", "OIDC client secret")
	daemonCmd.Flags().StringVar(&cfg.DashboardOIDCRedirectURL, "dashboard.auth.oidc.redirect-url", "", "OIDC redirect URL (e.g., http://localhost:8080/auth/callback)")
	daemonCmd.Flags().StringSliceVar(&cfg.DashboardOIDCAllowedUsers, "dashboard.auth.oidc.allowed-users", nil, "Allowed user emails (comma-separated)")
	daemonCmd.Flags().StringSliceVar(&cfg.DashboardOIDCAllowedDomains, "dashboard.auth.oidc.allowed-domains", nil, "Allowed email domains (comma-separated)")
}

func runDaemon(cmd *cobra.Command, args []string) error {
	setupLogging()

	slog.Info("starting docker-backup daemon",
		"docker_host", cfg.DockerHost,
		"poll_interval", cfg.PollInterval,
	)

	if err := cfg.ParseStoragePools(); err != nil {
		return err
	}

	if len(cfg.StoragePools) == 0 {
		slog.Error("no storage pools configured, use --storage flag to configure at least one")
		os.Exit(1)
	}

	slog.Info("configured storage pools", "count", len(cfg.StoragePools))
	for name, pool := range cfg.StoragePools {
		slog.Info("storage pool", "name", name, "type", pool.Type)
	}

	if err := cfg.ParseNotifyDSNs(); err != nil {
		return err
	}

	notifyMgr := notification.NewManager()
	for name, dsn := range cfg.NotifyDSNs {
		notifier, err := notification.CreateNotifierFromDSN(name, dsn)
		if err != nil {
			slog.Error("failed to create notifier", "name", name, "error", err)
			return err
		}
		notifyMgr.AddNotifier(name, notifier)
		slog.Info("notification provider configured", "name", name)
	}

	if notifyMgr.NotifierCount() > 0 {
		slog.Info("configured notification providers", "count", notifyMgr.NotifierCount())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poolManager, err := storage.NewPoolManager(cfg.StoragePools, cfg.DefaultStorage)
	if err != nil {
		slog.Error("failed to initialize storage pools", "error", err)
		return err
	}

	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		slog.Error("failed to connect to Docker", "error", err)
		return err
	}
	defer func() {
		_ = dockerClient.Close()
	}()

	sched := scheduler.New()

	retentionMgr := retention.New(poolManager)

	backupMgr := backup.NewManager(
		dockerClient,
		poolManager,
		sched,
		retentionMgr,
		notifyMgr,
		cfg,
	)

	apiServer := api.NewServer(socketPath)
	apiServer.SetBackupTrigger(backupMgr.TriggerBackup)
	apiServer.SetBackupLister(backupMgr.ListBackups)
	apiServer.SetBackupDeleter(backupMgr.DeleteBackup)
	apiServer.SetBackupRestorer(backupMgr.RestoreBackup)

	go func() {
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("API server error", "error", err)
		}
	}()

	var dashboardServer *dashboard.Server
	if cfg.DashboardAddr != "" {
		dashboardServer = dashboard.NewServer(cfg.DashboardAddr, backupMgr, poolManager, sched, notifyMgr, cfg)
		go func() {
			if err := dashboardServer.Start(); err != nil && err != http.ErrServerClosed {
				slog.Error("dashboard server error", "error", err)
			}
		}()
	}

	sched.Start()

	if err := backupMgr.Start(ctx); err != nil {
		slog.Error("failed to start backup manager", "error", err)
		return err
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	slog.Info("received shutdown signal", "signal", sig)

	cancel()

	sched.Stop()
	if err := apiServer.Shutdown(context.Background()); err != nil {
		slog.Warn("API server shutdown error", "error", err)
	}
	if dashboardServer != nil {
		if err := dashboardServer.Shutdown(context.Background()); err != nil {
			slog.Warn("dashboard server shutdown error", "error", err)
		}
	}

	slog.Info("daemon stopped")
	return nil
}
