package current

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"bifrost/db/migrations"
	"bifrost/internal/holdings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHistoryWithoutAccountsReturnsUnknown(t *testing.T) {
	svc := NewService(context.Background(), nil, nil)
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/overview", nil))
	assertHistoryJSON(t, rec, `"totalUsd":null`, `"asOf":null`, `"allocation":[]`, `"quality":"unavailable"`)
}

func TestDayCloseHasFixedBoundaryAndNoCashFlowInferences(t *testing.T) {
	url := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	account := uuid()
	if _, err := pool.Exec(ctx, "INSERT INTO bifrost.accounts(id,exchange_uid,alias,enabled_from) VALUES($1,$2,'synthetic','2023-05-01')", account, "history-test-"+account); err != nil {
		t.Fatal(err)
	}
	insert := func(at time.Time, boundary *time.Time, usd int, lateWallet string) {
		t.Helper()
		batch := uuid()
		t.Cleanup(func() {
			for _, q := range []string{
				"DELETE FROM bifrost.current_items WHERE batch_id=$1",
				"DELETE FROM bifrost.current_runs WHERE batch_id=$1",
				"DELETE FROM bifrost.current_batches WHERE id=$1",
			} {
				if _, err := pool.Exec(ctx, q, batch); err != nil {
					t.Error(err)
				}
			}
		})
		if _, err := pool.Exec(ctx, "INSERT INTO bifrost.current_batches(id,requested_at,accounts,day_close_at) VALUES($1,$2,to_jsonb($3::text[]),$4)", batch, at, []string{account}, boundary); err != nil {
			t.Fatal(err)
		}
		for _, wallet := range holdings.WalletNames {
			observed := at.Add(12 * time.Second)
			if wallet == lateWallet {
				observed = at.Add(8 * time.Minute)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO bifrost.current_runs(batch_id,account_id,wallet,state,observed_at,quantity_complete) VALUES($1,$2,$3,'succeeded',$4,true)", batch, account, wallet, observed); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, "INSERT INTO bifrost.current_items(batch_id,account_id,wallet,item_key,asset,asset_class,quantity,price_usd) VALUES($1,$2,'spot','USD','USD','other',$3::integer,1)", batch, account, usd); err != nil {
			t.Fatal(err)
		}
	}
	// Clean up fixtures while the pool is still in scope.
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM bifrost.accounts WHERE id=$1", account); err != nil {
			t.Error(err)
		}
	})
	start := time.Date(2023, 5, 10, 0, 0, 0, 0, beijing)
	close := start.AddDate(0, 0, 1)
	next := close.AddDate(0, 0, 1)
	insert(start, &start, 100, "")
	insert(close, &close, 125, "")
	insert(next, &next, 130, "usdm")
	insert(next.Add(8*time.Minute), nil, 132, "")

	svc := NewService(ctx, pool, nil)
	handler := svc.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	selection := "?accountIds=" + account
	assertHistoryJSON(t, get("/api/v1/calendar/2023-05-10"+selection), `"openingUsd":"100"`, `"closingUsd":"125"`, `"assetChangeUsd":"25"`, `"pnlUsd":null`, `"netFlowUsd":null`)
	assertHistoryJSON(t, get("/api/v1/calendar/2023-05-11"+selection), `"openingUsd":"125"`, `"closingUsd":null`, `"assetChangeUsd":null`)
	assertHistoryJSON(t, get("/api/v1/calendar/2023-05-09"+selection), `"openingUsd":null`, `"closingUsd":"100"`)
	assertHistoryJSON(t, get("/api/v1/calendar"+selection+"&month=2023-05"), `"recordedChangeSumUsd":"25"`, `"changeDays":1`, `"pnlDays":0`)
}

func assertHistoryJSON(t *testing.T, rec *httptest.ResponseRecorder, fragments ...string) {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("unexpected HTTP %d: %s", rec.Code, rec.Body)
	}
	for _, fragment := range fragments {
		if !strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("missing %s from %s", fragment, rec.Body)
		}
	}
}
