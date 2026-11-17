package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

const saltBytes = 16
const digestBytes = 32
const sessionLifetime = 7 * 24 * time.Hour
const recentLogin = 10 * time.Minute

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`)
var errUnauthorized = errors.New("authentication required")

type admin struct {
	ID          []byte
	Name        string
	Credentials []webauthn.Credential
}

func (a admin) WebAuthnID() []byte                         { return a.ID }
func (a admin) WebAuthnName() string                       { return a.Name }
func (a admin) WebAuthnDisplayName() string                { return a.Name }
func (a admin) WebAuthnCredentials() []webauthn.Credential { return a.Credentials }

func AdminExists(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, "select exists(select 1 from bifrost.auth_admin)").Scan(&exists)
	return exists, err
}

// SetAdmin 创建管理员，或在主机发起恢复时更换账号标识并撤销旧凭据。
func SetAdmin(ctx context.Context, pool *pgxpool.Pool, name, password string, recover bool) error {
	if !usernamePattern.MatchString(name) {
		return errors.New("用户名须为 1 至 64 个英文字母、数字或 . _ @ -，并以字母或数字开头")
	}
	if len(password) < 12 || len(password) > 1024 {
		return errors.New("密码须为 12 至 1024 字节")
	}
	salt := make([]byte, saltBytes)
	id := make([]byte, digestBytes)
	rand.Read(salt)
	rand.Read(id)
	pwhash := append(salt, passwordDigest(password, salt)...)

	return withAuthLock(ctx, pool, func(tx pgx.Tx) error {
		if !recover {
			_, err := tx.Exec(ctx, "insert into bifrost.auth_admin(user_id,username,password_hash) values($1,$2,$3) on conflict(singleton) do nothing", id, name, pwhash)
			return err
		}
		tag, err := tx.Exec(ctx, "update bifrost.auth_admin set user_id=$1, username=$2, password_hash=$3 where singleton", id, name, pwhash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("管理员尚未初始化")
		}
		if _, err = tx.Exec(ctx, "delete from bifrost.auth_sessions"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "delete from bifrost.auth_passkeys")
		return err
	})
}

func passwordDigest(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, digestBytes)
}

func checkPassword(encoded []byte, password string) bool {
	validLength := len(encoded) == saltBytes+digestBytes
	if !validLength {
		encoded = make([]byte, saltBytes+digestBytes)
	}
	digest := passwordDigest(password, encoded[:saltBytes])
	return subtle.ConstantTimeCompare(digest, encoded[saltBytes:]) == 1 && validLength
}

// 认证写入量低。用同一事务锁串行化新增凭据、登录及恢复，避免恢复后写回过期状态。
// Authentication transactions must use a different key from the long-lived scheduler lock.
const authLockID = 1578283633

func withAuthLock(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock($1)", authLockID); err != nil {
			return err
		}
		return fn(tx)
	})
}

type passkeyInfo struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func listPasskeys(ctx context.Context, pool *pgxpool.Pool, user []byte) ([]passkeyInfo, error) {
	rows, err := pool.Query(ctx, "select credential_id, created_at, last_used_at from bifrost.auth_passkeys where user_id=$1 order by created_at,credential_id", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []passkeyInfo{}
	for rows.Next() {
		var item passkeyInfo
		var id []byte
		if err := rows.Scan(&id, &item.CreatedAt, &item.LastUsedAt); err != nil {
			return nil, err
		}
		item.ID = tokenEncoding.EncodeToString(id)
		items = append(items, item)
	}
	return items, rows.Err()
}

type authDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadAdmin(ctx context.Context, db authDB, userID []byte) (admin, error) {
	a := admin{ID: userID}
	if err := db.QueryRow(ctx, "select username from bifrost.auth_admin where user_id=$1", userID).Scan(&a.Name); err != nil {
		return a, err
	}
	rows, err := db.Query(ctx, "select credential from bifrost.auth_passkeys where user_id=$1", userID)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var c webauthn.Credential
		if err := rows.Scan(&c); err != nil {
			return a, fmt.Errorf("read passkey: %w", err)
		}
		a.Credentials = append(a.Credentials, c)
	}
	return a, rows.Err()
}

func updatePasskey(ctx context.Context, tx pgx.Tx, user []byte, credential *webauthn.Credential) error {
	payload, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "update bifrost.auth_passkeys set credential=$1, last_used_at=now() where user_id=$2 and credential_id=$3", payload, user, credential.ID)
	if err == nil && tag.RowsAffected() == 0 {
		return errUnauthorized
	}
	return err
}
