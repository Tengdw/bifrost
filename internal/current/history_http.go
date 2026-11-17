package current

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type historyWindow struct {
	From     string           `json:"from"`
	To       string           `json:"to"`
	Days     []dailySummary   `json:"days"`
	PNL      *decimal.Decimal `json:"pnlUsd"`
	NetFlow  *decimal.Decimal `json:"netFlowUsd"`
	Return   *decimal.Decimal `json:"returnRate"`
	Coverage historyCoverage  `json:"coverage"`
}

type historyCalendar struct {
	Month       string           `json:"month"`
	Days        []dailySummary   `json:"days"`
	RecordedPNL *decimal.Decimal `json:"recordedPnlSumUsd"`
	Change      *decimal.Decimal `json:"recordedChangeSumUsd"`
	PNLDays     int              `json:"pnlDays"`
	ChangeDays  int              `json:"changeDays"`
	Coverage    historyCoverage  `json:"coverage"`
}

func (s *Service) serveHistory(w http.ResponseWriter, r *http.Request, selected []string) {
	path := r.URL.Path
	q := r.URL.Query()
	allowed := map[string]bool{"accountIds": true}
	now := time.Now()
	last := dayStart(now).AddDate(0, 0, -1)
	from, to := last, last
	switch path {
	case "/api/v1/overview":
		allowed["basis"] = true
		if basis := q.Get("basis"); basis != "" && basis != "day_close" {
			fail(w, 400, "invalid_basis")
			return
		}
	case "/api/v1/performance":
		allowed["days"] = true
		days := 30
		if _, ok := q["days"]; ok {
			var err error
			days, err = strconv.Atoi(q.Get("days"))
			if err != nil || (days != 7 && days != 30 && days != 90) {
				fail(w, 400, "invalid_days")
				return
			}
		}
		from = to.AddDate(0, 0, 1-days)
	case "/api/v1/calendar":
		allowed["month"] = true
		month := q.Get("month")
		var err error
		from, err = time.ParseInLocation("2006-01", month, beijing)
		if err != nil || from.Year() < 1 || from.Format("2006-01") != month {
			fail(w, 400, "invalid_month")
			return
		}
		to = from.AddDate(0, 1, -1)
	default:
		input := strings.TrimPrefix(path, "/api/v1/calendar/")
		var err error
		from, err = time.ParseInLocation(dateLayout, input, beijing)
		if err != nil || from.Year() < 1 || from.Format(dateLayout) != input {
			fail(w, 400, "invalid_date")
			return
		}
		to = from
	}
	for key, values := range q {
		if !allowed[key] || (key != "accountIds" && len(values) != 1) {
			fail(w, 400, "invalid_query")
			return
		}
	}
	h, err := s.loadHistory(r.Context(), from, to.AddDate(0, 0, 1), now, selected)
	if err != nil {
		log.Print("read day-close observations failed")
		fail(w, 503, "read_failed")
		return
	}
	switch path {
	case "/api/v1/overview":
		reply(w, 200, h.overview(last))
	case "/api/v1/performance":
		days, _, _, coverage := h.period(from, to)
		reply(w, 200, historyWindow{From: from.Format(dateLayout), To: to.Format(dateLayout), Days: days, Coverage: coverage})
	case "/api/v1/calendar":
		days, subtotal, count, coverage := h.period(from, to)
		reply(w, 200, historyCalendar{Month: from.Format("2006-01"), Days: days, Change: subtotal, ChangeDays: count, Coverage: coverage})
	default:
		reply(w, 200, h.day(from))
	}
}
