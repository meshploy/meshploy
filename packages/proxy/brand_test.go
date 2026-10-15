package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// The edge's own pages are signed with the brand of the organisation the
// route belongs to, escaped, and with Meshploy's when there is none.
func TestTheEdgesPagesCarryTheBrand(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	if got := (&Handler{}).brandName(ctx, "app.acme.test", org); got != "Meshploy" {
		t.Errorf("without a database: %q", got)
	}

	db.RegisterBrandSource(func(_ context.Context, _ *gorm.DB, id uuid.UUID) (db.Brand, bool) {
		return db.Brand{Name: "Acme <Cloud>"}, id == org
	})
	t.Cleanup(func() { db.RegisterBrandSource(nil) })

	h := &Handler{db: &gorm.DB{}}
	name := h.brandName(ctx, "app.acme.test", org)
	if name != "Acme &lt;Cloud&gt;" {
		t.Fatalf("brand = %q", name)
	}
	for _, page := range []string{notFoundPage, notSharedPage} {
		out := fmt.Sprintf(page, name, "app.acme.test")
		if strings.Contains(out, "%!") || strings.Contains(out, "Meshploy") || !strings.Contains(out, name) || !strings.Contains(out, "app.acme.test") {
			t.Errorf("page renders wrong:\n%s", out)
		}
	}
}
