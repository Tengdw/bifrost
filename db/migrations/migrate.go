package migrations

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 001_snapshot.sql
var initial string

// Apply 仅供显式初始化隔离演示库；服务启动不迁移数据库。
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, "select to_regclass('bifrost.batches') is not null").Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		_, err := tx.Exec(ctx, initial)
		return err
	})
}
