package accounts

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"io"
	"net/http"
	"strings"
)

// Handler 接在主服务的会话鉴权与同源变更校验之后。
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts")
		var a Account
		var err error
		status := 200
		if path == "" && r.Method == http.MethodGet {
			items, e := s.List(r.Context())
			respond(w, items, e, 200)
			return
		}
		var id string
		if path != "" {
			id = strings.Split(strings.TrimPrefix(path, "/"), "/")[0]
			var u pgtype.UUID
			if e := u.Scan(id); e != nil {
				respond(w, nil, ErrInvalid, 400)
				return
			}
		}
		switch {
		case path == "" && r.Method == http.MethodPost:
			var input struct {
				Alias string `json:"alias"`
				Credentials
			}
			if !decode(w, r, &input) {
				return
			}
			a, err = s.Bind(r.Context(), input.Alias, input.Credentials)
			status = 201
		case path == "/"+id && r.Method == http.MethodPatch:
			var input struct {
				Alias   *string `json:"alias"`
				Enabled *bool   `json:"enabled"`
			}
			if !decode(w, r, &input) {
				return
			}
			a, err = s.Update(r.Context(), id, input.Alias, input.Enabled)
		case path == "/"+id+"/credentials" && r.Method == http.MethodPut:
			var input Credentials
			if !decode(w, r, &input) {
				return
			}
			a, err = s.Rotate(r.Context(), id, input)
		default:
			http.NotFound(w, r)
			return
		}
		respond(w, a, err, status)
	})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		respond(w, nil, ErrInvalid, 400)
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		respond(w, nil, ErrInvalid, 400)
		return false
	}
	return true
}
func respond(w http.ResponseWriter, body any, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		code := "account_unavailable"
		status = 503
		switch {
		case errors.Is(err, ErrInvalid):
			status = 400
			code = "invalid_account"
		case errors.Is(err, ErrConflict):
			status = 409
			code = "identity_conflict"
		case errors.Is(err, ErrDisabled):
			status = 409
			code = "account_disabled"
		case errors.Is(err, pgx.ErrNoRows):
			status = 404
			code = "account_not_found"
		}
		body = map[string]string{"code": code, "message": http.StatusText(status), "requestId": uuid()}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
