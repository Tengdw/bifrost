package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type BinanceVerifier struct{ Client *http.Client }

func NewBinanceVerifier(client *http.Client) *BinanceVerifier {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &BinanceVerifier{Client: &copyClient}
}

// Verify 使用币安现货账户的稳定 UID；写权限必须全部关闭。
// https://developers.binance.com/en/docs/catalog/core-trading-wallet/api/rest-api/account
func (v *BinanceVerifier) Verify(ctx context.Context, c Credentials) (string, error) {
	var clock struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := v.request(ctx, "/api/v3/time", nil, c, false, &clock); err != nil || clock.ServerTime <= 0 {
		return "", ErrUnavailable
	}
	params := url.Values{"timestamp": {strconv.FormatInt(clock.ServerTime, 10)}, "recvWindow": {"5000"}}
	var restrictions map[string]json.RawMessage
	if err := v.request(ctx, "/sapi/v1/account/apiRestrictions", params, c, true, &restrictions); err != nil {
		return "", ErrUnavailable
	}
	var reading bool
	if err := json.Unmarshal(restrictions["enableReading"], &reading); err != nil || !reading {
		return "", ErrInvalid
	}
	// 只接入已核实关闭的写权限；新增未知 enable* 权限为 true 也拒绝。
	readFlags := map[string]bool{"enableReading": true, "enableFixReadOnly": true}
	required := []string{"enableWithdrawals", "enableInternalTransfer", "enableMargin", "enableFutures", "permitsUniversalTransfer", "enableVanillaOptions", "enableSpotAndMarginTrading"}
	for _, name := range required {
		var enabled *bool
		if err := json.Unmarshal(restrictions[name], &enabled); err != nil || enabled == nil || *enabled {
			return "", ErrInvalid
		}
	}
	for name, raw := range restrictions {
		if readFlags[name] {
			continue
		}
		if len(name) > 6 && name[:6] == "enable" {
			var flag bool
			if json.Unmarshal(raw, &flag) == nil && flag {
				return "", ErrInvalid
			}
		}
	}
	var account struct {
		UID json.Number `json:"uid"`
	}
	if err := v.request(ctx, "/api/v3/account", params, c, true, &account); err != nil {
		return "", ErrUnavailable
	}
	if account.UID == "" || account.UID == "0" {
		return "", ErrInvalid
	}
	for _, r := range account.UID.String() {
		if r < '0' || r > '9' {
			return "", ErrInvalid
		}
	}
	return account.UID.String(), nil
}
func (v *BinanceVerifier) request(ctx context.Context, path string, params url.Values, c Credentials, signed bool, out any) error {
	if params == nil {
		params = url.Values{}
	}
	query := params.Encode()
	if signed {
		mac := hmac.New(sha256.New, []byte(c.APISecret))
		_, _ = mac.Write([]byte(query))
		query += "&signature=" + hex.EncodeToString(mac.Sum(nil))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.binance.com"+path+"?"+query, nil)
	if err != nil {
		return ErrUnavailable
	}
	if signed {
		req.Header.Set("X-MBX-APIKEY", c.APIKey)
	}
	response, err := v.Client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ErrUnavailable
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return ErrUnavailable
	}
	return nil
}
