package main

import (
	"fmt"
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
	sem := make(chan struct{}, 2)
	for _, p := range ps {
		out[p.ID] = map[string]*PeriodData{}
		for _, k := range keys {
			per, _ := periodByKey(k)
			pd := &PeriodData{}
			out[p.ID][k] = pd
			wg.Add(1)
			go func(p Portfolio, per Period, pd *PeriodData) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
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
