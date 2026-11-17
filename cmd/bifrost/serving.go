package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type serving struct {
	Addr, Origin, Host string
}

func isLoopback(host string) bool {
	return net.ParseIP(host).IsLoopback() || strings.EqualFold(host, "localhost")
}

func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func servingConfig(port uint, listen, publicOrigin string) (serving, error) {
	cfg := serving{Addr: listen}
	if cfg.Addr == "" {
		cfg.Addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	host, portText, err := net.SplitHostPort(cfg.Addr)
	if err != nil || !validPort(portText) {
		return cfg, errors.New("BIFROST_LISTEN_ADDR must be host:port with a port between 1 and 65535")
	}
	if publicOrigin == "" {
		if !isLoopback(host) {
			return cfg, errors.New("non-loopback listeners require BIFROST_PUBLIC_ORIGIN")
		}
		publicOrigin = "http://" + net.JoinHostPort(host, portText)
	}
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return cfg, errors.New("BIFROST_PUBLIC_ORIGIN must be an origin without path, query, fragment or userinfo")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(host) && isLoopback(u.Hostname())) {
		return cfg, errors.New("BIFROST_PUBLIC_ORIGIN requires HTTPS outside loopback development")
	}
	if p := u.Port(); p != "" && !validPort(p) {
		return cfg, errors.New("invalid public origin port")
	}
	cfg.Host = strings.ToLower(u.Host)
	// 浏览器省略默认端口，反向代理应保留浏览器请求的 Host。
	if u.Scheme == "https" && u.Port() == "443" {
		cfg.Host = strings.TrimSuffix(cfg.Host, ":443")
	}
	if u.Scheme == "http" && u.Port() == "80" {
		cfg.Host = strings.TrimSuffix(cfg.Host, ":80")
	}
	cfg.Origin = u.Scheme + "://" + cfg.Host
	return cfg, nil
}

func applicationHandler(api, authentication http.Handler, cfg serving) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/auth/", authentication)
	mux.Handle("/api/", api)
	serveIndex := func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "web/dist/index.html") }
	for _, path := range []string{"/latest", "/accounts", "/security"} {
		mux.HandleFunc("GET "+path, serveIndex)
	}
	mux.Handle("GET /assets/", http.FileServer(http.Dir("web/dist")))
	mux.HandleFunc("GET /{$}", serveIndex)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Host, cfg.Host) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
