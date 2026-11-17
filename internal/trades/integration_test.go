package trades

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"bifrost/db/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixtureExchange struct {
	at      time.Time
	scanned bool
}

func (f *fixtureExchange) Do(_ context.Context, method, path string, q url.Values, out any) error {
	if method != http.MethodGet {
		return fmt.Errorf("expected GET, got %s", method)
	}
	var body string
	switch path {
	case "/api/v3/exchangeInfo":
		body = `{"symbols":[{"symbol":"FAKEUSD","baseAsset":"FAKE","quoteAsset":"USD"}]}`
	case "/api/v3/openOrders":
		body = "[]"
	case "/api/v3/allOrders":
		if q.Get("symbol") != "FAKEUSD" {
			return fmt.Errorf("query did not discover FAKEUSD")
		}
		f.scanned = true
		body = fmt.Sprintf(`[
 {"symbol":"FAKEUSD","orderId":9007199254740993,"status":"FILLED","type":"LIMIT","side":"BUY","origQty":"1","executedQty":"1","time":%d},
 {"symbol":"FAKEUSD","orderId":12,"status":"FILLED","type":"LIMIT","side":"SELL","origQty":"1","executedQty":"1","time":%d},
 {"symbol":"FAKEUSD","orderId":13,"status":"CANCELED","type":"LIMIT","side":"BUY","origQty":"4","executedQty":"0","time":%d}
 ]`, f.at.UnixMilli(), f.at.Add(time.Second).UnixMilli(), f.at.Add(2*time.Second).UnixMilli())
	case "/api/v3/myTrades":
		body = fmt.Sprintf(`[
 {"symbol":"FAKEUSD","id":1,"orderId":9007199254740993,"qty":"1","quoteQty":"5","price":"5","commission":"0.01","commissionAsset":"FEE","isBuyer":true,"time":%d},
 {"symbol":"FAKEUSD","id":2,"orderId":12,"qty":"1","quoteQty":"5","price":"5","commission":"0.01","commissionAsset":"FEE","isBuyer":false,"time":%d}
 ]`, f.at.UnixMilli(), f.at.Add(time.Second).UnixMilli())
	default:
		return fmt.Errorf("unexpected URL: %s", path)
	}
	return json.Unmarshal([]byte(body), out)
}

func TestSpotDiscoveryAndReplayInPostgres(t *testing.T) {
	dsn := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var account string
	err = pool.QueryRow(ctx, `INSERT INTO bifrost.accounts(id,exchange_uid,alias)
 VALUES (gen_random_uuid(),gen_random_uuid()::text,'trade fixture') RETURNING id::text`).Scan(&account)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-20 * time.Minute).UTC().Truncate(time.Millisecond)
	end := start.Add(15 * time.Minute)
	for _, at := range []time.Time{start, end} {
		var batch string
		err = pool.QueryRow(ctx, `INSERT INTO bifrost.current_batches(id,requested_at,accounts)
 VALUES(gen_random_uuid(),$1,jsonb_build_array($2::text)) RETURNING id::text`, at, account).Scan(&batch)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO bifrost.current_runs(batch_id,account_id,wallet,state,observed_at,quantity_complete)
 VALUES($1,$2,'spot','partial',$3,true)`, batch, account, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = pool.Exec(ctx, `INSERT INTO bifrost.trade_coverage(account_id,wallet,started_at) VALUES($1,'spot',$2)`, account, start)
	if err != nil {
		t.Fatal(err)
	}
	s := New(ctx, pool, nil)
	c := collector{requester: &fixtureExchange{at: start.Add(time.Minute)}, wallet: "spot"}
	for range 2 {
		if err = s.collectOrders(ctx, c, account, start, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !c.requester.(*fixtureExchange).scanned {
		t.Fatal("did not discover zero-balance trading pair")
	}
	q := url.Values{"accountId": {account}, "wallet": {"spot"}, "asset": {"FAKE"}, "from": {start.Format(time.RFC3339Nano)}, "to": {end.Add(time.Minute).Format(time.RFC3339Nano)}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/replay?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	var got replay
	if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Coverage.State != "covered" || len(got.Orders) != 3 || len(got.Fills) != 2 || got.Fills[0].OrderID != "9007199254740993" ||
		got.Opening == nil || got.Opening.Quantity != "0" || got.Closing == nil || got.Closing.Quantity != "0" ||
		got.UnexplainedDifference == nil || *got.UnexplainedDifference != "0" {
		t.Fatalf("unexpected replay: %s", w.Body)
	}
}
