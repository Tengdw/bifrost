package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"bifrost/db/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

const testOrigin = "https://bifrost.example"
const testLoginPath = "/api/v1/auth/login"

func sendRequest(origin, method, path, body string, handler http.Handler, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, testOrigin+path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func expectStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("got HTTP %d, want %d: %s", response.Code, want, response.Body)
	}
}

func TestPasskeyChallengeAndOrigin(t *testing.T) {
	s, err := New(nil, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	bad := sendRequest("https://other.example", "POST", testLoginPath+"/passkey/options", "", handler)
	expectStatus(t, bad, http.StatusForbidden)

	start := sendRequest(testOrigin, "POST", testLoginPath+"/passkey/options", "", handler)
	expectStatus(t, start, http.StatusOK)
	var options struct {
		PublicKey struct {
			RPID             string
			Challenge        string
			UserVerification string
		}
	}
	if err := json.Unmarshal(start.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if options.PublicKey.RPID != "bifrost.example" || options.PublicKey.Challenge == "" || options.PublicKey.UserVerification != "required" {
		t.Fatal("unexpected passkey options")
	}
	cookie := start.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unexpected ceremony cookie attributes")
	}
	invalid := sendRequest(testOrigin, "POST", testLoginPath+"/passkey", "{}", handler, cookie)
	expectStatus(t, invalid, http.StatusBadRequest)
	if !strings.Contains(invalid.Body.String(), "invalid_passkey_response") {
		t.Fatal(invalid.Body)
	}
	replay := sendRequest(testOrigin, "POST", testLoginPath+"/passkey", "{}", handler, cookie)
	expectStatus(t, replay, http.StatusBadRequest)
	if !strings.Contains(replay.Body.String(), "ceremony_expired") {
		t.Fatal(replay.Body)
	}
}

func TestPersistedPasswordSessionsAndRecovery(t *testing.T) {
	dsn := os.Getenv("BIFROST_AUTH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIFROST_AUTH_TEST_DATABASE_URL to a dedicated empty test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if exists, err := AdminExists(ctx, pool); err != nil || exists {
		t.Fatal("requires a dedicated database without an administrator", err)
	}
	if err = SetAdmin(ctx, pool, "demo", "first-demo-password", false); err != nil {
		t.Fatal(err)
	}
	s, err := New(pool, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	protected := s.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	expectStatus(t, sendRequest(testOrigin, "GET", "/api/v1/capabilities", "", protected,
		&http.Cookie{Name: "bifrost_session", Value: "previous-static-token"}), http.StatusUnauthorized)
	expectStatus(t, sendRequest(testOrigin, "POST", testLoginPath, `{"username":"demo","password":"incorrect-password"}`, handler), http.StatusUnauthorized)
	result := sendRequest(testOrigin, "POST", testLoginPath, `{"username":"demo","password":"first-demo-password"}`, handler)
	expectStatus(t, result, http.StatusNoContent)
	cookie := result.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unexpected session cookie attributes")
	}
	restarted, err := New(pool, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, sendRequest(testOrigin, "GET", "/api/v1/auth/passkeys", "", restarted.Handler(), cookie), http.StatusOK)
	expectStatus(t, sendRequest(testOrigin, "GET", "/api/v1/capabilities", "", protected, cookie), http.StatusNoContent)
	expectStatus(t, sendRequest("https://other.example", "POST", "/api/v1/refresh", "", protected, cookie), http.StatusForbidden)
	expectStatus(t, sendRequest(testOrigin, "POST", "/api/v1/auth/passkeys/options", "", handler, cookie), http.StatusOK)
	logout := sendRequest(testOrigin, "POST", "/api/v1/auth/logout", "", handler, cookie)
	expectStatus(t, logout, http.StatusNoContent)
	expectStatus(t, sendRequest(testOrigin, "GET", "/api/v1/capabilities", "", protected, cookie), http.StatusUnauthorized)
	result = sendRequest(testOrigin, "POST", testLoginPath, `{"username":"demo","password":"first-demo-password"}`, handler)
	expectStatus(t, result, http.StatusNoContent)
	old := result.Result().Cookies()[0]
	if old.Value == cookie.Value {
		t.Fatal("session token reused")
	}
	if err = SetAdmin(ctx, pool, "demo", "second-demo-password", true); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, sendRequest(testOrigin, "GET", "/api/v1/capabilities", "", protected, old), http.StatusUnauthorized)
	expectStatus(t, sendRequest(testOrigin, "POST", testLoginPath, `{"username":"demo","password":"first-demo-password"}`, handler), http.StatusUnauthorized)
	expectStatus(t, sendRequest(testOrigin, "POST", testLoginPath, `{"username":"demo","password":"second-demo-password"}`, handler), http.StatusNoContent)
}
