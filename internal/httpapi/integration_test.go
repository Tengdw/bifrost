package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"bifrost/db/migrations"
	"bifrost/internal/httpapi"
	"bifrost/internal/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersistedOverview(t *testing.T) {
	url := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires fresh isolated PostgreSQL; see ticket 01")
	}
	ctx := context.Background()
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err = migrations.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	store := postgres.Store{Pool: p}
	get := func(path string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		httpapi.New(store).ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if err = store.SeedExample(ctx, false); err != nil {
		t.Fatal(err)
	}
	first := get("/api/v1/overview?basis=latest")
	if first["totalUsd"] != "1000" {
		t.Fatal(first)
	}
	if err = store.SeedExample(ctx, true); err != nil {
		t.Fatal(err)
	}
	latest := get("/api/v1/overview?basis=latest")
	if v, ok := latest["totalUsd"]; !ok || v != nil || latest["pricedSubtotalUsd"] != "800" {
		t.Fatal(latest)
	}
	for _, tc := range []struct {
		batch string
		value any
	}{{first["batchId"].(string), "200"}, {latest["batchId"].(string), nil}} {
		body := get("/api/v1/holdings?assetClass=us_equity&batchId=" + tc.batch)
		items := body["items"].([]any)
		if len(items) != 1 {
			t.Fatal(body)
		}
		row := items[0].(map[string]any)
		v, ok := row["valueUsd"]
		if !ok || v != tc.value || row["asset"] != "EQ_MU" || row["quantity"] != "2" {
			t.Fatal(row)
		}
	}
}

// 验证只读总览不能被明细参数回拨到已经过期的完整批次。
func TestOverviewRejectsPinnedBatch(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/overview?basis=latest&batchId=00000000-0000-4000-8000-000000000010", nil)
	w := httptest.NewRecorder()
	httpapi.New(postgres.Store{}).ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("got %d, expected invalid latest query", w.Code)
	}
}
