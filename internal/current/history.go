package current

import (
	"context"
	"slices"
	"time"

	"bifrost/internal/holdings"
	"github.com/shopspring/decimal"
)

const dateLayout = "2006-01-02"
const noCashFlows = "尚无完整现金流覆盖，无法计算盈亏和收益率"

type historyCoverage struct {
	holdings.Coverage
	ObservedAt *time.Time `json:"asOf"`
}

type dayOverview struct {
	holdings.Overview
	CloseDate  string          `json:"date"`
	CloseBatch string          `json:"batchId,omitempty"`
	ObservedAt *time.Time      `json:"asOf"`
	Quality    historyCoverage `json:"coverage"`
}

type dailySummary struct {
	Date          string           `json:"date"`
	OpeningUSD    *decimal.Decimal `json:"openingUsd"`
	ClosingUSD    *decimal.Decimal `json:"closingUsd"`
	AssetChange   *decimal.Decimal `json:"assetChangeUsd"`
	NetFlow       *decimal.Decimal `json:"netFlowUsd"`
	PNL           *decimal.Decimal `json:"pnlUsd"`
	ReturnRate    *decimal.Decimal `json:"returnRate"`
	Coverage      historyCoverage  `json:"coverage"`
	ReturnMissing string           `json:"returnMissingReason"`
}

type boundaryObservation struct {
	id       string
	earliest time.Time
	latest   time.Time
	reasons  []string
	wallets  map[string]map[string]bool
	items    map[string][]holdings.Item
}

type history struct {
	selected []string
	stamp    time.Time
	closes   map[string]*boundaryObservation
}

func (s *Service) loadHistory(ctx context.Context, from, to, now time.Time, selected []string) (history, error) {
	h := history{selected: selected, stamp: dayStart(now), closes: make(map[string]*boundaryObservation)}
	if len(h.selected) == 0 && s.AllAccounts != nil {
		var err error
		h.selected, err = s.AllAccounts(ctx)
		if err != nil {
			return history{}, err
		}
	}
	if len(h.selected) == 0 || from.After(h.stamp) {
		return h, nil
	}
	if to.After(h.stamp) {
		to = h.stamp
	}
	rows, err := s.pool.Query(ctx, `
		SELECT b.day_close_at,b.id::text,r.account_id::text,r.wallet,r.observed_at,
		       COALESCE(r.reason,''),r.quantity_complete,i.asset,i.asset_class,
		       i.quantity::text,i.price_usd::text
		FROM bifrost.current_batches b
		JOIN bifrost.current_runs r ON r.batch_id=b.id
		LEFT JOIN bifrost.current_items i ON (i.batch_id,i.account_id,i.wallet)=(r.batch_id,r.account_id,r.wallet)
		WHERE b.day_close_at >= $1 AND b.day_close_at <= $2 AND r.account_id::text=ANY($3)
		ORDER BY b.day_close_at,r.account_id,r.wallet,i.item_key
	`, from, to, h.selected)
	if err != nil {
		return history{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var boundary time.Time
		var observedAt *time.Time
		var id, account, wallet, reason string
		var asset, class, qty, usd *string
		var complete bool
		if err := rows.Scan(&boundary, &id, &account, &wallet, &observedAt, &reason, &complete, &asset, &class, &qty, &usd); err != nil {
			return history{}, err
		}
		key := boundary.In(beijing).Format(dateLayout)
		b := h.closes[key]
		if b == nil {
			b = &boundaryObservation{id: id, wallets: make(map[string]map[string]bool), items: make(map[string][]holdings.Item)}
			h.closes[key] = b
		}
		if b.wallets[account] == nil {
			b.wallets[account] = make(map[string]bool)
		}
		if _, seen := b.wallets[account][wallet]; !seen {
			validTime := observedAt != nil && !observedAt.Before(boundary) && observedAt.Before(boundary.Add(observationWindow))
			b.wallets[account][wallet] = validTime && complete
			if reason != "" {
				b.reasons = append(b.reasons, account+" "+wallet+" "+reason)
			}
			if validTime {
				if b.earliest.IsZero() || observedAt.Before(b.earliest) {
					b.earliest = *observedAt
				}
				if observedAt.After(b.latest) {
					b.latest = *observedAt
				}
			}
		}
		if qty != nil && b.wallets[account][wallet] {
			quantity, err := decimal.NewFromString(*qty)
			if err != nil {
				return history{}, err
			}
			item := holdings.Item{Wallet: wallet, Asset: *asset, Class: *class, Quantity: quantity}
			if usd != nil {
				price, err := decimal.NewFromString(*usd)
				if err != nil {
					return history{}, err
				}
				item.Price = &price
			}
			b.items[account] = append(b.items[account], item)
		}
	}
	return h, rows.Err()
}

func (h history) overview(date time.Time) dayOverview {
	boundary := date.AddDate(0, 0, 1)
	b := h.closes[boundary.Format(dateLayout)]
	snapshots := make([]holdings.Snapshot, 0, len(h.selected))
	for _, id := range h.selected {
		s := holdings.Snapshot{Account: id}
		if b != nil {
			s.At = b.latest
			s.ID = b.id
			s.Items = b.items[id]
			if b.latest.Sub(b.earliest) <= observationWindow {
				for _, wallet := range holdings.WalletNames {
					if b.wallets[id][wallet] {
						s.Wallets = append(s.Wallets, wallet)
					}
				}
			}
		}
		snapshots = append(snapshots, s)
	}
	overview := combine(snapshots)
	coverage := historyCoverage{Coverage: overview.Coverage}
	coverage.Accounts = []string{}
	for _, account := range overview.Accounts {
		if account.Total != nil {
			coverage.Accounts = append(coverage.Accounts, account.Account)
		}
	}
	if b != nil {
		coverage.Reasons = append(coverage.Reasons, b.reasons...)
	}
	if b != nil && !b.latest.IsZero() {
		coverage.ObservedAt = &b.latest
	} else {
		coverage.Quality = "unavailable"
		switch {
		case !date.Before(h.stamp):
			coverage.Reasons = []string{"北京时间当天尚未结束"}
		case len(h.selected) == 0:
			coverage.Reasons = []string{"没有选中可查看的账号"}
		default:
			coverage.Reasons = append([]string{"尚无该日的日终观测"}, coverage.Reasons...)
		}
		overview.TotalUSD = nil
	}
	return dayOverview{Overview: overview, CloseDate: date.Format(dateLayout), CloseBatch: overview.Batch, ObservedAt: coverage.ObservedAt, Quality: coverage}
}

func (h history) day(date time.Time) dailySummary {
	close := h.overview(date)
	result := dailySummary{
		Date: close.CloseDate, ClosingUSD: close.TotalUSD, Coverage: close.Quality,
		ReturnMissing: noCashFlows,
	}
	if !date.Before(h.stamp) {
		result.ReturnMissing = "北京时间当天尚未结束"
		return result
	}
	open := h.overview(date.AddDate(0, 0, -1))
	result.OpeningUSD = open.TotalUSD
	result.Coverage.Accounts = intersectAccounts(result.Coverage.Accounts, open.Quality.Accounts)
	if result.ClosingUSD == nil {
		result.ReturnMissing = "日终权益不完整"
	} else if result.OpeningUSD == nil {
		result.ReturnMissing = "日初权益不完整"
		result.Coverage.Quality = "partial"
		result.Coverage.Reasons = append(result.Coverage.Reasons, result.ReturnMissing)
	} else {
		diff := result.ClosingUSD.Sub(*result.OpeningUSD)
		result.AssetChange = &diff
	}
	return result
}

func intersectAccounts(a, b []string) []string {
	out := []string{}
	for _, id := range a {
		if slices.Contains(b, id) {
			out = append(out, id)
		}
	}
	return out
}

func (h history) period(from, to time.Time) ([]dailySummary, *decimal.Decimal, int, historyCoverage) {
	days := []dailySummary{}
	var last *time.Time
	total := decimal.Zero
	usable := 0
	comparable := append([]string{}, h.selected...)
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		point := h.day(date)
		days = append(days, point)
		comparable = intersectAccounts(comparable, point.Coverage.Accounts)
		if point.Coverage.ObservedAt != nil {
			last = point.Coverage.ObservedAt
		}
		if point.AssetChange != nil {
			total = total.Add(*point.AssetChange)
			usable++
		}
	}
	reasons := []string{noCashFlows}
	if usable < len(days) {
		reasons = append(reasons, "部分日期没有可比的日初和日终权益")
	}
	coverage := historyCoverage{Coverage: holdings.Coverage{Quality: "partial", Reasons: reasons, Accounts: comparable}, ObservedAt: last}
	if last == nil {
		coverage.Quality = "unavailable"
	}
	if usable == 0 {
		return days, nil, 0, coverage
	}
	return days, &total, usable, coverage
}
