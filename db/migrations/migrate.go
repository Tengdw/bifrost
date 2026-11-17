package migrations

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

// Apply upgrades an empty database or the original ticket-01 schema atomically.
// Callers must explicitly select the database; serving HTTP never runs migrations.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock(1578283631)"); err != nil {
			return err
		}
		var initial bool
		if err := tx.QueryRow(ctx, "select to_regclass('bifrost.batches') is not null").Scan(&initial); err != nil {
			return err
		}
		entries, err := files.ReadDir(".")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".sql") {
				continue
			}
			if name != "001_snapshot.sql" {
				var applied bool
				if err := tx.QueryRow(ctx, "select exists(select 1 from bifrost.schema_migrations where name=$1)", name).Scan(&applied); err != nil {
					return err
				}
				if applied {
					continue
				}
			}
			if name != "001_snapshot.sql" || !initial {
				sql, err := files.ReadFile(name)
				if err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, string(sql)); err != nil {
					return fmt.Errorf("migration %s: %w", name, err)
				}
			}
			if name == "001_snapshot.sql" {
				if _, err = tx.Exec(ctx, "create table if not exists bifrost.schema_migrations(name text primary key, applied_at timestamptz not null default now())"); err != nil {
					return err
				}
			}
			if _, err = tx.Exec(ctx, "insert into bifrost.schema_migrations(name) values ($1) on conflict do nothing", name); err != nil {
				return err
			}
		}
		return nil
	})
}
