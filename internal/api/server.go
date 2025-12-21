package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shyim/docker-backup/internal/storage"
)

// DefaultSocketPath is the default Unix socket path
const DefaultSocketPath = "/var/run/docker-backup.sock"

// BackupTrigger is a function that triggers a backup for a container
// If configName is provided, it triggers a specific backup config; otherwise all configs
type BackupTrigger func(ctx context.Context, containerName string, configName ...string) error

// BackupLister is a function that lists backups for a container
type BackupLister func(ctx context.Context, containerName string) ([]storage.BackupFile, error)

// BackupDeleter is a function that deletes a backup
type BackupDeleter func(ctx context.Context, containerName, backupKey string) error

// BackupRestorer is a function that restores a backup
type BackupRestorer func(ctx context.Context, containerName, backupKey string) error

// BackupResponse is the response for a backup trigger request
type BackupResponse struct {
	Success   bool   `json:"success"`
	Container string `json:"container"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ListResponse is the response for a backup list request
type ListResponse struct {
	Success   bool                 `json:"success"`
	Container string               `json:"container"`
	Backups   []storage.BackupFile `json:"backups,omitempty"`
	Error     string               `json:"error,omitempty"`
}

// DeleteResponse is the response for a backup delete request
type DeleteResponse struct {
	Success   bool   `json:"success"`
	Container string `json:"container"`
	Key       string `json:"key,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// RestoreResponse is the response for a backup restore request
type RestoreResponse struct {
	Success   bool   `json:"success"`
	Container string `json:"container"`
	Key       string `json:"key,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Server provides HTTP API over Unix socket
type Server struct {
	socketPath     string
	server         *http.Server
	listener       net.Listener
	backupTrigger  BackupTrigger
	backupLister   BackupLister
	backupDeleter  BackupDeleter
	backupRestorer BackupRestorer
}

// NewServer creates a new API server
func NewServer(socketPath string) *Server {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	return &Server{
		socketPath: socketPath,
	}
}

// SetBackupTrigger sets the function to call when a backup is triggered
func (s *Server) SetBackupTrigger(trigger BackupTrigger) {
	s.backupTrigger = trigger
}

// SetBackupLister sets the function to call when listing backups
func (s *Server) SetBackupLister(lister BackupLister) {
	s.backupLister = lister
}

// SetBackupDeleter sets the function to call when deleting a backup
func (s *Server) SetBackupDeleter(deleter BackupDeleter) {
	s.backupDeleter = deleter
}

// SetBackupRestorer sets the function to call when restoring a backup
func (s *Server) SetBackupRestorer(restorer BackupRestorer) {
	s.backupRestorer = restorer
}

// Start begins serving API endpoints on Unix socket
func (s *Server) Start() error {
	if err := os.RemoveAll(s.socketPath); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	s.listener = listener

	if err := os.Chmod(s.socketPath, 0660); err != nil {
		_ = listener.Close()
		return err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/backup/run/", s.handleBackupRun)
	mux.HandleFunc("/backup/list/", s.handleBackupList)
	mux.HandleFunc("/backup/delete/", s.handleBackupDelete)
	mux.HandleFunc("/backup/restore/", s.handleBackupRestore)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	slog.Info("starting API server", "socket", s.socketPath)
	return s.server.Serve(listener)
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	err := s.server.Shutdown(ctx)

	_ = os.RemoveAll(s.socketPath)

	return err
}

// SocketPath returns the socket path
func (s *Server) SocketPath() string {
	return s.socketPath
}

func (s *Server) handleBackupRun(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(BackupResponse{
			Success: false,
			Error:   "method not allowed, use POST",
		})
		return
	}

	containerName := strings.TrimPrefix(r.URL.Path, "/backup/run/")
	containerName = strings.TrimSpace(containerName)

	if containerName == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(BackupResponse{
			Success: false,
			Error:   "container name is required",
		})
		return
	}

	slog.Info("backup triggered via API", "container", containerName)

	if err := s.backupTrigger(r.Context(), containerName); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(BackupResponse{
			Success:   false,
			Container: containerName,
			Error:     err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(BackupResponse{
		Success:   true,
		Container: containerName,
		Message:   "backup completed successfully",
	})
}

func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(ListResponse{
			Success: false,
			Error:   "method not allowed, use GET",
		})
		return
	}

	containerName := strings.TrimPrefix(r.URL.Path, "/backup/list/")
	containerName = strings.TrimSpace(containerName)

	if containerName == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ListResponse{
			Success: false,
			Error:   "container name is required",
		})
		return
	}

	backups, err := s.backupLister(r.Context(), containerName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(ListResponse{
			Success:   false,
			Container: containerName,
			Error:     err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ListResponse{
		Success:   true,
		Container: containerName,
		Backups:   backups,
	})
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(DeleteResponse{
			Success: false,
			Error:   "method not allowed, use DELETE",
		})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/backup/delete/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(DeleteResponse{
			Success: false,
			Error:   "container name and backup key are required (format: /backup/delete/{container}/{key})",
		})
		return
	}

	containerName := strings.TrimSpace(parts[0])
	backupKey := strings.TrimSpace(parts[1])

	slog.Info("backup delete requested via API", "container", containerName, "key", backupKey)

	if err := s.backupDeleter(r.Context(), containerName, backupKey); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(DeleteResponse{
			Success:   false,
			Container: containerName,
			Key:       backupKey,
			Error:     err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(DeleteResponse{
		Success:   true,
		Container: containerName,
		Key:       backupKey,
		Message:   "backup deleted successfully",
	})
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(RestoreResponse{
			Success: false,
			Error:   "method not allowed, use POST",
		})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/backup/restore/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(RestoreResponse{
			Success: false,
			Error:   "container name and backup key are required (format: /backup/restore/{container}/{key})",
		})
		return
	}

	containerName := strings.TrimSpace(parts[0])
	backupKey := strings.TrimSpace(parts[1])

	slog.Info("backup restore requested via API", "container", containerName, "key", backupKey)

	if err := s.backupRestorer(r.Context(), containerName, backupKey); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(RestoreResponse{
			Success:   false,
			Container: containerName,
			Key:       backupKey,
			Error:     err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(RestoreResponse{
		Success:   true,
		Container: containerName,
		Key:       backupKey,
		Message:   "backup restored successfully",
	})
}
