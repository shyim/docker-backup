package notification

import (
	"context"
	"fmt"
	"time"

	gonotifier "github.com/shyim/go-notifier"
)

// CreateNotifierFromDSN creates a notifier instance from a DSN string
// DSN format examples:
// - telegram://BOT_TOKEN@default?channel=CHAT_ID
// - slack://BOT_TOKEN@default?channel=CHANNEL_ID
// - discord://WEBHOOK_TOKEN@default?webhook_id=WEBHOOK_ID
// - gotify://APP_TOKEN@SERVER_HOST
// - microsoftteams://default?webhook_url=WEBHOOK_URL
func CreateNotifierFromDSN(name, dsn string) (Notifier, error) {
	transport, err := gonotifier.NewTransportFromDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport from DSN: %w", err)
	}

	return &dsnNotifier{
		name:      name,
		transport: transport,
	}, nil
}

// dsnNotifier wraps go-notifier transport to implement our Notifier interface
type dsnNotifier struct {
	name      string
	transport gonotifier.TransportInterface
}

func (n *dsnNotifier) Name() string {
	return n.name
}

func (n *dsnNotifier) Send(ctx context.Context, event Event) error {
	message := formatEventMessage(event)
	chatMessage := gonotifier.NewChatMessage(message)

	_, err := n.transport.Send(ctx, chatMessage)
	return err
}

// formatEventMessage formats an event into a text message
func formatEventMessage(event Event) string {
	var title string

	switch event.Type {
	case EventBackupStarted:
		title = "Backup Started"
	case EventBackupCompleted:
		title = "Backup Completed"
	case EventBackupFailed:
		title = "Backup Failed"
	case EventRestoreStarted:
		title = "Restore Started"
	case EventRestoreCompleted:
		title = "Restore Completed"
	case EventRestoreFailed:
		title = "Restore Failed"
	default:
		title = string(event.Type)
	}

	msg := fmt.Sprintf("%s\n\n", title)
	msg += fmt.Sprintf("Container: %s\n", event.ContainerName)
	msg += fmt.Sprintf("Type: %s\n", event.BackupType)

	if event.BackupKey != "" {
		msg += fmt.Sprintf("Key: %s\n", event.BackupKey)
	}

	if event.Size > 0 {
		msg += fmt.Sprintf("Size: %s\n", formatSize(event.Size))
	}

	if event.Duration > 0 {
		msg += fmt.Sprintf("Duration: %s\n", event.Duration.Round(time.Millisecond))
	}

	if event.Error != nil {
		msg += fmt.Sprintf("\nError: %s", event.Error.Error())
	}

	return msg
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
