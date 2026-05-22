package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type NotificationService struct {
	db *gorm.DB
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

type CreateNotificationInput struct {
	Name   string
	Type   meshdb.NotificationChannelType
	Config map[string]string
	Events []string
}

type UpdateNotificationInput struct {
	Name    *string
	Config  map[string]string
	Events  []string
	Enabled *bool
}

func (s *NotificationService) List(ctx context.Context, orgID uuid.UUID) ([]meshdb.NotificationChannel, error) {
	var rows []meshdb.NotificationChannel
	err := s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Order("created_at asc").
		Find(&rows).Error
	return rows, err
}

func (s *NotificationService) Create(ctx context.Context, orgID uuid.UUID, in CreateNotificationInput) (*meshdb.NotificationChannel, error) {
	switch in.Type {
	case meshdb.NotificationEmail, meshdb.NotificationWebhook,
		meshdb.NotificationSlack, meshdb.NotificationDiscord:
	default:
		return nil, fmt.Errorf("unsupported channel type %q", in.Type)
	}
	if err := validateNotificationConfig(in.Type, in.Config); err != nil {
		return nil, err
	}
	cfg := make(meshdb.JSONObject, len(in.Config))
	for k, v := range in.Config {
		cfg[k] = v
	}
	row := meshdb.NotificationChannel{
		OrganizationID: orgID,
		Name:           in.Name,
		Type:           in.Type,
		Config:         cfg,
		Events:         in.Events,
		Enabled:        true,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *NotificationService) Update(ctx context.Context, id, orgID uuid.UUID, in UpdateNotificationInput) (*meshdb.NotificationChannel, error) {
	var row meshdb.NotificationChannel
	if err := s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		First(&row).Error; err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Config != nil {
		if err := validateNotificationConfig(row.Type, in.Config); err != nil {
			return nil, err
		}
		cfg := make(meshdb.JSONObject, len(in.Config))
		for k, v := range in.Config {
			cfg[k] = v
		}
		updates["config"] = cfg
	}
	if in.Events != nil {
		updates["events"] = meshdb.StringArray(in.Events)
	}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *NotificationService) Delete(ctx context.Context, id, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		Delete(&meshdb.NotificationChannel{}).Error
}

func validateNotificationConfig(t meshdb.NotificationChannelType, cfg map[string]string) error {
	switch t {
	case meshdb.NotificationEmail:
		if cfg["address"] == "" {
			return fmt.Errorf("email channel requires config.address")
		}
	case meshdb.NotificationWebhook:
		if cfg["url"] == "" {
			return fmt.Errorf("webhook channel requires config.url")
		}
	case meshdb.NotificationSlack, meshdb.NotificationDiscord:
		if cfg["webhook_url"] == "" {
			return fmt.Errorf("%s channel requires config.webhook_url", t)
		}
	}
	return nil
}

// ─── Dispatch ─────────────────────────────────────────────────────────────────

// NotificationData carries event context. Populate whichever fields are relevant.
type NotificationData struct {
	ServiceName string
	ProjectName string
	NodeName    string
}

// Dispatch sends event to all enabled channels for the org that subscribe to it.
// Errors are logged and swallowed — notification failures must not affect callers.
func (s *NotificationService) Dispatch(ctx context.Context, orgID uuid.UUID, event string, data NotificationData) {
	var channels []meshdb.NotificationChannel
	if err := s.db.WithContext(ctx).
		Where("organization_id = ? AND enabled = true", orgID).
		Find(&channels).Error; err != nil {
		log.Printf("notification dispatch: load channels: %v", err)
		return
	}

	// Lazy-load SMTP config once if any email channel is subscribed.
	var emailCfg *meshdb.OrgEmailConfig
	emailCfgLoaded := false

	for _, ch := range channels {
		if !slices.Contains(ch.Events, event) {
			continue
		}

		if ch.Type == meshdb.NotificationEmail && !emailCfgLoaded {
			var cfg meshdb.OrgEmailConfig
			if s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&cfg).Error == nil {
				emailCfg = &cfg
			}
			emailCfgLoaded = true
		}

		if err := sendNotification(ch, event, data, emailCfg); err != nil {
			log.Printf("notification %q (%s): %v", ch.Name, ch.Type, err)
		}
	}
}

// ─── Senders ──────────────────────────────────────────────────────────────────

var eventTitles = map[string]string{
	"deploy.success": "Deployment succeeded",
	"deploy.failed":  "Deployment failed",
	"backup.success": "Backup succeeded",
	"backup.failed":  "Backup failed",
	"node.offline":   "Node went offline",
	"job.failed":     "Job failed",
}

var slackColors = map[string]string{
	"deploy.success": "#22c55e",
	"deploy.failed":  "#ef4444",
	"backup.success": "#22c55e",
	"backup.failed":  "#ef4444",
	"node.offline":   "#f97316",
	"job.failed":     "#ef4444",
}

var discordColors = map[string]int{
	"deploy.success": 0x22c55e,
	"deploy.failed":  0xef4444,
	"backup.success": 0x22c55e,
	"backup.failed":  0xef4444,
	"node.offline":   0xf97316,
	"job.failed":     0xef4444,
}

func sendNotification(ch meshdb.NotificationChannel, event string, data NotificationData, emailCfg *meshdb.OrgEmailConfig) error {
	switch ch.Type {
	case meshdb.NotificationWebhook:
		return sendWebhook(ch, event, data)
	case meshdb.NotificationSlack:
		return sendSlack(ch, event, data)
	case meshdb.NotificationDiscord:
		return sendDiscord(ch, event, data)
	case meshdb.NotificationEmail:
		if emailCfg == nil {
			return fmt.Errorf("no SMTP provider configured for this org")
		}
		return sendEmail(ch, event, data, *emailCfg)
	default:
		return nil
	}
}

// ── Webhook ───────────────────────────────────────────────────────────────────

func sendWebhook(ch meshdb.NotificationChannel, event string, data NotificationData) error {
	url, _ := ch.Config["url"].(string)
	if url == "" {
		return fmt.Errorf("missing config.url")
	}
	body, err := json.Marshal(map[string]any{
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]string{
			"service": data.ServiceName,
			"project": data.ProjectName,
			"node":    data.NodeName,
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret, _ := ch.Config["secret"].(string); secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Meshploy-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// ── Slack ─────────────────────────────────────────────────────────────────────

type slackPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color  string       `json:"color"`
	Fields []slackField `json:"fields,omitempty"`
	Footer string       `json:"footer"`
	Ts     int64        `json:"ts"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

func sendSlack(ch meshdb.NotificationChannel, event string, data NotificationData) error {
	webhookURL, _ := ch.Config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing config.webhook_url")
	}
	title := eventTitles[event]
	if title == "" {
		title = event
	}
	color := slackColors[event]
	if color == "" {
		color = "#6b7280"
	}
	var fields []slackField
	if data.ServiceName != "" {
		fields = append(fields, slackField{"Service", data.ServiceName, true})
	}
	if data.ProjectName != "" {
		fields = append(fields, slackField{"Project", data.ProjectName, true})
	}
	if data.NodeName != "" {
		fields = append(fields, slackField{"Node", data.NodeName, true})
	}
	return postJSON(webhookURL, slackPayload{
		Text: title,
		Attachments: []slackAttachment{{
			Color:  color,
			Fields: fields,
			Footer: "Meshploy",
			Ts:     time.Now().Unix(),
		}},
	})
}

// ── Discord ───────────────────────────────────────────────────────────────────

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Color       int           `json:"color"`
	Footer      discordFooter `json:"footer"`
	Timestamp   string        `json:"timestamp"`
}

type discordFooter struct {
	Text string `json:"text"`
}

func sendDiscord(ch meshdb.NotificationChannel, event string, data NotificationData) error {
	webhookURL, _ := ch.Config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing config.webhook_url")
	}
	title := eventTitles[event]
	if title == "" {
		title = event
	}
	color := discordColors[event]
	var desc string
	switch {
	case data.ServiceName != "" && data.ProjectName != "":
		desc = fmt.Sprintf("**%s** in **%s**", data.ServiceName, data.ProjectName)
	case data.NodeName != "":
		desc = fmt.Sprintf("**%s**", data.NodeName)
	case data.ServiceName != "":
		desc = fmt.Sprintf("**%s**", data.ServiceName)
	}
	return postJSON(webhookURL, discordPayload{
		Embeds: []discordEmbed{{
			Title:       title,
			Description: desc,
			Color:       color,
			Footer:      discordFooter{"Meshploy"},
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}},
	})
}

// ── Email ─────────────────────────────────────────────────────────────────────

func sendEmail(ch meshdb.NotificationChannel, event string, data NotificationData, cfg meshdb.OrgEmailConfig) error {
	to, _ := ch.Config["address"].(string)
	if to == "" {
		return fmt.Errorf("missing config.address")
	}
	title := eventTitles[event]
	if title == "" {
		title = event
	}

	from := cfg.FromAddress
	if cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromAddress)
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "Subject: [Meshploy] %s\r\n", title)
	fmt.Fprintf(&msg, "From: %s\r\n", from)
	fmt.Fprintf(&msg, "To: %s\r\n", to)
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&msg, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&msg, "\r\n%s\r\n", title)
	if data.ServiceName != "" {
		fmt.Fprintf(&msg, "\r\nService: %s", data.ServiceName)
	}
	if data.ProjectName != "" {
		fmt.Fprintf(&msg, "\r\nProject: %s", data.ProjectName)
	}
	if data.NodeName != "" {
		fmt.Fprintf(&msg, "\r\nNode: %s", data.NodeName)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, string(cfg.Password), cfg.Host)

	if cfg.UseTLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			return fmt.Errorf("tls dial: %w", err)
		}
		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
		defer client.Close()
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
		if err := client.Mail(cfg.FromAddress); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		defer w.Close()
		_, err = io.WriteString(w, msg.String())
		return err
	}
	return smtp.SendMail(addr, auth, cfg.FromAddress, []string{to}, []byte(msg.String()))
}

// ─── Shared ───────────────────────────────────────────────────────────────────

func postJSON(url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
