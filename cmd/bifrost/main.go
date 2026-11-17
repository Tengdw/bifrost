package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	origin := "http://" + addr
	cert, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	if *mode == "readonly" {
		if cert == "" || keyFile == "" {
			return errors.New("readonly mode requires TLS_CERT_FILE and TLS_KEY_FILE")
		}
		origin = "https://" + addr
	}
	mux := http.NewServeMux()
	api, err := workspace(ctx, pool, *mode, token, origin)
	if err != nil {
		return err
	}
	mux.Handle("/api/", api)
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Forbidden", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if subtle.ConstantTimeCompare([]byte(r.PostFormValue("token")), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "bifrost_session", Value: token, HttpOnly: true, Secure: *mode == "readonly", SameSite: http.SameSiteStrictMode, Path: "/"})
		http.Redirect(w, r, "/latest", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /latest", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "web/dist/index.html") })
	mux.Handle("GET /assets/", http.FileServer(http.Dir("web/dist")))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/latest", 302) })
	server := http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Host, addr) {
			http.Error(w, "Forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("%s workspace: %s/latest", *mode, origin)
	if *mode == "readonly" {
		return server.ListenAndServeTLS(cert, keyFile)
	}
	return server.ListenAndServe()
}
