---
icon: lucide/bell
---

# Adding Notification Providers

This guide walks you through creating a new notification provider for docker-backup.

## Interfaces

Notification providers implement two interfaces:

### NotifierType

Factory for creating notifier instances:

```go
type NotifierType interface {
    // Name returns the notifier type identifier
    Name() string

    // Create creates a notifier instance from options
    Create(name string, options map[string]string) (Notifier, error)
}
```

### Notifier

The actual notification operations:

```go
type Notifier interface {
    // Name returns this notifier instance's name
    Name() string

    // Send sends a notification for an event
    Send(ctx context.Context, event Event) error
}
```

## Event Structure

Events contain information about what happened:

```go
type Event struct {
    Type          EventType     // Event type
    ContainerName string        // Container name
    ConfigName    string        // Backup config name
    BackupType    string        // Backup type (postgres, mysql, etc)
    Storage       string        // Storage pool name
    Size          int64         // Backup size (for completed)
    Duration      time.Duration // Backup duration (for completed)
    Error         error         // Error (for failed)
}

type EventType string

const (
    EventBackupStarted   EventType = "backup_started"
    EventBackupCompleted EventType = "backup_completed"
    EventBackupFailed    EventType = "backup_failed"
    EventRestoreStarted  EventType = "restore_started"
    EventRestoreCompleted EventType = "restore_completed"
    EventRestoreFailed   EventType = "restore_failed"
)
```

## Example: Slack Notifier

### Step 1: Create Package

Create `internal/notifiers/slack/slack.go`:

```go
package slack

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/shyim/docker-backup/internal/notification"
)

func init() {
    notification.Register(&SlackType{})
}

type SlackType struct{}

func (t *SlackType) Name() string {
    return "slack"
}
```

### Step 2: Implement Create

Parse options and create notifier:

```go
func (t *SlackType) Create(name string, options map[string]string) (notification.Notifier, error) {
    webhookURL := options["webhook-url"]
    if webhookURL == "" {
        return nil, fmt.Errorf("slack notifier %q requires 'webhook-url'", name)
    }

    return &SlackNotifier{
        name:       name,
        webhookURL: webhookURL,
        channel:    options["channel"],  // Optional
        username:   options["username"], // Optional
    }, nil
}
```

### Step 3: Implement Notifier

```go
type SlackNotifier struct {
    name       string
    webhookURL string
    channel    string
    username   string
}

func (s *SlackNotifier) Name() string {
    return s.name
}

func (s *SlackNotifier) Send(ctx context.Context, event notification.Event) error {
    message := s.formatMessage(event)

    payload := map[string]interface{}{
        "text": message,
    }

    if s.channel != "" {
        payload["channel"] = s.channel
    }
    if s.username != "" {
        payload["username"] = s.username
    }

    body, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("failed to marshal payload: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to send request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("slack returned status %d", resp.StatusCode)
    }

    return nil
}

func (s *SlackNotifier) formatMessage(event notification.Event) string {
    var emoji, status string

    switch event.Type {
    case notification.EventBackupStarted:
        emoji = ":hourglass:"
        status = "Started"
    case notification.EventBackupCompleted:
        emoji = ":white_check_mark:"
        status = "Completed"
    case notification.EventBackupFailed:
        emoji = ":x:"
        status = "Failed"
    case notification.EventRestoreStarted:
        emoji = ":arrows_counterclockwise:"
        status = "Restore Started"
    case notification.EventRestoreCompleted:
        emoji = ":white_check_mark:"
        status = "Restore Completed"
    case notification.EventRestoreFailed:
        emoji = ":x:"
        status = "Restore Failed"
    }

    msg := fmt.Sprintf("%s *Backup %s*\n", emoji, status)
    msg += fmt.Sprintf("Container: `%s`\n", event.ContainerName)
    msg += fmt.Sprintf("Config: `%s`\n", event.ConfigName)

    if event.Type == notification.EventBackupCompleted {
        msg += fmt.Sprintf("Size: %s\n", formatSize(event.Size))
        msg += fmt.Sprintf("Duration: %s\n", event.Duration.Round(time.Millisecond))
    }

    if event.Error != nil {
        msg += fmt.Sprintf("Error: `%s`\n", event.Error.Error())
    }

    return msg
}

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
```

### Step 4: Register Plugin

Add import to `internal/notifiers/registry.go`:

```go
package notifiers

import (
    _ "github.com/shyim/docker-backup/internal/notifiers/discord"
    _ "github.com/shyim/docker-backup/internal/notifiers/slack"
    _ "github.com/shyim/docker-backup/internal/notifiers/telegram"
)
```

## Configuration Options

Options are passed as `map[string]string` from CLI flags or environment variables:

```bash
# CLI
--notify=slack.webhook-url=https://hooks.slack.com/...
--notify=slack.channel=#backups
--notify=slack.username=Backup Bot

# Environment
DOCKER_BACKUP_NOTIFY_SLACK_WEBHOOK_URL=https://hooks.slack.com/...
DOCKER_BACKUP_NOTIFY_SLACK_CHANNEL=#backups
DOCKER_BACKUP_NOTIFY_SLACK_USERNAME=Backup Bot
```

## Best Practices

### Option Validation

Validate required options in `Create()`:

```go
func (t *SlackType) Create(name string, options map[string]string) (notification.Notifier, error) {
    webhookURL := options["webhook-url"]
    if webhookURL == "" {
        return nil, fmt.Errorf("slack notifier %q requires 'webhook-url'", name)
    }
    // Validate URL format
    if !strings.HasPrefix(webhookURL, "https://") {
        return nil, fmt.Errorf("slack webhook URL must use HTTPS")
    }
    // ...
}
```

### Context Handling

Always use context for HTTP requests:

```go
func (s *SlackNotifier) Send(ctx context.Context, event notification.Event) error {
    req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, body)
    // ...
}
```

### Error Handling

Provide clear error messages:

```go
if resp.StatusCode != http.StatusOK {
    body, _ := io.ReadAll(resp.Body)
    return fmt.Errorf("slack API error (status %d): %s", resp.StatusCode, body)
}
```

### Message Formatting

Use appropriate formatting for the platform:

```go
// Slack uses Markdown-like formatting
msg := fmt.Sprintf("*Bold* `code` _%s_", text)

// Discord uses similar but slightly different syntax
msg := fmt.Sprintf("**Bold** `code` *%s*", text)

// Telegram supports HTML or Markdown
msg := fmt.Sprintf("<b>Bold</b> <code>code</code> <i>%s</i>", text)
```

### Rate Limiting

Consider implementing rate limiting for high-frequency events:

```go
type SlackNotifier struct {
    lastSent time.Time
    minInterval time.Duration
}

func (s *SlackNotifier) Send(ctx context.Context, event notification.Event) error {
    if time.Since(s.lastSent) < s.minInterval {
        return nil // Skip to avoid rate limiting
    }
    s.lastSent = time.Now()
    // ...
}
```

## Testing

Create tests for your notifier:

```go
func TestSlackNotifier(t *testing.T) {
    // Mock HTTP server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != "POST" {
            t.Errorf("Expected POST, got %s", r.Method)
        }
        if r.Header.Get("Content-Type") != "application/json" {
            t.Errorf("Expected JSON content type")
        }
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    notifier := &SlackNotifier{
        name:       "test",
        webhookURL: server.URL,
    }

    event := notification.Event{
        Type:          notification.EventBackupCompleted,
        ContainerName: "postgres",
        ConfigName:    "db",
        Size:          1024 * 1024,
        Duration:      5 * time.Second,
    }

    err := notifier.Send(context.Background(), event)
    if err != nil {
        t.Errorf("Send failed: %v", err)
    }
}

func TestSlackFormat(t *testing.T) {
    notifier := &SlackNotifier{name: "test"}

    event := notification.Event{
        Type:          notification.EventBackupFailed,
        ContainerName: "postgres",
        ConfigName:    "db",
        Error:         fmt.Errorf("connection timeout"),
    }

    msg := notifier.formatMessage(event)

    if !strings.Contains(msg, ":x:") {
        t.Error("Expected failure emoji")
    }
    if !strings.Contains(msg, "connection timeout") {
        t.Error("Expected error message")
    }
}
```

## Complete Examples

See existing implementations:

- **Telegram**: `internal/notifiers/telegram/telegram.go`
- **Discord**: `internal/notifiers/discord/discord.go`
