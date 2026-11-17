package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"bifrost/internal/accounts"
	"bifrost/internal/binance"
	"bifrost/internal/current"
	"bifrost/internal/exchange"
	"bifrost/internal/httpapi"
	"bifrost/internal/postgres"
	"bifrost/internal/trades"
	"github.com/jackc/pgx/v5/pgxpool"
)

func readSecret(path string) (string, error) {
	if path == "" {
		return "", errors.New("secret file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("cannot read configured secret file")
	}
	return strings.TrimSpace(string(data)), nil
}

func workspace(ctx context.Context, pool *pgxpool.Pool, mode string) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"mode": mode})
	})
	if mode == "snapshot" {
		mux.Handle("/api/", httpapi.New(postgres.Store{Pool: pool}))
	} else {
		encoded, err := readSecret(os.Getenv("MASTER_KEY_FILE"))
		if err != nil {
			return nil, err
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return nil, errors.New("MASTER_KEY_FILE must contain 32 bytes in base64")
		}
		client := exchange.Client()
		var v accounts.Verifier = accounts.NewBinanceVerifier(client)
		if mode == "synthetic" {
			v = exchange.Synthetic{}
		}
		a, err := accounts.New(pool, key, v)
		if err != nil {
			return nil, err
		}
		// 合成与实盘绑定不能共享同一数据库，避免测试凭据被送到外网。
		var stored string
		if err = pool.QueryRow(ctx, "select mode from bifrost.workspace_config where singleton").Scan(&stored); err != nil || stored != mode {
			return nil, errors.New("workspace mode mismatch; explicitly migrate a dedicated database with matching -mode")
		}
		srv := current.NewService(ctx, pool, func(ctx context.Context, id, wallet string) (binance.Result, error) {
			c, err := a.Credentials(ctx, id)
			if err != nil {
				return binance.Result{}, err
			}
			collector := binance.Collector{Client: &exchange.Reader{Credentials: c, HTTP: client}}
			if mode == "synthetic" {
				f := exchange.Synthetic{B: c.APIKey == "demo-b"}
				collector.Client = f
				collector.Prices = f.Prices(current.BatchTime(ctx))
			}
			return collector.Collect(ctx, wallet)
		})
		srv.AllAccounts = func(ctx context.Context) ([]string, error) {
			list, err := a.List(ctx)
			if err != nil {
				return nil, err
			}
			ids := []string{}
			for _, account := range list {
				if account.DisabledAt == nil {
					ids = append(ids, account.ID)
				}
			}
			return ids, nil
		}
		srv.Validate = func(ctx context.Context, id string) error { _, err := a.Credentials(ctx, id); return err }
		if mode == "readonly" {
			tradeService := trades.New(ctx, pool, func(ctx context.Context, id string) (binance.Requester, error) {
				credentials, err := a.Credentials(ctx, id)
				if err != nil {
					return nil, err
				}
				return &exchange.Reader{Credentials: credentials, HTTP: client}, nil
			})
			srv.OnWalletRecorded = tradeService.Trigger
			mux.Handle("GET /api/v1/replay", tradeService.Handler())
			automatic := os.Getenv("BIFROST_DISABLE_AUTO_SYNC") != "1"
			if automatic {
				a.OnBound = srv.CollectBound
			}
			if err := srv.StartScheduler(ctx, automatic); err != nil {
				return nil, err
			}
		} else if err := srv.Recover(ctx); err != nil {
			return nil, err
		}
		ah := a.Handler()
		mux.Handle("/api/v1/accounts", ah)
		mux.Handle("/api/v1/accounts/", ah)
		mux.Handle("/api/", srv.Handler())
	}
	return mux, nil
}
