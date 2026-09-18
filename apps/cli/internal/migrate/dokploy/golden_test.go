package dokploy

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Golden plans.
//
// A server's reading is a Source; what an operator decides from is the Plan
// built out of it. These fixtures pin that translation: each testdata/*.source
// .json is read, planned, and compared against the plan recorded beside it. A
// change in what Meshploy promises about somebody's server then shows up as a
// diff to approve rather than as a surprise on a live migration.
//
// The fixtures are written by hand to cover shapes the four reference servers
// have between them - they are not captured dumps, and they carry no data from
// anyone's server. Real inventories, scrubbed, are still worth adding.
//
// Regenerate after an intended change:
//
//	go test ./internal/migrate/dokploy/ -run Golden -update
//
// then read the diff before committing it.
var update = flag.Bool("update", false, "rewrite the recorded plans from the fixtures")

// plannedAt is fixed so a recorded plan does not change by being read again.
var plannedAt = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

func TestGoldenPlans(t *testing.T) {
	sources, err := filepath.Glob("testdata/*.source.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no fixtures in testdata")
	}

	for _, path := range sources {
		name := strings.TrimSuffix(filepath.Base(path), ".source.json")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var src Source
			// Unknown fields are a typo in a fixture, not something to read
			// past: a misspelled key would silently plan an emptier server.
			dec := json.NewDecoder(strings.NewReader(string(raw)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&src); err != nil {
				t.Fatalf("%s: %v", path, err)
			}

			got, err := json.MarshalIndent(BuildPlan(src, plannedAt), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')

			goldenPath := filepath.Join("testdata", name+".plan.json")
			if *update {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", goldenPath)
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("%v (run the test with -update to record it)", err)
			}
			if string(got) != string(want) {
				t.Errorf("the plan for %s changed; run -update and read the diff.\n%s", name, firstDifference(string(want), string(got)))
			}
		})
	}
}

// firstDifference points at the line that changed, since the whole plan is far
// too long to read in a test failure.
func firstDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return "line " + itoa(i+1) + ":\n  recorded: " + strings.TrimSpace(w) + "\n  now:      " + strings.TrimSpace(g)
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
