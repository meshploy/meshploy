// Package help is Meshploy's explanation of itself: what each feature is and
// how it behaves, written once and read in three places. The console bundles
// the topics for its help drawer, tooltips and empty states; the docs site
// publishes them as Concepts; and the MCP server can serve them to a
// connected assistant. One text, so the three never disagree.
//
// A topic is a Markdown file in topics/ with a header:
//
//	---
//	id: environments
//	title: Projects & environments
//	summary: One sentence for search results and link previews.
//	pages: [project-overview]
//	---
//
// then sections, each a level-two heading with a stable id the console links
// to, and at the end a "## Terms {#terms}" section whose entries are the
// glossary behind the console's tooltips:
//
//	## Promotion groups {#groups}
//	...
//	## Terms {#terms}
//	- **Entry level** {#entry-level -> groups}: The lowest level on a group's path...
//
// The header is a deliberately small subset of YAML (one-line values and
// one-line lists), parsed here without a dependency.
package help

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

//go:embed topics/*.md
var files embed.FS

// Topic is one explanation.
type Topic struct {
	ID       string
	Title    string
	Summary  string
	Pages    []string
	Sections []Section
	Terms    []Term
	// Body is the Markdown after the header, as written.
	Body string
}

// Section is one level-two heading and what follows it, up to the next.
type Section struct {
	ID    string
	Title string
	Body  string
}

// Term is a word the console explains in place: a short text, and the
// section a reader goes to for the rest.
type Term struct {
	ID      string
	Label   string
	Text    string
	Section string
}

var (
	headingRe = regexp.MustCompile(`^## (.+?) \{#([a-z0-9-]+)\}\s*$`)
	termRe    = regexp.MustCompile(`^- \*\*(.+?)\*\* \{#([a-z0-9-]+) -> ([a-z0-9-]+)\}: (.+)$`)
)

// Topics returns every topic, by id.
func Topics() ([]Topic, error) {
	names, err := fs.Glob(files, "topics/*.md")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Topic, 0, len(names))
	for _, name := range names {
		raw, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		t, err := Parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// Get returns the topic with id.
func Get(id string) (Topic, bool) {
	topics, err := Topics()
	if err != nil {
		return Topic{}, false
	}
	for _, t := range topics {
		if t.ID == id {
			return t, true
		}
	}
	return Topic{}, false
}

// Parse reads one topic file.
func Parse(raw string) (Topic, error) {
	var t Topic
	rest, ok := strings.CutPrefix(raw, "---\n")
	if !ok {
		return t, fmt.Errorf("no header: a topic starts with ---")
	}
	header, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return t, fmt.Errorf("header is not closed with ---")
	}
	for _, line := range strings.Split(header, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "id":
			t.ID = value
		case "title":
			t.Title = value
		case "summary":
			t.Summary = value
		case "pages":
			for _, p := range strings.Split(strings.Trim(value, "[]"), ",") {
				if p = strings.TrimSpace(p); p != "" {
					t.Pages = append(t.Pages, p)
				}
			}
		}
	}
	if t.ID == "" || t.Title == "" {
		return t, fmt.Errorf("header needs an id and a title")
	}
	t.Body = strings.TrimLeft(body, "\n")

	var cur *Section
	for _, line := range strings.Split(t.Body, "\n") {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			t.Sections = append(t.Sections, Section{ID: m[2], Title: m[1]})
			cur = &t.Sections[len(t.Sections)-1]
			continue
		}
		if strings.HasPrefix(line, "## ") {
			return t, fmt.Errorf("heading %q has no {#id}", line)
		}
		if cur == nil {
			continue
		}
		if cur.ID == "terms" {
			if m := termRe.FindStringSubmatch(line); m != nil {
				t.Terms = append(t.Terms, Term{ID: m[2], Label: m[1], Section: m[3], Text: m[4]})
			}
			continue
		}
		cur.Body += line + "\n"
	}
	for i := range t.Sections {
		t.Sections[i].Body = strings.TrimSpace(t.Sections[i].Body)
	}
	return t, nil
}
