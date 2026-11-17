package trades

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

var validAsset = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,50}$`)

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		p := selection{account: q.Get("accountId"), wallet: q.Get("wallet"), asset: strings.TrimSpace(q.Get("asset")), side: q.Get("positionSide")}
		if p.side == "" {
			p.side = "BOTH"
		}
		var id pgtype.UUID
		if err := id.Scan(p.account); err != nil || !id.Valid || !validWallet(p.wallet) ||
			(p.asset != "" && !validAsset.MatchString(p.asset)) || (p.side != "BOTH" && p.side != "LONG" && p.side != "SHORT") ||
			(p.wallet == "spot" && p.side != "BOTH") {
			httpError(w, 400, "invalid_selection", "请选择有效的账号、钱包和资产。")
			return
		}
		var err error
		p.from, err = time.Parse(time.RFC3339, q.Get("from"))
		if err == nil {
			p.to, err = time.Parse(time.RFC3339, q.Get("to"))
		}
		if err != nil || !p.from.Before(p.to) || p.to.Sub(p.from) > 90*24*time.Hour {
			httpError(w, 400, "invalid_interval", "请选择最长 90 天的有效时间段。")
			return
		}
		result, err := s.replay(r.Context(), p)
		switch {
		case errors.Is(err, errUnknownAccount):
			httpError(w, 404, "account_not_found", "账号不存在。")
		case errors.Is(err, errTooManyEvents):
			httpError(w, 422, "interval_too_large", "交易记录过多，请缩短时间段。")
		case err != nil:
			log.Print("read position replay failed")
			httpError(w, 500, "read_failed", "无法读取持仓变动。")
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		}
	})
}

func httpError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message, "requestId": "local"})
}
