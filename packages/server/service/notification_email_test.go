package service

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"strings"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
)

func sampleNotice() notice {
	s := &NotificationService{consoleURL: "https://console.example.com/"}
	n := s.notice("Ops", "deploy.failed", NotificationData{
		ServiceName: "api<script>",
		ProjectName: "Storefront",
		StackName:   "shop",
		Link:        "/projects/p/services/s/deployments/d",
		Error:       failureTail("step 1\nstep 2\nnpm ERR! missing script: build", nil),
	})
	n.At = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	return n
}

// The email is what someone reads at 3am: it has to say what broke, where,
// and link to it, in both parts, with nothing from the event able to become
// markup.
func TestEmailCarriesTheFactsTheLinkAndTheError(t *testing.T) {
	cfg := meshdb.OrgEmailConfig{FromAddress: "alerts@example.com", FromName: "Meshploy"}
	raw, err := buildEmail(sampleNotice(), cfg, "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "[Meshploy] Deployment failed: api<script>" {
		t.Errorf("subject = %q", subject)
	}
	if msg.Header.Get("Date") == "" || msg.Header.Get("Message-ID") == "" {
		t.Error("missing Date or Message-ID, which spam filters count against a message")
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	r := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p) // NextPart decodes quoted-printable
		kind, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		parts[kind] = string(b)
	}
	link := "https://console.example.com/projects/p/services/s/deployments/d"
	for kind, body := range parts {
		for _, want := range []string{"Storefront", "stack shop", "npm ERR! missing script: build", link, "console.example.com"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s part is missing %q", kind, want)
			}
		}
	}
	if len(parts) != 2 {
		t.Fatalf("got parts %v, want text/plain and text/html", len(parts))
	}
	if strings.Contains(parts["text/html"], "<script>") {
		t.Error("a service name became markup in the HTML part")
	}
	if strings.Contains(parts["text/plain"], "step 1") == false {
		t.Error("short error lost its first lines")
	}
	if p := os.Getenv("MESHPLOY_EMAIL_PREVIEW"); p != "" {
		_ = os.WriteFile(p, []byte(parts["text/html"]), 0o644)
	}
}

// Without a console URL there is nothing to link to, and no half-built link.
func TestNoticeWithoutConsoleURLHasNoLink(t *testing.T) {
	n := (&NotificationService{}).notice("Ops", "node.offline", NotificationData{NodeName: "w1", Link: "/nodes/x"})
	if n.Link != "" || n.Console != "" {
		t.Errorf("link %q console %q, want both empty", n.Link, n.Console)
	}
}

func TestFailureTailKeepsTheEnd(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "line")
	}
	lines = append(lines, "the actual error")
	got := failureTail(strings.Join(lines, "\n"), nil)
	if !strings.HasSuffix(got, "the actual error") || strings.Count(got, "\n") != 7 {
		t.Errorf("tail = %q", got)
	}
}
