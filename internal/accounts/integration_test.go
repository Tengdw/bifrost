package accounts

import (
	"bifrost/db/migrations"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

type testVerifier struct{ uid string }

func (v testVerifier) Verify(_ context.Context, c Credentials) (string, error) {
	if c.APIKey == "different" {
		return "synthetic-other", nil
	}
	return v.uid, nil
}
func TestBindRotateDisablePersisted(t *testing.T) {
	url := os.Getenv("BIFROST_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated postgres required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	service, err := New(pool, bytes.Repeat([]byte{1}, 32), testVerifier{uid: "synthetic-" + uuid()})
	if err != nil {
		t.Fatal(err)
	}
	c := Credentials{"synthetic", "secret-1"}
	a, err := service.Bind(ctx, "测试账号", c)
	if err != nil {
		t.Fatal(err)
	}
	c.APISecret = "secret-2"
	same, err := service.Bind(ctx, "改名", c)
	if err != nil || same.ID != a.ID {
		t.Fatal(same, err)
	}
	if _, err = service.Rotate(ctx, a.ID, Credentials{"different", "secret-3"}); !errors.Is(err, ErrConflict) {
		t.Fatal("accepted another UID", err)
	}
	got, err := service.Credentials(ctx, a.ID)
	if err != nil || got != c {
		t.Fatal("rotation did not preserve active credential", err)
	}
	enabled := false
	if _, err = service.Update(ctx, a.ID, nil, &enabled); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Credentials(ctx, a.ID); !errors.Is(err, ErrDisabled) {
		t.Fatal("disabled account credentials accepted", err)
	}
	list, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a2 := range list {
		if a2.ID == a.ID {
			found = a2.DisabledAt != nil
		}
	}
	if !found {
		t.Fatal("disabled history disappeared")
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(c.APISecret)) || bytes.Contains(encoded, []byte("apiKey")) {
		t.Fatal("secret in account response")
	}
	var cipher []byte
	if err = pool.QueryRow(ctx, "SELECT ciphertext FROM bifrost.account_credentials WHERE account_id=$1 AND active", a.ID).Scan(&cipher); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, []byte(c.APISecret)) {
		t.Fatal("plaintext persisted")
	}
}
