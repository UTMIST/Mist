package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations
var embedded embed.FS

// Owners lists migration directories in apply order. Later owners may
// reference earlier owners' tables, never the reverse.
var Owners = []string{"identity", "storage", "scheduler", "executor"}

var migrationName = regexp.MustCompile(`^([0-9]{4})_[a-z0-9_]+\.sql$`)

type migration struct {
	owner    string
	version  int
	name     string
	sql      string
	checksum string
}

// Migrate applies the embedded migrations of every owner.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return err
	}
	return migrateFS(ctx, pool, sub, Owners)
}

// migrateFS applies <owner>/NNNN_name.sql files from fsys. Each file runs in
// its own transaction with its bookkeeping row. Applied files are immutable:
// a changed checksum is an error, not a reason to re-run.
func migrateFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, owners []string) error {
	var all []migration
	for _, owner := range owners {
		ms, err := loadOwner(fsys, owner)
		if err != nil {
			return err
		}
		all = append(all, ms...)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	defer conn.Release()

	// A session lock on this connection serializes concurrent mistd starts.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", LockMigrate); err != nil {
		return fmt.Errorf("db: migrate lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", LockMigrate)

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			owner      text        NOT NULL,
			version    integer     NOT NULL,
			name       text        NOT NULL,
			checksum   text        NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (owner, version)
		)`); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}

	applied := map[[2]string]string{}
	rows, err := conn.Query(ctx, "SELECT owner, version, checksum FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	for rows.Next() {
		var owner, sum string
		var version int
		if err := rows.Scan(&owner, &version, &sum); err != nil {
			return fmt.Errorf("db: migrate: %w", err)
		}
		applied[[2]string{owner, strconv.Itoa(version)}] = sum
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}

	for _, m := range all {
		key := [2]string{m.owner, strconv.Itoa(m.version)}
		if sum, ok := applied[key]; ok {
			if sum != m.checksum {
				return fmt.Errorf("db: migration %s/%s changed after it was applied", m.owner, m.name)
			}
			continue
		}
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return err
			}
			_, err := tx.Exec(ctx,
				"INSERT INTO schema_migrations (owner, version, name, checksum) VALUES ($1, $2, $3, $4)",
				m.owner, m.version, m.name, m.checksum)
			return err
		})
		if err != nil {
			return fmt.Errorf("db: migration %s/%s: %w", m.owner, m.name, err)
		}
	}
	return nil
}

func loadOwner(fsys fs.FS, owner string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, owner)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // owner has no tables yet
	}
	if err != nil {
		return nil, fmt.Errorf("db: read migrations/%s: %w", owner, err)
	}
	var ms []migration
	for _, e := range entries {
		match := migrationName.FindStringSubmatch(e.Name())
		if e.IsDir() || match == nil {
			return nil, fmt.Errorf("db: unexpected file migrations/%s/%s", owner, e.Name())
		}
		version, _ := strconv.Atoi(match[1])
		if n := len(ms); n > 0 && ms[n-1].version == version {
			return nil, fmt.Errorf("db: duplicate migration version %s/%04d", owner, version)
		}
		body, err := fs.ReadFile(fsys, path.Join(owner, e.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		ms = append(ms, migration{
			owner:    owner,
			version:  version,
			name:     e.Name(),
			sql:      string(body),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	// ReadDir sorts by name, and names start with the zero-padded version.
	if !slices.IsSortedFunc(ms, func(a, b migration) int { return a.version - b.version }) {
		return nil, fmt.Errorf("db: migrations/%s are not ordered", owner)
	}
	return ms, nil
}
