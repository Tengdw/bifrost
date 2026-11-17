package current

import (
	"bifrost/internal/holdings"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func fail(w http.ResponseWriter, status int, code string) {
	reply(w, status, map[string]string{"code": code, "message": http.StatusText(status), "requestId": uuid()})
}
func validID(s string) bool { var id pgtype.UUID; return id.Scan(s) == nil && id.Valid }
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api/v1/refresh" && r.Method == "POST" {
			var req struct {
				Accounts []string `json:"accountIds"`
				Wallets  []string `json:"wallets"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				fail(w, 400, "invalid_refresh")
				return
			}
			if len(req.Accounts) == 0 {
				fail(w, 400, "accounts_required")
				return
			}
			seen := map[string]bool{}
			for _, a := range req.Accounts {
				if !validID(a) || seen[a] {
					fail(w, 400, "invalid_account")
					return
				}
				seen[a] = true
			}
			if req.Wallets == nil {
				req.Wallets = holdings.WalletNames
			}
			seen = map[string]bool{}
			for _, wallet := range req.Wallets {
				if !slices.Contains(holdings.WalletNames, wallet) || seen[wallet] {
					fail(w, 400, "invalid_wallet")
					return
				}
				seen[wallet] = true
			}
			if len(req.Wallets) == 0 {
				fail(w, 400, "wallets_required")
				return
			}
			tasks, err := s.Refresh(r.Context(), req.Accounts, req.Wallets)
			if err != nil {
				fail(w, 503, "refresh_failed")
				return
			}
			reply(w, 202, tasks)
			return
		}
		if strings.HasPrefix(path, "/api/v1/sync/") && r.Method == "GET" {
			id := strings.TrimPrefix(path, "/api/v1/sync/")
			if !validID(id) {
				fail(w, 400, "invalid_batch")
				return
			}
			tasks, err := s.Status(r.Context(), id)
			if err != nil {
				fail(w, 503, "read_failed")
				return
			}
			if len(tasks) == 0 {
				fail(w, 404, "no_batch")
				return
			}
			reply(w, 200, tasks)
			return
		}
		if r.Method != "GET" || (path != "/api/v1/overview" && path != "/api/v1/holdings" && path != "/api/v1/positions") {
			fail(w, 404, "not_found")
			return
		}
		q := r.URL.Query()
		selected := q["accountIds"]
		for _, a := range selected {
			if !validID(a) {
				fail(w, 400, "invalid_account")
				return
			}
		}
		if path == "/api/v1/overview" {
			if q.Get("basis") != "latest" {
				fail(w, 404, "no_day_close")
				return
			}
			if q.Get("batchId") != "" {
				fail(w, 400, "invalid_overview_query")
				return
			}
		}
		if q.Get("asOf") != "" {
			fail(w, 400, "as_of_not_available")
			return
		}
		id := q.Get("batchId")
		if id != "" && !validID(id) {
			fail(w, 400, "invalid_batch")
			return
		}
		b, err := s.Load(r.Context(), id, selected)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 404, "no_snapshot")
			return
		}
		if err != nil {
			fail(w, 503, "read_failed")
			return
		}
		if path == "/api/v1/overview" {
			reply(w, 200, b.Overview())
			return
		}
		wallet, class := q.Get("wallet"), q.Get("assetClass")
		if wallet != "" && !slices.Contains(holdings.WalletNames, wallet) || class != "" && class != "other" && class != "us_equity" {
			fail(w, 400, "invalid_filter")
			return
		}
		rows := []map[string]any{}
		for _, run := range b.Runs {
			if wallet != "" && wallet != run.Wallet {
				continue
			}
			if path == "/api/v1/positions" {
				for _, p := range run.Result.Positions {
					raw, _ := json.Marshal(p)
					var row map[string]any
					_ = json.Unmarshal(raw, &row)
					row["accountId"] = run.Account
					row["observedAt"] = run.Result.ObservedAt
					rows = append(rows, row)
				}
				continue
			}
			for _, item := range run.Result.Items {
				if class != "" && class != item.Class {
					continue
				}
				quality := "complete"
				reasons := []string{}
				if item.Value() == nil {
					quality = "unpriced"
					reasons = append(reasons, item.MissingReason())
				}
				if !run.Result.Complete {
					quality = "partial"
					reasons = append(reasons, run.Result.Reasons...)
				}
				row := map[string]any{"accountId": run.Account, "wallet": run.Wallet, "itemKey": item.Key, "asset": item.Asset, "assetClass": item.Class, "underlyingSymbol": item.Symbol, "quantity": item.Quantity, "valueUsd": item.Value(), "observedAt": run.Result.ObservedAt, "components": map[string]string{}, "coverage": holdings.Coverage{Quality: quality, Reasons: reasons, AsOf: run.Result.ObservedAt, Accounts: []string{run.Account}}}
				if c := run.Result.Components[item.Key]; c != nil {
					row["components"] = c
				}
				if quote, ok := run.Result.Quotes[item.Key]; ok {
					row["price"] = map[string]any{"priceUsd": quote.Price, "source": quote.Source, "pricedAt": quote.PricedAt, "path": quote.Path}
				}
				rows = append(rows, row)
			}
		}
		// 当前批次限制在两分钟；分页始终固定同批并绑定账号与筛选。
		start := 0
		if cursor := q.Get("cursor"); cursor != "" {
			raw, e := base64.RawURLEncoding.DecodeString(cursor)
			parts := strings.Split(string(raw), ":")
			valid := e == nil && len(parts) == 3
			if valid {
				start, e = strconv.Atoi(parts[1])
				valid = e == nil && parts[0] == b.ID && parts[2] == queryKey(q)
			}
			if !valid {
				fail(w, 400, "invalid_cursor")
				return
			}
		}
		limit := 50
		if text := q.Get("limit"); text != "" {
			if _, err := fmt.Sscanf(text, "%d", &limit); err != nil || limit < 1 || limit > 200 {
				fail(w, 400, "invalid_limit")
				return
			}
		}
		if start < 0 || start > len(rows) {
			fail(w, 400, "invalid_cursor")
			return
		}
		end := min(start+limit, len(rows))
		var next *string
		if end < len(rows) {
			v := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%d:%s", b.ID, end, queryKey(q))))
			next = &v
		}
		reply(w, 200, map[string]any{"items": rows[start:end], "nextCursor": next})
	})
}
func queryKey(q url.Values) string {
	clone := url.Values{}
	for k, v := range q {
		if k != "cursor" {
			clone[k] = v
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(clone.Encode())))
}
