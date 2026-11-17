package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"bifrost/internal/holdings"
	"bifrost/internal/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Print("response interrupted")
	}
}
func problem(w http.ResponseWriter, status int, code string) {
	write(w, status, map[string]any{"code": code, "message": http.StatusText(status), "requestId": rand.Text()})
}
func New(store postgres.Store, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		c, err := r.Cookie("bifrost_session")
		if token == "" || err != nil || subtle.ConstantTimeCompare([]byte(token), []byte(c.Value)) != 1 {
			problem(w, 401, "unauthorized")
			return
		}
		if r.Method != "GET" {
			problem(w, 405, "method")
			return
		}
		switch r.URL.Path {
		case "/api/v1/overview":
			serveOverview(w, r, store)
		case "/api/v1/holdings":
			serveItems(w, r, store)
		default:
			problem(w, 404, "not_found")
		}
	})
}
func serveOverview(w http.ResponseWriter, r *http.Request, store postgres.Store) {
	q := r.URL.Query()
	for k := range q {
		if k != "accountIds" && k != "basis" {
			problem(w, 400, "invalid_overview_query")
			return
		}
	}
	switch q.Get("basis") {
	case "latest":
	case "", "day_close":
		problem(w, 404, "no_day_close")
		return
	default:
		problem(w, 400, "invalid_basis")
		return
	}
	snap, ok := readSnapshot(w, r, store, "", time.Time{})
	if ok {
		write(w, 200, holdings.Summarize(snap))
	}
}
func serveItems(w http.ResponseWriter, r *http.Request, store postgres.Store) {
	q := r.URL.Query()
	var err error
	wallet, class := q.Get("wallet"), q.Get("assetClass")
	if wallet != "" && !slices.Contains(holdings.WalletNames, wallet) || class != "" && class != "us_equity" && class != "other" {
		problem(w, 400, "invalid_filter")
		return
	}
	id := q.Get("batchId")
	var at time.Time
	if a := q.Get("asOf"); a != "" {
		at, err = time.Parse(time.RFC3339Nano, a)
		if err != nil {
			problem(w, 400, "invalid_time")
			return
		}
	}
	limit := 50
	if l := q.Get("limit"); l != "" {
		limit, err = strconv.Atoi(l)
		if err != nil || limit < 1 || limit > 200 {
			problem(w, 400, "invalid_limit")
			return
		}
	}
	start := 0
	if cur := q.Get("cursor"); cur != "" {
		var valid bool
		id, start, valid = decodeCursor(cur, q, id)
		if !valid {
			problem(w, 400, "invalid_cursor")
			return
		}
	}
	if id != "" {
		var u pgtype.UUID
		if err := u.Scan(id); err != nil {
			problem(w, 400, "invalid_batch")
			return
		}
	}
	snap, ok := readSnapshot(w, r, store, id, at)
	if !ok {
		return
	}
	rows := make([]map[string]any, 0, len(snap.Items))
	for _, i := range snap.Items {
		if wallet != "" && wallet != i.Wallet || class != "" && class != i.Class {
			continue
		}
		quality := "complete"
		reasons := []string{}
		if i.Price == nil {
			quality = "unpriced"
			reasons = append(reasons, i.MissingReason())
		}
		rows = append(rows, map[string]any{"accountId": snap.Account, "wallet": i.Wallet, "itemKey": i.Key, "asset": i.Asset, "assetClass": i.Class, "underlyingSymbol": i.Symbol, "quantity": i.Quantity, "valueUsd": i.Value(), "observedAt": snap.At, "components": map[string]string{}, "coverage": holdings.Coverage{Quality: quality, Reasons: reasons, AsOf: snap.At, Accounts: []string{snap.Account}}})
	}
	if start > len(rows) {
		problem(w, 400, "invalid_cursor")
		return
	}
	end := start + min(limit, len(rows)-start)
	var next *string
	if end < len(rows) {
		v := encodeCursor(snap.ID, end, q)
		next = &v
	}
	write(w, 200, map[string]any{"items": rows[start:end], "nextCursor": next})
}
func readSnapshot(w http.ResponseWriter, r *http.Request, store postgres.Store, id string, at time.Time) (holdings.Snapshot, bool) {
	snap, err := store.Load(r.Context(), id, at)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "no_snapshot")
		return snap, false
	}
	if err != nil {
		log.Printf("snapshot read failed: %v", err)
		problem(w, 503, "read_failed")
		return snap, false
	}
	for _, account := range r.URL.Query()["accountIds"] {
		if account != snap.Account {
			problem(w, 404, "unknown_account")
			return snap, false
		}
	}

	return snap, true
}
func filterKey(q url.Values) string {
	copyQ := url.Values{}
	for k, v := range q {
		if k != "cursor" {
			copyQ[k] = v
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(copyQ.Encode())))
}
func encodeCursor(id string, start int, q url.Values) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s/%d/%s", id, start, filterKey(q))))
}
func decodeCursor(cur string, q url.Values, id string) (string, int, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil {
		return "", 0, false
	}
	parts := strings.Split(string(raw), "/")
	if len(parts) != 3 || id != "" && parts[0] != id || parts[2] != filterKey(q) {
		return "", 0, false
	}
	n, err := strconv.Atoi(parts[1])
	return parts[0], n, err == nil && n >= 0
}
