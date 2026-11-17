package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"bifrost/internal/holdings"

	"github.com/shopspring/decimal"
)

type Requester interface {
	Do(context.Context, string, string, url.Values, any) error
}
type Collector struct {
	Client     Requester
	Prices     *PriceBook
	Now        func() time.Time
	BStocks    map[string]bool
	MarketOpen func(context.Context, string, time.Time) (bool, error)
}
type Result struct {
	Items      []holdings.Item
	ObservedAt time.Time
	Complete   bool
	Reasons    []string
	Positions  []Position
	Quotes     map[string]Quote
	Components map[string]map[string]string
	blocked    map[string]bool
	Mode       string
}
type Position struct {
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Quantity    string `json:"quantity"`
	MarginAsset string `json:"marginAsset"`
	EntryPrice  string `json:"entryPrice"`
	MarkPrice   string `json:"markPrice"`
	Unrealized  string `json:"unrealized"`
	Notional    string `json:"notional"`
}

func (c Collector) Collect(ctx context.Context, wallet string) (Result, error) {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now()
	}
	r := Result{ObservedAt: now, Complete: true, Reasons: []string{}, Items: []holdings.Item{}, Positions: []Position{}, Quotes: map[string]Quote{}, Components: map[string]map[string]string{}, blocked: map[string]bool{}}
	var err error
	switch wallet {
	case "spot":
		err = c.spot(ctx, &r)
	case "funding":
		err = c.funding(ctx, &r)
	case "earn":
		err = c.earn(ctx, &r)
	case "usdm":
		err = c.futures(ctx, &r)
	default:
		err = fmt.Errorf("unsupported wallet")
	}
	if err != nil {
		r.Complete = false
		return r, err
	}
	for j := range r.Items {
		i := &r.Items[j]
		if r.blocked[i.Key] || i.Quantity.IsZero() {
			continue
		}
		quote, ok := c.Prices.Resolve(i.Asset, i.Class, r.ObservedAt)
		if i.Symbol != nil {
			quote, ok = c.equityQuote(ctx, *i.Symbol, r.ObservedAt)
		}
		if ok {
			i.Price = &quote.Price
			r.Quotes[i.Key] = quote
		} else {
			r.incomplete(i.Asset + " 缺少合格 USD 报价")
		}
	}
	return r, nil
}
func (r *Result) incomplete(reason string) { r.Complete = false; r.Reasons = append(r.Reasons, reason) }
func (r *Result) item(wallet, key, asset, class, qty string) error {
	if key == "" || asset == "" {
		return fmt.Errorf("missing holding identity")
	}
	n, err := decimal.NewFromString(qty)
	if err != nil {
		return fmt.Errorf("invalid %s quantity", wallet)
	}
	if n.IsZero() {
		return nil
	}
	r.Items = append(r.Items, holdings.Item{Wallet: wallet, Key: key, Asset: asset, Class: class, Quantity: n})
	return nil
}
func (r *Result) block(key, reason string) {
	r.blocked[key] = true
	r.incomplete(reason)
	if r.Components[key] == nil {
		r.Components[key] = map[string]string{}
	}
	r.Components[key]["valuationBlockedReason"] = reason
}
func num(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(s)
}
func add(a, b string) (string, error) {
	x, e := decimal.NewFromString(a)
	if e != nil {
		return "", e
	}
	y, e := decimal.NewFromString(b)
	return x.Add(y).String(), e
}
func (c Collector) spot(ctx context.Context, r *Result) error {
	var body struct {
		Balances *[]struct{ Asset, Free, Locked string }
	}
	if e := c.Client.Do(ctx, "GET", "/api/v3/account", nil, &body); e != nil {
		return e
	}
	if body.Balances == nil {
		return fmt.Errorf("spot balances missing")
	}
	for _, b := range *body.Balances {
		q, e := add(b.Free, b.Locked)
		if e != nil {
			return e
		}
		if e = r.item("spot", b.Asset, b.Asset, "other", q); e != nil {
			return e
		}
		r.Components[b.Asset] = map[string]string{"free": b.Free, "locked": b.Locked}
	}
	return nil
}

// positionID 不经过 float64，保留交易所的长整数原文。
type positionID string

func (p *positionID) UnmarshalJSON(raw []byte) error {
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	} else {
		value = string(raw)
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(value) {
		return fmt.Errorf("invalid position ID")
	}
	*p = positionID(value)
	return nil
}

type fundingRow struct{ Asset, Free, Locked, Freeze, Withdrawing string }

func (c Collector) funding(ctx context.Context, r *Result) error {
	var rows []fundingRow
	if e := c.Client.Do(ctx, "POST", "/sapi/v1/asset/get-funding-asset", nil, &rows); e != nil {
		return e
	}
	tokens := map[string]bool{}
	for code, yes := range c.BStocks {
		tokens[code] = yes
	}
	var tokenRows []struct{ AssetCode string }
	if e := c.Client.Do(ctx, "GET", "/sapi/v1/equity/market/tokenized-assets", nil, &tokenRows); e == nil {
		for _, row := range tokenRows {
			if row.AssetCode != "" {
				tokens[row.AssetCode] = true
			}
		}
	} else {
		r.incomplete("bStocks目录不可用，资产映射待核对")
	}
	var wallets []struct {
		WalletName    string
		AssetBalances *[]fundingRow
	}
	if e := c.Client.Do(ctx, "GET", "/sapi/v1/asset/wallet/balance", url.Values{"needBalanceDetail": {"true"}}, &wallets); e != nil {
		return e
	}
	var directory struct{ Symbols []struct{ Symbol string } }
	if e := c.Client.Do(ctx, "GET", "/sapi/v1/equity/market/exchangeInfo", nil, &directory); e != nil {
		r.incomplete("股票目录读取失败")
	}
	known := map[string]bool{}
	for _, s := range directory.Symbols {
		known[s.Symbol] = true
	}
	stocks := map[string]string{}
	found := false
	for _, w := range wallets {
		if w.WalletName != "Funding" {
			continue
		}
		if w.AssetBalances == nil {
			break
		}
		found = true
		for _, a := range *w.AssetBalances {
			if strings.HasPrefix(a.Asset, "EQ_") {
				q, e := add(a.Free, a.Locked)
				if e != nil {
					return e
				}
				stocks[a.Asset] = q
				if a.Freeze == "" || a.Withdrawing == "" {
					r.block(a.Asset, "股票冻结或在途字段缺失")
				}
				for _, v := range []string{a.Freeze, a.Withdrawing} {
					n, e := num(v)
					if e != nil {
						return e
					}
					if !n.IsZero() {
						r.block(a.Asset, a.Asset+" 股票冻结或在途待对账")
					}
				}
			}
		}
	}
	if !found {
		r.incomplete("资金钱包未返回股票明细")
	}
	for _, b := range rows {
		q, e := add(b.Free, b.Locked)
		if e != nil {
			return e
		}
		if r.Components[b.Asset] == nil {
			r.Components[b.Asset] = map[string]string{}
		}
		r.Components[b.Asset]["free"] = b.Free
		r.Components[b.Asset]["locked"] = b.Locked
		r.Components[b.Asset]["freeze"] = b.Freeze
		r.Components[b.Asset]["withdrawing"] = b.Withdrawing
		if b.Freeze == "" || b.Withdrawing == "" {
			r.block(b.Asset, "资金冻结或在途字段缺失")
		}
		for _, v := range []string{b.Freeze, b.Withdrawing} {
			n, e := num(v)
			if e != nil {
				return e
			}
			if !n.IsZero() {
				r.block(b.Asset, b.Asset+" 冻结或在途余额待对账")
			}
		}
		if stock, ok := stocks[b.Asset]; ok {
			a, e := num(stock)
			if e != nil {
				return e
			}
			otherQuantity, e := num(q)
			if e != nil {
				return e
			}
			if !a.Equal(otherQuantity) {
				r.block(b.Asset, "股票双来源数量不一致")
			}
			continue
		}
		if tokens[b.Asset] {
			r.incomplete(b.Asset + " bStocks 待验证")
		}
		if strings.HasPrefix(b.Asset, "EQ_") {
			r.block(b.Asset, b.Asset+" 股票钱包归属待核对")
		}
		if e = r.item("funding", b.Asset, b.Asset, "other", q); e != nil {
			return e
		}
	}
	for asset, q := range stocks {
		symbol := strings.TrimPrefix(asset, "EQ_")
		class := "other"
		if known[symbol] {
			class = "us_equity"
		} else {
			r.block(asset, asset+" 股票目录不匹配")
		}
		if e := r.item("funding", asset, asset, class, q); e != nil {
			return e
		}
		if class == "us_equity" && len(r.Items) > 0 && r.Items[len(r.Items)-1].Key == asset {
			r.Items[len(r.Items)-1].Symbol = &symbol
		}
	}
	return nil
}
func (c Collector) earn(ctx context.Context, r *Result) error {
	for _, kind := range []string{"flexible", "locked"} {
		expectedTotal := -1
		count := 0
		seen := map[string]bool{}
		for page := 1; page <= 10000; page++ {
			var body struct {
				Rows *[]struct {
					ProductID                                                          string     `json:"productId"`
					PositionID                                                         positionID `json:"positionId"`
					Asset, TotalAmount, Amount, Status, CollateralAmount, RedeemingAmt string
				}
				Total *int
			}
			if e := c.Client.Do(ctx, "GET", "/sapi/v1/simple-earn/"+kind+"/position", url.Values{"current": {fmt.Sprint(page)}, "size": {"100"}}, &body); e != nil {
				return e
			}
			if body.Total == nil || *body.Total < 0 {
				return fmt.Errorf("earn total missing or invalid")
			}
			if expectedTotal < 0 {
				expectedTotal = *body.Total
			} else if expectedTotal != *body.Total {
				return fmt.Errorf("earn total changed during pagination")
			}
			if body.Rows == nil {
				return fmt.Errorf("earn rows missing")
			}
			for _, row := range *body.Rows {
				key, q := kind+":"+row.ProductID, row.TotalAmount
				if kind == "locked" {
					key, q = kind+":"+string(row.PositionID), row.Amount
				}
				if key == kind+":" {
					return fmt.Errorf("earn position identity missing")
				}
				if seen[key] {
					return fmt.Errorf("earn duplicate position")
				}
				seen[key] = true
				if kind == "locked" && row.Status != "HOLDING" {
					r.block(key, "定期非持有状态待对账")
				}
				for _, v := range []string{row.CollateralAmount, row.RedeemingAmt} {
					n, e := num(v)
					if e != nil {
						return e
					}
					if !n.IsZero() {
						r.block(key, "赚币赎回或抵押归属待对账")
					}
				}
				if e := r.item("earn", key, row.Asset, "other", q); e != nil {
					return e
				}
				if r.Components[key] == nil {
					r.Components[key] = map[string]string{}
				}
				r.Components[key]["product"] = kind
				r.Components[key]["status"] = row.Status
				r.Components[key]["collateralAmount"] = row.CollateralAmount
				r.Components[key]["redeemingAmt"] = row.RedeemingAmt
			}
			count += len(*body.Rows)
			if count > *body.Total {
				return fmt.Errorf("earn rows exceed total")
			}
			if count == *body.Total {
				break
			}
			if len(*body.Rows) == 0 || page == 10000 {
				return fmt.Errorf("earn incomplete pagination")
			}
		}
	}
	return nil
}
func (c Collector) futures(ctx context.Context, r *Result) error {
	var config struct {
		MultiAssetsMargin *bool
		PortfolioMargin   bool
		AccountType       string
	}
	if e := c.Client.Do(ctx, "GET", "/fapi/v1/accountConfig", nil, &config); e != nil {
		return e
	}
	if config.PortfolioMargin || strings.Contains(strings.ToUpper(config.AccountType), "PORTFOLIO") {
		r.Mode = "out_of_scope"
		return fmt.Errorf("portfolio margin account is out of scope")
	}
	if config.MultiAssetsMargin == nil {
		r.Mode = "unknown"
		return fmt.Errorf("futures mode unsupported or unknown")
	}
	var body struct {
		TotalMarginBalance string
		Assets             *[]struct{ Asset, MarginBalance string }
	}
	if e := c.Client.Do(ctx, "GET", "/fapi/v3/account", nil, &body); e != nil {
		return e
	}
	if *config.MultiAssetsMargin {
		r.Mode = "multi_assets"
		if e := r.item("usdm", "USD", "USD", "other", body.TotalMarginBalance); e != nil {
			return e
		}
	} else {
		r.Mode = "single_asset"
		if body.Assets == nil {
			return fmt.Errorf("futures assets missing")
		}
		for _, a := range *body.Assets {
			if e := r.item("usdm", a.Asset, a.Asset, "other", a.MarginBalance); e != nil {
				return e
			}
		}
	}
	var positions []struct{ Symbol, PositionSide, PositionAmt, MarginAsset, EntryPrice, MarkPrice, UnRealizedProfit, Notional string }
	if e := c.Client.Do(ctx, "GET", "/fapi/v3/positionRisk", nil, &positions); e != nil {
		return e
	}
	for _, p := range positions {
		r.Positions = append(r.Positions, Position{p.Symbol, p.PositionSide, p.PositionAmt, p.MarginAsset, p.EntryPrice, p.MarkPrice, p.UnRealizedProfit, p.Notional})
	}
	return nil
}

// equityQuote 要求已验证的市场日历；目录 tradability 不代表现在正在交易。
func (c Collector) equityQuote(ctx context.Context, symbol string, at time.Time) (Quote, bool) {
	if c.MarketOpen == nil {
		return c.Prices.Resolve(symbol, "us_equity", at)
	}
	open, err := c.MarketOpen(ctx, symbol, at)
	if err != nil {
		return Quote{}, false
	}
	if !open {
		q, ok := c.Prices.Resolve(symbol, "us_equity", at)
		return q, ok && q.VerifiedClose
	}
	var body struct{ Symbol, BidPrice, AskPrice string }
	if err := c.Client.Do(ctx, "GET", "/sapi/v1/equity/market/quote", url.Values{"symbol": {symbol}}, &body); err != nil || body.Symbol != symbol {
		return Quote{}, false
	}
	bid, err := decimal.NewFromString(body.BidPrice)
	if err != nil {
		return Quote{}, false
	}
	ask, err := decimal.NewFromString(body.AskPrice)
	if err != nil {
		return Quote{}, false
	}
	q, ok := EquityMidpoint(bid, ask, at, "binance-equity-quote")
	q.MarketOpen = true
	return q, ok
}
