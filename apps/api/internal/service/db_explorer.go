package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	db "github.com/meshploy/packages/db"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type DBExplorerService struct {
	db *gorm.DB
}

// QueryResult holds the tabular result of a DB query.
type QueryResult struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Count   int             `json:"count"`
}

// SchemaTable describes one table / collection.
type SchemaTable struct {
	Name    string         `json:"name"`
	Columns []SchemaColumn `json:"columns"`
}

type SchemaColumn struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
}

// loadConfig fetches the DatabaseConfig and returns the host:port to dial.
// The API runs outside the K8s cluster so it connects via NodePort over the
// Tailscale mesh using any online node's IP.
func (s *DBExplorerService) loadConfig(ctx context.Context, serviceID uuid.UUID) (*db.DatabaseConfig, string, error) {
	var dc db.DatabaseConfig
	if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&dc).Error; err != nil {
		return nil, "", fmt.Errorf("database config not found")
	}
	if dc.Slug == "" {
		return nil, "", fmt.Errorf("database not provisioned yet")
	}
	if dc.NodePort == 0 {
		return nil, "", fmt.Errorf("NodePort not yet assigned — wait for provisioning to complete")
	}

	// Pick any online node reachable over the mesh.
	var node db.Node
	if err := s.db.WithContext(ctx).
		Where("status = ? AND k8s_member = ?", "online", true).
		First(&node).Error; err != nil {
		return nil, "", fmt.Errorf("no online cluster node found")
	}

	addr := fmt.Sprintf("%s:%d", node.TailscaleIP, dc.NodePort)
	return &dc, addr, nil
}

// ─── Schema ───────────────────────────────────────────────────────────────────

func (s *DBExplorerService) Schema(ctx context.Context, serviceID uuid.UUID) ([]SchemaTable, error) {
	dc, host, err := s.loadConfig(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	switch dc.Engine {
	case "postgres":
		return s.pgSchema(ctx, dc, host)
	case "mysql":
		return s.mysqlSchema(ctx, dc, host)
	case "redis":
		return s.redisSchema(ctx, dc, host)
	case "mongodb":
		return nil, fmt.Errorf("mongodb schema introspection not yet supported")
	default:
		return nil, fmt.Errorf("unsupported engine: %s", dc.Engine)
	}
}

func (s *DBExplorerService) pgSchema(ctx context.Context, dc *db.DatabaseConfig, addr string) ([]SchemaTable, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		dc.DBUser, string(dc.DBPassword), addr, dc.DBName)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT c.table_name, c.column_name, c.data_type,
		       CASE c.is_nullable WHEN 'YES' THEN true ELSE false END
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_name = c.table_name AND t.table_schema = c.table_schema
		WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_name, c.ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("query schema: %w", err)
	}
	defer rows.Close()
	return collectSchema(rows)
}

func (s *DBExplorerService) mysqlSchema(ctx context.Context, dc *db.DatabaseConfig, addr string) ([]SchemaTable, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s",
		dc.DBUser, string(dc.DBPassword), addr, dc.DBName)
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer sqlDB.Close()

	rows, err := sqlDB.QueryContext(ctx, `
		SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE,
		       CASE IS_NULLABLE WHEN 'YES' THEN 1 ELSE 0 END
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME, ORDINAL_POSITION`, dc.DBName)
	if err != nil {
		return nil, fmt.Errorf("query schema: %w", err)
	}
	defer rows.Close()

	tableMap := map[string]*SchemaTable{}
	var order []string
	for rows.Next() {
		var tbl, col, dtype string
		var nullable int
		if err := rows.Scan(&tbl, &col, &dtype, &nullable); err != nil {
			continue
		}
		if _, ok := tableMap[tbl]; !ok {
			tableMap[tbl] = &SchemaTable{Name: tbl}
			order = append(order, tbl)
		}
		tableMap[tbl].Columns = append(tableMap[tbl].Columns, SchemaColumn{
			Name: col, DataType: dtype, Nullable: nullable == 1,
		})
	}
	result := make([]SchemaTable, 0, len(order))
	for _, name := range order {
		result = append(result, *tableMap[name])
	}
	return result, nil
}

func (s *DBExplorerService) redisSchema(ctx context.Context, dc *db.DatabaseConfig, addr string) ([]SchemaTable, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: string(dc.DBPassword),
	})
	defer rdb.Close()

	var keys []string
	var cursor uint64
	for {
		batch, next, err := rdb.Scan(ctx, cursor, "*", 50).Result()
		if err != nil {
			return nil, fmt.Errorf("scan keys: %w", err)
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 || len(keys) >= 200 {
			break
		}
	}

	// Group by prefix (first segment before ":" separator).
	groups := map[string][]string{}
	var order []string
	for _, k := range keys {
		prefix := k
		if i := strings.Index(k, ":"); i > 0 {
			prefix = k[:i] + ":*"
		}
		if _, ok := groups[prefix]; !ok {
			order = append(order, prefix)
		}
		groups[prefix] = append(groups[prefix], k)
	}

	tables := make([]SchemaTable, 0, len(order))
	for _, prefix := range order {
		t := SchemaTable{Name: prefix}
		for _, k := range groups[prefix] {
			t.Columns = append(t.Columns, SchemaColumn{Name: k, DataType: "key"})
		}
		tables = append(tables, t)
	}
	return tables, nil
}

// helper: collect pgx rows into schema tables
func collectSchema(rows pgx.Rows) ([]SchemaTable, error) {
	tableMap := map[string]*SchemaTable{}
	var order []string
	for rows.Next() {
		var tbl, col, dtype string
		var nullable bool
		if err := rows.Scan(&tbl, &col, &dtype, &nullable); err != nil {
			continue
		}
		if _, ok := tableMap[tbl]; !ok {
			tableMap[tbl] = &SchemaTable{Name: tbl}
			order = append(order, tbl)
		}
		tableMap[tbl].Columns = append(tableMap[tbl].Columns, SchemaColumn{
			Name: col, DataType: dtype, Nullable: nullable,
		})
	}
	result := make([]SchemaTable, 0, len(order))
	for _, name := range order {
		result = append(result, *tableMap[name])
	}
	return result, nil
}

// ─── Query ────────────────────────────────────────────────────────────────────

const maxRows = 200

func (s *DBExplorerService) Query(ctx context.Context, serviceID uuid.UUID, query string, readOnly bool) (*QueryResult, error) {
	dc, host, err := s.loadConfig(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	switch dc.Engine {
	case "postgres":
		return s.pgQuery(ctx, dc, host, query, readOnly)
	case "mysql":
		return s.mysqlQuery(ctx, dc, host, query, readOnly)
	case "redis":
		return s.redisQuery(ctx, dc, host, query)
	case "mongodb":
		return nil, fmt.Errorf("mongodb query not yet supported")
	default:
		return nil, fmt.Errorf("unsupported engine: %s", dc.Engine)
	}
}

func (s *DBExplorerService) pgQuery(ctx context.Context, dc *db.DatabaseConfig, addr, query string, readOnly bool) (*QueryResult, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		dc.DBUser, string(dc.DBPassword), addr, dc.DBName)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Always rollback in read-only mode; commit for write mode.
		if readOnly {
			tx.Rollback(ctx)
		}
	}()

	rows, err := tx.Query(ctx, query)
	if err != nil {
		tx.Rollback(ctx)
		return nil, fmt.Errorf("%w", err)
	}
	defer rows.Close()

	result, err := pgxRowsToResult(rows)
	if err != nil {
		return nil, err
	}

	if !readOnly {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
	}
	return result, nil
}

func (s *DBExplorerService) mysqlQuery(ctx context.Context, dc *db.DatabaseConfig, addr, query string, readOnly bool) (*QueryResult, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s",
		dc.DBUser, string(dc.DBPassword), addr, dc.DBName)
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer sqlDB.Close()

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if readOnly {
			tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("%w", err)
	}
	defer rows.Close()

	result, err := sqlRowsToResult(rows)
	if err != nil {
		return nil, err
	}

	if !readOnly {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
	}
	return result, nil
}

func (s *DBExplorerService) redisQuery(ctx context.Context, dc *db.DatabaseConfig, addr, command string) (*QueryResult, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: string(dc.DBPassword),
	})
	defer rdb.Close()

	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	args := make([]interface{}, len(parts))
	for i, p := range parts {
		args[i] = p
	}

	val, err := rdb.Do(ctx, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	return &QueryResult{
		Columns: []string{"result"},
		Rows:    [][]interface{}{{fmt.Sprintf("%v", val)}},
		Count:   1,
	}, nil
}

// ─── Row collectors ───────────────────────────────────────────────────────────

func pgxRowsToResult(rows pgx.Rows) (*QueryResult, error) {
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = string(f.Name)
	}
	var allRows [][]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		row := make([]interface{}, len(vals))
		for i, v := range vals {
			row[i] = fmt.Sprintf("%v", v)
		}
		allRows = append(allRows, row)
		if len(allRows) >= maxRows {
			break
		}
	}
	return &QueryResult{Columns: cols, Rows: allRows, Count: len(allRows)}, nil
}

func sqlRowsToResult(rows *sql.Rows) (*QueryResult, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var allRows [][]interface{}
	for rows.Next() {
		scanArgs := make([]interface{}, len(cols))
		vals := make([]interface{}, len(cols))
		for i := range vals {
			scanArgs[i] = &vals[i]
		}
		if err := rows.Scan(scanArgs...); err != nil {
			continue
		}
		row := make([]interface{}, len(cols))
		for i, v := range vals {
			row[i] = fmt.Sprintf("%v", v)
		}
		allRows = append(allRows, row)
		if len(allRows) >= maxRows {
			break
		}
	}
	return &QueryResult{Columns: cols, Rows: allRows, Count: len(allRows)}, nil
}
