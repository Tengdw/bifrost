// Package exchange limits outbound traffic to the supported read-only endpoints.
package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bifrost/internal/accounts"
	sdk "github.com/binance/binance-connector-go"
)

type Reader struct {
	Credentials  accounts.Credentials
	HTTP         *http.Client
	offsetMillis int64
	clockReady   bool
}

var endpoints = map[string]string{
	"/api/v3/account":                         "GET",
	"/sapi/v1/asset/get-funding-asset":        "POST",
	"/sapi/v1/asset/wallet/balance":           "GET",
	"/sapi/v1/equity/market/exchangeInfo":     "GET",
	"/sapi/v1/equity/market/quote":            "GET",
	"/sapi/v1/equity/market/tokenized-assets": "GET",
	"/sapi/v1/simple-earn/flexible/position":  "GET",
	"/sapi/v1/simple-earn/locked/position":    "GET",
	"/fapi/v1/accountConfig":                  "GET",
	"/fapi/v3/account":                        "GET",
	"/fapi/v3/positionRisk":                   "GET",
}

func Client() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// 每个 Reader 只属于一次钱包采集，按交易所时钟校准签名，不记录签名请求。
func (r *Reader) calibrate(ctx context.Context, futures bool) error {
	if r.clockReady {
		return nil
	}
	addr := "https://api.binance.com/api/v3/time"
	if futures {
		addr = "https://fapi.binance.com/fapi/v1/time"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return errors.New("invalid clock request")
	}
	before := time.Now()
	res, err := r.HTTP.Do(req)
	if err != nil {
		return errors.New("exchange clock unavailable")
	}
	defer res.Body.Close()
	var clock struct{ ServerTime int64 }
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 2048)).Decode(&clock) != nil || clock.ServerTime <= 0 {
		return errors.New("invalid exchange clock")
	}
	r.offsetMillis = before.Add(time.Since(before)/2).UnixMilli() - clock.ServerTime
	r.clockReady = true
	return nil
}

func (r *Reader) Do(ctx context.Context, method, path string, args url.Values, out any) error {
	if endpoints[path] != method || method == "" {
		return errors.New("unsupported read endpoint")
	}
	keyOnly := strings.HasPrefix(path, "/sapi/v1/equity/market/")
	if !keyOnly {
		if err := r.calibrate(ctx, strings.HasPrefix(path, "/fapi/")); err != nil {
			return err
		}
	}
	if path == "/api/v3/account" {
		c := sdk.NewClient(r.Credentials.APIKey, r.Credentials.APISecret)
		c.TimeOffset = r.offsetMillis
		c.HTTPClient = r.HTTP
		data, err := c.NewGetAccountService().Do(ctx)
		if err != nil {
			return errors.New("spot balance unavailable")
		}
		raw, err := json.Marshal(data)
		if err != nil {
			return errors.New("invalid spot response")
		}
		return json.Unmarshal(raw, out)
	}
	base := "https://api.binance.com"
	if strings.HasPrefix(path, "/fapi/") {
		base = "https://fapi.binance.com"
	}
	q := url.Values{}
	for k, vs := range args {
		for _, v := range vs {
			q.Add(k, v)
		}
	}

	if !keyOnly {
		q.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli()-r.offsetMillis, 10))
		q.Set("recvWindow", "5000")
	}
	query := q.Encode()
	if !keyOnly {
		mac := hmac.New(sha256.New, []byte(r.Credentials.APISecret))
		mac.Write([]byte(query))
		query += "&signature=" + hex.EncodeToString(mac.Sum(nil))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path+"?"+query, nil)
	if err != nil {
		return errors.New("invalid read request")
	}
	req.Header.Set("X-MBX-APIKEY", r.Credentials.APIKey)
	res, err := r.HTTP.Do(req)
	if err != nil {
		return errors.New("exchange unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("exchange read failed (HTTP %d)", res.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out); err != nil {
		return errors.New("invalid exchange response")
	}
	return nil
}
