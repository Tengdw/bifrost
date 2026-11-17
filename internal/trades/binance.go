package trades

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"bifrost/internal/binance"
	"github.com/shopspring/decimal"
)

const pageLimit = 1000

type symbol struct{ Symbol, BaseAsset, QuoteAsset string }

type orderRow struct {
	Symbol, Side, PositionSide, Status, Type string
	OrderID                                  int64
	OrigQty, ExecutedQty                     decimal.Decimal
	Time, UpdateTime                         int64
	UsedSOR                                  bool
}

type fillRow struct {
	Symbol, Side, PositionSide, CommissionAsset string
	ID, OrderID, Time                           int64
	Qty, QuoteQty, Price, Commission            decimal.Decimal
	IsBuyer                                     *bool
	SOR                                         bool
}

func (f fillRow) direction() string {
	if f.IsBuyer == nil {
		return f.Side
	}
	if *f.IsBuyer {
		return "BUY"
	}
	return "SELL"
}

func (o orderRow) update() int64 {
	if o.UpdateTime < o.Time {
		return o.Time
	}
	return o.UpdateTime
}

type collector struct {
	requester binance.Requester
	wallet    string
	pace      *pacers
	validate  func(context.Context) error
}
type pacers struct {
	mu            sync.Mutex
	spot, futures quota
}

type quota struct {
	next, resume time.Time
}

func (p *pacers) quota(wallet string) (*quota, time.Duration) {
	if wallet == "usdm" {
		return &p.futures, 40 * time.Millisecond
	}
	// Leave room for portfolio requests.
	return &p.spot, 16 * time.Millisecond
}

func (p *pacers) wait(ctx context.Context, wallet string, weight int) error {
	for ctx.Err() == nil {
		p.mu.Lock()
		q, unit := p.quota(wallet)
		next := q.next
		if q.resume.After(next) {
			next = q.resume
		}
		now := time.Now()
		if !next.After(now) {
			q.next = now.Add(unit * time.Duration(weight))
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func (p *pacers) backoff(wallet string, delay time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	q, _ := p.quota(wallet)
	until := time.Now().Add(delay)
	if until.After(q.resume) {
		q.resume = until
	}
}

func (c collector) get(ctx context.Context, path string, weight int, q url.Values, out any) error {
	if c.pace != nil {
		if err := c.pace.wait(ctx, c.wallet, weight); err != nil {
			return err
		}
	}
	if c.validate != nil {
		if err := c.validate(ctx); err != nil {
			return err
		}
	}
	if err := c.requester.Do(ctx, http.MethodGet, path, q, out); err != nil {
		var throttled interface{ RetryDelay() time.Duration }
		if c.pace != nil && errors.As(err, &throttled) {
			c.pace.backoff(c.wallet, throttled.RetryDelay())
		}
		return fmt.Errorf("%s read failed: %w", path, err)
	}
	return nil
}

func (c collector) open(ctx context.Context) ([]orderRow, error) {
	path, weight := "/api/v3/openOrders", 80
	if c.wallet == "usdm" {
		path, weight = "/fapi/v1/openOrders", 40
	}
	var rows []orderRow
	err := c.get(ctx, path, weight, nil, &rows)
	return rows, err
}

func (c collector) order(ctx context.Context, symbol string, id int64) (orderRow, error) {
	path, weight := "/api/v3/order", 4
	if c.wallet == "usdm" {
		path, weight = "/fapi/v1/order", 1
	}
	var row orderRow
	err := c.get(ctx, path, weight, url.Values{"symbol": {symbol}, "orderId": {strconv.FormatInt(id, 10)}}, &row)
	return row, err
}

func (c collector) directory(ctx context.Context) (map[string]symbol, error) {
	var response struct{ Symbols []symbol }
	err := c.get(ctx, "/api/v3/exchangeInfo", 20, url.Values{"permissions": {"SPOT"}, "showPermissionSets": {"false"}}, &response)
	if err != nil {
		return nil, err
	}
	m := make(map[string]symbol, len(response.Symbols))
	for _, s := range response.Symbols {
		if s.Symbol != "" && s.BaseAsset != "" && s.QuoteAsset != "" {
			m[s.Symbol] = s
		}
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("empty spot symbol directory")
	}
	return m, nil
}

func (c collector) orders(ctx context.Context, symbol string, from, to time.Time) ([]orderRow, error) {
	path, weight := "/api/v3/allOrders", 20
	if c.wallet == "usdm" {
		path, weight = "/fapi/v1/allOrders", 5
	}
	return rangeRead[orderRow](ctx, c, path, weight, symbol, from.UnixMilli(), to.UnixMilli())
}

func (c collector) fills(ctx context.Context, symbol string, from, to time.Time) ([]fillRow, error) {
	path, weight := "/api/v3/myTrades", 20
	if c.wallet == "usdm" {
		path, weight = "/fapi/v1/userTrades", 5
	}
	return rangeRead[fillRow](ctx, c, path, weight, symbol, from.UnixMilli(), to.UnixMilli())
}

type allocationRow struct {
	fillRow
	AllocationID int64
}

func (c collector) allocations(ctx context.Context, symbol string, from, to time.Time) ([]fillRow, error) {
	rows, err := rangeRead[allocationRow](ctx, c, "/api/v3/myAllocations", 20, symbol, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	fills := make([]fillRow, 0, len(rows))
	for _, row := range rows {
		if row.AllocationID < 0 {
			return nil, fmt.Errorf("invalid allocation ID")
		}
		row.fillRow.ID = row.AllocationID
		row.fillRow.SOR = true
		fills = append(fills, row.fillRow)
	}
	return fills, nil
}

func (c collector) incomeSymbols(ctx context.Context, from, to time.Time) ([]string, error) {
	type incomeRow struct{ Symbol, IncomeType, TradeID string }
	rows, err := rangeRead[incomeRow](ctx, c, "/fapi/v1/income", 30, "", from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, r := range rows {
		if r.Symbol != "" && (r.TradeID != "" || r.IncomeType == "COMMISSION" || r.IncomeType == "REALIZED_PNL") {
			names = append(names, r.Symbol)
		}
	}
	return names, nil
}

// Split saturated time pages rather than skipping trades with the same timestamp.
func rangeRead[T any](ctx context.Context, c collector, path string, weight int, symbol string, from, to int64) ([]T, error) {
	q := url.Values{"startTime": {strconv.FormatInt(from, 10)}, "endTime": {strconv.FormatInt(to, 10)}, "limit": {strconv.Itoa(pageLimit)}}
	if symbol != "" {
		q.Set("symbol", symbol)
	}
	var rows []T
	if err := c.get(ctx, path, weight, q, &rows); err != nil {
		return nil, err
	}
	if len(rows) < pageLimit {
		return rows, nil
	}
	if from >= to {
		return nil, fmt.Errorf("%s saturated within one millisecond", path)
	}
	mid := from + (to-from)/2
	left, err := rangeRead[T](ctx, c, path, weight, symbol, from, mid)
	if err != nil {
		return nil, err
	}
	right, err := rangeRead[T](ctx, c, path, weight, symbol, mid+1, to)
	return append(left, right...), err
}
