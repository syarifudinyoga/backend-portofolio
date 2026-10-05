package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

const advisoryLockID int64 = 731946208124

type migration struct {
	version  int64
	name     string
	checksum string
	sql      string
}

func Apply(ctx context.Context, db *pgxpool.Pool) (resultErr error) {
	pending, err := loadMigrations()
	if err != nil {
		return err
	}

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire database connection for migrations: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		return fmt.Errorf("lock database migrations: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", advisoryLockID); err != nil {
			resultErr = fmt.Errorf("unlock database migrations: %w (migration result: %v)", err, resultErr)
		}
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create migration history table: %w", err)
	}

	rows, err := conn.Query(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	applied := make(map[int64]migration)
	for rows.Next() {
		var item migration
		if err := rows.Scan(&item.version, &item.name, &item.checksum); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration history: %w", err)
		}
		applied[item.version] = item
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate migration history: %w", err)
	}
	rows.Close()

	available := make(map[int64]migration, len(pending))
	for _, item := range pending {
		available[item.version] = item
	}
	for version, item := range applied {
		current, exists := available[version]
		if !exists {
			return fmt.Errorf("migration %03d (%s) is recorded in the database but missing from this build", version, item.name)
		}
		if item.name != current.name || item.checksum != current.checksum {
			return fmt.Errorf("migration %03d (%s) differs from the version already applied; add a new migration instead of editing it", version, item.name)
		}
	}

	for _, item := range pending {
		if _, exists := applied[item.version]; exists {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %03d (%s): %w", item.version, item.name, err)
		}
		if _, err := tx.Exec(ctx, item.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %03d (%s): %w", item.version, item.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
			item.version, item.name, item.checksum); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %03d (%s): %w", item.version, item.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %03d (%s): %w", item.version, item.name, err)
		}
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Strings(names)

	result := make([]migration, 0, len(names))
	versions := make(map[int64]string, len(names))
	for _, name := range names {
		version, err := parseVersion(name)
		if err != nil {
			return nil, err
		}
		if previous, exists := versions[version]; exists {
			return nil, fmt.Errorf("migrations %q and %q use the same version %d", previous, name, version)
		}
		content, err := files.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(content))
		result = append(result, migration{
			version:  version,
			name:     path.Base(name),
			checksum: checksum,
			sql:      string(content),
		})
		versions[version] = name
	}
	return result, nil
}

func parseVersion(name string) (int64, error) {
	base := path.Base(name)
	prefix, _, found := strings.Cut(base, "_")
	if !found || len(prefix) < 3 {
		return 0, fmt.Errorf("migration filename %q must start with a three-digit version and underscore", base)
	}
	for _, char := range prefix {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("migration filename %q has a non-numeric version", base)
		}
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("migration filename %q has an invalid version", base)
	}
	if !strings.HasSuffix(base, ".sql") || len(base) == len(prefix)+1+len(".sql") {
		return 0, fmt.Errorf("migration filename %q must have a name and .sql extension", base)
	}
	return version, nil
}
