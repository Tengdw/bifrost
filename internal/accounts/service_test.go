package accounts

import (
	"bytes"
	"testing"
)

func TestCredentialEncryptionBindsAccount(t *testing.T) {
	s, err := New(nil, bytes.Repeat([]byte{1}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Credentials{APIKey: "synthetic-key", APISecret: "synthetic-secret"}
	cipher, nonce, err := s.encrypt("account-a", want)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, []byte(want.APISecret)) {
		t.Fatal("plaintext credential")
	}
	got, err := s.decrypt("account-a", cipher, nonce)
	if err != nil || got != want {
		t.Fatalf("round trip %v", err)
	}
	if _, err = s.decrypt("account-b", cipher, nonce); err == nil {
		t.Fatal("wrong ownership accepted")
	}
}
