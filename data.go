package main

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Portfolio struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Currency      string `json:"currency_code"`
	InceptionDate string `json:"inception_date"`
}

type Gains struct {
	CapitalGain         float64 `json:"capital_gain"`
	CapitalGainPercent  float64 `json:"capital_gain_percent"`
	PayoutGain          float64 `json:"payout_gain"`
	PayoutGainPercent   float64 `json:"payout_gain_percent"`
	CurrencyGain        float64 `json:"currency_gain"`
	CurrencyGainPercent float64 `json:"currency_gain_percent"`
	TotalGain           float64 `json:"total_gain"`
	TotalGainPercent    float64 `json:"total_gain_percent"`
}

type Instrument struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Market   string `json:"market_code"`
	Currency string `json:"currency_code"`
	Sector   string `json:"sector_classification_name"`
	Industry string `json:"industry_classification_name"`
}

type Holding struct {
	Gains
	ID         int        `json:"id"`
	Instrument Instrument `json:"instrument"`
	Quantity   float64    `json:"quantity"`
	Value      float64    `json:"value"`
	Price      float64    `json:"instrument_price"`
}

type Report struct {
	Gains
	Value     float64   `json:"value"`
	StartDate string    `json:"start_date"`
	EndDate   string    `json:"end_date"`
	Holdings  []Holding `json:"holdings"`
}

type Period struct {
	Key, Label string
	Annualise  bool // Sharesight returns this period's % per annum (anything over a year)
	start      func(now time.Time) time.Time
}

var periods = []Period{
	{"today", "Today", false, func(n time.Time) time.Time { return prevWeekday(lastTradingDay(n)) }},
	{"1w", "1 week", false, func(n time.Time) time.Time { return n.AddDate(0, 0, -7) }},
	{"1m", "1 month", false, func(n time.Time) time.Time { return n.AddDate(0, -1, 0) }},
	{"ytd", "This year", false, func(n time.Time) time.Time { return time.Date(n.Year()-1, 12, 31, 0, 0, 0, 0, n.Location()) }},
	{"1y", "1 year", false, func(n time.Time) time.Time { return n.AddDate(-1, 0, 0) }},
	{"3y", "3 years", true, func(n time.Time) time.Time { return n.AddDate(-3, 0, 0) }},
	{"5y", "5 years", true, func(n time.Time) time.Time { return n.AddDate(-5, 0, 0) }},
	{"all", "All time", true, nil},
}

func periodByKey(k string) (Period, bool) {
	for _, p := range periods {
		if p.Key == k {
			return p, true
		}
	}
	return Period{}, false
}

var sydney, _ = time.LoadLocation("Australia/Sydney")

func today() time.Time { return time.Now().In(sydney) }

// lastTradingDay is today on a weekday, else the Friday before (public holidays not handled).
func lastTradingDay(n time.Time) time.Time {
	switch n.Weekday() {
	case time.Saturday:
		return n.AddDate(0, 0, -1)
	case time.Sunday:
		return n.AddDate(0, 0, -2)
	}
	return n
}

func prevWeekday(n time.Time) time.Time {
	n = n.AddDate(0, 0, -1)
	for n.Weekday() == time.Saturday || n.Weekday() == time.Sunday {
		n = n.AddDate(0, 0, -1)
	}
	return n
}

// dayLabel is "Today" on a weekday, else the name of the last trading day.
func dayLabel() string {
	n := today()
	if d := lastTradingDay(n); d.Day() != n.Day() {
		return d.Format("Monday")
	}
	return "Today"
}

func portfolios(filter string) ([]Portfolio, error) {
	var r struct {
		Portfolios []Portfolio `json:"portfolios"`
	}
	if err := get("portfolios", nil, &r); err != nil {
		return nil, err
	}
	if filter == "" {
		return r.Portfolios, nil
	}
	var out []Portfolio
	for _, p := range r.Portfolios {
		if strings.EqualFold(p.Name, filter) || fmt.Sprint(p.ID) == filter {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		var names []string
		for _, p := range r.Portfolios {
			names = append(names, p.Name)
		}
		return nil, fmt.Errorf("no portfolio %q (have: %s)", filter, strings.Join(names, ", "))
	}
	return out, nil
}

// periodStart returns the report start date, clamped to the portfolio's inception.
func periodStart(p Portfolio, per Period) string {
	if per.start == nil {
		return p.InceptionDate
	}
	s := per.start(today()).Format("2006-01-02")
	if p.InceptionDate != "" && s < p.InceptionDate {
		return p.InceptionDate
	}
	return s
}

func performance(p Portfolio, start string) (*Report, error) {
	q := url.Values{"start_date": {start}, "end_date": {today().Format("2006-01-02")}}
	var r struct {
		Report Report `json:"report"`
	}
	if err := get(fmt.Sprintf("portfolios/%d/performance", p.ID), q, &r); err != nil {
		return nil, err
	}
	return &r.Report, nil
}

// PeriodData holds one portfolio's report for one period.
type PeriodData struct {
	Report *Report
}

// fetchAll gets the listed periods for every portfolio, concurrently.
func fetchAll(ps []Portfolio, keys []string) (map[int]map[string]*PeriodData, error) {
	out := map[int]map[string]*PeriodData{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	for _, p := range ps {
		out[p.ID] = map[string]*PeriodData{}
		for _, k := range keys {
			per, _ := periodByKey(k)
			pd := &PeriodData{}
			out[p.ID][k] = pd
			wg.Add(1)
			go func(p Portfolio, per Period, pd *PeriodData) {
				defer wg.Done()
				r, err := performance(p, periodStart(p, per))
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					return
				}
				pd.Report = r
			}(p, per, pd)
		}
	}
	wg.Wait()
	return out, firstErr
}

type IndexChart struct {
	Dates []string `json:"dates"`
	Lines []struct {
		Label  string    `json:"label"`
		Type   string    `json:"type"`
		Values []float64 `json:"values"`
	} `json:"lines"`
}

func indexChart(p Portfolio, start string) (*IndexChart, error) {
	var c IndexChart
	q := url.Values{"start_date": {start}, "end_date": {today().Format("2006-01-02")}}
	err := get(fmt.Sprintf("portfolios/%d/performance_index_chart", p.ID), q, &c)
	return &c, err
}

// ---- price history ----
// Sharesight has no per-holding chart, so a holding's price history comes from performance reports
// ending on past dates: one report per date covers every holding in the portfolio. Today's point
// comes from the report already loaded.

type pricePoint struct {
	Date  string
	Price float64
	Qty   float64
}

// split is a share split (or consolidation) found in a price history: Ratio new shares per old.
type split struct {
	Date  string
	Ratio float64
}

// splitRatio is true when r (or 1/r, for a consolidation) is a usual split ratio. Real splits change
// the quantity held exactly; a buy or sale in the same month as a price fall usually doesn't land
// on one of these.
func splitRatio(r float64) bool {
	if r < 1 {
		r = 1 / r
	}
	for _, s := range []float64{1.5, 2, 2.5, 3, 4, 5, 6, 7, 8, 10, 15, 20, 25, 30, 40, 50, 100} {
		if math.Abs(r/s-1) < 0.01 {
			return true
		}
	}
	return false
}

// adjustSplits rescales prices before each split so the series is comparable with today's price.
// Sharesight reports past prices as they were, so a 15:1 split looks like a 93% fall. A split
// shows as the quantity held changing by some factor while the price moves by its inverse.
func adjustSplits(pts []pricePoint) ([]pricePoint, []split) {
	out := append([]pricePoint(nil), pts...)
	var found []split
	factor := 1.0
	for i := len(out) - 1; i > 0; i-- {
		a, b := pts[i-1], pts[i]
		if a.Qty > 0 && b.Qty > 0 && a.Price > 0 {
			r := b.Qty / a.Qty
			if splitRatio(r) && math.Abs(b.Price/a.Price*r-1) < 0.2 {
				factor /= r
				found = append([]split{{b.Date, r}}, found...)
			}
		}
		out[i-1].Price = pts[i-1].Price * factor
	}
	return out, found
}

// histTTL is how long a past-dated report is kept. Past figures rarely change, but a transaction
// entered late can alter them, so they don't last forever.
const histTTL = 7 * 24 * time.Hour

// chartPeriods are the price chart's periods. Each uses a fixed date grid (weekdays, Fridays or
// month ends) so the reports stay cached as the days go by, and periods on the same grid share them.
var chartPeriods = []struct {
	Key, Grid string
	Back      func(time.Time) time.Time // start of the period; nil = since inception
}{
	{"1m", "daily", func(t time.Time) time.Time { return t.AddDate(0, -1, 0) }},
	{"3m", "weekly", func(t time.Time) time.Time { return t.AddDate(0, -3, 0) }},
	{"6m", "weekly", func(t time.Time) time.Time { return t.AddDate(0, -6, 0) }},
	{"1y", "weekly", func(t time.Time) time.Time { return t.AddDate(-1, 0, 0) }},
	{"3y", "monthly", func(t time.Time) time.Time { return t.AddDate(-3, 0, 0) }},
	{"5y", "monthly", func(t time.Time) time.Time { return t.AddDate(-5, 0, 0) }},
	{"all", "monthly", nil},
}

const defaultChartPeriod = 3 // 1y

// historyDates are the grid dates for chart period per in the time before now (not today, not
// before inception), oldest first.
func historyDates(p Portfolio, now time.Time, per int) []string {
	cp := chartPeriods[per]
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	from := time.Time{}
	if cp.Back != nil {
		from = cp.Back(end)
	}
	if inc, err := time.ParseInLocation("2006-01-02", p.InceptionDate, now.Location()); err == nil && inc.After(from) {
		from = inc
	}
	var out []string
	add := func(d time.Time) bool {
		if d.Before(from) {
			return false
		}
		out = append(out, d.Format("2006-01-02"))
		return true
	}
	d := end.AddDate(0, 0, -1)
	switch cp.Grid {
	case "daily":
		for ; ; d = d.AddDate(0, 0, -1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			if !add(d) {
				break
			}
		}
	case "weekly":
		for d.Weekday() != time.Friday {
			d = d.AddDate(0, 0, -1)
		}
		for add(d) {
			d = d.AddDate(0, 0, -7)
		}
	case "monthly":
		// last day of each month before today
		m := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, end.Location()).AddDate(0, 0, -1)
		for add(m) {
			m = time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, m.Location()).AddDate(0, 0, -1)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// priceHistory fetches p's prices on chart period per's dates, by holding ID, oldest first.
// Dates that fail are left out; failed says how many.
func priceHistory(p Portfolio, per int) (byHolding map[int][]pricePoint, failed int, err error) {
	dates := historyDates(p, today(), per)
	reports := make([]*Report, len(dates))
	errs := make([]error, len(dates))
	var wg sync.WaitGroup
	for i, d := range dates {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			var r struct {
				Report Report `json:"report"`
			}
			q := url.Values{"start_date": {p.InceptionDate}, "end_date": {d}}
			if errs[i] = getTTL(fmt.Sprintf("portfolios/%d/performance", p.ID), q, &r, histTTL); errs[i] == nil {
				reports[i] = &r.Report
			}
		}(i, d)
	}
	wg.Wait()
	byHolding = map[int][]pricePoint{}
	for i, r := range reports {
		if r == nil {
			failed++
			err = errs[i]
			continue
		}
		for _, h := range r.Holdings {
			if h.Quantity != 0 && h.Price != 0 {
				byHolding[h.ID] = append(byHolding[h.ID], pricePoint{dates[i], h.Price, h.Quantity})
			}
		}
	}
	if failed < len(dates) {
		err = nil // partial history is still worth showing
	}
	return byHolding, failed, err
}
