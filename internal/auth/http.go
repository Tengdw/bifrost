package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const challengeLifetime = 3 * time.Minute
const passwordAttemptWindow = time.Minute
const passwordAttemptsPerWindow = 8

var tokenEncoding = base64.RawURLEncoding.Strict()

type Service struct {
	pool          *pgxpool.Pool
	webauthn      *webauthn.WebAuthn
	origin        string
	secure        bool
	passwordSlots chan struct{}

	mu         sync.Mutex
	attempts   []time.Time
	ceremonies map[[sha256.Size]byte]ceremony
}

type principal struct {
	id        []byte
	hash      [sha256.Size]byte
	createdAt time.Time
}

type ceremony struct {
	data        webauthn.SessionData
	purpose     string
	sessionHash [sha256.Size]byte
	expires     time.Time
}

func New(pool *pgxpool.Pool, origin string) (*Service, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("invalid authentication origin")
	}
	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: challengeLifetime, TimeoutUVD: challengeLifetime}
	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName:          "Bifrost",
		RPID:                   u.Hostname(),
		RPOrigins:              []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired},
		Timeouts:               webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
	if err != nil {
		return nil, err
	}
	return &Service{
		pool: pool, webauthn: w, origin: origin, secure: u.Scheme == "https",
		passwordSlots: make(chan struct{}, 2), ceremonies: make(map[[sha256.Size]byte]ceremony),
	}, nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", s.passwordLogin)
	mux.HandleFunc("POST /api/v1/auth/login/passkey/options", s.passkeyLoginOptions)
	mux.HandleFunc("POST /api/v1/auth/login/passkey", s.passkeyLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.protected(s.logout))
	mux.HandleFunc("GET /api/v1/auth/passkeys", s.protected(s.passkeyList))
	mux.HandleFunc("POST /api/v1/auth/passkeys/options", s.protected(s.passkeyRegisterOptions))
	mux.HandleFunc("POST /api/v1/auth/passkeys", s.protected(s.passkeyRegister))
	mux.HandleFunc("DELETE /api/v1/auth/passkeys/{id}", s.protected(s.passkeyDelete))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.checkOrigin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		mux.ServeHTTP(w, r)
	})
}

func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.checkOrigin(w, r) {
			return
		}
		if _, err := s.authenticate(r); err != nil {
			authError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) protected(next func(http.ResponseWriter, *http.Request, principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.authenticate(r)
		if err != nil {
			authError(w, err)
			return
		}
		next(w, r, p)
	}
}

func (s *Service) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != s.origin {
		problem(w, http.StatusForbidden, "origin_rejected")
		return false
	}
	return true
}

func (s *Service) cookieName(name string) string {
	if s.secure {
		return "__Host-bifrost_" + name
	}
	return "bifrost_local_" + name
}

func (s *Service) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(name), Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.secure,
	})
}

func newToken() (string, [sha256.Size]byte) {
	data := make([]byte, digestBytes)
	rand.Read(data)
	return tokenEncoding.EncodeToString(data), sha256.Sum256(data)
}

func (s *Service) readHash(r *http.Request, name string) ([sha256.Size]byte, error) {
	c, err := r.Cookie(s.cookieName(name))
	if err != nil || len(c.Value) != tokenEncoding.EncodedLen(digestBytes) {
		return [sha256.Size]byte{}, errUnauthorized
	}
	data, err := tokenEncoding.DecodeString(c.Value)
	if err != nil || len(data) != digestBytes {
		return [sha256.Size]byte{}, errUnauthorized
	}
	return sha256.Sum256(data), nil
}

func (s *Service) authenticate(r *http.Request) (principal, error) {
	hash, err := s.readHash(r, "session")
	if err != nil {
		return principal{}, err
	}
	p := principal{hash: hash}
	err = s.pool.QueryRow(r.Context(), `select s.user_id, s.created_at
		from bifrost.auth_sessions s join bifrost.auth_admin a on a.user_id=s.user_id
		where s.token_hash=$1 and s.expires_at > now()`, hash[:]).Scan(&p.id, &p.createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = errUnauthorized
	}
	return p, err
}

func (s *Service) issueSession(r *http.Request, tx pgx.Tx, userID []byte) (string, error) {
	value, hash := newToken()
	if _, err := tx.Exec(r.Context(), "delete from bifrost.auth_sessions where expires_at <= now()"); err != nil {
		return "", err
	}
	if previous, err := s.readHash(r, "session"); err == nil {
		if _, err = tx.Exec(r.Context(), "delete from bifrost.auth_sessions where token_hash=$1", previous[:]); err != nil {
			return "", err
		}
	}
	tag, err := tx.Exec(r.Context(), `insert into bifrost.auth_sessions(token_hash,user_id,expires_at)
		select $1,user_id,$2 from bifrost.auth_admin where user_id=$3`, hash[:], time.Now().Add(sessionLifetime), userID)
	if err == nil && tag.RowsAffected() == 0 {
		err = errUnauthorized
	}
	return value, err
}

func (s *Service) allowPasswordAttempt(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := 0
	for first < len(s.attempts) && !s.attempts[first].Add(passwordAttemptWindow).After(now) {
		first++
	}
	s.attempts = s.attempts[first:]
	if len(s.attempts) >= passwordAttemptsPerWindow {
		return false
	}
	s.attempts = append(s.attempts, now)
	return true
}

func tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	problem(w, http.StatusTooManyRequests, "try_again_later")
}

func (s *Service) passwordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || d.Decode(new(json.RawMessage)) != io.EOF || len(body.Password) > 1024 || len(body.Username) > 64 {
		problem(w, http.StatusBadRequest, "invalid_login")
		return
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		tooManyRequests(w)
		return
	}
	if !s.allowPasswordAttempt(time.Now()) {
		tooManyRequests(w)
		return
	}
	var id, pwhash []byte
	var name string
	err := s.pool.QueryRow(r.Context(), "select user_id,username,password_hash from bifrost.auth_admin where singleton").Scan(&id, &name, &pwhash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		authError(w, err)
		return
	}
	if !checkPassword(pwhash, body.Password) || subtle.ConstantTimeCompare([]byte(name), []byte(strings.TrimSpace(body.Username))) != 1 {
		authError(w, errUnauthorized)
		return
	}
	var value string
	err = withAuthLock(r.Context(), s.pool, func(tx pgx.Tx) error {
		var err error
		value, err = s.issueSession(r, tx, id)
		return err
	})
	s.finishLogin(w, value, err)
}

func (s *Service) finishLogin(w http.ResponseWriter, value string, err error) {
	if err != nil {
		authError(w, err)
		return
	}
	s.setCookie(w, "session", value, int(sessionLifetime/time.Second))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request, p principal) {
	err := withAuthLock(r.Context(), s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), "delete from bifrost.auth_sessions where token_hash=$1", p.hash[:])
		return err
	})
	if err != nil {
		authError(w, err)
		return
	}
	s.setCookie(w, "session", "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func sessionStillActive(ctx context.Context, tx pgx.Tx, p principal) error {
	var exists bool
	err := tx.QueryRow(ctx, `select exists(select 1 from bifrost.auth_sessions s join bifrost.auth_admin a on a.user_id=s.user_id
		where s.token_hash=$1 and s.user_id=$2 and s.expires_at > now())`, p.hash[:], p.id).Scan(&exists)
	if err == nil && !exists {
		err = errUnauthorized
	}
	return err
}

func jsonResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Print("auth response interrupted")
	}
}

func problem(w http.ResponseWriter, status int, code string) {
	jsonResponse(w, status, map[string]string{"code": code, "message": http.StatusText(status), "requestId": rand.Text()})
}

func authError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnauthorized) || errors.Is(err, pgx.ErrNoRows) {
		problem(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	log.Printf("authentication operation failed: %T", err)
	problem(w, http.StatusInternalServerError, "auth_failed")
}
