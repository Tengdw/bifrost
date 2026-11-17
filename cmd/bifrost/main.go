package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"bifrost/db/migrations"

	"bifrost/internal/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	seed := flag.String("seed", "", "仅向隔离库装入 complete 或 unpriced 合成批次")
	port := flag.Uint("port", 8080, "本机端口")
	mode := flag.String("mode", "snapshot", "snapshot、synthetic 或 readonly")
	migrate := flag.Bool("migrate", false, "显式迁移指定工作库后退出")
	flag.Parse()
	url := os.Getenv("BIFROST_DEMO_DATABASE_URL")
	if *mode != "snapshot" && *mode != "synthetic" && *mode != "readonly" {
		return errors.New("unknown mode")
	}
	if *mode != "snapshot" {
		var err error
		url, err = readSecret(os.Getenv("DATABASE_URL_FILE"))
		if err != nil {
			return err
		}
	}
	if url == "" {
		return fmt.Errorf("set BIFROST_DEMO_DATABASE_URL to an isolated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("invalid demo database configuration")
	}
	defer pool.Close()
	if *migrate {
		if *mode == "snapshot" {
			return errors.New("use -mode synthetic or readonly for workspace migration")
		}
		if err := migrations.Apply(ctx, pool); err != nil {
			return errors.New("workspace migration failed")
		}
		var actual string
		if err := pool.QueryRow(ctx, "insert into bifrost.workspace_config(singleton,mode) values(true,$1) on conflict(singleton) do update set mode=bifrost.workspace_config.mode returning mode", *mode).Scan(&actual); err != nil || actual != *mode {
			return errors.New("workspace database initialized for a different mode")
		}
		return nil
	}
	store := postgres.Store{Pool: pool}
	if *seed != "" {
		if *mode != "snapshot" {
			return errors.New("seed only available in snapshot mode")
		}
		if *seed != "complete" && *seed != "unpriced" {
			return fmt.Errorf("unknown fixture")
		}
		if err := migrations.Apply(ctx, pool); err != nil {
			return fmt.Errorf("cannot initialize demo schema")
		}
		if err := store.SeedExample(ctx, *seed == "unpriced"); err != nil {
			return fmt.Errorf("cannot store example snapshot")
		}
		return nil
	}
	token := os.Getenv("BIFROST_DEMO_SESSION")
	if *mode != "snapshot" {
		token, err = readSecret(os.Getenv("SESSION_TOKEN_FILE"))
		if err != nil {
			return err
		}
	}
	if len(token) < 32 {
		return fmt.Errorf("set a random BIFROST_DEMO_SESSION (32+ characters)")
	}
	cfg, err := servingConfig(*mode, *port, os.Getenv("BIFROST_LISTEN_ADDR"), os.Getenv("BIFROST_PUBLIC_ORIGIN"), os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE"))
	if err != nil {
		return err
	}
	api, err := workspace(ctx, pool, *mode, token, cfg.Origin)
	if err != nil {
		return err
	}
	server := http.Server{Addr: cfg.Addr, Handler: applicationHandler(api, token, cfg), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("%s workspace: %s/latest", *mode, cfg.Origin)
	if cfg.Secure {
		return server.ListenAndServeTLS(cfg.Cert, cfg.Key)
	}
	return server.ListenAndServe()
}
