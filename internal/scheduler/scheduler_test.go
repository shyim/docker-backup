package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	s := New()
	require.NotNil(t, s, "expected non-nil scheduler")
	assert.NotNil(t, s.cron, "expected cron instance")
	assert.NotNil(t, s.jobs, "expected jobs map")
}

func TestAddJob(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	err := s.AddJob("container1", "* * * * *", func(ctx context.Context) {})
	require.NoError(t, err)
	assert.True(t, s.HasJob("container1"), "expected job to exist")
	assert.Equal(t, 1, s.JobCount())
}

func TestAddJob_InvalidSchedule(t *testing.T) {
	s := New()

	err := s.AddJob("container1", "invalid cron", func(ctx context.Context) {})
	assert.Error(t, err, "expected error for invalid cron schedule")
}

func TestAddJob_ReplacesExisting(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	var counter int32

	// Add first job
	err := s.AddJob("container1", "* * * * *", func(ctx context.Context) {
		atomic.AddInt32(&counter, 1)
	})
	require.NoError(t, err)

	// Add replacement job with same ID
	err = s.AddJob("container1", "*/5 * * * *", func(ctx context.Context) {
		atomic.AddInt32(&counter, 10)
	})
	require.NoError(t, err)

	// Should still be only 1 job
	assert.Equal(t, 1, s.JobCount(), "expected 1 job after replacement")
}

func TestRemoveJob(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	s.AddJob("container1", "* * * * *", func(ctx context.Context) {})
	require.True(t, s.HasJob("container1"), "job should exist before removal")

	s.RemoveJob("container1")

	assert.False(t, s.HasJob("container1"), "job should not exist after removal")
	assert.Equal(t, 0, s.JobCount())
}

func TestRemoveJob_NonExistent(t *testing.T) {
	s := New()

	// Should not panic
	s.RemoveJob("nonexistent")
}

func TestHasJob(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	assert.False(t, s.HasJob("container1"), "job should not exist initially")

	s.AddJob("container1", "* * * * *", func(ctx context.Context) {})

	assert.True(t, s.HasJob("container1"), "job should exist after adding")
	assert.False(t, s.HasJob("container2"), "non-added job should not exist")
}

func TestJobCount(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	assert.Equal(t, 0, s.JobCount(), "expected 0 jobs initially")

	s.AddJob("container1", "* * * * *", func(ctx context.Context) {})
	assert.Equal(t, 1, s.JobCount())

	s.AddJob("container2", "* * * * *", func(ctx context.Context) {})
	assert.Equal(t, 2, s.JobCount())

	s.RemoveJob("container1")
	assert.Equal(t, 1, s.JobCount(), "expected 1 job after removal")
}

func TestListJobs(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	s.AddJob("container1", "0 3 * * *", func(ctx context.Context) {})
	s.AddJob("container2", "0 * * * *", func(ctx context.Context) {})

	jobs := s.ListJobs()
	require.Len(t, jobs, 2)

	job1, exists := jobs["container1"]
	require.True(t, exists, "container1 job should exist in list")
	assert.Equal(t, "container1", job1.ContainerID)
	assert.False(t, job1.NextRun.IsZero(), "NextRun should not be zero")

	job2, exists := jobs["container2"]
	require.True(t, exists, "container2 job should exist in list")
	assert.Equal(t, "container2", job2.ContainerID)
}

func TestListJobs_Empty(t *testing.T) {
	s := New()

	jobs := s.ListJobs()
	assert.Empty(t, jobs)
}

func TestUpdateJob(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	s.AddJob("container1", "0 3 * * *", func(ctx context.Context) {})

	// Get initial next run time
	jobs := s.ListJobs()
	initialNextRun := jobs["container1"].NextRun

	// Update to a different schedule
	err := s.UpdateJob("container1", "0 * * * *", func(ctx context.Context) {})
	require.NoError(t, err)

	// Next run should be different (hourly vs daily)
	jobs = s.ListJobs()
	newNextRun := jobs["container1"].NextRun

	// The hourly schedule should have an earlier next run than daily
	assert.True(t, newNextRun.Before(initialNextRun), "hourly schedule should have earlier next run than daily")
}

func TestScheduler_ConcurrentAccess(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	done := make(chan bool)

	// Concurrent adds
	for i := 0; i < 10; i++ {
		go func(id int) {
			containerID := "container" + string(rune('0'+id))
			s.AddJob(containerID, "* * * * *", func(ctx context.Context) {})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 10, s.JobCount())

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			s.ListJobs()
			s.JobCount()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Concurrent removes
	for i := 0; i < 10; i++ {
		go func(id int) {
			containerID := "container" + string(rune('0'+id))
			s.RemoveJob(containerID)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 0, s.JobCount(), "expected 0 jobs after removal")
}

func TestScheduler_StartStop(t *testing.T) {
	s := New()

	s.Start()

	ctx := s.Stop()
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(time.Second):
		t.Error("Stop should complete quickly")
	}
}

func TestScheduler_ValidCronSchedules(t *testing.T) {
	s := New()

	schedules := []string{
		"* * * * *",       // Every minute
		"0 * * * *",       // Every hour
		"0 0 * * *",       // Every day at midnight
		"0 3 * * *",       // Every day at 3 AM
		"*/15 * * * *",    // Every 15 minutes
		"0 0 * * 0",       // Every Sunday
		"0 0 1 * *",       // First day of every month
		"30 4 1,15 * *",   // 4:30 AM on 1st and 15th
	}

	for _, schedule := range schedules {
		t.Run(schedule, func(t *testing.T) {
			err := s.AddJob("test", schedule, func(ctx context.Context) {})
			assert.NoError(t, err, "schedule %q should be valid", schedule)
			s.RemoveJob("test")
		})
	}
}

func TestScheduler_InvalidCronSchedules(t *testing.T) {
	s := New()

	schedules := []string{
		"",
		"invalid",
		"* * *",           // Too few fields
		"* * * * * *",     // Too many fields (6-field not enabled)
		"60 * * * *",      // Invalid minute
		"* 24 * * *",      // Invalid hour
		"* * 32 * *",      // Invalid day
		"* * * 13 *",      // Invalid month
		"* * * * 7",       // Invalid day of week (should be 0-6)
	}

	for _, schedule := range schedules {
		t.Run(schedule, func(t *testing.T) {
			err := s.AddJob("test", schedule, func(ctx context.Context) {})
			assert.Error(t, err, "schedule %q should be invalid", schedule)
			s.RemoveJob("test")
		})
	}
}
