package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"time"

	"bifrost/internal/holdings"
	"bifrost/internal/postgres/snapshotdb"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type Store struct{ Pool *pgxpool.Pool }

func timestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

// SeedExample 仅接受已知合成场景；批次一经提交不可修改。
func (s Store) SeedExample(ctx context.Context, missing bool) error {
	price := decimal.NewFromInt(100)
	snap := holdings.Example(&price)
	if missing {
		snap = holdings.Example(nil)
	}
	var batch, account pgtype.UUID
	if err := batch.Scan(snap.ID); err != nil {
		return err
	}
	if err := account.Scan(snap.Account); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		q := snapshotdb.New(tx)
		if err := q.CreateAccount(ctx, snapshotdb.CreateAccountParams{ID: account, ExchangeUid: "synthetic-01", Alias: "合成账号"}); err != nil {
			return err
		}
		n, err := q.CreateBatch(ctx, snapshotdb.CreateBatchParams{ID: batch, AccountID: account, ObservedAt: timestamp(snap.At)})
		if err != nil || n == 0 {
			return err
		}
		for _, wallet := range snap.Wallets {
			run, err := q.CreateRun(ctx, snapshotdb.CreateRunParams{BatchID: batch, AccountID: account, Wallet: wallet})
			if err != nil {
				return err
			}
			for _, i := range snap.Items {
				if i.Wallet != wallet {
					continue
				}
				p := snapshotdb.CreateHoldingParams{RunID: run, ItemKey: i.Key, Asset: i.Asset, AssetClass: i.Class, UnderlyingSymbol: i.Symbol, QuantityText: i.Quantity.String()}
				if i.Price != nil {
					price, value, source := i.Price.String(), i.Value().String(), "synthetic"
					p.PriceText = &price
					p.ValueText = &value
					p.PriceSource = &source
					p.PricedAt = timestamp(snap.At)
				}
				if err := q.CreateHolding(ctx, p); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// readError 只公开操作及错误类别，连接串、SQL 参数及账户详情不写日志。
func readError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, pgx.ErrNoRows)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return fmt.Errorf("%s: SQLSTATE %s", op, pgErr.Code)
	}
	return fmt.Errorf("%s: %T", op, err)
}
func (s Store) Load(ctx context.Context, id string, at time.Time) (holdings.Snapshot, error) {
	var snap holdings.Snapshot
	var batch pgtype.UUID
	if id != "" {
		if err := batch.Scan(id); err != nil {
			return snap, err
		}
	}
	var account pgtype.UUID
	if err := account.Scan(holdings.ExampleAccount); err != nil {
		return snap, err
	}
	q := snapshotdb.New(s.Pool)
	b, err := q.LoadBatch(ctx, snapshotdb.LoadBatchParams{AccountID: account, Batch: batch, AsOf: timestamp(at)})
	if err != nil {
		return snap, readError("load batch", err)
	}
	snap.ID = b.ID.String()
	snap.Account = b.AccountID.String()
	snap.At = b.ObservedAt.Time.UTC()
	snap.Wallets, err = q.ListWallets(ctx, snapshotdb.ListWalletsParams{BatchID: b.ID, AccountID: b.AccountID})
	if err != nil {
		return snap, readError("load observed wallets", err)
	}
	rows, err := q.ListItems(ctx, snapshotdb.ListItemsParams{BatchID: b.ID, AccountID: b.AccountID})
	if err != nil {
		return snap, readError("load holdings", err)
	}
	for _, r := range rows {
		quantity, err := decimal.NewFromString(r.QuantityText)
		if err != nil {
			return snap, fmt.Errorf("parse quantity: %w", err)
		}
		i := holdings.Item{Wallet: r.Wallet, Key: r.ItemKey, Asset: r.Asset, Class: r.AssetClass, Symbol: r.UnderlyingSymbol, Quantity: quantity}
		if r.PriceText != "" {
			price, err := decimal.NewFromString(r.PriceText)
			if err != nil {
				return snap, err
			}
			i.Price = &price
		}
		snap.Items = append(snap.Items, i)
	}
	return snap, nil
}
