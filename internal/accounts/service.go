package accounts

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("invalid account request")
var ErrConflict = errors.New("exchange identity conflict")
var ErrDisabled = errors.New("account disabled")
var ErrUnavailable = errors.New("account storage unavailable")

type Credentials struct {
	APIKey    string `json:"apiKey"`
	APISecret string `json:"apiSecret"`
}
type Account struct {
	ID          string     `json:"id"`
	Alias       string     `json:"alias"`
	Exchange    string     `json:"exchange"`
	EnabledFrom time.Time  `json:"enabledFrom"`
	DisabledAt  *time.Time `json:"disabledAt"`
}
type Verifier interface {
	Verify(context.Context, Credentials) (string, error)
}
type Service struct {
	pool     *pgxpool.Pool
	aead     cipher.AEAD
	verifier Verifier
}

func New(pool *pgxpool.Pool, key []byte, verifier Verifier) (*Service, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, aead: aead, verifier: verifier}, nil
}
func (s *Service) encrypt(id string, c Credentials) ([]byte, []byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return s.aead.Seal(nil, nonce, raw, []byte(id)), nonce, nil
}
func (s *Service) decrypt(id string, body, nonce []byte) (Credentials, error) {
	var c Credentials
	if len(nonce) != s.aead.NonceSize() {
		return c, ErrUnavailable
	}
	raw, err := s.aead.Open(nil, nonce, body, []byte(id))
	if err != nil {
		return c, ErrUnavailable
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, ErrUnavailable
	}
	return c, nil
}
func (s *Service) verify(ctx context.Context, c Credentials) (string, error) {
	if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.APISecret) == "" {
		return "", ErrInvalid
	}
	if s.verifier == nil {
		return "", ErrUnavailable
	}
	uid, err := s.verifier.Verify(ctx, c)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(uid) == "" {
		return "", ErrInvalid
	}
	return uid, nil
}
func uuid() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic("system randomness unavailable")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func scanAccount(row pgx.Row) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Alias, &a.Exchange, &a.EnabledFrom, &a.DisabledAt)
	return a, err
}

const fields = "id::text,alias,exchange,enabled_from,disabled_at"

func (s *Service) List(ctx context.Context) ([]Account, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+fields+" FROM bifrost.accounts ORDER BY enabled_from,id")
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, ErrUnavailable
		}
		out = append(out, a)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}
func (s *Service) Bind(ctx context.Context, alias string, c Credentials) (Account, error) {
	var out Account
	if strings.TrimSpace(alias) == "" {
		return out, ErrInvalid
	}
	uid, err := s.verify(ctx, c)
	if err != nil {
		return out, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// 相同 UID 的并发绑定串行化，轮换沿用既有主键。
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", uid); err != nil {
			return ErrUnavailable
		}
		var id string
		err := tx.QueryRow(ctx, "SELECT id::text FROM bifrost.accounts WHERE exchange_uid=$1 FOR UPDATE", uid).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			id = uuid()
			_, err = tx.Exec(ctx, "INSERT INTO bifrost.accounts(id,exchange_uid,alias,last_verified_at) VALUES($1,$2,$3,now())", id, uid, alias)
		} else if err == nil {
			var disabled *time.Time
			if err = tx.QueryRow(ctx, "SELECT disabled_at FROM bifrost.accounts WHERE id=$1", id).Scan(&disabled); err != nil {
				return ErrUnavailable
			}
			if disabled != nil {
				return ErrDisabled
			}
			_, err = tx.Exec(ctx, "UPDATE bifrost.accounts SET alias=$2,last_verified_at=now() WHERE id=$1", id, alias)
		}
		if err != nil {
			return ErrUnavailable
		}
		if err = s.saveCredentials(ctx, tx, id, c); err != nil {
			return err
		}
		out, err = scanAccount(tx.QueryRow(ctx, "SELECT "+fields+" FROM bifrost.accounts WHERE id=$1", id))
		return err
	})
	if err != nil {
		return Account{}, sanitize(err)
	}
	return out, nil
}
func (s *Service) saveCredentials(ctx context.Context, tx pgx.Tx, id string, c Credentials) error {
	body, nonce, err := s.encrypt(id, c)
	if err != nil {
		return ErrUnavailable
	}
	_, err = tx.Exec(ctx, "UPDATE bifrost.account_credentials SET active=false WHERE account_id=$1 AND active", id)
	if err != nil {
		return ErrUnavailable
	}
	_, err = tx.Exec(ctx, "INSERT INTO bifrost.account_credentials(account_id,version,ciphertext,nonce,master_key_version,active) SELECT $1,COALESCE(MAX(version),0)+1,$2,$3,'v1',true FROM bifrost.account_credentials WHERE account_id=$1", id, body, nonce)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
func sanitize(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) || errors.Is(err, ErrDisabled) || errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return ErrUnavailable
}
func (s *Service) Rotate(ctx context.Context, id string, c Credentials) (Account, error) {
	uid, err := s.verify(ctx, c)
	if err != nil {
		return Account{}, err
	}
	var a Account
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var existing string
		var disabled *time.Time
		if err := tx.QueryRow(ctx, "SELECT exchange_uid,disabled_at FROM bifrost.accounts WHERE id=$1 FOR UPDATE", id).Scan(&existing, &disabled); err != nil {
			return err
		}
		if existing != uid {
			return ErrConflict
		}
		if disabled != nil {
			return ErrDisabled
		}
		if err := s.saveCredentials(ctx, tx, id, c); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE bifrost.accounts SET last_verified_at=now() WHERE id=$1", id); err != nil {
			return err
		}
		a, err = scanAccount(tx.QueryRow(ctx, "SELECT "+fields+" FROM bifrost.accounts WHERE id=$1", id))
		return err
	})
	return a, sanitize(err)
}
func (s *Service) Update(ctx context.Context, id string, alias *string, enabled *bool) (Account, error) {
	if (alias == nil && enabled == nil) || (enabled != nil && *enabled) || (alias != nil && strings.TrimSpace(*alias) == "") {
		return Account{}, ErrInvalid
	}
	a, err := scanAccount(s.pool.QueryRow(ctx, "UPDATE bifrost.accounts SET alias=COALESCE($2,alias),disabled_at=CASE WHEN $3::boolean=false THEN COALESCE(disabled_at,now()) ELSE disabled_at END WHERE id=$1 RETURNING "+fields, id, alias, enabled))
	return a, sanitize(err)
}
func (s *Service) Credentials(ctx context.Context, id string) (Credentials, error) {
	var body, nonce []byte
	var disabled *time.Time
	err := s.pool.QueryRow(ctx, "SELECT a.disabled_at,c.ciphertext,c.nonce FROM bifrost.accounts a JOIN bifrost.account_credentials c ON c.account_id=a.id AND c.active WHERE a.id=$1", id).Scan(&disabled, &body, &nonce)
	if err != nil {
		return Credentials{}, sanitize(err)
	}
	if disabled != nil {
		return Credentials{}, ErrDisabled
	}
	return s.decrypt(id, body, nonce)
}
