// Package trades records read-only exchange orders and fills, then reconciles
// their effects against observed positions.
package trades

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"bifrost/internal/binance"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RequesterForAccount func(context.Context, string) (binance.Requester, error)

type Service struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	requester RequesterForAccount
	mu        sync.Mutex
	running   map[string]bool
	pace      *pacers
}

func New(ctx context.Context, pool *pgxpool.Pool, requester RequesterForAccount) *Service {
	return &Service{ctx: ctx, pool: pool, requester: requester,
		running: make(map[string]bool), pace: &pacers{}}
}

// Trigger keeps trade pulls independent of wallet snapshot collection.
func (s *Service) Trigger(account, wallet string) {
	if !validWallet(wallet) || s.ctx.Err() != nil {
		return
	}
	key := account + "/" + wallet
	s.mu.Lock()
	if s.running[key] {
		s.mu.Unlock()
		return
	}
	s.running[key] = true
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); delete(s.running, key); s.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(s.ctx, 50*time.Minute)
		defer cancel()
		if err := s.collect(ctx, account, wallet); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("collect %s trade history failed (%s)", wallet, err)
		}
	}()
}

// Quantity completeness is separate from USD price coverage.
const quantityObservation = `state IN ('succeeded','partial') AND quantity_complete AND observed_at IS NOT NULL`

func validWallet(wallet string) bool { return wallet == "spot" || wallet == "usdm" }

func (s *Service) collect(ctx context.Context, account, wallet string) error {
	// The first observation after this migration anchors trade history. A failed
	// query on this first run must not shift the baseline on the next attempt.
	var start time.Time
	var through *time.Time
	err := s.pool.QueryRow(ctx, `INSERT INTO bifrost.trade_coverage(account_id,wallet,started_at)
 SELECT $1,$2,observed_at FROM bifrost.current_runs
 WHERE account_id=$1 AND wallet=$2 AND `+quantityObservation+`
 AND observed_at >= (SELECT applied_at FROM bifrost.schema_migrations WHERE name='020_trades.sql')
 ORDER BY observed_at LIMIT 1
 ON CONFLICT(account_id,wallet) DO UPDATE SET started_at=bifrost.trade_coverage.started_at
 RETURNING started_at,through_at`, account, wallet).Scan(&start, &through)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errors.New("read trade coverage")
	}
	get, err := s.requester(ctx, account)
	if err != nil {
		return s.setFailure(account, wallet, errors.New("read account credentials"))
	}
	c := collector{requester: get, wallet: wallet, pace: s.pace, validate: s.validateActive(account)}
	if err = s.collectOrders(ctx, c, account, start, through); err != nil {
		return s.setFailure(account, wallet, err)
	}
	return nil
}

// Catalog sweeps are longer-lived than balance calls. Recheck a disabled
// account during a sweep so it stops making exchange calls promptly.
func (s *Service) validateActive(account string) func(context.Context) error {
	var last time.Time
	return func(ctx context.Context) error {
		if time.Since(last) < 10*time.Second {
			return nil
		}
		var enabled bool
		if err := s.pool.QueryRow(ctx, `SELECT disabled_at IS NULL FROM bifrost.accounts WHERE id=$1`, account).
			Scan(&enabled); err != nil || !enabled {
			return errors.New("trade account unavailable or disabled")
		}
		last = time.Now()
		return nil
	}
}

func (s *Service) setFailure(account, wallet string, err error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancel()
	_, writeErr := s.pool.Exec(ctx, `UPDATE bifrost.trade_coverage SET reason=$3 WHERE account_id=$1 AND wallet=$2`,
		account, wallet, "订单或成交采集未覆盖所选时段，等待下次同步。")
	if writeErr != nil {
		return fmt.Errorf("%w; save trade failure state", err)
	}
	return err
}
