package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// JobFunc is the function signature for scheduled jobs
type JobFunc func(ctx context.Context)

// Scheduler manages cron jobs for container backups
type Scheduler struct {
	cron *cron.Cron
	jobs map[string]cron.EntryID // containerID -> entryID
	mu   sync.RWMutex
}

// New creates a new scheduler
func New() *Scheduler {
	return &Scheduler{
		cron: cron.New(cron.WithParser(cron.NewParser(
			cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
		))),
		jobs: make(map[string]cron.EntryID),
	}
}

// Start begins the scheduler
func (s *Scheduler) Start() {
	s.cron.Start()
	slog.Info("scheduler started")
}

// Stop gracefully stops the scheduler and waits for running jobs
func (s *Scheduler) Stop() context.Context {
	return s.cron.Stop()
}

// AddJob schedules a backup job for a container
func (s *Scheduler) AddJob(containerID, schedule string, job JobFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[containerID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, containerID)
	}

	ctx := context.Background()
	wrappedJob := func() {
		job(ctx)
	}

	entryID, err := s.cron.AddFunc(schedule, wrappedJob)
	if err != nil {
		return err
	}

	s.jobs[containerID] = entryID
	slog.Debug("added scheduled job", "container_id", containerID, "schedule", schedule)

	return nil
}

// RemoveJob removes a scheduled job for a container
func (s *Scheduler) RemoveJob(containerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[containerID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, containerID)
		slog.Debug("removed scheduled job", "container_id", containerID)
	}
}

// UpdateJob updates an existing job's schedule
func (s *Scheduler) UpdateJob(containerID, schedule string, job JobFunc) error {
	return s.AddJob(containerID, schedule, job)
}

// HasJob checks if a container has a scheduled job
func (s *Scheduler) HasJob(containerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.jobs[containerID]
	return exists
}

// JobCount returns the number of scheduled jobs
func (s *Scheduler) JobCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.jobs)
}

// JobInfo contains information about a scheduled job
type JobInfo struct {
	ContainerID string
	NextRun     time.Time
}

// ListJobs returns information about all scheduled jobs
func (s *Scheduler) ListJobs() map[string]JobInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]JobInfo, len(s.jobs))
	for containerID, entryID := range s.jobs {
		entry := s.cron.Entry(entryID)
		result[containerID] = JobInfo{
			ContainerID: containerID,
			NextRun:     entry.Next,
		}
	}
	return result
}
