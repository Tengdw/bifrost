package current

import (
	"bifrost/internal/binance"
	"bifrost/internal/holdings"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"slices"
	"strings"
	"sync"
	"time"
)

var ErrRunning = errors.New("batch is still running")
var ErrInvalidSelection = errors.New("invalid account selection")

type CollectFunc func(context.Context, string, string) (binance.Result, error)
type Service struct {
	pool        *pgxpool.Pool
	collect     CollectFunc
	ctx         context.Context
	mu          sync.Mutex
	running     map[string]string
	Validate    func(context.Context, string) error
	AllAccounts func(context.Context) ([]string, error)
	slots       chan struct{}
}
type Task struct {
	Batch   string     `json:"batchId"`
	Account string     `json:"accountId"`
	Wallet  string     `json:"wallet"`
	State   string     `json:"state"`
	Last    *time.Time `json:"lastSuccessAt"`
	Reason  *string    `json:"reason"`
}

func NewService(ctx context.Context, pool *pgxpool.Pool, collect CollectFunc) *Service {
	return &Service{pool: pool, collect: collect, ctx: ctx, running: map[string]string{}, slots: make(chan struct{}, 4)}
}
func uuid() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func (s *Service) Refresh(ctx context.Context, accounts, wallets []string) ([]Task, error) {
	for _, id := range accounts {
		if s.Validate == nil {
			return nil, fmt.Errorf("account validator required")
		}
		if err := s.Validate(ctx, id); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid()
	tasks := []Task{}
	fresh := []Task{}
	for _, a := range accounts {
		for _, w := range wallets {
			t := Task{Batch: id, Account: a, Wallet: w, State: "started"}
			if old := s.running[a+"/"+w]; old != "" {
				t.Batch = old
				t.State = "already_running"
			} else {
				fresh = append(fresh, t)
			}
			tasks = append(tasks, t)
		}
	}
	if len(fresh) == 0 {
		return tasks, nil
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		raw, _ := json.Marshal(accounts)
		if _, err := tx.Exec(ctx, "INSERT INTO bifrost.current_batches(id,requested_at,accounts) VALUES($1,$2,$3)", id, time.Now().UTC(), raw); err != nil {
			return err
		}
		for _, t := range fresh {
			if _, err := tx.Exec(ctx, "INSERT INTO bifrost.current_runs(batch_id,account_id,wallet,state) VALUES($1,$2,$3,'running')", id, t.Account, t.Wallet); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persist refresh request failed")
	}
	for _, t := range fresh {
		s.running[t.Account+"/"+t.Wallet] = id
	}
	started := time.Now()
	for _, t := range fresh {
		go s.run(t, started)
	}
	return tasks, nil
}
func (s *Service) run(t Task, started time.Time) {
	defer func() { s.mu.Lock(); delete(s.running, t.Account+"/"+t.Wallet); s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(context.WithValue(s.ctx, batchTimeKey{}, started), 2*time.Minute)
	defer cancel()
	var r binance.Result
	var err error
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
		r, err = s.collect(ctx, t.Account, t.Wallet)
	case <-ctx.Done():
		err = ctx.Err()
	}
	state := "succeeded"
	reason := ""

	if err != nil {
		state = "failed"
		reason = "采集失败，请检查只读权限和网络"
		r = binance.Result{}
	} else {
		if !r.Complete {
			state = "partial"
			reason = joinReasons(r.Reasons)
		}
		if time.Since(started) > 2*time.Minute || r.ObservedAt.Before(started.Add(-2*time.Minute)) || r.ObservedAt.After(started.Add(2*time.Minute)) {
			state = "partial"
			reason = "观察超出批次 2 分钟窗口"
			r.Complete = false
		}

	}
	// 调用方断开连接不取消共享刷新；服务关闭后的持久化仍给予短暂独立窗口。
	persist, cancelPersist := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancelPersist()
	if err := s.saveResult(persist, t, state, reason, r); err != nil {
		log.Print("persist current wallet result failed")
		_, markErr := s.pool.Exec(persist, "UPDATE bifrost.current_runs SET state='failed',reason='结果保存失败' WHERE batch_id=$1 AND account_id=$2 AND wallet=$3", t.Batch, t.Account, t.Wallet)
		if markErr != nil {
			log.Print("mark failed current run failed")
		}
	}
}
func joinReasons(reasons []string) string {
	out := ""
	for _, r := range reasons {
		if out != "" {
			out += "；"
		}
		out += r
	}
	return out
}
func (s *Service) Status(ctx context.Context, batch string) ([]Task, error) {
	rows, err := s.pool.Query(ctx, "SELECT batch_id::text,account_id::text,wallet,state,(SELECT max(ok.observed_at) FROM bifrost.current_runs ok WHERE ok.account_id=current_runs.account_id AND ok.wallet=current_runs.wallet AND ok.state='succeeded'),reason FROM bifrost.current_runs WHERE batch_id=$1 ORDER BY account_id,wallet", batch)
	if err != nil {
		return nil, fmt.Errorf("read sync status failed")
	}
	defer rows.Close()
	result := []Task{}
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.Batch, &t.Account, &t.Wallet, &t.State, &t.Last, &t.Reason); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

type Batch struct {
	ID       string
	At       time.Time
	Accounts []string
	Runs     []Run
}
type Run struct {
	Account, Wallet, State, Reason string
	Result                         binance.Result
	Stale                          *Run
	BatchID                        string
}

func (s *Service) Load(ctx context.Context, batch string, selected []string) (Batch, error) {
	var b Batch
	seen := map[string]bool{}
	for _, id := range selected {
		if seen[id] || !validID(id) {
			return b, ErrInvalidSelection
		}
		seen[id] = true
	}
	if len(selected) == 0 && s.AllAccounts != nil {
		var err error
		selected, err = s.AllAccounts(ctx)
		if err != nil {
			return b, err
		}
		if len(selected) == 0 {
			return b, pgx.ErrNoRows
		}
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, "SELECT id::text,requested_at,accounts FROM bifrost.current_batches WHERE ($1::uuid IS NULL OR id=$1::uuid) AND ($2::jsonb='[]'::jsonb OR accounts ?| ARRAY(SELECT jsonb_array_elements_text($2::jsonb))) ORDER BY requested_at DESC LIMIT 1", optional(batch), jsonList(selected)).Scan(&b.ID, &b.At, &raw)
	if err != nil {
		return b, err
	}
	if err = json.Unmarshal(raw, &b.Accounts); err != nil {
		return b, err
	}
	var running bool
	if err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bifrost.current_runs WHERE batch_id=$1 AND state='running')", b.ID).Scan(&running); err != nil {
		return b, err
	}
	if running {
		return b, ErrRunning
	}
	if len(selected) > 0 {
		b.Accounts = selected
	}
	rows, err := s.pool.Query(ctx, "SELECT account_id::text,wallet,state,COALESCE(reason,''),payload FROM bifrost.current_runs WHERE batch_id=$1 ORDER BY account_id,wallet", b.ID)
	if err != nil {
		return b, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Run
		var p []byte
		if err = rows.Scan(&r.Account, &r.Wallet, &r.State, &r.Reason, &p); err != nil {
			return b, err
		}
		if !slices.Contains(b.Accounts, r.Account) {
			continue
		}
		if len(p) > 0 {
			if err = json.Unmarshal(p, &r.Result); err != nil {
				return b, err
			}
		}
		b.Runs = append(b.Runs, r)
	}
	if err := rows.Err(); err != nil {
		return b, err
	}
	rows.Close()
	for i := range b.Runs {
		b.Runs[i].BatchID = b.ID
		if err := s.loadResult(ctx, b.ID, &b.Runs[i]); err != nil {
			return b, err
		}
	}
	for i := range b.Runs {
		if b.Runs[i].State == "failed" || b.Runs[i].State == "interrupted" {
			old, err := s.staleRun(ctx, b, b.Runs[i])
			if err != nil {
				return b, err
			}
			b.Runs[i].Stale = old
		}
	}
	return b, nil
}
func optional(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func jsonList(v []string) []byte {
	if v == nil {
		return []byte("[]")
	}
	b, _ := json.Marshal(v)
	return b
}
func (b Batch) Overview() holdings.Overview {
	snapshots := []holdings.Snapshot{}
	var earliest, latest time.Time
	for _, r := range b.Runs {
		if r.Result.ObservedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || r.Result.ObservedAt.Before(earliest) {
			earliest = r.Result.ObservedAt
		}
		if r.Result.ObservedAt.After(latest) {
			latest = r.Result.ObservedAt
		}
	}
	withinWindow := latest.Sub(earliest) <= 2*time.Minute
	for _, a := range b.Accounts {
		snap := holdings.Snapshot{ID: b.ID, Account: a, At: b.At}
		for _, r := range b.Runs {
			if r.Account != a {
				continue
			}
			if r.State != "failed" && r.State != "interrupted" {
				snap.Items = append(snap.Items, r.Result.Items...)
			}
			if withinWindow && r.State != "failed" && r.State != "running" && observed(r.Result) && r.Reason != "观察超出批次 2 分钟窗口" {
				snap.Wallets = append(snap.Wallets, r.Wallet)
			}
		}
		snapshots = append(snapshots, snap)
	}
	o := combine(snapshots)
	for _, r := range b.Runs {
		if r.Reason != "" {
			o.Coverage.Reasons = append(o.Coverage.Reasons, r.Account+" "+r.Wallet+" "+r.Reason)
		}
	}
	return o
}

type batchTimeKey struct{}

func BatchTime(ctx context.Context) time.Time {
	if t, ok := ctx.Value(batchTimeKey{}).(time.Time); ok {
		return t
	}
	return time.Now().UTC()
}

func observed(r binance.Result) bool {
	if r.Complete {
		return true
	}
	if len(r.Reasons) == 0 {
		return false
	}
	for _, reason := range r.Reasons {
		if !strings.Contains(reason, "缺少合格 USD 报价") {
			return false
		}
	}
	return true
}

// Recover 将上个进程尚未完成的刷新标为中断；仅在单实例启动时调用。
func (s *Service) Recover(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, "UPDATE bifrost.current_runs SET state='interrupted',reason='服务重启，原批次未完成' WHERE state='running'")
	if err != nil {
		return fmt.Errorf("recover interrupted sync failed")
	}
	return nil
}
