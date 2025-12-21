package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shyim/docker-backup/internal/notification"
)

func init() {
	notification.Register(&TelegramType{})
}

// TelegramType implements NotifierType for Telegram
type TelegramType struct{}

// Name returns the notifier type identifier
func (t *TelegramType) Name() string {
	return "telegram"
}

// Create instantiates a Telegram notifier from options
func (t *TelegramType) Create(name string, options map[string]string) (notification.Notifier, error) {
	token, ok := options["token"]
	if !ok || token == "" {
		return nil, fmt.Errorf("telegram notifier %q requires 'token' option", name)
	}

	chatID, ok := options["chat-id"]
	if !ok || chatID == "" {
		return nil, fmt.Errorf("telegram notifier %q requires 'chat-id' option", name)
	}

	return &TelegramNotifier{
		name:   name,
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// TelegramNotifier sends notifications via Telegram Bot API
type TelegramNotifier struct {
	name   string
	token  string
	chatID string
	client *http.Client
}

// Name returns the notifier instance name
func (t *TelegramNotifier) Name() string {
	return t.name
}

// Type returns the notifier type
func (t *TelegramNotifier) Type() string {
	return "telegram"
}

// Send sends a notification to Telegram
func (t *TelegramNotifier) Send(ctx context.Context, event notification.Event) error {
	message := t.formatMessage(event)

	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

// formatMessage formats an event into a Telegram message
func (t *TelegramNotifier) formatMessage(event notification.Event) string {
	var emoji, title string

	switch event.Type {
	case notification.EventBackupStarted:
		emoji = "🔄"
		title = "Backup Started"
	case notification.EventBackupCompleted:
		emoji = "✅"
		title = "Backup Completed"
	case notification.EventBackupFailed:
		emoji = "❌"
		title = "Backup Failed"
	case notification.EventRestoreStarted:
		emoji = "🔄"
		title = "Restore Started"
	case notification.EventRestoreCompleted:
		emoji = "✅"
		title = "Restore Completed"
	case notification.EventRestoreFailed:
		emoji = "❌"
		title = "Restore Failed"
	default:
		emoji = "ℹ️"
		title = string(event.Type)
	}

	msg := fmt.Sprintf("%s <b>%s</b>\n\n", emoji, title)
	msg += fmt.Sprintf("🐳 Container: <code>%s</code>\n", event.ContainerName)
	msg += fmt.Sprintf("📦 Type: <code>%s</code>\n", event.BackupType)

	if event.BackupKey != "" {
		msg += fmt.Sprintf("🔑 Key: <code>%s</code>\n", event.BackupKey)
	}

	if event.Size > 0 {
		msg += fmt.Sprintf("📊 Size: %s\n", formatSize(event.Size))
	}

	if event.Duration > 0 {
		msg += fmt.Sprintf("⏱ Duration: %s\n", event.Duration.Round(time.Millisecond))
	}

	if event.Error != nil {
		msg += fmt.Sprintf("\n⚠️ Error: <code>%s</code>", event.Error.Error())
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
