package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
)

func checkTargetDatabase(ctx context.Context, pool *pgxpool.Pool) error {
	target := os.Getenv("BIFROST_EXPECT_DATABASE")
	if target == "" {
		return nil
	} // Isolated synthetic test runners supply their own database.
	var actual string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil {
		return errors.New("cannot connect to configured PostgreSQL database")
	}
	if actual != target {
		return errors.New("connected database differs from BIFROST_EXPECT_DATABASE")
	}
	return nil
}
