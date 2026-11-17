package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestServingConfigurationRequiresHTTPSForContainer(t *testing.T) {
	for _, tc := range []struct {
		name, listen, origin, cert, key string
		valid                           bool
	}{
		{"local default", "", "", "", "", true},
		{"container HTTPS", ":8080", "https://bifrost.example:8443", "cert", "key", true},
		{"container without public origin", ":8080", "", "cert", "key", false},
		{"external HTTP", ":8080", "http://bifrost.example:8080", "", "", false},
		{"origin with path", ":8080", "https://bifrost.example/latest", "cert", "key", false},
		{"origin with query", ":8080", "https://bifrost.example?a=1", "cert", "key", false},
		{"origin with user", ":8080", "https://user@bifrost.example", "cert", "key", false},
		{"TLS half pair", "", "", "cert", "", false},
		{"public without TLS", "", "https://bifrost.example", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := servingConfig("synthetic", 8080, tc.listen, tc.origin, tc.cert, tc.key)
			if (err == nil) != tc.valid {
				t.Fatalf("configuration validity = %v, want %v", err, tc.valid)
			}
			if tc.name == "local default" && (cfg.Addr != "127.0.0.1:8080" || cfg.Origin != "http://127.0.0.1:8080") {
				t.Fatal(cfg)
			}
		})
	}
	if _, err := servingConfig("readonly", 8080, "", "", "", ""); err == nil {
		t.Fatal("readonly accepted without TLS")
	}
}
func TestHTTPSLoginAndHostProtection(t *testing.T) {
	cfg, err := servingConfig("synthetic", 8080, ":8080", "https://bifrost.example:8443", "cert", "key")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("x", 32)
	h := applicationHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), token, cfg)
	for _, tc := range []struct {
		host, origin string
		status       int
	}{
		{"bifrost.example:8443", "https://bifrost.example:8443", 303},
		{"bifrost.example:8443", "https://evil.example", 403},
		{"127.0.0.1:8080", "https://bifrost.example:8443", 403},
	} {
		form := url.Values{"token": {token}}
		r := httptest.NewRequest("POST", "https://"+tc.host+"/session", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Forwarded-Host", "bifrost.example:8443")
		r.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d", tc.host, tc.origin, w.Code)
		}
		if w.Code == 303 {
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
				t.Fatal(cookies)
			}
		}
	}
}

func TestHTTPSWorkspaceUsesPublicOriginForMutations(t *testing.T) {
	token := strings.Repeat("x", 32)
	cfg, err := servingConfig("synthetic", 8080, ":8080", "https://bifrost.example:8443", "cert", "key")
	if err != nil {
		t.Fatal(err)
	}
	// snapshot 提供无数据库依赖的相同会话/Origin边界。
	api, err := workspace(context.Background(), nil, "snapshot", token, cfg.Origin)
	if err != nil {
		t.Fatal(err)
	}
	h := applicationHandler(api, token, cfg)
	for _, tc := range []struct {
		origin string
		want   int
	}{
		{"https://evil.example", 403},
		{"", 403},
		{"https://bifrost.example:8443", 405},
	} {
		r := httptest.NewRequest("POST", cfg.Origin+"/api/v1/overview", nil)
		r.AddCookie(&http.Cookie{Name: "bifrost_session", Value: token})
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%q: got %d, want %d", tc.origin, w.Code, tc.want)
		}
	}
}

func TestDefaultHTTPSPortNormalizedForBrowserOrigin(t *testing.T) {
	cfg, err := servingConfig("readonly", 8080, ":8080", "https://Bifrost.Example:443", "cert", "key")
	if err != nil || cfg.Host != "bifrost.example" || cfg.Origin != "https://bifrost.example" {
		t.Fatal(cfg, err)
	}
}
