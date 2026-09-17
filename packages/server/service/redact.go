package service

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// An alert quotes the end of a build or job log, and leaves the organization:
// an inbox, a Slack workspace, a webhook receiver. A log that echoed a secret
// would carry it there, so the quote is filtered twice before it goes. First
// for the values this workload is actually given, which catches any secret
// however it is shaped; then for credential patterns, which catches what came
// from anywhere else.

const redacted = "[redacted]"

// minKnownSecret is the shortest configured value replaced wherever it appears.
// Shorter ones ("true", "3000", "prod") would blank ordinary words in the log,
// and are rarely the secret.
const minKnownSecret = 8

var secretPatterns = []struct {
	re   *regexp.Regexp
	with string
}{
	// A private key, whole or with its start cut off by the tail.
	{regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`), "[redacted private key]"},
	{regexp.MustCompile(`\A[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----`), "[redacted private key]"},
	// Credentials in a URL: https://user:token@host, postgres://u:p@db.
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`), "${1}" + redacted + "@"},
	// Authorization headers and bearer tokens.
	{regexp.MustCompile(`(?i)(authorization:\s*(?:basic|bearer|token)?\s*|bearer\s+)[A-Za-z0-9._~+/=-]{8,}`), "${1}" + redacted},
	// NAME=value and NAME: value where the name says what it is.
	{regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credentials?)[A-Za-z0-9_.-]*["']?\s*[=:]\s*["']?)[^\s"',;]{4,}`), "${1}" + redacted},
	// Tokens that announce their issuer.
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|glpat-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|sk-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|(?:mreg|mprov|magt)-[0-9a-f]{16,})\b`), redacted},
	// JSON Web Tokens.
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`), redacted},
}

// redactSecrets replaces the known values, longest first so one value that
// contains another is removed whole, then the credential patterns.
func redactSecrets(text string, known []string) string {
	var values []string
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if len(v) >= minKnownSecret && !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	}
	for _, v := range known {
		add(v)
		// A multi-line value (a PEM key, a JSON credential) is logged a line
		// at a time as often as whole.
		if strings.Contains(v, "\n") {
			for _, line := range strings.Split(v, "\n") {
				if len(strings.TrimSpace(line)) >= 16 {
					add(line)
				}
			}
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, v := range values {
		text = strings.ReplaceAll(text, v, redacted)
	}
	for _, p := range secretPatterns {
		text = p.re.ReplaceAllString(text, p.with)
	}
	return text
}

// envValues reads the values out of a KEY=VALUE block.
func envValues(block string) []string {
	var out []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			v := strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
			out = append(out, v, strings.ReplaceAll(v, `\n`, "\n"))
		}
	}
	return out
}

func groupValues(groups []db.VariableGroup) []string {
	var out []string
	for _, g := range groups {
		for _, item := range g.Items {
			out = append(out, string(item.Value))
		}
	}
	return out
}

// serviceSecrets is every value a service is configured with: its project's
// and its own environment, build environment and arguments, attached and
// system groups, and a database's password. Lookup errors leave values out,
// never the alert; the patterns still apply.
func (s *NotificationService) serviceSecrets(ctx context.Context, serviceID uuid.UUID) []string {
	conn := s.db.WithContext(ctx)
	var svc db.Service
	if conn.Preload("Project").First(&svc, "id = ?", serviceID).Error != nil {
		return nil
	}
	out := append(envValues(string(svc.Project.EnvVars)), envValues(string(svc.EnvVars))...)

	var bc db.BuildConfig
	if conn.First(&bc, "service_id = ?", serviceID).Error == nil {
		out = append(out, envValues(string(bc.BuildEnvVars))...)
		for _, v := range bc.BuildArgs {
			out = append(out, v)
		}
	}
	var dbc db.DatabaseConfig
	if conn.First(&dbc, "service_id = ?", serviceID).Error == nil {
		out = append(out, string(dbc.DBPassword))
	}

	groups := &VariableGroupService{db: s.db}
	if attached, err := groups.ListForService(ctx, serviceID); err == nil {
		out = append(out, groupValues(attached)...)
	}
	var system []db.VariableGroup
	if conn.Preload("Items").Where("service_id = ?", serviceID).Find(&system).Error == nil {
		out = append(out, groupValues(system)...)
	}
	return out
}

// jobSecrets is every value a job is configured with.
func (s *NotificationService) jobSecrets(ctx context.Context, job *db.Job) []string {
	out := envValues(string(job.EnvVars))
	var project db.Project
	if s.db.WithContext(ctx).Select("env_vars").First(&project, "id = ?", job.ProjectID).Error == nil {
		out = append(out, envValues(string(project.EnvVars))...)
	}
	if attached, err := (&VariableGroupService{db: s.db}).ListForJob(ctx, job.ID); err == nil {
		out = append(out, groupValues(attached)...)
	}
	return out
}
