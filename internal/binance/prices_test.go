package binance

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func TestFrozenUSDConversionAndExpiry(t *testing.T) {
	at := time.Unix(1000, 0)
	fx := Quote{Price: d("0.998"), Source: "syntheticFX", Path: "USDT/USD", PricedAt: at}
	direct := map[string]Quote{"BTC": {Price: d("60000"), Source: "syntheticUSD", PricedAt: at}}
	book := FreezePrices(direct, nil, &fx)
	fx.Price = d("1")
	direct["BTC"] = Quote{Price: d("1")}
	q, ok := book.Resolve("USDT", "other", at)
	if !ok || q.Price.Mul(d("100")).String() != "99.8" {
		t.Fatal(q)
	}
	q, ok = book.Resolve("BTC", "other", at)
	if !ok || q.Price.String() != "60000" {
		t.Fatal(q)
	}
	if _, ok = book.Resolve("BTC", "other", at.Add(6*time.Minute)); ok {
		t.Fatal("expired price accepted")
	}
	var empty *PriceBook
	if _, ok = empty.Resolve("USDT", "other", at); ok {
		t.Fatal("implicit stablecoin parity")
	}
}
func TestStockMidpointAndVerifiedClose(t *testing.T) {
	at := time.Unix(1000, 0)
	if _, ok := EquityMidpoint(d("102"), d("98"), at, "synthetic"); ok {
		t.Fatal("inverted market")
	}
	q, ok := EquityMidpoint(d("99"), d("101"), at, "synthetic")
	if !ok || q.Price.String() != "100" {
		t.Fatal(q)
	}
	book := FreezePrices(map[string]Quote{"us_equity:MU": q}, nil, nil)
	if _, ok = book.Resolve("MU", "us_equity", at.Add(6*time.Minute)); ok {
		t.Fatal("unverified close accepted")
	}
	q.VerifiedClose = true
	q.TradingDay = "1970-01-01"
	book = FreezePrices(map[string]Quote{"us_equity:MU": q}, nil, nil)
	if _, ok = book.Resolve("MU", "us_equity", at.Add(6*time.Minute)); !ok {
		t.Fatal("verified close unavailable")
	}
}

func TestAssetClassNamespacesDoNotShareTicker(t *testing.T) {
	at := time.Unix(1000, 0)
	p := FreezePrices(map[string]Quote{"MU": {Price: d("5"), Source: "crypto", PricedAt: at}}, nil, nil)
	if _, ok := p.Resolve("MU", "us_equity", at); ok {
		t.Fatal("crypto ticker priced as equity")
	}
	q := Quote{Price: d("100"), Source: "equity", PricedAt: at, MarketOpen: true}
	p = FreezePrices(map[string]Quote{"us_equity:MU": q}, nil, nil)
	if _, ok := p.Resolve("MU", "other", at); ok {
		t.Fatal("equity ticker priced as crypto")
	}
	if _, ok := p.Resolve("MU", "us_equity", at); !ok {
		t.Fatal("namespaced equity price unavailable")
	}
	q.MarketOpen = false
	p = FreezePrices(map[string]Quote{"us_equity:MU": q}, nil, nil)
	if _, ok := p.Resolve("MU", "us_equity", at); ok {
		t.Fatal("unknown session accepted")
	}
}
