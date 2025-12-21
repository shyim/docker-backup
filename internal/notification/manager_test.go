package notification

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockNotifier is a test implementation of Notifier
type mockNotifier struct {
	name      string
	typeName  string
	sendFunc  func(ctx context.Context, event Event) error
	sendCount int32
}

func (m *mockNotifier) Name() string {
	return m.name
}

func (m *mockNotifier) Type() string {
	return m.typeName
}

func (m *mockNotifier) Send(ctx context.Context, event Event) error {
	atomic.AddInt32(&m.sendCount, 1)
	if m.sendFunc != nil {
		return m.sendFunc(ctx, event)
	}
	return nil
}

func (m *mockNotifier) getSendCount() int {
	return int(atomic.LoadInt32(&m.sendCount))
}

func TestNewManager(t *testing.T) {
	mgr := NewManager()
	require.NotNil(t, mgr)
	assert.NotNil(t, mgr.notifiers, "expected notifiers map to be initialized")
	assert.Equal(t, 0, mgr.NotifierCount())
}

func TestManager_AddNotifier(t *testing.T) {
	mgr := NewManager()
	notifier := &mockNotifier{name: "test", typeName: "mock"}

	mgr.AddNotifier("test", notifier)

	assert.Equal(t, 1, mgr.NotifierCount())
}

func TestManager_AddNotifier_Multiple(t *testing.T) {
	mgr := NewManager()

	mgr.AddNotifier("telegram", &mockNotifier{name: "telegram", typeName: "telegram"})
	mgr.AddNotifier("discord", &mockNotifier{name: "discord", typeName: "discord"})
	mgr.AddNotifier("slack", &mockNotifier{name: "slack", typeName: "slack"})

	assert.Equal(t, 3, mgr.NotifierCount())
}

func TestManager_AddNotifier_Replace(t *testing.T) {
	mgr := NewManager()

	notifier1 := &mockNotifier{name: "test", typeName: "mock1"}
	notifier2 := &mockNotifier{name: "test", typeName: "mock2"}

	mgr.AddNotifier("test", notifier1)
	mgr.AddNotifier("test", notifier2)

	assert.Equal(t, 1, mgr.NotifierCount(), "expected 1 notifier after replacement")

	// Check that the replacement is used
	notifiers := mgr.ListNotifiers()
	require.Len(t, notifiers, 1)
	assert.Equal(t, "mock2", notifiers[0].Type, "expected replacement notifier to be used")
}

func TestManager_Notify_SingleProvider(t *testing.T) {
	mgr := NewManager()
	notifier := &mockNotifier{name: "telegram", typeName: "telegram"}
	mgr.AddNotifier("telegram", notifier)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
		BackupType:    "postgres",
	}

	mgr.Notify(ctx, event, []string{"telegram"})

	assert.Equal(t, 1, notifier.getSendCount())
}

func TestManager_Notify_MultipleProviders(t *testing.T) {
	mgr := NewManager()
	telegram := &mockNotifier{name: "telegram", typeName: "telegram"}
	discord := &mockNotifier{name: "discord", typeName: "discord"}

	mgr.AddNotifier("telegram", telegram)
	mgr.AddNotifier("discord", discord)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
	}

	mgr.Notify(ctx, event, []string{"telegram", "discord"})

	assert.Equal(t, 1, telegram.getSendCount(), "expected 1 telegram send")
	assert.Equal(t, 1, discord.getSendCount(), "expected 1 discord send")
}

func TestManager_Notify_EmptyProviders(t *testing.T) {
	mgr := NewManager()
	notifier := &mockNotifier{name: "telegram", typeName: "telegram"}
	mgr.AddNotifier("telegram", notifier)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
	}

	mgr.Notify(ctx, event, []string{})

	assert.Equal(t, 0, notifier.getSendCount(), "expected no sends with empty providers list")
}

func TestManager_Notify_NilProviders(t *testing.T) {
	mgr := NewManager()
	notifier := &mockNotifier{name: "telegram", typeName: "telegram"}
	mgr.AddNotifier("telegram", notifier)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
	}

	mgr.Notify(ctx, event, nil)

	assert.Equal(t, 0, notifier.getSendCount(), "expected no sends with nil providers")
}

func TestManager_Notify_UnknownProvider(t *testing.T) {
	mgr := NewManager()
	notifier := &mockNotifier{name: "telegram", typeName: "telegram"}
	mgr.AddNotifier("telegram", notifier)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
	}

	// Should not panic, just log warning
	mgr.Notify(ctx, event, []string{"unknown"})

	assert.Equal(t, 0, notifier.getSendCount(), "expected no sends for unknown provider")
}

func TestManager_Notify_PartialMatch(t *testing.T) {
	mgr := NewManager()
	telegram := &mockNotifier{name: "telegram", typeName: "telegram"}
	mgr.AddNotifier("telegram", telegram)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
	}

	// One exists, one doesn't
	mgr.Notify(ctx, event, []string{"telegram", "unknown"})

	assert.Equal(t, 1, telegram.getSendCount())
}

func TestManager_Notify_SendError(t *testing.T) {
	mgr := NewManager()
	notifier := &mockNotifier{
		name:     "failing",
		typeName: "mock",
		sendFunc: func(ctx context.Context, event Event) error {
			return errors.New("send failed")
		},
	}
	mgr.AddNotifier("failing", notifier)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
	}

	// Should not panic, just log error
	mgr.Notify(ctx, event, []string{"failing"})

	assert.Equal(t, 1, notifier.getSendCount(), "expected 1 send attempt")
}

func TestManager_Notify_Concurrent(t *testing.T) {
	mgr := NewManager()

	var sendCount int32
	notifier := &mockNotifier{
		name:     "test",
		typeName: "mock",
		sendFunc: func(ctx context.Context, event Event) error {
			atomic.AddInt32(&sendCount, 1)
			time.Sleep(10 * time.Millisecond) // Simulate work
			return nil
		},
	}
	mgr.AddNotifier("test", notifier)

	ctx := context.Background()
	event := Event{
		Type:          EventBackupCompleted,
		ContainerName: "postgres",
	}

	// Send multiple notifications concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.Notify(ctx, event, []string{"test"})
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(10), atomic.LoadInt32(&sendCount))
}

func TestManager_NotifierCount(t *testing.T) {
	mgr := NewManager()

	assert.Equal(t, 0, mgr.NotifierCount(), "expected 0 initially")

	mgr.AddNotifier("a", &mockNotifier{name: "a", typeName: "mock"})
	assert.Equal(t, 1, mgr.NotifierCount())

	mgr.AddNotifier("b", &mockNotifier{name: "b", typeName: "mock"})
	assert.Equal(t, 2, mgr.NotifierCount())
}

func TestManager_ListNotifiers(t *testing.T) {
	mgr := NewManager()

	mgr.AddNotifier("telegram", &mockNotifier{name: "telegram", typeName: "telegram"})
	mgr.AddNotifier("discord", &mockNotifier{name: "discord", typeName: "discord"})

	notifiers := mgr.ListNotifiers()
	require.Len(t, notifiers, 2)

	// Check that both are present (order is not guaranteed)
	found := make(map[string]string)
	for _, n := range notifiers {
		found[n.Name] = n.Type
	}

	assert.Equal(t, "telegram", found["telegram"], "expected telegram notifier")
	assert.Equal(t, "discord", found["discord"], "expected discord notifier")
}

func TestManager_ListNotifiers_Empty(t *testing.T) {
	mgr := NewManager()

	notifiers := mgr.ListNotifiers()
	assert.Empty(t, notifiers)
}

func TestManager_ConcurrentAddAndNotify(t *testing.T) {
	mgr := NewManager()

	done := make(chan bool)

	// Add notifiers concurrently
	for i := 0; i < 5; i++ {
		go func(id int) {
			name := string(rune('a' + id))
			mgr.AddNotifier(name, &mockNotifier{name: name, typeName: "mock"})
			done <- true
		}(i)
	}

	// Notify concurrently
	ctx := context.Background()
	event := Event{Type: EventBackupCompleted}
	for i := 0; i < 5; i++ {
		go func() {
			mgr.Notify(ctx, event, []string{"a", "b", "c"})
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestEvent_Fields(t *testing.T) {
	now := time.Now()
	err := errors.New("test error")

	event := Event{
		Type:          EventBackupFailed,
		ContainerName: "postgres",
		BackupType:    "postgres",
		BackupKey:     "postgres/db/2024-01-15/backup.sql",
		Size:          1024,
		Duration:      5 * time.Second,
		Error:         err,
		Timestamp:     now,
	}

	assert.Equal(t, EventBackupFailed, event.Type)
	assert.Equal(t, "postgres", event.ContainerName)
	assert.Equal(t, int64(1024), event.Size)
	assert.Equal(t, 5*time.Second, event.Duration)
	assert.Equal(t, err, event.Error)
	assert.Equal(t, now, event.Timestamp)
}

func TestEventTypes(t *testing.T) {
	types := []EventType{
		EventBackupStarted,
		EventBackupCompleted,
		EventBackupFailed,
		EventRestoreStarted,
		EventRestoreCompleted,
		EventRestoreFailed,
	}

	for _, et := range types {
		assert.NotEmpty(t, et, "event type should not be empty")
	}

	// Ensure they're all unique
	seen := make(map[EventType]bool)
	for _, et := range types {
		assert.False(t, seen[et], "duplicate event type: %s", et)
		seen[et] = true
	}
}
