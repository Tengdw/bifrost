package binance

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

type fixture map[string]string

func (f fixture) Do(_ context.Context, _ string, path string, q url.Values, out any) error {
	return json.Unmarshal([]byte(f[path+q.Get("current")]), out)
}
func TestSpotFreeAndLocked(t *testing.T) {
	c := Collector{Client: fixture{"/api/v3/account": `{"balances":[{"asset":"BTC","free":"0.08","locked":"0.02"}]}`}, Now: func() time.Time { return time.Unix(1000, 0) }}
	r, e := c.Collect(context.Background(), "spot")
	if e != nil || r.Items[0].Quantity.String() != "0.1" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestFundingStockIsCountedOnceAndMismatchRemainsIncomplete(t *testing.T) {
	f := fixture{
		"/sapi/v1/equity/market/tokenized-assets": `[]`,
		"/sapi/v1/asset/get-funding-asset":        `[{"asset":"USD","free":"800","locked":"0","freeze":"0","withdrawing":"0"},{"asset":"EQ_MU","free":"2","locked":"0","freeze":"0","withdrawing":"0"}]`,
		"/sapi/v1/asset/wallet/balance":           `[{"walletName":"Funding","assetBalances":[{"asset":"EQ_MU","free":"2","locked":"0","freeze":"0","withdrawing":"0"}]}]`,
		"/sapi/v1/equity/market/exchangeInfo":     `{"symbols":[{"symbol":"MU"}]}`}
	at := time.Unix(1000, 0)
	book := FreezePrices(map[string]Quote{"us_equity:MU": {Price: d("100"), Source: "synthetic", PricedAt: at, MarketOpen: true}}, nil, nil)
	c := Collector{Client: f, Prices: book, Now: func() time.Time { return at }}
	r, e := c.Collect(context.Background(), "funding")
	if e != nil || len(r.Items) != 2 || !r.Complete {
		t.Fatalf("%+v %v", r, e)
	}
	sum := d("0")
	for _, i := range r.Items {
		sum = sum.Add(*i.Value())
	}
	if sum.String() != "1000" {
		t.Fatal(sum)
	}
	f["/sapi/v1/asset/get-funding-asset"] = `[{"asset":"EQ_MU","free":"3","locked":"0"}]`
	r, e = c.Collect(context.Background(), "funding")
	if e != nil || r.Complete || len(r.Items) != 1 || r.Items[0].Quantity.String() != "2" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestEarnPaginatesWithoutCountingRewards(t *testing.T) {
	c := Collector{Client: fixture{
		"/sapi/v1/simple-earn/flexible/position1": `{"rows":[{"productId":"F1","asset":"USDT","totalAmount":"300","cumulativeTotalRewards":"7"}],"total":2}`,
		"/sapi/v1/simple-earn/flexible/position2": `{"rows":[{"productId":"F2","asset":"USDT","totalAmount":"0"}],"total":2}`,
		"/sapi/v1/simple-earn/locked/position1":   `{"rows":[{"positionId":"12","asset":"USDT","amount":"200","status":"HOLDING"}],"total":1}`}}
	r, e := c.Collect(context.Background(), "earn")
	if e != nil {
		t.Fatal(e)
	}
	sum := d("0")
	for _, i := range r.Items {
		sum = sum.Add(i.Quantity)
	}
	if sum.String() != "500" || len(r.Items) != 3 {
		t.Fatalf("%+v", r)
	}
	c.Client = fixture{"/sapi/v1/simple-earn/flexible/position1": `{"rows":[{"productId":"F1","asset":"USDT","totalAmount":"300"}],"total":2}`}
	if _, e = c.Collect(context.Background(), "earn"); e == nil {
		t.Fatal("incomplete pagination accepted")
	}
}
func TestFuturesNotionalNeverEntersEquity(t *testing.T) {
	f := fixture{"/fapi/v1/accountConfig": `{"multiAssetsMargin":false}`, "/fapi/v3/account": `{"assets":[{"asset":"USDT","marginBalance":"2200"}]}`, "/fapi/v3/positionRisk": `[{"symbol":"BTCUSDT","positionSide":"LONG","positionAmt":"1","notional":"20000"}]`}
	c := Collector{Client: f}
	r, e := c.Collect(context.Background(), "usdm")
	if e != nil || len(r.Items) != 1 || r.Items[0].Quantity.String() != "2200" || r.Positions[0].Notional != "20000" {
		t.Fatalf("%+v %v", r, e)
	}
	f["/fapi/v1/accountConfig"] = `{"multiAssetsMargin":true}`
	f["/fapi/v3/account"] = `{"totalMarginBalance":"2195.6","assets":[{"asset":"USDT","marginBalance":"2200"}]}`
	r, e = c.Collect(context.Background(), "usdm")
	if e != nil || len(r.Items) != 1 || r.Items[0].Asset != "USD" || r.Items[0].Value().String() != "2195.6" {
		t.Fatalf("%+v %v", r, e)
	}
	f["/fapi/v1/accountConfig"] = `{}`
	if _, e = c.Collect(context.Background(), "usdm"); e == nil {
		t.Fatal("unknown mode accepted")
	}
}
func TestMissingSpotQuantityNeverBecomesZero(t *testing.T) {
	c := Collector{Client: fixture{"/api/v3/account": `{"balances":[{"asset":"BTC","free":"0"}]}`}}
	if _, err := c.Collect(context.Background(), "spot"); err == nil {
		t.Fatal("missing locked accepted as zero")
	}
}

func TestEquityQuoteRequiresVerifiedSession(t *testing.T) {
	at := time.Unix(1000, 0)
	c := Collector{Client: fixture{"/sapi/v1/equity/market/quote": `{"symbol":"MU","bidPrice":"99","askPrice":"101"}`}}
	if _, ok := c.equityQuote(context.Background(), "MU", at); ok {
		t.Fatal("missing calendar accepted")
	}
	c.MarketOpen = func(context.Context, string, time.Time) (bool, error) { return true, nil }
	q, ok := c.equityQuote(context.Background(), "MU", at)
	if !ok || q.Price.String() != "100" || q.BidPrice.String() != "99" {
		t.Fatal(q)
	}
	c.MarketOpen = func(context.Context, string, time.Time) (bool, error) { return false, nil }
	if _, ok := c.equityQuote(context.Background(), "MU", at); ok {
		t.Fatal("intraday quote accepted during closed market")
	}
}
func TestTokenizedBalanceKeepsOriginalWalletAndDoesNotBecomeShares(t *testing.T) {
	c := Collector{Client: fixture{
		"/sapi/v1/asset/get-funding-asset":        `[{"asset":"MUB","free":"5","locked":"0","freeze":"0","withdrawing":"0"}]`,
		"/sapi/v1/asset/wallet/balance":           `[{"walletName":"Funding","assetBalances":[]}]`,
		"/sapi/v1/equity/market/exchangeInfo":     `{"symbols":[{"symbol":"MU"}]}`,
		"/sapi/v1/equity/market/tokenized-assets": `[{"assetCode":"MUB","underlyingEquitySymbol":"MU","multiplier":"2","multiplierValid":true}]`}}
	r, err := c.Collect(context.Background(), "funding")
	if err != nil || r.Complete || len(r.Items) != 1 || r.Items[0].Asset != "MUB" || r.Items[0].Class != "other" || r.Items[0].Quantity.String() != "5" {
		t.Fatalf("%+v %v", r, err)
	}
}
