package current

import (
	"bifrost/internal/binance"
	"bifrost/internal/holdings"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func decimalText(v *decimal.Decimal) any {
	if v == nil {
		return nil
	}
	return v.String()
}
func (s *Service) saveResult(ctx context.Context, t Task, state, reason string, r binance.Result) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		metadata := r
		metadata.Items = nil
		metadata.Positions = nil
		metadata.Quotes = nil
		payload, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		for _, i := range r.Items {
			var info []byte
			var bid, ask any
			if q, ok := r.Quotes[i.Key]; ok {
				metadata := q
				metadata.Price = decimal.Zero
				metadata.BidPrice = nil
				metadata.AskPrice = nil
				info, err = json.Marshal(metadata)
				if err != nil {
					return err
				}
				bid = decimalText(q.BidPrice)
				ask = decimalText(q.AskPrice)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO bifrost.current_items(batch_id,account_id,wallet,item_key,asset,asset_class,underlying_symbol,quantity,usd_value,price_usd,price_bid,price_ask,quote_info) VALUES($1,$2,$3,$4,$5,$6,$7,$8::text::numeric,$9::text::numeric,$10::text::numeric,$11::text::numeric,$12::text::numeric,$13)", t.Batch, t.Account, t.Wallet, i.Key, i.Asset, i.Class, i.Symbol, i.Quantity.String(), decimalText(i.Value()), decimalText(i.Price), bid, ask, info); err != nil {
				return err
			}
		}
		for _, p := range r.Positions {
			if _, err = tx.Exec(ctx, "INSERT INTO bifrost.current_positions(batch_id,account_id,wallet,symbol,side,quantity,margin_asset,entry_price,mark_price,unrealized,notional) VALUES($1,$2,$3,$4,$5,$6::text::numeric,$7,$8::text::numeric,$9::text::numeric,$10::text::numeric,$11::text::numeric)", t.Batch, t.Account, t.Wallet, p.Symbol, p.Side, p.Quantity, p.MarginAsset, p.EntryPrice, p.MarkPrice, p.Unrealized, p.Notional); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, "UPDATE bifrost.current_runs SET state=$4,reason=$5,observed_at=$6,payload=$7 WHERE batch_id=$1 AND account_id=$2 AND wallet=$3", t.Batch, t.Account, t.Wallet, state, reason, r.ObservedAt, payload)
		return err
	})
}
func (s *Service) loadResult(ctx context.Context, batch string, r *Run) error {
	rows, err := s.pool.Query(ctx, "SELECT item_key,asset,asset_class,underlying_symbol,quantity::text,price_usd::text,price_bid::text,price_ask::text,quote_info FROM bifrost.current_items WHERE batch_id=$1 AND account_id=$2 AND wallet=$3 ORDER BY item_key", batch, r.Account, r.Wallet)
	if err != nil {
		return err
	}
	defer rows.Close()
	r.Result.Quotes = map[string]binance.Quote{}
	for rows.Next() {
		var i holdings.Item
		var qty string
		var price, bid, ask *string
		var info []byte
		i.Wallet = r.Wallet
		if err := rows.Scan(&i.Key, &i.Asset, &i.Class, &i.Symbol, &qty, &price, &bid, &ask, &info); err != nil {
			return err
		}
		i.Quantity, err = decimal.NewFromString(qty)
		if err != nil {
			return err
		}
		if price != nil {
			v, e := decimal.NewFromString(*price)
			if e != nil {
				return e
			}
			i.Price = &v
		}
		if len(info) > 0 {
			var q binance.Quote
			if err = json.Unmarshal(info, &q); err != nil {
				return err
			}
			if i.Price != nil {
				q.Price = *i.Price
			}
			if bid != nil {
				v, e := decimal.NewFromString(*bid)
				if e != nil {
					return e
				}
				q.BidPrice = &v
			}
			if ask != nil {
				v, e := decimal.NewFromString(*ask)
				if e != nil {
					return e
				}
				q.AskPrice = &v
			}
			r.Result.Quotes[i.Key] = q
		}
		r.Result.Items = append(r.Result.Items, i)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	positions, err := s.pool.Query(ctx, "SELECT symbol,side,quantity::text,margin_asset,entry_price::text,mark_price::text,unrealized::text,notional::text FROM bifrost.current_positions WHERE batch_id=$1 AND account_id=$2 AND wallet=$3 ORDER BY symbol,side", batch, r.Account, r.Wallet)
	if err != nil {
		return err
	}
	defer positions.Close()
	for positions.Next() {
		var p binance.Position
		if err = positions.Scan(&p.Symbol, &p.Side, &p.Quantity, &p.MarginAsset, &p.EntryPrice, &p.MarkPrice, &p.Unrealized, &p.Notional); err != nil {
			return err
		}
		r.Result.Positions = append(r.Result.Positions, p)
	}
	return positions.Err()
}
