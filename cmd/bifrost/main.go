package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bifrost/db/migrations"
	"bifrost/internal/auth"
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
	initAdmin := flag.Bool("admin-init", false, "交互式初始化管理员")
	recoverAdmin := flag.Bool("admin-recover", false, "交互式恢复管理员，并撤销旧会话和 Passkey")
	demoAdmin := flag.Bool("demo-admin", false, "初始化隔离演示库的 demo 账号")
	flag.Parse()
	actions := 0
	for _, enabled := range []bool{*migrate, *seed != "", *initAdmin, *recoverAdmin, *demoAdmin} {
		if enabled {
			actions++
		}
	}
	if actions > 1 {
		return errors.New("select a single maintenance action")
	}
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("invalid demo database configuration")
	}
	defer func() {
		stop()
		pool.Close()
	}()
	if err := checkTargetDatabase(ctx, pool); err != nil {
		return err
	}
	if *migrate {
		if *mode == "snapshot" {
			return errors.New("use -mode synthetic or readonly for workspace migration")
		}
		var configured bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass('bifrost.workspace_config') IS NOT NULL").Scan(&configured); err != nil {
			return errors.New("cannot check workspace mode")
		}
		if configured {
			var compatible bool
			if err := pool.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM bifrost.workspace_config WHERE mode<>$1)", *mode).Scan(&compatible); err != nil || !compatible {
				return errors.New("workspace database initialized for a different mode; archive the old workspace before migrating")
			}
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
	if *demoAdmin {
		return runDemoAdmin(ctx, pool, *mode)
	}
	if *initAdmin || *recoverAdmin {
		return runAdmin(ctx, pool, *recoverAdmin)
	}
	cfg, err := servingConfig(*port, os.Getenv("BIFROST_LISTEN_ADDR"), os.Getenv("BIFROST_PUBLIC_ORIGIN"))
	if err != nil {
		return err
	}
	initialized, err := auth.AdminExists(ctx, pool)
	if err != nil {
		return errors.New("cannot read administrator state; migrate the database first")
	}
	if !initialized {
		return errors.New("initialize the administrator with -admin-init before starting the server")
	}
	authentication, err := auth.New(pool, cfg.Origin)
	if err != nil {
		return err
	}
	api, err := workspace(ctx, pool, *mode)
	if err != nil {
		return err
	}
	server := http.Server{
		Addr: cfg.Addr, Handler: applicationHandler(authentication.Require(api), authentication.Handler(), cfg),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	log.Printf("%s workspace: %s", *mode, cfg.Origin)
	shutdown := make(chan error, 1)
	go func() {
		<-ctx.Done()
		timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdown <- server.Shutdown(timeout)
	}()
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return <-shutdown
	}
	return err
}
