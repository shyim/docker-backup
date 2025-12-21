package discord

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
	notification.Register(&DiscordType{})
}

// DiscordType implements NotifierType for Discord
type DiscordType struct{}

// Name returns the notifier type identifier
func (t *DiscordType) Name() string {
	return "discord"
}

// Create instantiates a Discord notifier from options
func (t *DiscordType) Create(name string, options map[string]string) (notification.Notifier, error) {
	webhookURL, ok := options["webhook-url"]
	if !ok || webhookURL == "" {
		return nil, fmt.Errorf("discord notifier %q requires 'webhook-url' option", name)
	}

	username := options["username"]
	if username == "" {
		username = "Docker Backup"
	}

	return &DiscordNotifier{
		name:       name,
		webhookURL: webhookURL,
		username:   username,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// DiscordNotifier sends notifications via Discord Webhooks
type DiscordNotifier struct {
	name       string
	webhookURL string
	username   string
	client     *http.Client
}

// Name returns the notifier instance name
func (d *DiscordNotifier) Name() string {
	return d.name
}

// Type returns the notifier type
func (d *DiscordNotifier) Type() string {
	return "discord"
}

// Send sends a notification to Discord
func (d *DiscordNotifier) Send(ctx context.Context, event notification.Event) error {
	embed := d.createEmbed(event)

	payload := map[string]interface{}{
		"username": d.username,
		"embeds":   []map[string]interface{}{embed},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord API returned status %d", resp.StatusCode)
	}

	return nil
}

// createEmbed creates a Discord embed for an event
func (d *DiscordNotifier) createEmbed(event notification.Event) map[string]interface{} {
	var title string
	var color int

	switch event.Type {
	case notification.EventBackupStarted:
		title = "Backup Started"
		color = 3447003 // Blue
	case notification.EventBackupCompleted:
		title = "Backup Completed"
		color = 3066993 // Green
	case notification.EventBackupFailed:
		title = "Backup Failed"
		color = 15158332 // Red
	case notification.EventRestoreStarted:
		title = "Restore Started"
		color = 3447003 // Blue
	case notification.EventRestoreCompleted:
		title = "Restore Completed"
		color = 3066993 // Green
	case notification.EventRestoreFailed:
		title = "Restore Failed"
		color = 15158332 // Red
	default:
		title = string(event.Type)
		color = 9807270 // Gray
	}

	fields := []map[string]interface{}{
		{
			"name":   "Container",
			"value":  fmt.Sprintf("`%s`", event.ContainerName),
			"inline": true,
		},
		{
			"name":   "Type",
			"value":  fmt.Sprintf("`%s`", event.BackupType),
			"inline": true,
		},
	}

	if event.BackupKey != "" {
		fields = append(fields, map[string]interface{}{
			"name":   "Key",
			"value":  fmt.Sprintf("`%s`", event.BackupKey),
			"inline": false,
		})
	}

	if event.Size > 0 {
		fields = append(fields, map[string]interface{}{
			"name":   "Size",
			"value":  formatSize(event.Size),
			"inline": true,
		})
	}

	if event.Duration > 0 {
		fields = append(fields, map[string]interface{}{
			"name":   "Duration",
			"value":  event.Duration.Round(time.Millisecond).String(),
			"inline": true,
		})
	}

	if event.Error != nil {
		fields = append(fields, map[string]interface{}{
			"name":   "Error",
			"value":  fmt.Sprintf("```%s```", event.Error.Error()),
			"inline": false,
		})
	}

	embed := map[string]interface{}{
		"title":     title,
		"color":     color,
		"fields":    fields,
		"timestamp": event.Timestamp.Format(time.RFC3339),
	}

	return embed
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
