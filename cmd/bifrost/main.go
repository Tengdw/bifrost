package main

import (
	"context"
	"crypto/subtle"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bifrost/db/migrations"
	"bifrost/internal/httpapi"
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
	flag.Parse()
	url := os.Getenv("BIFROST_DEMO_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("set BIFROST_DEMO_DATABASE_URL to an isolated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("invalid demo database configuration")
	}
	defer pool.Close()
	store := postgres.Store{Pool: pool}
	if *seed != "" {
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
	if len(token) < 32 {
		return fmt.Errorf("set a random BIFROST_DEMO_SESSION (32+ characters)")
	}
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.New(store, token))
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "http://"+addr {
			http.Error(w, "Forbidden", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if subtle.ConstantTimeCompare([]byte(r.PostFormValue("token")), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "bifrost_session", Value: token, HttpOnly: true, SameSite: http.SameSiteStrictMode, Path: "/"})
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
	log.Printf("synthetic snapshot viewer: http://%s/latest", addr)
	return server.ListenAndServe()
}
