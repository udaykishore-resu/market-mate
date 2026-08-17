package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockID is the advisory lock every replica takes before migrating.
//
// Deployments roll several replicas at once and they all boot with
// MM_MIGRATE=true. Without the lock they race: two transactions can both see an
// empty schema_migrations, both run 0001, and the second fails on a duplicate
// object — turning a routine deploy into a crash loop. The constant is
// arbitrary but must stay stable; it is derived from "marketmate".
const migrationLockID int64 = 7_412_965_331_057

// migrationTimeout bounds the whole apply, including waiting for the lock.
const migrationTimeout = 60 * time.Second

type migration struct {
	name string
	sql  string
}

// Migrate applies every embedded migration that has not run yet.
//
// Idempotent and safe to run concurrently: the advisory lock serialises
// replicas, and each file's application and its bookkeeping row commit in the
// same transaction, so a migration is never recorded as applied unless it was.
func (s *Store) Migrate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
	defer cancel()

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection for migration: %w", err)
	}
	defer conn.Release()

	// Session-level lock, released explicitly below: a transaction-level lock
	// would be released at the end of the first migration's transaction and let
	// another replica in halfway through the set.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("taking migration lock: %w", err)
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
			log.Printf("storage: releasing migration lock: %v", err)
		}
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied, err := appliedMigrations(ctx, conn.Conn())
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.name] {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("applying %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (name) VALUES ($1) ON CONFLICT DO NOTHING`, m.name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("recording %s: %w", m.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing %s: %w", m.name, err)
		}
		log.Printf("storage: applied migration %s", m.name)
	}
	return nil
}

func appliedMigrations(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

// loadMigrations reads the embedded files in lexical order, which is why they
// are numbered.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		out = append(out, migration{name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}
