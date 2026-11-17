package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type serving struct {
	Addr, Origin, Host, Cert, Key string
	Secure                        bool
}

func servingConfig(mode string, port uint, listen, publicOrigin, cert, key string) (serving, error) {
	cfg := serving{Addr: listen, Cert: cert, Key: key}
	if cfg.Addr == "" {
		cfg.Addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	host, portText, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return cfg, errors.New("BIFROST_LISTEN_ADDR must be host:port")
	}
	n, err := strconv.Atoi(portText)
	if err != nil || n < 1 || n > 65535 {
		return cfg, errors.New("listen port must be between 1 and 65535")
	}
	if (cert == "") != (key == "") {
		return cfg, errors.New("TLS_CERT_FILE and TLS_KEY_FILE must be configured together")
	}
	cfg.Secure = cert != ""
	if mode == "readonly" && !cfg.Secure {
		return cfg, errors.New("readonly mode requires TLS_CERT_FILE and TLS_KEY_FILE")
	}
	loopback := net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	if publicOrigin == "" {
		if !loopback {
			return cfg, errors.New("non-loopback listeners require BIFROST_PUBLIC_ORIGIN and TLS")
		}
		cfg.Host = net.JoinHostPort(host, portText)
		cfg.Origin = "http://" + cfg.Host
		if cfg.Secure {
			cfg.Origin = "https://" + cfg.Host
		}
		return cfg, nil
	}
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return cfg, errors.New("BIFROST_PUBLIC_ORIGIN must be an HTTPS origin without path, query, fragment or userinfo")
	}
	if !cfg.Secure {
		return cfg, errors.New("BIFROST_PUBLIC_ORIGIN requires TLS_CERT_FILE and TLS_KEY_FILE")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return cfg, errors.New("invalid public origin port")
		}
	}
	// 浏览器会将 HTTPS 默认端口规范化；Origin 与 Host 使用同一规范值。
	cfg.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		cfg.Host = strings.TrimSuffix(cfg.Host, ":443")
	}
	cfg.Origin = "https://" + cfg.Host
	return cfg, nil
}

func applicationHandler(api http.Handler, token string, cfg serving) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != cfg.Origin {
			http.Error(w, "Forbidden", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if subtle.ConstantTimeCompare([]byte(r.PostFormValue("token")), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "bifrost_session", Value: token, HttpOnly: true, Secure: cfg.Secure, SameSite: http.SameSiteStrictMode, Path: "/"})
		http.Redirect(w, r, "/latest", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /latest", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "web/dist/index.html") })
	mux.Handle("GET /assets/", http.FileServer(http.Dir("web/dist")))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/latest", 302) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Host, cfg.Host) {
			http.Error(w, "Forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
