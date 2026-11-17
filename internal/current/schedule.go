package current

import (
	"context"
	"fmt"
	"log"
	"time"

	"bifrost/internal/holdings"
	"github.com/jackc/pgx/v5"
)

const collectionInterval = 15 * time.Minute
const scheduleLockID = 1578283632

var beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

func dayStart(at time.Time) time.Time {
	local := at.In(beijing)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, beijing)
}

// StartScheduler 用独立连接持有单实例锁，旧进程退出时由数据库释放。
func (s *Service) StartScheduler(ctx context.Context, enabled bool) error {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig)
	if err != nil {
		return fmt.Errorf("cannot establish scheduler connection")
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", scheduleLockID).Scan(&acquired); err != nil || !acquired {
		_ = conn.Close(ctx)
		return fmt.Errorf("cannot acquire workspace collector lock; check for another running instance")
	}
	if err := s.Recover(ctx); err != nil {
		_ = conn.Close(ctx)
		return err
	}
	go func() {
		defer conn.Close(context.Background())
		if !enabled {
			<-s.ctx.Done()
			return
		}
		s.scheduleLoop(conn)
	}()
	return nil
}

func (s *Service) scheduleLoop(lock *pgx.Conn) {
	for s.ctx.Err() == nil {
		if err := lock.Ping(s.ctx); err != nil {
			if s.ctx.Err() == nil {
				log.Print("workspace collector lost its scheduler connection; restart the service")
			}
			return
		}
		now := time.Now()
		slot := now.Truncate(collectionInterval)
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		err := s.collectPeriod(ctx, slot, now)
		cancel()
		if err != nil && s.ctx.Err() == nil {
			log.Print("could not schedule workspace collection")
		}
		timer := time.NewTimer(time.Until(slot.Add(collectionInterval)))
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (s *Service) collectPeriod(ctx context.Context, slot, now time.Time) error {
	boundary := time.Time{}
	if slot.Equal(dayStart(slot)) && now.Sub(slot) < observationWindow {
		var committed bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bifrost.current_batches WHERE day_close_at=$1)", slot).Scan(&committed); err != nil {
			return err
		}
		if !committed {
			boundary = slot
		}
	}
	ids, err := s.AllAccounts(ctx)
	if err != nil {
		return err
	}
	if boundary.IsZero() {
		missing := make([]string, 0, len(ids))
		for _, id := range ids {
			var observed bool
			// 存在完整的数量观测即可跳过；缺 USD 价格无法靠短时间重启修复。
			err := s.pool.QueryRow(ctx, `SELECT EXISTS(
				SELECT 1 FROM bifrost.current_batches b
				JOIN bifrost.current_runs r ON r.batch_id=b.id
				WHERE b.requested_at >= $1 AND b.requested_at < $2
					AND r.account_id=$3 AND r.wallet=ANY($4) AND r.quantity_complete
				GROUP BY b.id
				HAVING count(*)=$5 AND max(r.observed_at)-min(r.observed_at)<=interval '2 minutes'
			)`, slot, slot.Add(collectionInterval), id, holdings.WalletNames, len(holdings.WalletNames)).Scan(&observed)
			if err != nil {
				return err
			}
			if !observed {
				missing = append(missing, id)
			}
		}
		ids = missing
	}
	// 如果刚列出的账号被停用或其凭据失效，允许其它账号和钱包继续采集。
	_, err = s.refresh(ctx, ids, holdings.WalletNames, boundary, false)
	return err
}

func (s *Service) CollectBound(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		if _, err := s.Refresh(ctx, []string{id}, holdings.WalletNames); err != nil && s.ctx.Err() == nil {
			log.Print("could not schedule collection after account binding")
		}
	}()
}
