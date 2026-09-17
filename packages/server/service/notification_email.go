package service

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"strings"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
)

//go:embed templates/notification.html
var notificationHTML string

// Inline styles and tables only: they are what Gmail and Outlook render the
// same way. No images, so nothing is fetched when the mail is opened.
var notificationTemplate = template.Must(template.New("notification").Parse(notificationHTML))

// summary is the one line under the title: what the event is about.
func (n notice) summary() string {
	if n.Data.Detail != "" {
		return n.Data.Detail
	}
	d, _ := eventDef(n.Event)
	return d.Description
}

// buildEmail renders a notice as a multipart message: plain text for clients
// that want it, HTML for the rest.
func buildEmail(n notice, cfg meshdb.OrgEmailConfig, to string) ([]byte, error) {
	from := cfg.FromAddress
	if cfg.FromName != "" {
		from = mime.QEncoding.Encode("utf-8", cfg.FromName) + " <" + cfg.FromAddress + ">"
	}
	subject := "[Meshploy] " + n.Title
	if n.Data.ServiceName != "" {
		subject += ": " + n.Data.ServiceName
	} else if n.Data.NodeName != "" {
		subject += ": " + n.Data.NodeName
	}

	manageURL := ""
	if n.ConsoleBase != "" && n.Channel != "" {
		manageURL = n.ConsoleBase + "/integrations/notifications"
	}

	var html bytes.Buffer
	if err := notificationTemplate.Execute(&html, map[string]any{
		"Title":      n.Title,
		"Summary":    n.summary(),
		"Colour":     n.colourHex(),
		"Facts":      n.facts(),
		"Error":      n.Data.Error,
		"Link":       n.Link,
		"Console":    n.Console,
		"ConsoleURL": n.ConsoleBase,
		"Channel":    n.Channel,
		"ManageURL":  manageURL,
	}); err != nil {
		return nil, err
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%s\n", n.Title)
	if s := n.summary(); s != "" {
		fmt.Fprintf(&text, "%s\n", s)
	}
	text.WriteString("\n")
	for _, f := range n.facts() {
		fmt.Fprintf(&text, "%-9s %s\n", f.Label, f.Value)
	}
	if n.Data.Error != "" {
		fmt.Fprintf(&text, "\nWhat it said:\n%s\n", n.Data.Error)
	}
	if n.Link != "" {
		fmt.Fprintf(&text, "\nView in console: %s\n", n.Link)
	}
	text.WriteString("\n--\nSent by Meshploy")
	if n.Console != "" {
		text.WriteString(" at " + n.Console)
	}
	if n.Channel != "" {
		fmt.Fprintf(&text, " to the %q channel", n.Channel)
	}
	text.WriteString(".\n")

	domain := "meshploy.local"
	if at := strings.LastIndex(cfg.FromAddress, "@"); at >= 0 {
		domain = cfg.FromAddress[at+1:]
	}
	boundary := "meshploy-" + strings.ReplaceAll(uuid.NewString(), "-", "")

	var msg bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&msg, "%s: %s\r\n", k, v) }
	header("From", from)
	header("To", to)
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", n.At.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	header("Message-ID", "<"+uuid.NewString()+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	msg.WriteString("\r\n")

	for _, part := range []struct{ kind, body string }{
		{"text/plain", text.String()},
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
