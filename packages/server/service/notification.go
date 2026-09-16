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
	// StackName names the stack a service belongs to, so an alert says what
	// the thing is without a second lookup. Empty for a standalone service,
	// which the senders render as such.
	StackName string
	// Detail is one line of context the event carries: who joined, what was
	// created. Used where a service and project name say nothing.
	Detail string
}

// ForService builds the payload for an event about a service, naming the stack
// it belongs to. An alert that says only "redis" leaves the reader asking which
// redis, and the answer is one join away.
func (s *NotificationService) ForService(ctx context.Context, svc *meshdb.Service, projectName string) NotificationData {
	data := NotificationData{ServiceName: svc.Name, ProjectName: projectName}
	if svc.StackID != nil {
		var stack meshdb.Stack
		if s.db.WithContext(ctx).Select("name").First(&stack, "id = ?", *svc.StackID).Error == nil {
			data.StackName = stack.Name
		}
	}
	return data
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

// EventTone is how an event reads at a glance, and the only thing the colours
// are derived from.
type EventTone string

const (
	ToneGood    EventTone = "good"
	ToneBad     EventTone = "bad"
	ToneWarning EventTone = "warning"
)

// EventDef describes one event to everything that needs to know about it: the
// senders, and the console's picker.
//
// One catalogue rather than a map per concern. The console used to carry its
// own hand-written list, and job.failed was dispatched for months while being
// absent from it -- an event nobody could subscribe to, and so never delivered.
type EventDef struct {
	Event       string    `json:"event"`
	Group       string    `json:"group"`
	Title       string    `json:"title"`
	Description string    `json:"description" doc:"When this fires, in plain words"`
	Tone        EventTone `json:"tone"`
	// Recommended events make up the set a new channel starts with: the ones
	// that mean something is wrong.
	Recommended bool `json:"recommended"`
}

var eventCatalogue = []EventDef{
	{"deploy.success", "Deploy", "Deployment succeeded", "A service finished deploying and is running. Covers builds, image deploys, rollbacks and databases.", ToneGood, false},
	{"deploy.failed", "Deploy", "Deployment failed", "A deploy did not finish: the build failed, or the workload never became ready.", ToneBad, true},

	{"service.crashed", "Service", "Service crashed", "A service that was running stopped working on its own, with no deploy involved.", ToneBad, true},
	{"service.recovered", "Service", "Service recovered", "A failing service started working again without a deploy.", ToneGood, false},

	{"job.success", "Job", "Job succeeded", "A job run finished with exit code 0.", ToneGood, false},
	{"job.failed", "Job", "Job failed", "A job run exited non-zero, or could not start.", ToneBad, true},

	{"backup.success", "Backup", "Backup succeeded", "A scheduled or manual backup was written to storage.", ToneGood, false},
	{"backup.failed", "Backup", "Backup failed", "A backup did not reach storage. The last good copy is older than you think.", ToneBad, true},
	{"restore.success", "Backup", "Restore succeeded", "A restore finished and the data is back.", ToneGood, false},
	{"restore.failed", "Backup", "Restore failed", "A restore did not finish. The service may be holding partial data.", ToneBad, true},

	{"member.joined", "Organization", "Member joined", "Someone accepted an invitation and can now sign in to this organization.", ToneWarning, true},
	{"agent.token_created", "Organization", "Agent token created", "A token was minted for an agent. It can act on this organization until revoked.", ToneWarning, true},

	{"node.offline", "Node", "Node went offline", "A node stopped answering on the mesh. Its workloads reschedule only if another node can take them.", ToneWarning, true},
	{"node.online", "Node", "Node came back", "A node that was reported offline is answering again.", ToneGood, false},
}

// Events is the catalogue, for the console's picker.
func Events() []EventDef { return eventCatalogue }

func eventDef(event string) (EventDef, bool) {
	for _, d := range eventCatalogue {
		if d.Event == event {
			return d, true
		}
	}
	return EventDef{}, false
}

func eventTitle(event string) string {
	if d, ok := eventDef(event); ok {
		return d.Title
	}
	return event
}

// The tone decides the colour, so a new event cannot ship with a title and no
// colour, which is what three parallel maps allowed.
var toneHex = map[EventTone]string{ToneGood: "#22c55e", ToneBad: "#ef4444", ToneWarning: "#f97316"}
var toneInt = map[EventTone]int{ToneGood: 0x22c55e, ToneBad: 0xef4444, ToneWarning: 0xf97316}

func eventSlackColor(event string) string {
	d, _ := eventDef(event)
	if c, ok := toneHex[d.Tone]; ok {
		return c
	}
	return "#64748b"
}

func eventDiscordColor(event string) int {
	d, _ := eventDef(event)
	if c, ok := toneInt[d.Tone]; ok {
		return c
	}
	return 0x64748b
}

// origin says what kind of service this is: part of a stack, or standalone.
// Empty when the event is not about a service at all.
func (d NotificationData) origin() string {
	if d.ServiceName == "" {
		return ""
	}
	if d.StackName != "" {
		return "stack " + d.StackName
	}
	return "standalone"
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
	title := eventTitle(event)
	if title == "" {
		title = event
	}
	color := eventSlackColor(event)
	if color == "" {
		color = "#6b7280"
	}
	var fields []slackField
	if data.ServiceName != "" {
		fields = append(fields, slackField{"Service", data.ServiceName, true})
		fields = append(fields, slackField{"Belongs to", data.origin(), true})
	}
	if data.ProjectName != "" {
		fields = append(fields, slackField{"Project", data.ProjectName, true})
	}
	if data.NodeName != "" {
		fields = append(fields, slackField{"Node", data.NodeName, true})
	}
	if data.Detail != "" {
		fields = append(fields, slackField{"Detail", data.Detail, false})
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
	title := eventTitle(event)
	if title == "" {
		title = event
	}
	color := eventDiscordColor(event)
	var desc string
	switch {
	case data.ServiceName != "" && data.ProjectName != "":
		desc = fmt.Sprintf("**%s** (%s) in **%s**", data.ServiceName, data.origin(), data.ProjectName)
	case data.NodeName != "":
		desc = fmt.Sprintf("**%s**", data.NodeName)
	case data.ServiceName != "":
		desc = fmt.Sprintf("**%s**", data.ServiceName)
	case data.Detail != "":
		desc = data.Detail
	}
	if data.Detail != "" && data.ServiceName != "" {
		desc += "\n" + data.Detail
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
	title := eventTitle(event)
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
		fmt.Fprintf(&msg, "\r\nService: %s (%s)", data.ServiceName, data.origin())
	}
	if data.ProjectName != "" {
		fmt.Fprintf(&msg, "\r\nProject: %s", data.ProjectName)
	}
	if data.Detail != "" {
		fmt.Fprintf(&msg, "\r\n%s", data.Detail)
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
