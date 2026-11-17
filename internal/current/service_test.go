package current

import (
	"bifrost/internal/binance"
	"bifrost/internal/holdings"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestSelectedAccountsAreIndependent(t *testing.T) {
	makeSnap := func(id string, n int64) holdings.Snapshot {
		p := decimal.NewFromInt(n)
		return holdings.Snapshot{ID: "batch", Account: id, At: time.Now(), Wallets: holdings.WalletNames, Items: []holdings.Item{{Wallet: "spot", Class: "other", Quantity: decimal.NewFromInt(1), Price: &p}}}
	}
	a, b := makeSnap("A", 100), makeSnap("B", 250)
	if o := combine([]holdings.Snapshot{a, b}); o.TotalUSD.String() != "350" || o.Accounts[0].Wallets[0].ValueUSD.String() != "100" {
		t.Fatal(o)
	}
	b.Wallets = []string{"spot", "funding", "earn"}
	o := combine([]holdings.Snapshot{a, b})
	if o.TotalUSD != nil || o.Subtotal.String() != "350" || o.Wallets[0].Share != nil {
		t.Fatal(o)
	}
	if o := combine([]holdings.Snapshot{a}); o.TotalUSD.String() != "100" {
		t.Fatal(o)
	}
}

func TestMissingStockPriceDoesNotHideKnownCash(t *testing.T) {
	s := holdings.Example(nil)
	b := Batch{ID: s.ID, At: s.At, Accounts: []string{s.Account}}
	for _, w := range holdings.WalletNames {
		r := binance.Result{ObservedAt: s.At, Complete: true, Reasons: []string{}}
		state := "succeeded"
		if w == "funding" {
			r.Items = s.Items
			r.Complete = false
			r.Reasons = []string{"EQ_MU 缺少合格 USD 报价"}
			state = "partial"
		}
		b.Runs = append(b.Runs, Run{Account: s.Account, Wallet: w, State: state, Result: r})
	}
	o := b.Overview()
	if o.TotalUSD != nil || o.Classes[1].ValueUSD == nil || o.Classes[1].ValueUSD.String() != "800" || o.Wallets[1].ValueUSD != nil || o.Wallets[0].ValueUSD == nil {
		t.Fatalf("known cash hidden: %+v", o)
	}
}

func TestNegativeWalletDisablesEveryAllocation(t *testing.T) {
	p, n := decimal.NewFromInt(100), decimal.NewFromInt(-20)
	s := holdings.Snapshot{Account: "A", At: time.Now(), Wallets: holdings.WalletNames, Items: []holdings.Item{
		{Wallet: "spot", Class: "other", Quantity: decimal.NewFromInt(1), Price: &p},
		{Wallet: "usdm", Class: "other", Quantity: decimal.NewFromInt(1), Price: &n},
	}}
	o := combine([]holdings.Snapshot{s})
	if o.TotalUSD.String() != "80" {
		t.Fatal(o)
	}
	for _, gs := range [][]holdings.Group{o.Wallets, o.Classes, o.Accounts[0].Wallets} {
		for _, g := range gs {
			if g.Share != nil {
				t.Fatal("negative wallet has misleading allocation")
			}
		}
	}
}
