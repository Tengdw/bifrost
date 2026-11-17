package holdings

import (
	"github.com/shopspring/decimal"
	"testing"
)

func TestCompleteFunding(t *testing.T) {
	price := decimal.NewFromInt(100)
	snapshot := Example(&price)
	result := Summarize(snapshot)
	if result.TotalUSD == nil || result.TotalUSD.String() != "1000" || result.Classes[0].ValueUSD.String() != "200" || result.Wallets[1].ValueUSD.String() != "1000" {
		t.Fatalf("unexpected complete overview: %+v", result)
	}
}
func TestUnpricedStockKeepsQuantityAndSubtotal(t *testing.T) {
	snapshot := Example(nil)
	o := Summarize(snapshot)
	if o.TotalUSD != nil || o.Subtotal.String() != "800" || o.Wallets[1].ValueUSD != nil || o.Wallets[1].Subtotal.String() != "800" || o.Classes[0].Subtotal != nil || o.Classes[1].ValueUSD.String() != "800" || !o.Wallets[0].ValueUSD.IsZero() || o.Wallets[0].Share != nil || o.Classes[1].Share != nil || snapshot.Items[1].Quantity.String() != "2" {
		t.Fatalf("unexpected partial overview: %+v", o)
	}
}
