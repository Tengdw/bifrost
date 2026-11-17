package exchange

import (
	"bifrost/internal/accounts"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestEquityMarketDataRequiresKeyWithoutSignature(t *testing.T) {
	r := Reader{Credentials: accounts.Credentials{APIKey: "synthetic-only", APISecret: "fixture-only"}, HTTP: &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-MBX-APIKEY") != "synthetic-only" || req.URL.Query().Has("signature") || req.URL.Host != "api.binance.com" {
			t.Fatal("incorrect market data authentication")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}}
	for _, path := range []string{"/sapi/v1/equity/market/exchangeInfo", "/sapi/v1/equity/market/tokenized-assets", "/sapi/v1/equity/market/quote"} {
		var out any
		if err := r.Do(context.Background(), "GET", path, nil, &out); err != nil {
			t.Fatal(err)
		}
	}
}
