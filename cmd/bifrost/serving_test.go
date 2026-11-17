package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServingConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, listen, origin, wantOrigin string
	}{
		{"local default", "", "", "http://127.0.0.1:8080"},
		{"Vite proxy", "127.0.0.1:8080", "http://localhost:5173", "http://localhost:5173"},
		{"HTTPS proxy", ":8080", "https://Bifrost.Example:443", "https://bifrost.example"},
		{"HTTPS proxy custom port", ":8080", "https://bifrost.example:8443", "https://bifrost.example:8443"},
		{"container without public origin", ":8080", "", ""},
		{"external HTTP origin", "127.0.0.1:8080", "http://bifrost.example", ""},
		{"external HTTP listener", ":8080", "http://localhost:5173", ""},
		{"origin with path", ":8080", "https://bifrost.example/latest", ""},
		{"origin with query", ":8080", "https://bifrost.example?a=1", ""},
		{"origin with user", ":8080", "https://user@bifrost.example", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := servingConfig(8080, tc.listen, tc.origin)
			if tc.wantOrigin == "" {
				if err == nil {
					t.Fatalf("configuration should fail: %+v", cfg)
				}
			} else if err != nil || cfg.Origin != tc.wantOrigin {
				t.Fatalf("got %+v, %v", cfg, err)
			}
		})
	}
}

func TestHTTPProxyRequiresActualPublicHostAndSeparatesAuth(t *testing.T) {
	cfg, err := servingConfig(8080, ":8080", "https://bifrost.example:8443")
	if err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	auth := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := applicationHandler(api, auth, cfg)
	for _, tc := range []struct {
		host, path string
		status     int
	}{
		{"bifrost.example:8443", "/api/v1/auth/login", 204},
		{"bifrost.example:8443", "/api/v1/capabilities", 202},
		{"127.0.0.1:8080", "/api/v1/auth/login", 403},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+tc.path, nil)
		r.Header.Set("X-Forwarded-Host", cfg.Host)
		r.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d, want %d", tc.host, tc.path, w.Code, tc.status)
		}
	}
}
