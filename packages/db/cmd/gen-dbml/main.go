package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	db "github.com/meshploy/packages/db"
	"gorm.io/gorm/schema"
)

// models must stay in sync with the AutoMigrate list in db.go.
var models = []any{
	&db.User{},
	&db.Organization{},
	&db.OrganizationMember{},
	&db.ResourcePermission{},
	&db.Project{},
	&db.Node{},
	&db.Service{},
	&db.BuildConfig{},
	&db.DatabaseConfig{},
	&db.Route{},
	&db.Deployment{},
	&db.StorageIntegration{},
	&db.RegistryIntegration{},
	&db.BackupConfig{},
	&db.NotificationChannel{},
	&db.Template{},
}

func main() {
	out := flag.String("o", "../../meshploy_schema.dbml", "output file path")
	flag.Parse()

	cacheStore := &sync.Map{}
	namer := schema.NamingStrategy{}

	var sb strings.Builder
	sb.WriteString("// Meshploy CE — DBML Schema\n")
	sb.WriteString("// Auto-generated — run `go generate ./packages/db/...` to regenerate\n")
	sb.WriteString("// DO NOT EDIT MANUALLY\n\n")

	for _, m := range models {
		s, err := schema.Parse(m, cacheStore, namer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %T: %v\n", m, err)
			os.Exit(1)
		}
		writeTable(&sb, s)
	}

	// Partial unique indexes that GORM cannot express via struct tags alone.
	// These mirror applyConstraints() in db.go.
	sb.WriteString("// ── Manual constraints (partial unique indexes) ──────────────────────────\n")
	sb.WriteString("// The following cannot be derived from struct tags; they are applied by\n")
	sb.WriteString("// applyConstraints() in db.go at startup.\n")
	sb.WriteString("//\n")
	sb.WriteString("// CREATE UNIQUE INDEX idx_one_owner_per_org\n")
	sb.WriteString("//   ON organization_members (organization_id)\n")
	sb.WriteString("//   WHERE role = 'owner' AND deleted_at IS NULL;\n")

	if err := os.WriteFile(*out, []byte(sb.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *out)
}

// ── Table ────────────────────────────────────────────────────────────────────

func writeTable(sb *strings.Builder, s *schema.Schema) {
	// Build FK ref map from BelongsTo relationships so we can annotate columns.
	fkRefs := map[string]string{}
	for _, rel := range s.Relationships.BelongsTo {
		for _, ref := range rel.References {
			if ref.ForeignKey != nil && ref.PrimaryKey != nil {
				fkRefs[ref.ForeignKey.DBName] = fmt.Sprintf(
					`"public"."%s"."%s"`,
					ref.PrimaryKey.Schema.Table, ref.PrimaryKey.DBName,
				)
			}
		}
	}

	fmt.Fprintf(sb, "Table \"public\".\"%s\" {\n", s.Table)

	for _, f := range s.Fields {
		if f.DBName == "" || f.IgnoreMigration {
			continue
		}
		writeField(sb, f, fkRefs)
	}

	if lines := buildIndexLines(s); len(lines) > 0 {
		sb.WriteString("\n  Indexes {\n")
		for _, l := range lines {
			fmt.Fprintf(sb, "    %s\n", l)
		}
		sb.WriteString("  }\n")
	}

	sb.WriteString("}\n\n")
}

// ── Field ────────────────────────────────────────────────────────────────────

func writeField(sb *strings.Builder, f *schema.Field, fkRefs map[string]string) {
	typ := resolveType(f)
	attrs := buildAttrs(f, fkRefs)
	col := `"` + f.DBName + `"`
	if attrs != "" {
		fmt.Fprintf(sb, "  %-24s %-15s [%s]\n", col, typ, attrs)
	} else {
		fmt.Fprintf(sb, "  %-24s %s\n", col, typ)
	}
}

func resolveType(f *schema.Field) string {
	// Explicit gorm:"type:..." tag takes priority.
	if t := strings.ToLower(string(f.DataType)); t != "" {
		switch {
		case strings.HasPrefix(t, "uuid"):
			return "uuid"
		case t == "text":
			return "text"
		case strings.HasPrefix(t, "jsonb"):
			return "jsonb"
		case strings.HasPrefix(t, "varchar"):
			return t
		case t == "timestamptz", t == "timestamp with time zone":
			return "timestamptz"
		// GORM v1.31 sets DataType to Go primitive names — map them to Postgres types.
		case t == "string":
			return "varchar(500)"
		case t == "time", t == "datetime":
			return "timestamptz"
		case t == "float", t == "float32", t == "float64":
			return "real"
		case t == "bool":
			return "boolean"
		case t == "int", t == "int8", t == "int16", t == "int32", t == "int64",
			t == "uint", t == "uint8", t == "uint16", t == "uint32", t == "uint64":
			return "int"
		default:
			return t
		}
	}
	// Well-known column names that need explicit types.
	switch f.DBName {
	case "deleted_at", "created_at", "updated_at",
		"last_seen_at", "last_built_at", "last_backup_at", "deployed_at":
		return "timestamptz"
	}
	// Derive from GORM's abstract data type.
	switch f.GORMDataType {
	case schema.String:
		return "varchar(500)"
	case schema.Int:
		return "int"
	case schema.Uint:
		return "int"
	case schema.Float:
		return "real"
	case schema.Bool:
		return "boolean"
	case schema.Time:
		return "timestamptz"
	case schema.Bytes:
		return "bytea"
	}
	return "varchar(500)"
}

func buildAttrs(f *schema.Field, fkRefs map[string]string) string {
	var parts []string

	if f.PrimaryKey {
		parts = append(parts, "pk")
	}
	if !f.PrimaryKey && f.Unique {
		parts = append(parts, "unique")
	}
	if f.NotNull || f.PrimaryKey {
		parts = append(parts, "not null")
	}
	if dv := resolveDefault(f); dv != "" {
		parts = append(parts, "default: "+dv)
	}
	if ref, ok := fkRefs[f.DBName]; ok {
		parts = append(parts, "ref: > "+ref)
	}

	return strings.Join(parts, ", ")
}

func resolveDefault(f *schema.Field) string {
	// GORM auto-manages these at application level; show DB defaults for clarity.
	if f.AutoCreateTime > 0 || f.AutoUpdateTime > 0 {
		return "`NOW()`"
	}
	if !f.HasDefaultValue || f.DefaultValue == "" {
		return ""
	}
	dv := f.DefaultValue
	// Function call → backtick notation
	if strings.ContainsAny(dv, "()") {
		return fmt.Sprintf("`%s`", strings.ToUpper(dv))
	}
	if dv == "true" {
		return "TRUE"
	}
	if dv == "false" {
		return "FALSE"
	}
	// Already single-quoted by GORM tag (e.g. default:"'main'") — return as-is
	if len(dv) >= 2 && dv[0] == '\'' && dv[len(dv)-1] == '\'' {
		return dv
	}
	// Pure number (no quotes needed)
	isNum := true
	for _, c := range dv {
		if c != '-' && c != '.' && (c < '0' || c > '9') {
			isNum = false
			break
		}
	}
	if isNum {
		return dv
	}
	return fmt.Sprintf("'%s'", dv)
}

// ── Indexes ──────────────────────────────────────────────────────────────────

func buildIndexLines(s *schema.Schema) []string {
	indexes := s.ParseIndexes()

	// Sort for deterministic output
	sort.Slice(indexes, func(i, j int) bool {
		return indexes[i].Name < indexes[j].Name
	})

	var lines []string
	for _, idx := range indexes {
		cols := make([]string, 0, len(idx.Fields))
		for _, opt := range idx.Fields {
			if opt.Field != nil && opt.DBName != "" {
				cols = append(cols, `"`+opt.DBName+`"`)
			}
		}
		if len(cols) == 0 {
			continue
		}

		var col string
		if len(cols) == 1 {
			col = cols[0]
		} else {
			col = "(" + strings.Join(cols, ", ") + ")"
		}

		var attrs []string
		if strings.EqualFold(idx.Class, "UNIQUE") {
			attrs = append(attrs, "unique")
		}
		attrs = append(attrs, fmt.Sprintf(`name: "%s"`, idx.Name))

		lines = append(lines, fmt.Sprintf("%s [%s]", col, strings.Join(attrs, ", ")))
	}

	return lines
}
