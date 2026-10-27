package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		sep := strings.Index(name, "_")
		if sep < 0 {
			continue
		}
		v, err := strconv.Atoi(name[:sep])
		if err != nil {
			continue
		}
		data, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: name, sql: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// Migrate applies any pending migrations/*.up.sql files, in order, each in its
// own transaction. It is safe to run on every boot and from several instances
// at once (a Postgres advisory lock serialises them).
//
// A database created by hand from 000001 before this runner existed is
// detected (the projects table exists but schema_migrations doesn't) and
// baselined at version 1 instead of failing on "relation already exists".
func Migrate(ctx context.Context, pool *sql.DB, logger *slog.Logger) error {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Close()

	const lockID = 727274
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		return fmt.Errorf("taking migration lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID)

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	var applied int
	if err := conn.QueryRowContext(ctx, `SELECT COALESCE(max(version), 0) FROM schema_migrations`).Scan(&applied); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	if applied == 0 {
		var existing sql.NullString
		if err := conn.QueryRowContext(ctx, `SELECT to_regclass('public.projects')::text`).Scan(&existing); err != nil {
			return fmt.Errorf("checking existing schema: %w", err)
		}
		if existing.Valid {
			logger.Info("existing schema detected, baselining at version 1")
			if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (1)`); err != nil {
				return fmt.Errorf("baselining: %w", err)
			}
			applied = 1
		}
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("loading migrations: %w", err)
	}

	for _, m := range migrations {
		if m.version <= applied {
			continue
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("applying %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		logger.Info("applied migration", "migration", m.name)
	}
	return nil
}
