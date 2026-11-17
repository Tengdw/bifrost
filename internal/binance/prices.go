package binance

import (
	"time"

	"github.com/shopspring/decimal"
)

type Quote struct {
	BidPrice      *decimal.Decimal `json:"bidPrice,omitempty"`
	AskPrice      *decimal.Decimal `json:"askPrice,omitempty"`
	Price         decimal.Decimal  `json:"price"`
	Source        string           `json:"source"`
	Path          string           `json:"path"`
	PricedAt      time.Time        `json:"pricedAt"`
	TradingDay    string           `json:"tradingDay,omitempty"`
	MarketOpen    bool             `json:"marketOpen,omitempty"`
	VerifiedClose bool             `json:"verifiedClose,omitempty"`
}

// PriceBook 的输入在批次开始时复制；同批汇率固定，不因后续行情更新而变化。
type PriceBook struct {
	direct map[string]Quote
	usdt   map[string]Quote
	fx     *Quote
}

func FreezePrices(direct, usdt map[string]Quote, fx *Quote) *PriceBook {
	p := &PriceBook{direct: map[string]Quote{}, usdt: map[string]Quote{}}
	for k, v := range direct {
		p.direct[k] = v
	}
	for k, v := range usdt {
		p.usdt[k] = v
	}
	if fx != nil {
		v := *fx
		p.fx = &v
	}
	return p
}
func valid(q Quote, at time.Time) bool {
	return q.Price.IsPositive() && q.Source != "" && !q.PricedAt.IsZero() && !q.PricedAt.After(at) && at.Sub(q.PricedAt) <= 5*time.Minute
}
func (p *PriceBook) Resolve(asset, class string, at time.Time) (Quote, bool) {
	if asset == "USD" {
		return Quote{Price: decimal.NewFromInt(1), Source: "USD", Path: "USD", PricedAt: at}, true
	}
	if p == nil {
		return Quote{}, false
	}
	key := asset
	if class == "us_equity" {
		key = "us_equity:" + asset
	}
	if q, ok := p.direct[key]; ok {
		if class == "us_equity" && q.VerifiedClose && q.TradingDay != "" && !q.PricedAt.After(at) && q.Price.IsPositive() {
			return q, true
		}
		if valid(q, at) && (class != "us_equity" || q.MarketOpen) {
			return q, true
		}
	}
	if class == "us_equity" || p.fx == nil || !valid(*p.fx, at) {
		return Quote{}, false
	}
	if asset == "USDT" {
		return *p.fx, true
	}
	q, ok := p.usdt[asset]
	if !ok || !valid(q, at) {
		return Quote{}, false
	}
	q.Price = q.Price.Mul(p.fx.Price)
	q.Source += " × " + p.fx.Source
	q.Path = asset + "/USDT × USDT/USD"
	if p.fx.PricedAt.Before(q.PricedAt) {
		q.PricedAt = p.fx.PricedAt
	}
	return q, true
}
func EquityMidpoint(bid, ask decimal.Decimal, at time.Time, source string) (Quote, bool) {
	if !bid.IsPositive() || !ask.IsPositive() || bid.GreaterThan(ask) {
		return Quote{}, false
	}
	return Quote{BidPrice: &bid, AskPrice: &ask, Price: bid.Add(ask).Div(decimal.NewFromInt(2)), Source: source, Path: "USD bid/ask midpoint", PricedAt: at}, true
}
