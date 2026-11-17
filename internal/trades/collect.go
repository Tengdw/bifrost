package trades

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

const overlap = 10 * time.Minute
const settleDelay = 2 * time.Minute
const timeWindow = 24*time.Hour - time.Millisecond

type orderKey struct {
	symbol string
	id     int64
}

type trackedOrder struct {
	orderKey
	active, usedSOR, executed bool
}

func (s *Service) collectOrders(ctx context.Context, c collector, account string, start time.Time, through *time.Time) error {
	cutoff := time.Now().Add(-settleDelay).UTC().Truncate(time.Millisecond)
	directory := map[string]symbol{}
	if c.wallet == "spot" {
		var err error
		directory, err = c.directory(ctx)
		if err != nil {
			return err
		}
	}
	open, err := c.open(ctx)
	if err != nil {
		return err
	}
	if err = s.saveMarket(ctx, c.wallet, account, start, directory, open, nil); err != nil {
		return err
	}
	openKeys := make(map[orderKey]bool, len(open))
	for _, o := range open {
		openKeys[orderKey{o.Symbol, o.OrderID}] = true
	}
	from := start.UTC().Truncate(time.Millisecond)
	if through != nil && through.Add(-overlap).After(from) {
		from = through.Add(-overlap)
	}
	if cutoff.Before(from) {
		return nil
	}
	to := cutoff
	if from.Add(timeWindow).Before(to) {
		to = from.Add(timeWindow)
	}
	bySymbol := map[string][]orderRow{}
	tracked, err := s.trackedOrders(ctx, account, c.wallet, from)
	if err != nil {
		return err
	}
	executedPairs := map[string]bool{}
	allocationPairs := map[string]bool{}
	// Open orders and recently closed versions may have fills in the interval
	// even if the order was created well before it.
	for _, k := range tracked {
		executedPairs[k.symbol] = executedPairs[k.symbol] || k.executed
		allocationPairs[k.symbol] = allocationPairs[k.symbol] || k.usedSOR
		if _, ok := bySymbol[k.symbol]; !ok {
			bySymbol[k.symbol] = nil
		}
		if !k.active || openKeys[k.orderKey] {
			continue
		}
		row, err := c.order(ctx, k.symbol, k.id)
		if err != nil {
			return err
		}
		bySymbol[k.symbol] = append(bySymbol[k.symbol], row)
	}
	for _, o := range open {
		bySymbol[o.Symbol] = append(bySymbol[o.Symbol], o)
	}

	incomePairs := map[string]bool{}
	if c.wallet == "usdm" {
		orders, err := c.orders(ctx, "", from, to)
		if err != nil {
			return err
		}
		for _, o := range orders {
			bySymbol[o.Symbol] = append(bySymbol[o.Symbol], o)
		}
		names, err := c.incomeSymbols(ctx, from, to)
		if err != nil {
			return err
		}
		for _, pair := range names {
			incomePairs[pair] = true
			if _, ok := bySymbol[pair]; !ok {
				bySymbol[pair] = nil
			}
		}
	} else {
		// /allOrders requires a symbol. Query the whole accessible Spot catalog,
		// including pairs absent from both the opening and closing observations.
		for pair := range directory {
			if _, ok := bySymbol[pair]; !ok {
				bySymbol[pair] = nil
			}
		}
	}
	symbols := make([]string, 0, len(bySymbol))
	for pair := range bySymbol {
		symbols = append(symbols, pair)
	}
	slices.Sort(symbols)
	for _, pair := range symbols {
		orders := bySymbol[pair]
		if c.wallet == "spot" {
			if _, known := directory[pair]; !known {
				return errors.New("spot order symbol no longer appears in catalog")
			}
			all, err := c.orders(ctx, pair, from, to)
			if err != nil {
				return err
			}
			orders = append(orders, all...)
		}
		fetchFills := incomePairs[pair] || executedPairs[pair]
		fetchAllocations := allocationPairs[pair]
		for _, o := range orders {
			fetchFills = fetchFills || o.ExecutedQty.IsPositive()
			fetchAllocations = fetchAllocations || o.UsedSOR
		}
		var fills []fillRow
		if fetchFills {
			fills, err = c.fills(ctx, pair, from, to)
			if err != nil {
				return err
			}
		}
		if fetchAllocations {
			allocations, err := c.allocations(ctx, pair, from, to)
			if err != nil {
				return err
			}
			fills = append(fills, allocations...)
		}
		if err = s.saveMarket(ctx, c.wallet, account, start, directory, orders, fills); err != nil {
			return err
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE bifrost.trade_coverage
 SET through_at=GREATEST(through_at,$3),reason=NULL
 WHERE account_id=$1 AND wallet=$2`, account, c.wallet, to)
	if err != nil {
		return errors.New("save trade coverage")
	}
	return nil
}

func (s *Service) trackedOrders(ctx context.Context, account, wallet string, from time.Time) ([]trackedOrder, error) {
	rows, err := s.pool.Query(ctx, `SELECT symbol,order_id,active,used_sor,executed_quantity>0 FROM (
 SELECT DISTINCT ON (symbol,order_id) symbol,order_id,updated_at,executed_quantity,used_sor,
 (status IN ('NEW','PARTIALLY_FILLED','PENDING_NEW','PENDING_CANCEL')) AS active
 FROM bifrost.exchange_orders WHERE account_id=$1 AND wallet=$2
 ORDER BY symbol,order_id,updated_at DESC,executed_quantity DESC,active
 ) latest WHERE active OR updated_at>=$3`, account, wallet, from)
	if err != nil {
		return nil, errors.New("read tracked orders")
	}
	defer rows.Close()
	keys := []trackedOrder{}
	for rows.Next() {
		var k trackedOrder
		if err = rows.Scan(&k.symbol, &k.id, &k.active, &k.usedSOR, &k.executed); err != nil {
			return nil, errors.New("read tracked order")
		}
		keys = append(keys, k)
	}
	if rows.Err() != nil {
		return nil, errors.New("iterate tracked orders")
	}
	return keys, nil
}

func (s *Service) saveMarket(ctx context.Context, wallet, account string, baseline time.Time, directory map[string]symbol, orders []orderRow, fills []fillRow) error {
	if len(orders) == 0 && len(fills) == 0 {
		return nil
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var enabled bool
		if err := tx.QueryRow(ctx, `SELECT disabled_at IS NULL FROM bifrost.accounts WHERE id=$1 FOR SHARE`, account).
			Scan(&enabled); err != nil || !enabled {
			return errors.New("trade account unavailable or disabled")
		}

		for _, o := range orders {
			if o.Symbol == "" || o.OrderID < 0 || o.Time <= 0 || o.Status == "" ||
				(o.Side != "BUY" && o.Side != "SELL") || o.OrigQty.IsNegative() || o.ExecutedQty.IsNegative() {
				return errors.New("invalid exchange order")
			}
			meta := directory[o.Symbol]
			created, updated := time.UnixMilli(o.Time), time.UnixMilli(o.update())
			// An old order is meaningful only if still open or observed with a
			// post-baseline status update.
			if updated.Before(baseline) && o.Status != "NEW" && o.Status != "PARTIALLY_FILLED" && o.Status != "PENDING_NEW" && o.Status != "PENDING_CANCEL" {
				continue
			}
			_, err := tx.Exec(ctx, `INSERT INTO bifrost.exchange_orders
 (account_id,wallet,symbol,order_id,base_asset,quote_asset,side,position_side,status,order_type,
 orig_quantity,executed_quantity,created_at,updated_at,preexisting,used_sor)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::text::numeric,$12::text::numeric,$13,$14,$15,$16) ON CONFLICT DO NOTHING`,
				account, wallet, o.Symbol, o.OrderID, meta.BaseAsset, meta.QuoteAsset, o.Side, o.PositionSide, o.Status, o.Type,
				o.OrigQty.String(), o.ExecutedQty.String(), created, updated, created.Before(baseline), o.UsedSOR)
			if err != nil {
				return errors.New("store exchange order")
			}
		}
		for _, f := range fills {
			if f.Symbol == "" || f.ID < 0 || f.OrderID < 0 || f.Time <= 0 || !f.Qty.IsPositive() || !f.Price.IsPositive() ||
				f.QuoteQty.IsNegative() || (f.direction() != "BUY" && f.direction() != "SELL") ||
				(!f.Commission.IsZero() && f.CommissionAsset == "") {
				return errors.New("invalid exchange fill")
			}
			at := time.UnixMilli(f.Time)
			if at.Before(baseline) {
				continue
			}
			meta := directory[f.Symbol]
			if wallet == "spot" && meta.Symbol == "" {
				return errors.New("missing spot symbol metadata")
			}
			_, err := tx.Exec(ctx, `INSERT INTO bifrost.exchange_fills
 (account_id,wallet,symbol,trade_id,order_id,traded_at,base_asset,quote_asset,side,position_side,
 quantity,quote_quantity,price,fee,fee_asset,is_allocation)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::text::numeric,$12::text::numeric,$13::text::numeric,$14::text::numeric,$15,$16)
 ON CONFLICT DO NOTHING`, account, wallet, f.Symbol, f.ID, f.OrderID, at, meta.BaseAsset, meta.QuoteAsset,
				f.direction(), f.PositionSide, f.Qty.String(), f.QuoteQty.String(), f.Price.String(), f.Commission.String(), f.CommissionAsset, f.SOR)
			if err != nil {
				return errors.New("store exchange fill")
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save %s trade data: %w", wallet, err)
	}
	return nil
}
