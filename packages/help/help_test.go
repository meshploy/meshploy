package help

import "testing"

// Every topic parses, its ids are unique, and every term points at a section
// that exists: a tooltip's "Learn more" never lands nowhere.
func TestTopicsHoldTogether(t *testing.T) {
	topics, err := Topics()
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) == 0 {
		t.Fatal("no topics")
	}
	seen := map[string]bool{}
	for _, topic := range topics {
		if seen[topic.ID] {
			t.Errorf("topic id %q used twice", topic.ID)
		}
		seen[topic.ID] = true
		if len(topic.Pages) == 0 {
			t.Errorf("%s: names no console page", topic.ID)
		}
		sections := map[string]bool{}
		for _, s := range topic.Sections {
			if sections[s.ID] {
				t.Errorf("%s: section id %q used twice", topic.ID, s.ID)
			}
			sections[s.ID] = true
		}
		terms := map[string]bool{}
		for _, term := range topic.Terms {
			if terms[term.ID] {
				t.Errorf("%s: term id %q used twice", topic.ID, term.ID)
			}
			terms[term.ID] = true
			if !sections[term.Section] {
				t.Errorf("%s: term %q points at section %q, which does not exist", topic.ID, term.ID, term.Section)
			}
		}
	}
}

// The em-dash is not used in Meshploy's writing; a hyphen spaced out, a comma
// or a colon reads the same.
func TestTopicsUseNoEmDash(t *testing.T) {
	topics, err := Topics()
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range topics {
		for _, s := range []string{topic.Title, topic.Summary, topic.Body} {
			for i, r := range s {
				if r == '—' {
					t.Errorf("%s: em-dash at byte %d", topic.ID, i)
					break
				}
			}
		}
	}
}

// The environments topic is read whole: its sections, and its terms with the
// text the console's tooltips show.
func TestEnvironmentsTopicIsReadWhole(t *testing.T) {
	topic, ok := Get("environments")
	if !ok {
		t.Fatal("no environments topic")
	}
	if len(topic.Sections) < 10 {
		t.Errorf("%d sections, want the whole topic", len(topic.Sections))
	}
	var entry *Term
	for i := range topic.Terms {
		if topic.Terms[i].ID == "entry-level" {
			entry = &topic.Terms[i]
		}
	}
	if entry == nil || entry.Label != "Entry level" || entry.Section != "groups" || entry.Text == "" {
		t.Errorf("entry-level term read as %+v", entry)
	}
}
