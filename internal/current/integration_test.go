package current

import (
	"bifrost/db/migrations"
	"bifrost/internal/binance"
	"bifrost/internal/holdings"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshPersistsAndReusesInFlight(t *testing.T) {
	url := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx := context.Background()
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// 不清空或复用其他测试包的记录；每例使用独立 UUID 范围。
	if err = migrations.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	a, b := uuid(), uuid()
	for _, id := range []string{a, b} {
		if _, err = p.Exec(ctx, "INSERT INTO bifrost.accounts(id,exchange_uid,alias,enabled_from) VALUES($1,$2,'synthetic',now()-interval '1 minute')", id, "synthetic-"+id); err != nil {
			t.Fatal(err)
		}
	}
	gate := make(chan struct{})
	var calls atomic.Int32
	collect := func(ctx context.Context, id, wallet string) (binance.Result, error) {
		calls.Add(1)
		<-gate
		if id == b && wallet == "usdm" {
			return binance.Result{}, errors.New("simulated permissions")
		}
		items := []holdings.Item{}
		if wallet == "spot" {
			price := decimal.NewFromInt(100)
			if id == b {
				price = decimal.NewFromInt(250)
			}
			items = append(items, holdings.Item{Wallet: "spot", Key: "USD", Asset: "USD", Class: "other", Quantity: decimal.NewFromInt(1), Price: &price})
		}
		return binance.Result{Items: items, ObservedAt: BatchTime(ctx), Complete: true}, nil
	}
	svc := NewService(ctx, p, collect)
	svc.Validate = func(context.Context, string) error { return nil }
	tasks, err := svc.Refresh(ctx, []string{a, b}, holdings.WalletNames)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Refresh(ctx, []string{a}, []string{"spot"})
	if err != nil || again[0].Batch != tasks[0].Batch || again[0].State != "already_running" {
		t.Fatal(again, err)
	}
	if _, err = svc.Load(ctx, tasks[0].Batch, []string{a}); !errors.Is(err, ErrRunning) {
		t.Fatal("mutable batch returned", err)
	}
	close(gate)
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := svc.Status(ctx, tasks[0].Batch)
		if err != nil {
			t.Fatal(err)
		}
		ready := true
		for _, s := range status {
			if s.State == "running" {
				ready = false
			}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sync timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 8 {
		t.Fatal(calls.Load())
	}
	selected, err := svc.Load(ctx, tasks[0].Batch, []string{a})
	if err != nil {
		t.Fatal(err)
	}
	o := selected.Overview()
	if o.TotalUSD == nil || o.TotalUSD.String() != "100" {
		t.Fatal(o)
	}
	all, err := svc.Load(ctx, tasks[0].Batch, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	o = all.Overview()
	if o.TotalUSD != nil || o.Subtotal.String() != "350" || o.Wallets[0].Share != nil {
		raw, _ := json.Marshal(o)
		t.Fatal(string(raw))
	}
	// 新的单账号批不得被 latest A+B 跳过，旧的 B 也不能混入。
	next, err := svc.Refresh(ctx, []string{a}, []string{"spot"})
	if err != nil {
		t.Fatal(err)
	}
	for {
		status, e := svc.Status(ctx, next[0].Batch)
		if e != nil {
			t.Fatal(e)
		}
		if status[0].State != "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	latestBatch, err := svc.Load(ctx, "", []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if latestBatch.ID == tasks[0].Batch || latestBatch.Overview().TotalUSD != nil {
		t.Fatal("latest combined query fell back to old coverage")
	}

	// 模拟翻页后失败时，先前读到的行必须丢弃；旧成功观察另列，金额不混入。
	failedSvc := NewService(ctx, p, func(ctx context.Context, id, wallet string) (binance.Result, error) {
		price := decimal.NewFromInt(999)
		return binance.Result{ObservedAt: time.Now(), Items: []holdings.Item{{Wallet: wallet, Key: "bad", Asset: "USD", Class: "other", Quantity: decimal.NewFromInt(1), Price: &price}}}, errors.New("second page failed")
	})
	failedSvc.Validate = func(context.Context, string) error { return nil }
	failed, err := failedSvc.Refresh(ctx, []string{a}, []string{"spot"})
	if err != nil {
		t.Fatal(err)
	}
	for {
		status, e := failedSvc.Status(ctx, failed[0].Batch)
		if e != nil {
			t.Fatal(e)
		}
		if status[0].State != "running" {
			if status[0].Last == nil {
				t.Fatal("last successful observation missing")
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	failure, err := failedSvc.Load(ctx, failed[0].Batch, []string{a})
	if err != nil {
		t.Fatal(err)
	}
	if len(failure.Runs[0].Result.Items) != 0 || failure.Overview().Subtotal != nil {
		t.Fatal("failed page rows leaked")
	}
	stale := failure.staleWallets("", "")
	if len(stale) != 1 || stale[0]["valueUsd"].(*decimal.Decimal).String() != "100" {
		t.Fatal(stale)
	}
	svc.Validate = func(context.Context, string) error { return errors.New("disabled") }
	if _, err = svc.Refresh(ctx, []string{a}, []string{"spot"}); err == nil {
		t.Fatal("disabled account refresh accepted")
	}

}
