package current

import (
	"bifrost/internal/holdings"
	"github.com/shopspring/decimal"
)

func add(values []*decimal.Decimal) *decimal.Decimal {
	sum := decimal.Zero
	for _, v := range values {
		if v == nil {
			return nil
		}
		sum = sum.Add(*v)
	}
	return &sum
}
func combine(snapshots []holdings.Snapshot) holdings.Overview {
	o := holdings.Overview{Currency: "USD", Wallets: []holdings.Group{}, Classes: []holdings.Group{}, Accounts: []holdings.AccountRow{}, Coverage: holdings.Coverage{Quality: "complete", Reasons: []string{}, Accounts: []string{}}}
	totals, subtotals := []*decimal.Decimal{}, []*decimal.Decimal{}
	for n, s := range snapshots {
		single := holdings.Summarize(s)
		if n == 0 {
			o.Batch = s.ID
			o.At = s.At
			o.Coverage.AsOf = s.At
			o.Wallets = append([]holdings.Group(nil), single.Wallets...)
			o.Classes = append([]holdings.Group(nil), single.Classes...)
		} else {
			for _, pair := range [][2][]holdings.Group{{o.Wallets, single.Wallets}, {o.Classes, single.Classes}} {
				for j := range pair[0] {
					left, right := &pair[0][j], pair[1][j]
					left.ValueUSD = add([]*decimal.Decimal{left.ValueUSD, right.ValueUSD})
					if right.Subtotal != nil {
						if left.Subtotal == nil {
							v := decimal.Zero
							left.Subtotal = &v
						}
						v := left.Subtotal.Add(*right.Subtotal)
						left.Subtotal = &v
					}
					left.Coverage.Reasons = append(left.Coverage.Reasons, right.Coverage.Reasons...)
					left.Coverage.Accounts = append(left.Coverage.Accounts, s.Account)
					if right.Coverage.Quality != "complete" {
						left.Coverage.Quality = "partial"
					}
				}
			}
		}
		totals = append(totals, single.TotalUSD)
		if single.Subtotal != nil {
			subtotals = append(subtotals, single.Subtotal)
		}
		o.Accounts = append(o.Accounts, single.Accounts...)
		o.Coverage.Accounts = append(o.Coverage.Accounts, s.Account)
		o.Coverage.Reasons = append(o.Coverage.Reasons, single.Coverage.Reasons...)
	}
	o.TotalUSD = add(totals)
	if len(subtotals) > 0 {
		o.Subtotal = add(subtotals)
	}
	if o.TotalUSD == nil {
		o.Coverage.Quality = "partial"
	}
	nonnegative := true
	for _, snap := range snapshots {
		for _, item := range snap.Items {
			if v := item.Value(); v != nil && v.IsNegative() {
				nonnegative = false
			}
		}
	}
	if !nonnegative {
		for i := range o.Accounts {
			for j := range o.Accounts[i].Wallets {
				o.Accounts[i].Wallets[j].Share = nil
			}
		}
	}
	for _, groups := range [][]holdings.Group{o.Wallets, o.Classes} {
		for i := range groups {
			groups[i].Share = nil
			if nonnegative && o.TotalUSD != nil && o.TotalUSD.IsPositive() && groups[i].ValueUSD != nil {
				v := groups[i].ValueUSD.Div(*o.TotalUSD)
				groups[i].Share = &v
			}
		}
	}
	return o
}
