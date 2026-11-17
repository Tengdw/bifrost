package exchange

import (
	"bifrost/internal/accounts"
	"bifrost/internal/binance"
	"context"
	"encoding/json"
	"errors"
	"github.com/shopspring/decimal"
	"net/url"
	"strings"
	"time"
)

// Synthetic accepts only the documented fictional identities; no network calls.
type Synthetic struct{ B bool }

func (Synthetic) Verify(_ context.Context, c accounts.Credentials) (string, error) {
	if c.APISecret != "fixture-only" {
		return "", errors.New("invalid synthetic credentials")
	}
	switch c.APIKey {
	case "demo-a", "demo-a-rotated":
		return "fixture-a", nil
	case "demo-b":
		return "fixture-b", nil
	}
	return "", errors.New("unknown synthetic account")
}
func (Synthetic) Prices(at time.Time) *binance.PriceBook {
	fx := binance.Quote{Price: decimal.RequireFromString("0.998"), Path: "USDT/USD", Source: "synthetic", PricedAt: at}
	return binance.FreezePrices(map[string]binance.Quote{"BTC": {Price: decimal.NewFromInt(60000), Path: "BTC/USD", Source: "synthetic", PricedAt: at}}, nil, &fx)
}
func (s Synthetic) Do(_ context.Context, method, path string, q url.Values, out any) error {
	if endpoints[path] != method || method == "" {
		return errors.New("unsupported synthetic request")
	}
	data := ""
	switch path {
	case "/api/v3/account":
		data = `{"balances":[{"asset":"BTC","free":"0.08","locked":"0.02"}]}`
		if s.B {
			data = `{"balances":[{"asset":"USD","free":"250","locked":"0"}]}`
		}
	case "/sapi/v1/asset/get-funding-asset":
		data = `[{"asset":"USDT","free":"100","locked":"0","freeze":"0","withdrawing":"0"}]`
		if s.B {
			data = "[]"
		}
	case "/sapi/v1/asset/wallet/balance":
		data = `[{"walletName":"Funding","assetBalances":[]}]`
	case "/sapi/v1/equity/market/exchangeInfo":
		data = `{"symbols":[]}`
	case "/sapi/v1/equity/market/tokenized-assets":
		data = `[]`
	case "/sapi/v1/simple-earn/flexible/position", "/sapi/v1/simple-earn/locked/position":
		data = `{"rows":[],"total":0}`
		if !s.B {
			if strings.Contains(path, "flexible") {
				data = `{"rows":[{"asset":"USDT","productId":"F1","totalAmount":"300","cumulativeTotalRewards":"7"}],"total":1}`
			} else {
				data = `{"rows":[{"asset":"USDT","positionId":"12","amount":"200","status":"HOLDING"}],"total":1}`
			}
		}
	case "/fapi/v1/accountConfig":
		data = `{"multiAssetsMargin":false}`
	case "/fapi/v3/account":
		data = `{"assets":[{"asset":"USDT","marginBalance":"2200"}]}`
		if s.B {
			data = `{"assets":[]}`
		}
	case "/fapi/v3/positionRisk":
		data = `[{"symbol":"BTCUSDT","positionSide":"BOTH","positionAmt":"1","entryPrice":"18000","markPrice":"20000","unRealizedProfit":"200","notional":"20000","marginAsset":"USDT"}]`
		if s.B {
			data = "[]"
		}
	default:
		return errors.New("unsupported fixture")
	}
	return json.Unmarshal([]byte(data), out)
}
