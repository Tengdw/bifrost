package trades

import (
	"testing"
	"time"
)

func TestReplayUsesFillsAndActualFeeAsset(t *testing.T) {
	start := time.Unix(123456, 0)
	newFill := func(side, qty, quote, fee string, offset time.Duration) fill {
		return fill{base: "FAKE", quote: "USD", At: start.Add(offset), Side: side,
			Quantity: qty, QuoteQuantity: quote, Fee: fee, FeeAsset: "FEE"}
	}
	for _, tc := range []struct{ asset, first, last, delta, diff string }{
		{"FAKE", "0", "0", "0", "0"},
		{"USD", "10", "8", "-1", "-1"},
		{"FEE", "5", "4.7", "-0.3", "0"},
	} {
		t.Run(tc.asset, func(t *testing.T) {
			r := replay{
				Opening: &observation{ObservedAt: start, Quantity: tc.first},
				Closing: &observation{ObservedAt: start.Add(5 * time.Minute), Quantity: tc.last},
				Orders:  []order{{Status: "CANCELED", Quantity: "5", ExecutedQuantity: "0"}},
				Fills: []fill{
					newFill("BUY", "2", "6", "0.1", time.Minute),
					newFill("SELL", "2", "5", "0.2", 2*time.Minute),
					newFill("BUY", "1", "7", "0.5", 6*time.Minute),
				},
			}
			if err := reconcile(&r, selection{wallet: "spot", asset: tc.asset}); err != nil {
				t.Fatal(err)
			}
			if *r.ExplainedChange != tc.delta || *r.UnexplainedDifference != tc.diff {
				t.Fatalf("got explained=%s difference=%s, want %s and %s", *r.ExplainedChange, *r.UnexplainedDifference, tc.delta, tc.diff)
			}
			if r.Fills[2].BalanceAfter != nil {
				t.Fatal("fill after closing changed replay balance")
			}
		})
	}
}

func TestShortPositionUsesSignedQuantity(t *testing.T) {
	d, err := fillDelta(fill{Side: "SELL", Quantity: "0.3", Fee: "1", FeeAsset: "USDT"}, selection{wallet: "usdm", side: "SHORT"})
	if err != nil || d.String() != "-0.3" {
		t.Fatalf("delta = %s, error = %v", d, err)
	}
}
