package service

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
)

//go:embed templates/email.html
var emailHTML string

// Inline styles and tables only: they are what Gmail and Outlook render the
// same way. No images, so nothing is fetched when the mail is opened.
var emailTemplate = template.Must(template.New("email").Parse(emailHTML))

// The look every email shares unless a brand or a tone says otherwise: the
// console's emerald, with dark text on it as its buttons have.
const (
	emailColour     = "#10b981"
	emailButtonText = "#04231a"
)

// Email is what one message says. Its look is one for every email the
// platform sends: a notification, a sign-in link, an app shared.
type Email struct {
	Subject string
	Heading string
	Body    string
	// Facts and Error are what a notification adds: what it is about, and
	// what the thing itself said.
	Facts []fact
	Error string
	// Button and Link are the one thing to do.
	Button string
	Link   string
	// Note is said under the button, as how long the link works. Paste adds
	// the link written out, for a client that strips buttons: for a link
	// the email exists to carry.
	Note  string
	Paste bool
	// After is the small print of this message.
	After string

	// Filled in by the sender, from the brand. Colour is the bar across the
	// top, where a notification's tone replaces it.
	Brand        string
	Colour       string
	ButtonColour string
	ButtonText   string
	Footer       string
	FooterURL    string
	FooterLink   string
	ManageURL    string
}

// wear puts a brand on an email: its name, its colours, and the footer it
// signs with.
func (e *Email) wear(b meshdb.Brand) {
	e.Brand = b.Name
	if e.Colour == "" {
		e.Colour = emailColour
	}
	e.ButtonColour, e.ButtonText = emailColour, emailButtonText
	if b.Accent != "" {
		if e.Colour == emailColour {
			e.Colour = b.Accent
		}
		e.ButtonColour = b.Accent
		e.ButtonText = "#ffffff"
		if b.AccentForeground != "" {
			e.ButtonText = b.AccentForeground
		}
	}
	if e.Footer == "" {
		switch {
		case !b.Custom:
			e.Footer, e.FooterURL, e.FooterLink = "Powered by Meshploy", "https://meshploy.com", "meshploy.com"
		case b.PoweredBy:
			e.Footer, e.FooterURL, e.FooterLink = "Sent by "+b.Name+". Powered by Meshploy", "https://meshploy.com", "meshploy.com"
		default:
			e.Footer = "Sent by " + b.Name + "."
		}
	}
}

// text is the plain part: the same message, for clients that want it.
func (e *Email) text() string {
	var t strings.Builder
	t.WriteString(e.Heading + "\n")
	if e.Body != "" {
		t.WriteString(e.Body + "\n")
	}
	if len(e.Facts) > 0 {
		t.WriteString("\n")
		for _, f := range e.Facts {
			fmt.Fprintf(&t, "%-9s %s\n", f.Label, f.Value)
		}
	}
	if e.Error != "" {
		fmt.Fprintf(&t, "\nWhat it said:\n%s\n", e.Error)
	}
	if e.Link != "" {
		fmt.Fprintf(&t, "\n%s: %s\n", e.Button, e.Link)
	}
	if e.Note != "" {
		t.WriteString("\n" + e.Note + "\n")
	}
	if e.After != "" {
		t.WriteString("\n" + e.After + "\n")
	}
	t.WriteString("\n--\n" + e.Footer)
	if e.FooterURL != "" {
		t.WriteString(" - " + e.FooterURL)
	}
	t.WriteString("\n")
	return t.String()
}

// message renders an email as a multipart message: plain text for clients
// that want it, HTML for the rest.
func (e *Email) message(cfg meshdb.OrgEmailConfig, to string, at time.Time) ([]byte, error) {
	var html bytes.Buffer
	if err := emailTemplate.Execute(&html, e); err != nil {
		return nil, err
	}
	from := cfg.FromAddress
	if cfg.FromName != "" {
		from = mime.QEncoding.Encode("utf-8", cfg.FromName) + " <" + cfg.FromAddress + ">"
	}
	domain := "meshploy.local"
	if i := strings.LastIndex(cfg.FromAddress, "@"); i >= 0 {
		domain = cfg.FromAddress[i+1:]
	}
	boundary := "meshploy-" + strings.ReplaceAll(uuid.NewString(), "-", "")

	var msg bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&msg, "%s: %s\r\n", k, v) }
	header("From", from)
	header("To", to)
	header("Subject", mime.QEncoding.Encode("utf-8", e.Subject))
	header("Date", at.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	header("Message-ID", "<"+uuid.NewString()+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	msg.WriteString("\r\n")
	for _, part := range []struct{ kind, body string }{
		{"text/plain", e.text()},
		{"text/html", html.String()},
	} {
		fmt.Fprintf(&msg, "--%s\r\n", boundary)
		fmt.Fprintf(&msg, "Content-Type: %s; charset=utf-8\r\n", part.kind)
		msg.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&msg)
		if _, err := qp.Write([]byte(strings.ReplaceAll(part.body, "\n", "\r\n"))); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
		msg.WriteString("\r\n")
	}
	fmt.Fprintf(&msg, "--%s--\r\n", boundary)
	return msg.Bytes(), nil
}

// Send mails an email with the org's SMTP settings, in the org's brand, as
// notifications do, for an extension to reach people.
func (s *EmailConfigService) Send(ctx context.Context, orgID uuid.UUID, to string, e Email) error {
	cfg, err := s.Get(ctx, orgID)
	if err != nil || cfg == nil || cfg.Host == "" {
		return ErrNoEmailConfig
	}
	e.wear(meshdb.BrandOf(ctx, s.db, orgID))
	msg, err := e.message(*cfg, to, time.Now())
	if err != nil {
		return err
	}
	return deliverEmail(*cfg, to, msg)
}
