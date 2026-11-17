package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestVerifyRejectsWritePermission(t *testing.T) {
	for _, tc := range []struct {
		write bool
		uid   string
		want  bool
	}{{false, "12345", true}, {true, "12345", false}, {false, "", false}} {
		t.Run(tc.uid, func(t *testing.T) {
			c := Credentials{"synthetic-key", "synthetic-secret"}
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				var body string
				switch r.URL.Path {
				case "/api/v3/time":
					body = `{"serverTime":1770000000000}`
				case "/sapi/v1/account/apiRestrictions":
					q := r.URL.Query()
					signature := q.Get("signature")
					q.Del("signature")
					mac := hmac.New(sha256.New, []byte(c.APISecret))
					mac.Write([]byte(q.Encode()))
					if signature != hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("X-MBX-APIKEY") != c.APIKey {
						t.Fatal("signature verification failed")
					}
					body = `{"enableReading":true,"enableWithdrawals":false,"enableInternalTransfer":false,"enableMargin":false,"enableFutures":false,"permitsUniversalTransfer":false,"enableVanillaOptions":false,"enableSpotAndMarginTrading":false}`
					if tc.write {
						body = strings.Replace(body, `"enableWithdrawals":false`, `"enableWithdrawals":true`, 1)
					}
				case "/api/v3/account":
					body = `{}`
					if tc.uid != "" {
						body = `{"uid":` + tc.uid + `}`
					}
				default:
					t.Fatal("unexpected endpoint")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			uid, err := NewBinanceVerifier(client).Verify(context.Background(), c)
			if tc.want {
				if err != nil || uid != tc.uid {
					t.Fatal(uid, err)
				}
			} else if err == nil {
				t.Fatal("unsafe or unknown identity accepted")
			}
		})
	}
}
