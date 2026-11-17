package holdings

import (
	"slices"
	"time"

	"github.com/shopspring/decimal"
)

// Snapshot 仅包含同一账号、同一批次的已观察钱包。
type Snapshot struct {
	ID, Account string
	At          time.Time
	Wallets     []string
	Items       []Item
}
type Item struct {
	Wallet, Key, Asset, Class string
	Symbol                    *string
	Quantity                  decimal.Decimal
	Price                     *decimal.Decimal
}

const ExampleAccount = "00000000-0000-4000-8000-000000000001"

var WalletNames = []string{"spot", "funding", "earn", "usdm"}

func Example(stockPrice *decimal.Decimal) Snapshot {
	id := "00000000-0000-4000-8000-000000000010"
	minute := 0
	if stockPrice == nil {
		id = "00000000-0000-4000-8000-000000000011"
		minute = 15
	}
	symbol := "MU"
	one := decimal.NewFromInt(1)
	return Snapshot{ID: id, Account: ExampleAccount, At: time.Date(2026, 1, 2, 2, minute, 0, 0, time.UTC), Wallets: append([]string(nil), WalletNames...), Items: []Item{
		{Wallet: "funding", Key: "USD", Asset: "USD", Class: "other", Quantity: decimal.NewFromInt(800), Price: &one},
		{Wallet: "funding", Key: "EQ_MU", Asset: "EQ_MU", Class: "us_equity", Symbol: &symbol, Quantity: decimal.NewFromInt(2), Price: stockPrice}}}
}

type Coverage struct {
	Quality  string    `json:"quality"`
	Reasons  []string  `json:"reasons"`
	AsOf     time.Time `json:"asOf"`
	Accounts []string  `json:"comparableAccountIds"`
}
type Group struct {
	Wallet   string           `json:"wallet,omitempty"`
	Class    string           `json:"assetClass,omitempty"`
	ValueUSD *decimal.Decimal `json:"valueUsd"`
	Subtotal *decimal.Decimal `json:"pricedSubtotalUsd"`
	Share    *decimal.Decimal `json:"share"`
	Coverage Coverage         `json:"coverage"`
}
type AccountRow struct {
	Account string           `json:"accountId"`
	Total   *decimal.Decimal `json:"totalUsd"`
	Wallets []Group          `json:"wallets"`
}
type Overview struct {
	Batch    string           `json:"batchId"`
	Currency string           `json:"currency"`
	At       time.Time        `json:"asOf"`
	TotalUSD *decimal.Decimal `json:"totalUsd"`
	Subtotal *decimal.Decimal `json:"pricedSubtotalUsd"`
	Coverage Coverage         `json:"coverage"`
	Wallets  []Group          `json:"allocation"`
	Classes  []Group          `json:"assetClassAllocation"`
	Accounts []AccountRow     `json:"accountRows"`
}

func (s Snapshot) coverage() Coverage {
	return Coverage{"complete", []string{}, s.At, []string{s.Account}}
}
func (i Item) Value() *decimal.Decimal {
	if i.Price == nil {
		return nil
	}
	v := i.Quantity.Mul(*i.Price)
	return &v
}
func (s Snapshot) group(wallet, class string) Group {
	sum := decimal.Zero
	priced := false
	g := Group{Wallet: wallet, Class: class, Coverage: s.coverage()}
	for _, w := range WalletNames {
		if (wallet == "" || wallet == w) && !slices.Contains(s.Wallets, w) {
			g.Coverage.Quality = "partial"
			g.Coverage.Reasons = append(g.Coverage.Reasons, w+" 未观察")
		}
	}
	for _, i := range s.Items {
		if (wallet != "" && i.Wallet != wallet) || (class != "" && i.Class != class) {
			continue
		}
		if v := i.Value(); v != nil {
			sum = sum.Add(*v)
			priced = true
		} else {
			g.Coverage.Quality = "partial"
			g.Coverage.Reasons = append(g.Coverage.Reasons, i.MissingReason())
		}
	}
	if g.Coverage.Quality == "complete" {
		g.ValueUSD = &sum
	}
	if priced || g.ValueUSD != nil {
		g.Subtotal = &sum
	}
	return g
}
func Summarize(s Snapshot) Overview {
	all := s.group("", "")
	result := Overview{Batch: s.ID, Currency: "USD", At: s.At, TotalUSD: all.ValueUSD, Subtotal: all.Subtotal, Coverage: all.Coverage}
	for _, w := range WalletNames {
		result.Wallets = append(result.Wallets, s.group(w, ""))
	}
	for _, c := range []string{"us_equity", "other"} {
		result.Classes = append(result.Classes, s.group("", c))
	}
	for _, groups := range [][]Group{result.Wallets, result.Classes} {
		for j := range groups {
			if result.TotalUSD != nil && result.TotalUSD.IsPositive() && groups[j].ValueUSD != nil {
				share := groups[j].ValueUSD.Div(*result.TotalUSD)
				groups[j].Share = &share
			}
		}
	}
	result.Accounts = []AccountRow{{s.Account, result.TotalUSD, result.Wallets}}
	return result
}

func (i Item) MissingReason() string {
	unit := " 单位"
	if i.Class == "us_equity" {
		unit = " 股"
	}
	return i.Asset + " " + i.Quantity.String() + unit + "缺价"
}
