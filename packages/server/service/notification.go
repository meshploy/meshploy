package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type NotificationService struct {
	db *gorm.DB
	// domains gives the console's address on the primary domain, which is where
	// a link in a notification should go: FRONTEND_URL is written once at
	// install and keeps naming the old primary after it moves.
	domains *DomainService
	// consoleURL is the console's public base URL (FRONTEND_URL), for links in
	// a notification. Empty leaves the links out.
	consoleURL string
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
	if err != nil {
		return nil, err
	}
	return rows, s.withStatus(ctx, rows)
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
		if err := validRecipient(cfg["address"]); err != nil {
			return err
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
	ServiceName string `json:"service,omitempty"`
	ProjectName string `json:"project,omitempty"`
	NodeName    string `json:"node,omitempty"`
	// StackName names the stack a service belongs to, so an alert says what
	// the thing is without a second lookup. Empty for a standalone service,
	// which the senders render as such.
	StackName string `json:"stack,omitempty"`
	// Detail is one line of context the event carries: who joined, what was
	// created. Used where a service and project name say nothing.
	Detail string `json:"detail,omitempty"`
	// Link is the console path that shows what happened, such as the failed
	// deployment. Kept relative, so a retry links to the console as it is
	// addressed now.
	Link string `json:"link,omitempty"`
	// Error is the end of the failure's own output: the last lines of a build
	// or job log, or the reason a step gave up. Empty for a success.
	Error string `json:"error,omitempty"`
}

// failureTail keeps the last lines of a log, which is where a build or a job
// says why it stopped, with secrets filtered out, and bounds the size of an
// alert. known is the values the workload is configured with.
func failureTail(log string, known []string) string {
	const window, maxLines, maxBytes = 200, 8, 1200
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	// Filter a wider window than is kept, so a secret spanning lines (a key)
	// is recognised whole before the cut.
	if len(lines) > window {
		lines = lines[len(lines)-window:]
	}
	lines = strings.Split(redactSecrets(strings.Join(lines, "\n"), known), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(out) > maxBytes {
		out = "…" + out[len(out)-maxBytes:]
	}
	return out
}

// ForService builds the payload for an event about a service, naming the stack
// it belongs to. An alert that says only "redis" leaves the reader asking which
// redis, and the answer is one join away.
func (s *NotificationService) ForService(ctx context.Context, svc *meshdb.Service, projectName string) NotificationData {
	data := NotificationData{
		ServiceName: svc.Name,
		ProjectName: projectName,
		Link:        fmt.Sprintf("/projects/%s/services/%s/overview", svc.ProjectID, svc.ID),
	}
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
			emailCfg = s.emailConfig(ctx, orgID)
			emailCfgLoaded = true
		}

		s.attempt(ctx, ch, event, data, emailCfg, false, nil)
	}
}

// ─── Delivery log ─────────────────────────────────────────────────────────────

// TestEvent is what "Send test" sends. It is not in the catalogue, so nothing
// can subscribe to it.
const TestEvent = "notification.test"

const (
	deliveryRetention = 30 * 24 * time.Hour
	maxDeliveryError  = 2000
	// streakWindow bounds how far back a failing streak is counted.
	streakWindow = 50
)

func (s *NotificationService) emailConfig(ctx context.Context, orgID uuid.UUID) *meshdb.OrgEmailConfig {
	var cfg meshdb.OrgEmailConfig
	if s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&cfg).Error != nil {
		return nil
	}
	return &cfg
}

// attempt sends one event to one channel and records how it went. The record
// is written even when the caller's request has ended, since a dispatch often
// outlives the request that caused it.
func (s *NotificationService) attempt(ctx context.Context, ch meshdb.NotificationChannel, event string, data NotificationData, emailCfg *meshdb.OrgEmailConfig, test bool, retryOf *uuid.UUID) *meshdb.NotificationDelivery {
	err := sendNotification(ch, s.notice(ch.Name, event, data), emailCfg)
	row := meshdb.NotificationDelivery{
		OrganizationID: ch.OrganizationID,
		ChannelID:      ch.ID,
		Event:          event,
		Data:           data.record(),
		Success:        err == nil,
		Test:           test,
		RetryOf:        retryOf,
	}
	if err != nil {
		row.Error = deliveryError(err)
		log.Printf("notification %q (%s): %s", ch.Name, ch.Type, row.Error)
	}
	conn := s.db.WithContext(context.WithoutCancel(ctx))
	if err := conn.Create(&row).Error; err != nil {
		log.Printf("notification %q: record delivery: %v", ch.Name, err)
	}
	return &row
}

// StartDeliveryReaper deletes attempts older than the retention, hourly.
// Postgres has no row TTL, and pruning only when a channel sends would keep a
// quiet channel's old attempts forever.
func (s *NotificationService) StartDeliveryReaper(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		s.pruneDeliveries(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *NotificationService) pruneDeliveries(ctx context.Context) {
	res := s.db.WithContext(ctx).
		Where("created_at < ?", time.Now().Add(-deliveryRetention)).
		Delete(&meshdb.NotificationDelivery{})
	if res.Error != nil {
		log.Printf("notification deliveries: prune: %v", res.Error)
	}
}

// deliveryError is the error as stored and shown. An HTTP client error quotes
// the URL it failed on, and a Slack or Discord webhook URL is the credential
// itself, so the URL is left out.
func deliveryError(err error) string {
	var ue *url.Error
	msg := err.Error()
	if errors.As(err, &ue) {
		msg = fmt.Sprintf("%s: %v", ue.Op, ue.Err)
	}
	if len(msg) > maxDeliveryError {
		msg = msg[:maxDeliveryError]
	}
	return msg
}

func (d NotificationData) record() meshdb.JSONObject {
	out := meshdb.JSONObject{}
	b, _ := json.Marshal(d)
	_ = json.Unmarshal(b, &out)
	return out
}

func dataFromRecord(o meshdb.JSONObject) NotificationData {
	var d NotificationData
	b, _ := json.Marshal(o)
	_ = json.Unmarshal(b, &d)
	return d
}

// withStatus fills in each channel's latest attempt and failing streak.
func (s *NotificationService) withStatus(ctx context.Context, channels []meshdb.NotificationChannel) error {
	for i := range channels {
		var recent []meshdb.NotificationDelivery
		if err := s.db.WithContext(ctx).
			Where("channel_id = ?", channels[i].ID).
			Order("created_at desc").
			Limit(streakWindow).
			Find(&recent).Error; err != nil {
			return err
		}
		if len(recent) == 0 {
			continue
		}
		channels[i].LastDelivery = &recent[0]
		for _, d := range recent {
			if d.Success {
				break
			}
			channels[i].FailingStreak++
		}
	}
	return nil
}

func (s *NotificationService) channel(ctx context.Context, orgID, id uuid.UUID) (*meshdb.NotificationChannel, error) {
	var ch meshdb.NotificationChannel
	if err := s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		First(&ch).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

var testData = NotificationData{
	Detail: "Sent from the Meshploy console to check this channel works.",
	Link:   "/integrations/notifications",
}

// Test sends a test notification to a channel, paused or not, and records it.
func (s *NotificationService) Test(ctx context.Context, orgID, id uuid.UUID) (*meshdb.NotificationDelivery, error) {
	ch, err := s.channel(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	var emailCfg *meshdb.OrgEmailConfig
	if ch.Type == meshdb.NotificationEmail {
		emailCfg = s.emailConfig(ctx, orgID)
	}
	return s.attempt(ctx, *ch, TestEvent, testData, emailCfg, true, nil), nil
}

// TestEmailProvider sends a test email through the org's provider to one
// address. Nothing is recorded: there is no channel to record it against.
func (s *NotificationService) TestEmailProvider(ctx context.Context, orgID uuid.UUID, to string) error {
	if err := validRecipient(to); err != nil {
		return err
	}
	cfg := s.emailConfig(ctx, orgID)
	if cfg == nil {
		return fmt.Errorf("no email provider configured for this org")
	}
	ch := meshdb.NotificationChannel{Type: meshdb.NotificationEmail, Config: meshdb.JSONObject{"address": to}}
	data := testData
	data.Link = "/integrations/email"
	if err := sendEmail(ch, s.notice("", TestEvent, data), *cfg); err != nil {
		return errors.New(deliveryError(err))
	}
	return nil
}

// Deliveries lists a channel's attempts, newest first. A non-zero before pages
// back: only attempts older than it.
func (s *NotificationService) Deliveries(ctx context.Context, orgID, channelID uuid.UUID, failedOnly bool, before time.Time, limit int) ([]meshdb.NotificationDelivery, error) {
	if _, err := s.channel(ctx, orgID, channelID); err != nil {
		return nil, err
	}
	rows := make([]meshdb.NotificationDelivery, 0)
	q := s.db.WithContext(ctx).Where("channel_id = ?", channelID)
	if failedOnly {
		q = q.Where("success = false")
	}
	if !before.IsZero() {
		q = q.Where("created_at < ?", before)
	}
	err := q.Order("created_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// Retry sends a recorded attempt's event to its channel again, with the data
// it carried then, and records the new attempt.
func (s *NotificationService) Retry(ctx context.Context, orgID, deliveryID uuid.UUID) (*meshdb.NotificationDelivery, error) {
	var d meshdb.NotificationDelivery
	if err := s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", deliveryID, orgID).
		First(&d).Error; err != nil {
		return nil, err
	}
	ch, err := s.channel(ctx, orgID, d.ChannelID)
	if err != nil {
		return nil, err
	}
	var emailCfg *meshdb.OrgEmailConfig
	if ch.Type == meshdb.NotificationEmail {
		emailCfg = s.emailConfig(ctx, orgID)
	}
	return s.attempt(ctx, *ch, d.Event, dataFromRecord(d.Data), emailCfg, d.Test, &d.ID), nil
}

// validRecipient refuses anything but one plain address. It is written into
// the To header, so a line break would let it add headers of its own.
func validRecipient(addr string) error {
	if strings.ContainsAny(addr, "\r\n") {
		return fmt.Errorf("invalid email address")
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil || parsed.Address != addr {
		return fmt.Errorf("invalid email address %q", addr)
	}
	return nil
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
	{"backup.skipped", "Backup", "Backups not running", "A schedule came round for a database that is stopped, so there was nothing to back up. Sent once, not every night.", ToneWarning, true},
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
	if event == TestEvent {
		return "Test notification"
	}
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

// notice is one event rendered for sending: the same title, colour, link and
// facts whichever channel it goes to.
type notice struct {
	Event       string
	Title       string
	Tone        EventTone
	Data        NotificationData
	Link        string // absolute; empty when the console's URL is unknown
	Console     string // the console's host, for the footer
	ConsoleBase string // the console's base URL
	Channel     string // the channel's name; empty for a provider test
	At          time.Time
}

type fact struct{ Label, Value string }

// consoleBase is where a notification's links point: the console on the primary
// domain, or FRONTEND_URL on a gateway with no domain rows.
func (s *NotificationService) consoleBase() string {
	if s.domains != nil {
		if u := s.domains.GatewayPlatformURL(context.Background(), "console"); u != "" {
			return u
		}
	}
	return strings.TrimRight(s.consoleURL, "/")
}

func (s *NotificationService) notice(channel, event string, data NotificationData) notice {
	n := notice{Event: event, Title: eventTitle(event), Data: data, Channel: channel, At: time.Now()}
	if d, ok := eventDef(event); ok {
		n.Tone = d.Tone
	}
	if base := s.consoleBase(); base != "" {
		n.ConsoleBase = base
		if data.Link != "" {
			n.Link = base + data.Link
		}
		if u, err := url.Parse(base); err == nil {
			n.Console = u.Host
		}
	}
	return n
}

// facts are what the alert is about, in reading order.
func (n notice) facts() []fact {
	var out []fact
	if d := n.Data; d.ServiceName != "" {
		name := d.ServiceName
		if o := d.origin(); o != "" {
			name += " (" + o + ")"
		}
		label := "Service"
		if strings.HasPrefix(n.Event, "job.") {
			label, name = "Job", d.ServiceName
		}
		out = append(out, fact{label, name})
	}
	if n.Data.ProjectName != "" {
		out = append(out, fact{"Project", n.Data.ProjectName})
	}
	if n.Data.NodeName != "" {
		out = append(out, fact{"Node", n.Data.NodeName})
	}
	out = append(out, fact{"When", n.At.UTC().Format("2 Jan 2006, 15:04 UTC")})
	return out
}

func (n notice) colourHex() string {
	if c, ok := toneHex[n.Tone]; ok {
		return c
	}
	return "#64748b"
}

func (n notice) colourInt() int {
	if c, ok := toneInt[n.Tone]; ok {
		return c
	}
	return 0x64748b
}

func sendNotification(ch meshdb.NotificationChannel, n notice, emailCfg *meshdb.OrgEmailConfig) error {
	switch ch.Type {
	case meshdb.NotificationWebhook:
		return sendWebhook(ch, n)
	case meshdb.NotificationSlack:
		return sendSlack(ch, n)
	case meshdb.NotificationDiscord:
		return sendDiscord(ch, n)
	case meshdb.NotificationEmail:
		if emailCfg == nil {
			return fmt.Errorf("no SMTP provider configured for this org")
		}
		return sendEmail(ch, n, *emailCfg)
	default:
		return nil
	}
}

// ── Webhook ───────────────────────────────────────────────────────────────────

func sendWebhook(ch meshdb.NotificationChannel, n notice) error {
	target, _ := ch.Config["url"].(string)
	if target == "" {
		return fmt.Errorf("missing config.url")
	}
	body, err := json.Marshal(map[string]any{
		"event":     n.Event,
		"title":     n.Title,
		"timestamp": n.At.UTC().Format(time.RFC3339),
		"url":       n.Link,
		"data": map[string]string{
			"service": n.Data.ServiceName,
			"project": n.Data.ProjectName,
			"stack":   n.Data.StackName,
			"node":    n.Data.NodeName,
			"detail":  n.Data.Detail,
			"error":   n.Data.Error,
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret, _ := ch.Config["secret"].(string); secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Meshploy-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := notifyHTTP.Do(req)
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
	Color     string       `json:"color"`
	Title     string       `json:"title,omitempty"`
	TitleLink string       `json:"title_link,omitempty"`
	Text      string       `json:"text,omitempty"`
	Fields    []slackField `json:"fields,omitempty"`
	Footer    string       `json:"footer"`
	Ts        int64        `json:"ts"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

func footer(n notice) string {
	if n.Console != "" {
		return "Meshploy · " + n.Console
	}
	return "Meshploy"
}

func sendSlack(ch meshdb.NotificationChannel, n notice) error {
	webhookURL, _ := ch.Config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing config.webhook_url")
	}
	var fields []slackField
	for _, f := range n.facts() {
		fields = append(fields, slackField{f.Label, f.Value, true})
	}
	var text []string
	if n.Data.Detail != "" {
		text = append(text, n.Data.Detail)
	}
	if n.Data.Error != "" {
		text = append(text, "```"+n.Data.Error+"```")
	}
	att := slackAttachment{
		Color:  n.colourHex(),
		Text:   strings.Join(text, "\n"),
		Fields: fields,
		Footer: footer(n),
		Ts:     n.At.Unix(),
	}
	if n.Link != "" {
		att.Title, att.TitleLink = "View in console", n.Link
	}
	return postJSON(webhookURL, slackPayload{Text: n.Title, Attachments: []slackAttachment{att}})
}

// ── Discord ───────────────────────────────────────────────────────────────────

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	URL         string         `json:"url,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      discordFooter  `json:"footer"`
	Timestamp   string         `json:"timestamp"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordFooter struct {
	Text string `json:"text"`
}

func sendDiscord(ch meshdb.NotificationChannel, n notice) error {
	webhookURL, _ := ch.Config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing config.webhook_url")
	}
	var fields []discordField
	for _, f := range n.facts() {
		if f.Label == "When" {
			continue // the embed's timestamp shows it, in the reader's zone
		}
		fields = append(fields, discordField{f.Label, f.Value, true})
	}
	var desc []string
	if n.Data.Detail != "" {
		desc = append(desc, n.Data.Detail)
	}
	if n.Data.Error != "" {
		desc = append(desc, "```\n"+n.Data.Error+"\n```")
	}
	return postJSON(webhookURL, discordPayload{
		Embeds: []discordEmbed{{
			Title:       n.Title,
			URL:         n.Link,
			Description: strings.Join(desc, "\n"),
			Color:       n.colourInt(),
			Fields:      fields,
			Footer:      discordFooter{footer(n)},
			Timestamp:   n.At.UTC().Format(time.RFC3339),
		}},
	})
}

// ── Email ─────────────────────────────────────────────────────────────────────

func sendEmail(ch meshdb.NotificationChannel, n notice, cfg meshdb.OrgEmailConfig) error {
	to, _ := ch.Config["address"].(string)
	if to == "" {
		return fmt.Errorf("missing config.address")
	}
	msg, err := buildEmail(n, cfg, to)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	auth := smtp.PlainAuth("", cfg.Username, string(cfg.Password), cfg.Host)

	tlsCfg := &tls.Config{ServerName: cfg.Host}
	dialer := &net.Dialer{Timeout: 15 * time.Second}

	// Ports 465 and 2465 speak TLS from the first byte. Every other port (587,
	// 2525, 25) starts in plain text and upgrades with STARTTLS whenever the
	// server offers it; UseTLS makes that upgrade required.
	implicitTLS := cfg.Port == 465 || cfg.Port == 2465
	var conn net.Conn
	if implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Minute))
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else if cfg.UseTLS {
			return fmt.Errorf("%s does not offer STARTTLS", addr)
		}
	}
	if cfg.Username != "" {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(cfg.FromAddress); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	// Close is where the server accepts or rejects the message.
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return client.Quit()
}

// ─── Shared ───────────────────────────────────────────────────────────────────

// notifyHTTP bounds a webhook call, so a receiver that never answers cannot
// hold a dispatch, or a "Send test" request, open indefinitely.
var notifyHTTP = &http.Client{Timeout: 15 * time.Second}

func postJSON(url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := notifyHTTP.Post(url, "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
