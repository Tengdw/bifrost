package current

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"bifrost/db/migrations"
	"bifrost/internal/binance"
	"bifrost/internal/holdings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStartupSkipsAlreadyObservedAccountInSameSlot(t *testing.T) {
	url := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	batch, account := uuid(), uuid()
	slot := time.Date(2023, 5, 10, 14, 0, 0, 0, beijing)
	at := slot.Add(time.Minute)
	if _, err := pool.Exec(ctx, "INSERT INTO bifrost.current_batches(id,requested_at,accounts) VALUES($1,$2,to_jsonb($3::text[]))", batch, at, []string{account}); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM bifrost.current_batches WHERE id=$1", batch)
	defer pool.Exec(ctx, "DELETE FROM bifrost.current_runs WHERE batch_id=$1", batch)
	for _, wallet := range holdings.WalletNames {
		if _, err := pool.Exec(ctx, "INSERT INTO bifrost.current_runs(batch_id,account_id,wallet,state,observed_at,quantity_complete) VALUES($1,$2,$3,'partial',$4,true)", batch, account, wallet, at); err != nil {
			t.Fatal(err)
		}
	}
	// Context cancellation ensures that a regression can be examined without emitting any HTTP traffic.
	serviceCtx, cancel := context.WithCancel(ctx)
	cancel()
	svc := NewService(serviceCtx, pool, func(context.Context, string, string) (binance.Result, error) {
		return binance.Result{}, errors.New("unexpected collection")
	})
	svc.AllAccounts = func(context.Context) ([]string, error) { return []string{account}, nil }
	if err := svc.collectPeriod(ctx, slot, at); err != nil {
		t.Fatal(err)
	}
	var batches int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM bifrost.current_batches WHERE accounts ? $1", account).Scan(&batches); err != nil {
		t.Fatal(err)
	}
	if batches != 1 {
		t.Fatalf("a restart created %d batches for an already-observed account", batches)
	}
}
