package service

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/meshploy/packages/db"
)

// Each engine's default is a version the console offers and recommends, so a
// database made through the API and one made in the console start alike.
func TestDefaultDBVersionIsTheConsolesRecommendation(t *testing.T) {
	src, err := os.ReadFile("../../../apps/web/src/lib/db-versions.ts")
	if err != nil {
		t.Skipf("console source not here: %v", err)
	}
	text := string(src)
	for _, e := range []db.DatabaseEngine{db.DatabasePostgres, db.DatabaseMySQL, db.DatabaseRedis, db.DatabaseMongoDB, db.DatabaseDragonfly, db.DatabaseClickHouse} {
		block := regexp.MustCompile(`(?s)\n  ` + string(e) + `: \{(.*?)\n  \},`).FindStringSubmatch(text)
		if block == nil {
			t.Errorf("%s: not in db-versions.ts", e)
			continue
		}
		v := defaultDBVersion(e)
		if !strings.Contains(block[1], `recommended: "`+v+`"`) {
			t.Errorf("%s: the API defaults to %q, which the console does not recommend", e, v)
		}
		if !strings.Contains(block[1], `{ tag: "`+v+`"`) {
			t.Errorf("%s: the API defaults to %q, which the console does not offer", e, v)
		}
	}
}
