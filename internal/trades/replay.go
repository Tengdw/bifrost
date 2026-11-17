package trades

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type assetOption struct {
	Asset        string `json:"asset"`
	PositionSide string `json:"positionSide,omitempty"`
}

type coverage struct {
	StartedAt *time.Time `json:"startedAt"`
	Through   *time.Time `json:"through"`
	State     string     `json:"state"`
	Reason    string     `json:"reason,omitempty"`
}

type observation struct {
	ObservedAt time.Time `json:"observedAt"`
	Quantity   string    `json:"quantity"`
	batch      string
}

type order struct {
	ID               string    `json:"id"`
	Symbol           string    `json:"symbol"`
	Side             string    `json:"side"`
	PositionSide     string    `json:"positionSide,omitempty"`
	Status           string    `json:"status"`
	Type             string    `json:"type"`
	Quantity         string    `json:"quantity"`
	ExecutedQuantity string    `json:"executedQuantity"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	Preexisting      bool      `json:"preexisting"`
}

type fill struct {
	ID            string    `json:"id"`
	OrderID       string    `json:"orderId"`
	Symbol        string    `json:"symbol"`
	Side          string    `json:"side"`
	PositionSide  string    `json:"positionSide,omitempty"`
	Quantity      string    `json:"quantity"`
	QuoteQuantity string    `json:"quoteQuantity"`
	Price         string    `json:"price"`
	Fee           string    `json:"fee"`
	FeeAsset      string    `json:"feeAsset"`
	At            time.Time `json:"at"`
	Delta         string    `json:"delta"`
	BalanceAfter  *string   `json:"balanceAfter"`
	base, quote   string
}

type replay struct {
	Assets                []assetOption `json:"assets"`
	Coverage              coverage      `json:"coverage"`
	Opening               *observation  `json:"opening"`
	Closing               *observation  `json:"closing"`
	ExplainedChange       *string       `json:"explainedChange"`
	UnexplainedDifference *string       `json:"unexplainedDifference"`
	Orders                []order       `json:"orders"`
	Fills                 []fill        `json:"fills"`
}

type selection struct {
	account, wallet, asset, side string
	from, to                     time.Time
}

var errUnknownAccount = errors.New("account not found")
var errTooManyEvents = errors.New("selected interval contains too many records")

const eventLimit = 5000

func (s *Service) replay(ctx context.Context, p selection) (replay, error) {
	r := replay{Assets: []assetOption{}, Orders: []order{}, Fills: []fill{}}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bifrost.accounts WHERE id=$1)`, p.account).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errUnknownAccount
		}
		var reason *string
		err := tx.QueryRow(ctx, `SELECT started_at,through_at,reason FROM bifrost.trade_coverage WHERE account_id=$1 AND wallet=$2`, p.account, p.wallet).
			Scan(&r.Coverage.StartedAt, &r.Coverage.Through, &reason)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read replay coverage: %w", err)
		}
		r.Coverage.State = "pending"
		if r.Coverage.StartedAt == nil {
			r.Coverage.Reason = "尚未采集真实持仓和交易记录。"
			return nil
		}
		r.Assets, err = readAssets(ctx, tx, p, *r.Coverage.StartedAt)
		if err != nil {
			return fmt.Errorf("read replay assets: %w", err)
		}
		if p.asset != "" {
			if err = readReplay(ctx, tx, p, &r); err != nil {
				return err
			}
		}
		target := p.to
		if r.Closing != nil {
			target = r.Closing.ObservedAt
		}
		switch {
		case !target.After(*r.Coverage.StartedAt):
			r.Coverage.Reason = "所选时段早于首次真实采集。"
		case r.Coverage.Through != nil && !r.Coverage.Through.Before(target):
			r.Coverage.State = "covered"
		case reason != nil:
			r.Coverage.State, r.Coverage.Reason = "partial", *reason
		default:
			r.Coverage.State, r.Coverage.Reason = "collecting", "订单和成交仍在采集，当前结果可能不完整。"
		}
		return nil
	})
	return r, err
}

func readAssets(ctx context.Context, tx pgx.Tx, p selection, baseline time.Time) ([]assetOption, error) {
	query := `SELECT asset, '' FROM (
 SELECT asset FROM bifrost.current_items i JOIN bifrost.current_runs r USING (batch_id,account_id,wallet)
 WHERE i.account_id=$1 AND i.wallet=$2 AND r.observed_at>=$3
 UNION SELECT base_asset FROM bifrost.exchange_orders WHERE account_id=$1 AND wallet=$2
 UNION SELECT quote_asset FROM bifrost.exchange_orders WHERE account_id=$1 AND wallet=$2
 UNION SELECT fee_asset FROM bifrost.exchange_fills WHERE account_id=$1 AND wallet=$2
 ) a WHERE asset<>'' ORDER BY asset`
	if p.wallet == "usdm" {
		query = `SELECT symbol, position_side FROM (
 SELECT symbol,side AS position_side FROM bifrost.current_positions i JOIN bifrost.current_runs r USING (batch_id,account_id,wallet)
 WHERE i.account_id=$1 AND i.wallet=$2 AND r.observed_at>=$3
 UNION SELECT symbol,position_side FROM bifrost.exchange_orders WHERE account_id=$1 AND wallet=$2
 UNION SELECT symbol,position_side FROM bifrost.exchange_fills WHERE account_id=$1 AND wallet=$2
 ) a WHERE position_side IN ('BOTH','LONG','SHORT') ORDER BY symbol,position_side`
	}
	rows, err := tx.Query(ctx, query, p.account, p.wallet, baseline)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []assetOption{}
	for rows.Next() {
		var a assetOption
		if err := rows.Scan(&a.Asset, &a.PositionSide); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func readReplay(ctx context.Context, tx pgx.Tx, p selection, r *replay) error {
	var err error
	baseline := *r.Coverage.StartedAt
	// Keep the replay within the user-selected interval. An opening without
	// another usable observation cannot be reconciled.
	r.Opening, err = readObservation(ctx, tx, p, baseline, false)
	if err != nil {
		return fmt.Errorf("read opening quantity: %w", err)
	}
	r.Closing, err = readObservation(ctx, tx, p, baseline, true)
	if err != nil {
		return fmt.Errorf("read closing quantity: %w", err)
	}
	if r.Closing != nil && r.Closing.batch == r.Opening.batch {
		r.Closing = nil
	}
	r.Orders, err = readOrders(ctx, tx, p, baseline)
	if err != nil {
		return fmt.Errorf("read replay orders: %w", err)
	}
	r.Fills, err = readFills(ctx, tx, p)
	if err != nil {
		return fmt.Errorf("read replay fills: %w", err)
	}
	return reconcile(r, p)
}

func readObservation(ctx context.Context, tx pgx.Tx, p selection, baseline time.Time, last bool) (*observation, error) {
	orderBy := "ASC"
	if last {
		orderBy = "DESC"
	}
	o := observation{}
	err := tx.QueryRow(ctx, `SELECT batch_id::text,observed_at FROM bifrost.current_runs
 WHERE account_id=$1 AND wallet=$2 AND observed_at>=GREATEST($3::timestamptz,$4::timestamptz) AND observed_at<$5 AND `+
		quantityObservation+` ORDER BY observed_at `+orderBy+`,batch_id `+orderBy+` LIMIT 1`,
		p.account, p.wallet, p.from, baseline, p.to).Scan(&o.batch, &o.ObservedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.wallet == "spot" {
		err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0)::text FROM bifrost.current_items
 WHERE account_id=$1 AND wallet='spot' AND batch_id=$2 AND asset=$3`, p.account, o.batch, p.asset).Scan(&o.Quantity)
	} else {
		err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0)::text FROM bifrost.current_positions
 WHERE account_id=$1 AND wallet='usdm' AND batch_id=$2 AND symbol=$3 AND side=$4`, p.account, o.batch, p.asset, p.side).Scan(&o.Quantity)
	}
	return &o, err
}

func readOrders(ctx context.Context, tx pgx.Tx, p selection, baseline time.Time) ([]order, error) {
	rows, err := tx.Query(ctx, `SELECT order_id::text,symbol,side,position_side,status,order_type,
 orig_quantity::text,executed_quantity::text,created_at,updated_at,preexisting FROM bifrost.exchange_orders o
 WHERE account_id=$1 AND wallet=$2
 AND ((updated_at >= $5 AND updated_at < $6)
      OR (preexisting AND $5 <= $7 AND updated_at < $7)
      OR (created_at >= $5 AND created_at < $6 AND NOT EXISTS (
          SELECT 1 FROM bifrost.exchange_orders older WHERE older.account_id=o.account_id AND older.wallet=o.wallet
          AND older.symbol=o.symbol AND older.order_id=o.order_id AND older.updated_at<o.updated_at)))
 AND (($2='spot' AND (base_asset=$3 OR quote_asset=$3 OR EXISTS(
  SELECT 1 FROM bifrost.exchange_fills f WHERE f.account_id=o.account_id AND f.wallet=o.wallet
  AND f.symbol=o.symbol AND f.order_id=o.order_id AND f.fee_asset=$3 AND f.traded_at >= $5 AND f.traded_at < $6)))
  OR ($2='usdm' AND symbol=$3 AND position_side=$4))
 ORDER BY updated_at,symbol,order_id,executed_quantity,status LIMIT $8`, p.account, p.wallet, p.asset, p.side, p.from, p.to, baseline, eventLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []order{}
	for rows.Next() {
		var o order
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.PositionSide, &o.Status, &o.Type, &o.Quantity, &o.ExecutedQuantity,
			&o.CreatedAt, &o.UpdatedAt, &o.Preexisting); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if len(out) > eventLimit {
		return nil, errTooManyEvents
	}
	return out, rows.Err()
}

func readFills(ctx context.Context, tx pgx.Tx, p selection) ([]fill, error) {
	rows, err := tx.Query(ctx, `SELECT CASE WHEN is_allocation THEN 'sor:'||trade_id::text ELSE trade_id::text END,order_id::text,symbol,side,position_side,quantity::text,quote_quantity::text,
 price::text,fee::text,fee_asset,traded_at,base_asset,quote_asset FROM bifrost.exchange_fills
 WHERE account_id=$1 AND wallet=$2 AND traded_at >= $5 AND traded_at < $6
 AND (($2='spot' AND $3 IN (base_asset,quote_asset,fee_asset)) OR ($2='usdm' AND symbol=$3 AND position_side=$4))
 ORDER BY traded_at,symbol,trade_id,order_id,is_allocation LIMIT $7`, p.account, p.wallet, p.asset, p.side, p.from, p.to, eventLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []fill{}
	for rows.Next() {
		var f fill
		if err := rows.Scan(&f.ID, &f.OrderID, &f.Symbol, &f.Side, &f.PositionSide, &f.Quantity, &f.QuoteQuantity,
			&f.Price, &f.Fee, &f.FeeAsset, &f.At, &f.base, &f.quote); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if len(out) > eventLimit {
		return nil, errTooManyEvents
	}
	return out, rows.Err()
}

func fillDelta(f fill, p selection) (decimal.Decimal, error) {
	qty, err := decimal.NewFromString(f.Quantity)
	if err != nil {
		return decimal.Zero, err
	}
	if f.Side == "SELL" {
		qty = qty.Neg()
	}
	if p.wallet == "usdm" {
		return qty, nil
	}
	d := decimal.Zero
	if f.base == p.asset {
		d = d.Add(qty)
	}
	if f.quote == p.asset {
		quote, err := decimal.NewFromString(f.QuoteQuantity)
		if err != nil {
			return decimal.Zero, err
		}
		if f.Side == "BUY" {
			quote = quote.Neg()
		}
		d = d.Add(quote)
	}
	if f.FeeAsset == p.asset {
		fee, err := decimal.NewFromString(f.Fee)
		if err != nil {
			return decimal.Zero, err
		}
		d = d.Sub(fee)
	}
	return d, nil
}

func reconcile(r *replay, p selection) error {
	start := decimal.Zero
	if r.Opening != nil {
		var err error
		start, err = decimal.NewFromString(r.Opening.Quantity)
		if err != nil {
			return err
		}
	}
	during := decimal.Zero
	for i := range r.Fills {
		f := &r.Fills[i]
		d, err := fillDelta(*f, p)
		if err != nil {
			return err
		}
		f.Delta = d.String()
		if r.Opening != nil && f.At.After(r.Opening.ObservedAt) && (r.Closing == nil || !f.At.After(r.Closing.ObservedAt)) {
			during = during.Add(d)
			balance := start.Add(during).String()
			f.BalanceAfter = &balance
		}
	}
	if r.Opening == nil {
		return nil
	}
	explained := during.String()
	r.ExplainedChange = &explained
	if r.Closing == nil {
		return nil
	}
	end, err := decimal.NewFromString(r.Closing.Quantity)
	if err != nil {
		return err
	}
	diff := end.Sub(start).Sub(during).String()
	r.UnexplainedDifference = &diff
	return nil
}
