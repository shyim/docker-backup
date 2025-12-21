package dashboard

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/shyim/docker-backup/internal/backup"
	"github.com/shyim/docker-backup/internal/config"
	"github.com/shyim/docker-backup/internal/dashboard/auth"
	"github.com/shyim/docker-backup/internal/dashboard/static"
	"github.com/shyim/docker-backup/internal/dashboard/templates"
	"github.com/shyim/docker-backup/internal/notification"
	"github.com/shyim/docker-backup/internal/scheduler"
	"github.com/shyim/docker-backup/internal/storage"
)

// Server represents the dashboard HTTP server
type Server struct {
	server      *http.Server
	addr        string
	backupMgr   *backup.Manager
	poolManager *storage.PoolManager
	scheduler   *scheduler.Scheduler
	notifyMgr   *notification.Manager
	config      *config.Config
}

// Flash message translation keys with placeholders
var flashMessages = map[string]string{
	"backup_success":  "Backup completed successfully for {0}",
	"backup_failed":   "Backup failed for {0}",
	"delete_success":  "Backup deleted successfully",
	"delete_failed":   "Failed to delete backup",
	"restore_success": "Backup restored successfully for {0}",
	"restore_failed":  "Failed to restore backup for {0}",
}

// NewServer creates a new dashboard server
func NewServer(addr string, backupMgr *backup.Manager, poolManager *storage.PoolManager, sched *scheduler.Scheduler, notifyMgr *notification.Manager, cfg *config.Config) *Server {
	gin.SetMode(gin.ReleaseMode)

	s := &Server{
		addr:        addr,
		backupMgr:   backupMgr,
		poolManager: poolManager,
		scheduler:   sched,
		notifyMgr:   notifyMgr,
		config:      cfg,
	}

	router := gin.New()
	router.Use(gin.Recovery())

	// Setup cookie-based sessions (needed for OIDC and flash messages)
	store := cookie.NewStore([]byte("docker-backup-secret-key"))
	router.Use(sessions.Sessions("docker_backup", store))

	// Setup authentication - OIDC takes precedence over basic auth
	if cfg.DashboardOIDCProvider != "" {
		oidcAuth, err := auth.NewOIDCAuth(context.Background(), auth.OIDCConfig{
			Provider:       cfg.DashboardOIDCProvider,
			IssuerURL:      cfg.DashboardOIDCIssuerURL,
			ClientID:       cfg.DashboardOIDCClientID,
			ClientSecret:   cfg.DashboardOIDCClientSecret,
			RedirectURL:    cfg.DashboardOIDCRedirectURL,
			AllowedUsers:   cfg.DashboardOIDCAllowedUsers,
			AllowedDomains: cfg.DashboardOIDCAllowedDomains,
		})
		if err != nil {
			slog.Error("failed to initialize OIDC auth", "error", err)
		} else {
			// Register OIDC routes first
			oidcAuth.RegisterRoutes(router)
			// Then apply middleware
			router.Use(auth.OIDCAuthMiddleware(oidcAuth))
			slog.Info("dashboard OIDC auth enabled", "provider", cfg.DashboardOIDCProvider)
		}
	} else if cfg.DashboardBasicAuth != "" {
		// Fall back to basic auth
		htpasswd, err := auth.NewHtpasswdAuth(cfg.DashboardBasicAuth)
		if err != nil {
			slog.Error("failed to initialize basic auth", "error", err)
		} else {
			router.Use(auth.BasicAuthMiddleware(htpasswd))
			slog.Info("dashboard basic auth enabled", "users", htpasswd.UserCount())
		}
	}

	// Static files
	router.GET("/static/*filepath", func(c *gin.Context) {
		filepath := c.Param("filepath")
		data, err := static.Files.ReadFile(filepath[1:]) // Remove leading /
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		if strings.HasSuffix(filepath, ".js") {
			c.Header("Content-Type", "application/javascript")
		} else if strings.HasSuffix(filepath, ".css") {
			c.Header("Content-Type", "text/css")
		}
		c.Header("Cache-Control", "public, max-age=31536000")
		c.Writer.Write(data)
	})

	// Routes
	router.GET("/", s.handleIndex)
	router.GET("/backups", s.handleBackups)
	router.POST("/api/backup/trigger", s.handleTriggerBackup)
	router.GET("/api/backup/download", s.handleDownloadBackup)
	router.POST("/api/backup/delete", s.handleDeleteBackup)
	router.POST("/api/backup/restore", s.handleRestoreBackup)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	return s
}

// Start starts the dashboard server
func (s *Server) Start() error {
	slog.Info("starting dashboard server", "addr", s.addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("dashboard server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the dashboard server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// setFlash sets a flash message in the session
func setFlash(c *gin.Context, flashType, msgKey string, params ...string) {
	session := sessions.Default(c)
	session.AddFlash(flashType, "flash_type")
	session.AddFlash(msgKey, "flash_msg")
	session.AddFlash(strings.Join(params, "\x00"), "flash_params")
	session.Save()
}

// getFlash retrieves and clears flash message from session
func getFlash(c *gin.Context) *templates.FlashMessage {
	session := sessions.Default(c)

	flashType := session.Flashes("flash_type")
	flashMsg := session.Flashes("flash_msg")
	flashParams := session.Flashes("flash_params")
	session.Save()

	if len(flashType) == 0 || len(flashMsg) == 0 {
		return nil
	}

	msgKey, ok := flashMsg[0].(string)
	if !ok {
		return nil
	}

	message, ok := flashMessages[msgKey]
	if !ok {
		return nil
	}

	// Replace placeholders with params
	if len(flashParams) > 0 {
		if paramsStr, ok := flashParams[0].(string); ok && paramsStr != "" {
			params := strings.Split(paramsStr, "\x00")
			for i, param := range params {
				placeholder := fmt.Sprintf("{%d}", i)
				message = strings.ReplaceAll(message, placeholder, param)
			}
		}
	}

	typeStr, _ := flashType[0].(string)
	return &templates.FlashMessage{
		Type:    typeStr,
		Message: message,
	}
}

// handleIndex renders the main dashboard page
func (s *Server) handleIndex(c *gin.Context) {
	containers := s.backupMgr.GetContainers()
	jobs := s.scheduler.ListJobs()

	data := templates.IndexData{
		ContainerCount: len(containers),
		JobCount:       len(jobs),
		StorageCount:   s.poolManager.PoolCount(),
		Containers:     make([]templates.ContainerInfo, 0, len(containers)),
		Notifications:  make([]templates.NotificationInfo, 0),
		Flash:          getFlash(c),
	}

	// Add notifications
	if s.notifyMgr != nil {
		notifiers := s.notifyMgr.ListNotifiers()
		for _, n := range notifiers {
			data.Notifications = append(data.Notifications, templates.NotificationInfo{
				Name: n.Name,
				Type: n.Type,
			})
		}
	}

	for _, cont := range containers {
		containerInfo := templates.ContainerInfo{
			ID:      cont.ContainerID[:12],
			Name:    cont.ContainerName,
			Notify:  cont.Notify,
			Backups: make([]templates.BackupConfigInfo, 0, len(cont.Backups)),
		}

		for _, backup := range cont.Backups {
			// Build job key to look up next run time
			jobKey := cont.ContainerID + ":" + backup.Name
			nextRun := ""
			if job, ok := jobs[jobKey]; ok {
				nextRun = job.NextRun.Format("2006-01-02 15:04:05")
			}

			containerInfo.Backups = append(containerInfo.Backups, templates.BackupConfigInfo{
				Name:       backup.Name,
				BackupType: backup.BackupType,
				Schedule:   backup.Schedule,
				Retention:  backup.Retention,
				Storage:    backup.Storage,
				NextRun:    nextRun,
			})
		}

		data.Containers = append(data.Containers, containerInfo)
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := templates.Index(data).Render(c.Request.Context(), c.Writer); err != nil {
		slog.Error("failed to render template", "error", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
	}
}

// handleBackups renders backups for a specific container
func (s *Server) handleBackups(c *gin.Context) {
	containerName := c.Query("container")
	if containerName == "" {
		c.String(http.StatusBadRequest, "container parameter required")
		return
	}

	backups, err := s.backupMgr.ListBackups(c.Request.Context(), containerName)
	if err != nil {
		slog.Error("failed to list backups", "container", containerName, "error", err)
		c.String(http.StatusInternalServerError, "Failed to list backups")
		return
	}

	data := templates.BackupsData{
		ContainerName: containerName,
		ConfigNames:   make([]string, 0),
		BackupGroups:  make(map[string][]templates.BackupInfo),
		Flash:         getFlash(c),
	}

	// Group backups by config name (extracted from key: container/config/date/time.ext)
	configOrder := make(map[string]int)
	for _, b := range backups {
		configName := extractConfigName(b.Key)

		if _, exists := configOrder[configName]; !exists {
			configOrder[configName] = len(data.ConfigNames)
			data.ConfigNames = append(data.ConfigNames, configName)
		}

		data.BackupGroups[configName] = append(data.BackupGroups[configName], templates.BackupInfo{
			Key:          b.Key,
			ConfigName:   configName,
			Size:         formatSize(b.Size),
			LastModified: b.LastModified.Format("2006-01-02 15:04:05"),
		})
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := templates.Backups(data).Render(c.Request.Context(), c.Writer); err != nil {
		slog.Error("failed to render template", "error", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
	}
}

// extractConfigName extracts the config name from a backup key
// Key format: container-name/config-name/YYYY-MM-DD/HHMMSS.ext
func extractConfigName(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "default"
}

// handleTriggerBackup triggers an immediate backup
func (s *Server) handleTriggerBackup(c *gin.Context) {
	containerName := c.Query("container")
	if containerName == "" {
		c.String(http.StatusBadRequest, "container parameter required")
		return
	}

	configName := c.Query("config")

	// Get the referer to redirect back to the correct page
	redirectURL := c.GetHeader("Referer")
	if redirectURL == "" {
		redirectURL = "/"
	}

	// Run backup synchronously to get the result
	err := s.backupMgr.TriggerBackup(c.Request.Context(), containerName, configName)

	// Set flash message
	if err != nil {
		slog.Error("failed to trigger backup", "container", containerName, "error", err)
		setFlash(c, "error", "backup_failed", containerName)
	} else {
		setFlash(c, "success", "backup_success", containerName)
	}

	c.Redirect(http.StatusSeeOther, redirectURL)
}

// handleDeleteBackup deletes a backup file
func (s *Server) handleDeleteBackup(c *gin.Context) {
	containerName := c.Query("container")
	backupKey := c.Query("key")

	if containerName == "" || backupKey == "" {
		c.String(http.StatusBadRequest, "container and key parameters required")
		return
	}

	// Delete the backup
	err := s.backupMgr.DeleteBackup(c.Request.Context(), containerName, backupKey)

	// Redirect back to backups page with flash message
	redirectURL := fmt.Sprintf("/backups?container=%s", containerName)
	if err != nil {
		slog.Error("failed to delete backup", "container", containerName, "key", backupKey, "error", err)
		setFlash(c, "error", "delete_failed")
	} else {
		setFlash(c, "success", "delete_success")
	}

	c.Redirect(http.StatusSeeOther, redirectURL)
}

// handleRestoreBackup restores a backup
func (s *Server) handleRestoreBackup(c *gin.Context) {
	containerName := c.Query("container")
	backupKey := c.Query("key")

	if containerName == "" || backupKey == "" {
		c.String(http.StatusBadRequest, "container and key parameters required")
		return
	}

	// Restore the backup
	err := s.backupMgr.RestoreBackup(c.Request.Context(), containerName, backupKey)

	// Redirect back to backups page with flash message
	redirectURL := fmt.Sprintf("/backups?container=%s", containerName)
	if err != nil {
		slog.Error("failed to restore backup", "container", containerName, "key", backupKey, "error", err)
		setFlash(c, "error", "restore_failed", containerName)
	} else {
		setFlash(c, "success", "restore_success", containerName)
	}

	c.Redirect(http.StatusSeeOther, redirectURL)
}

// handleDownloadBackup downloads a backup file
func (s *Server) handleDownloadBackup(c *gin.Context) {
	containerName := c.Query("container")
	backupKey := c.Query("key")

	if containerName == "" || backupKey == "" {
		c.String(http.StatusBadRequest, "container and key parameters required")
		return
	}

	// Get the backup reader
	reader, err := s.backupMgr.GetBackup(c.Request.Context(), containerName, backupKey)
	if err != nil {
		slog.Error("failed to get backup", "container", containerName, "key", backupKey, "error", err)
		c.String(http.StatusInternalServerError, "Failed to get backup")
		return
	}
	defer reader.Close()

	// Extract filename from key (last part of path)
	parts := strings.Split(backupKey, "/")
	filename := parts[len(parts)-1]

	// Set headers for download
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Content-Type", "application/octet-stream")

	// Stream the backup to the response
	c.Stream(func(w io.Writer) bool {
		_, err := io.Copy(w, reader)
		if err != nil {
			slog.Error("failed to stream backup", "error", err)
		}
		return false
	})
}

// formatSize formats bytes into human-readable size
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
