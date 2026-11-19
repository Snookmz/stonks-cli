package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeData builds made-up portfolios: n holdings each, every period filled in.
func fakeData(names []string, n int) ([]Portfolio, map[int]map[string]*PeriodData) {
	var ps []Portfolio
	data := map[int]map[string]*PeriodData{}
	for pi, name := range names {
		p := Portfolio{ID: pi + 1, Name: name, InceptionDate: "2020-01-01"}
		ps = append(ps, p)
		data[p.ID] = map[string]*PeriodData{}
		for ki, k := range periodKeys() {
			r := &Report{Value: 0, EndDate: "2026-11-19"}
			for h := 0; h < n; h++ {
				v := float64(1000 * (h + 1))
				g := float64(ki+1) * float64(h-n/2)
				r.Holdings = append(r.Holdings, Holding{ID: pi*100 + h, Value: v, Quantity: 10, Price: v / 10,
					Instrument: Instrument{Code: fmt.Sprintf("H%02d", h), Name: fmt.Sprintf("Holding number %d", h), Market: "ASX"},
					Gains:      Gains{TotalGain: g * 10, TotalGainPercent: g}})
				r.Value += v
			}
			r.TotalGain, r.TotalGainPercent = 123, 1.5
			data[p.ID][k] = &PeriodData{Report: r}
		}
	}
	return ps, data
}

func drive(t *testing.T, m *model, keys ...string) string {
	return plain(driveRaw(t, m, keys...))
}

func driveRaw(t *testing.T, m *model, keys ...string) string {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
	return m.View()
}

func newTestModel(t *testing.T, names []string, n, w, h int) *model {
	m := &model{cursor: map[int]int{}, offset: map[int]int{}, chartPer: defaultChartPeriod}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	ps, data := fakeData(names, n)
	m.Update(loadedMsg{ps: ps, data: data})
	return m
}

func checkFits(t *testing.T, v string, w, h int) {
	t.Helper()
	lines := strings.Split(v, "\n")
	if len(lines) > h {
		t.Errorf("view is %d lines, window is %d:\n%s", len(lines), h, v)
	}
	for _, l := range lines {
		if width(l) > w {
			t.Errorf("line %d wide, window %d: %q", width(l), w, l)
		}
	}
}

// plain strips colour escapes so assertions work with colour on or off.
func plain(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc:
			esc = r != 'm'
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestUINavigation(t *testing.T) {
	saved := colour
	defer func() { colour = saved }()
	for _, colour = range []bool{false, true} {
		testNavigation(t)
	}
}

func testNavigation(t *testing.T) {
	for _, size := range [][2]int{{160, 50}, {100, 30}, {80, 24}} {
		w, h := size[0], size[1]
		m := newTestModel(t, []string{"Alpha", "Beta"}, 30, w, h)

		checkFits(t, m.View(), w, h) // with any colour escapes
		v := plain(m.View())
		if !strings.Contains(v, "All portfolios") || !strings.Contains(v, "Combined") {
			t.Fatalf("first tab should be Combined:\n%s", v)
		}
		checkFits(t, v, w, h)

		// enter on the second portfolio row opens its tab
		v = drive(t, m, "down", "enter")
		if m.portfolio() == nil || m.portfolio().Name != "Beta" {
			t.Fatalf("enter should open Beta, on tab %d", m.tab)
		}
		checkFits(t, v, w, h)

		// biggest holding first by default; scroll to the bottom keeps the cursor visible
		v = drive(t, m, "G")
		if !strings.Contains(v, "▸ H00") {
			t.Errorf("cursor should be on smallest holding H00 at the bottom:\n%s", v)
		}
		checkFits(t, v, w, h)

		v = drive(t, m, "enter")
		if !m.detail || !strings.Contains(v, "Holding number 0") {
			t.Fatalf("enter should open the holding:\n%s", v)
		}
		checkFits(t, v, w, h)
		v = drive(t, m, "k")
		if !strings.Contains(v, "Holding number 1") {
			t.Errorf("up in detail should move to the next holding:\n%s", v)
		}

		// sort by code keeps the same holding selected
		drive(t, m, "esc", "s", "s", "s", "s", "s")
		if sortKeys[m.sortIdx] != "code" || m.rows()[m.cursor[m.tab]].h.Instrument.Code != "H01" {
			t.Errorf("after sorting by code, want H01 selected, got %s", m.rows()[m.cursor[m.tab]].h.Instrument.Code)
		}

		drive(t, m, "tab")
		if m.tab != 0 {
			t.Errorf("tab should wrap to Combined, got %d", m.tab)
		}
		drive(t, m, "3")
		if m.portfolio().Name != "Beta" {
			t.Errorf("3 should jump to Beta")
		}
	}
}

func TestUISinglePortfolioHasNoCombinedTab(t *testing.T) {
	m := newTestModel(t, []string{"Solo"}, 5, 120, 40)
	if v := m.View(); strings.Contains(v, "Combined") || m.portfolio() == nil {
		t.Errorf("one portfolio should open straight on it:\n%s", v)
	}
}

func TestUIRefreshShrinksHoldings(t *testing.T) {
	m := newTestModel(t, []string{"Alpha", "Beta"}, 10, 120, 40)
	drive(t, m, "2", "G", "enter")
	ps, data := fakeData([]string{"Alpha", "Beta"}, 0)
	m.Update(loadedMsg{ps: ps, data: data})
	if m.detail {
		t.Errorf("detail should close when the portfolio has no holdings left")
	}
	_ = m.View()
}

func TestUIRetryFromErrorScreen(t *testing.T) {
	m := &model{cursor: map[int]int{}, offset: map[int]int{}, loading: true}
	m.Update(loadedMsg{err: fmt.Errorf("HTTP 403")})
	if !strings.Contains(m.View(), "Couldn't load") {
		t.Fatal("want error screen")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if !strings.Contains(m.View(), "Loading") {
		t.Errorf("r should show loading again:\n%s", m.View())
	}
}

func TestUIRefreshFailureKeepsData(t *testing.T) {
	m := newTestModel(t, []string{"Alpha", "Beta"}, 5, 120, 40)
	m.Update(loadedMsg{err: fmt.Errorf("HTTP 403")})
	v := m.View()
	if !strings.Contains(v, "refresh failed") || !strings.Contains(v, "Alpha") {
		t.Errorf("failed refresh should keep the earlier figures:\n%s", v)
	}
}

func TestHistoryDatesStable(t *testing.T) {
	p := Portfolio{InceptionDate: "2020-01-01"}
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, sydney)
	prev := historyDates(p, start, defaultChartPeriod)
	for i := 1; i < 30; i++ {
		day := start.AddDate(0, 0, i)
		cur := historyDates(p, day, defaultChartPeriod)
		old := map[string]bool{}
		for _, d := range prev {
			old[d] = true
		}
		added := 0
		for _, d := range cur {
			if !old[d] {
				added++
			}
			if dt, _ := time.Parse("2006-01-02", d); dt.Weekday() != time.Friday || d >= day.Format("2006-01-02") {
				t.Errorf("%s: %s should be a Friday before today", day.Format("2006-01-02"), d)
			}
		}
		if added > 1 {
			t.Errorf("%s: %d new dates, want at most 1", day.Format("2006-01-02"), added)
		}
		if len(cur) < 51 || len(cur) > 53 {
			t.Errorf("%s: %d weeks", day.Format("2006-01-02"), len(cur))
		}
		prev = cur
	}
	if d := historyDates(Portfolio{InceptionDate: "2025-12-20"}, start, defaultChartPeriod); len(d) != 1 || d[0] != "2025-12-26" {
		t.Errorf("inception should cut the history short: %v", d)
	}
}

// fakeHistory gives every holding in portfolio pid a year of weekly prices.
func fakeHistory(m *model, pid int) histMsg {
	prices := map[int][]pricePoint{}
	for _, h := range m.data[pid]["all"].Report.Holdings {
		for i, d := range historyDates(m.ps[pid-1], today(), m.chartPer) {
			prices[h.ID] = append(prices[h.ID], pricePoint{d, h.Price * (0.8 + float64(i%13)/30), h.Quantity})
		}
	}
	return histMsg{key: histKey{pid, m.chartPer}, prices: prices}
}

func TestUIPriceChart(t *testing.T) {
	saved := colour
	defer func() { colour = saved }()
	for _, colour = range []bool{false, true} {
		for _, size := range [][2]int{{160, 50}, {130, 40}, {100, 30}, {80, 24}} {
			w, h := size[0], size[1]
			m := newTestModel(t, []string{"Alpha", "Beta"}, 12, w, h)
			drive(t, m, "2")
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("opening a holding should start the price history fetch")
			}
			if v := plain(m.View()); !strings.Contains(v, "Loading the price history") && h > 24 {
				t.Errorf("want a loading note:\n%s", v)
			}
			checkFits(t, m.View(), w, h)
			// moving to other holdings, before and after it arrives, fetches nothing more
			if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown}); cmd != nil {
				t.Error("moving while history loads should not fetch again")
			}
			m.Update(fakeHistory(m, 1))
			if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyUp}); cmd != nil {
				t.Error("moving with history loaded should not fetch again")
			}
			drive(t, m, "esc")
			if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
				t.Error("reopening should not fetch again")
			}
			v := m.View()
			checkFits(t, v, w, h)
			if h >= 40 && !strings.Contains(plain(v), "since ") {
				t.Errorf("%dx%d: want a chart:\n%s", w, h, plain(v))
			}
		}
	}
}

func TestUIPriceChartErrorAndRetry(t *testing.T) {
	m := newTestModel(t, []string{"Alpha", "Beta"}, 5, 130, 40)
	drive(t, m, "2", "enter")
	m.Update(histMsg{key: histKey{1, m.chartPer}, err: fmt.Errorf("HTTP 403")})
	if v := plain(m.View()); !strings.Contains(v, "Price history unavailable") {
		t.Errorf("want error note:\n%s", v)
	}
	drive(t, m, "esc")
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Error("reopening after a failure should try again")
	}
}

func TestAreaChart(t *testing.T) {
	p := Portfolio{InceptionDate: "2020-01-01"}
	dates := historyDates(p, time.Date(2026, 11, 19, 9, 0, 0, 0, sydney), defaultChartPeriod)
	vals := make([]float64, len(dates))
	for i := range vals {
		vals[i] = 60 + float64(i%10)
	}
	for _, w := range []int{60, 100, 158} {
		lines := areaChart(vals, dates, w, 15, tickMonth, func(s string) string { return s })
		if lines == nil {
			t.Fatalf("w=%d: no chart", w)
		}
		n := len(lines)
		if !strings.HasSuffix(lines[n-3], "┘ 0") {
			t.Errorf("baseline should end at 0 on the right: %q", lines[n-3])
		}
		if !strings.Contains(lines[n-2], "Jan") || !strings.Contains(lines[n-1], "2026") {
			t.Errorf("want months and years under the chart:\n%s\n%s", lines[n-2], lines[n-1])
		}
		if !strings.Contains(lines[0], "├ ") {
			t.Errorf("top row should carry the top price label: %q", lines[0])
		}
		for _, l := range lines {
			if width(l) > w {
				t.Errorf("w=%d: line is %d wide: %q", w, width(l), l)
			}
		}
	}
	if areaChart(vals, dates, 30, 4, tickMonth, func(s string) string { return s }) != nil {
		t.Error("too small should give no chart")
	}
}

func TestHistoryDatesAllPeriods(t *testing.T) {
	p := Portfolio{InceptionDate: "2020-03-15"}
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, sydney)
	want := map[string][2]int{"1m": {20, 24}, "3m": {12, 14}, "6m": {25, 27}, "1y": {51, 53},
		"3y": {35, 37}, "5y": {59, 61}, "all": {69, 71}}
	for per, cp := range chartPeriods {
		prev := historyDates(p, start, per)
		for i := 1; i < 60; i++ {
			day := start.AddDate(0, 0, i)
			cur := historyDates(p, day, per)
			old := map[string]bool{}
			for _, d := range prev {
				old[d] = true
			}
			added := 0
			for j, d := range cur {
				if !old[d] {
					added++
				}
				dt, _ := time.Parse("2006-01-02", d)
				ok := map[string]bool{
					"daily":   dt.Weekday() != time.Saturday && dt.Weekday() != time.Sunday,
					"weekly":  dt.Weekday() == time.Friday,
					"monthly": dt.AddDate(0, 0, 1).Day() == 1,
				}[cp.Grid]
				if !ok || d >= day.Format("2006-01-02") || d < p.InceptionDate || (j > 0 && d <= cur[j-1]) {
					t.Errorf("%s on %s: bad date %s", cp.Key, day.Format("2006-01-02"), d)
				}
			}
			if added > 1 {
				t.Errorf("%s on %s: %d new dates, want at most 1", cp.Key, day.Format("2006-01-02"), added)
			}
			if w := want[cp.Key]; i == 1 && (len(cur) < w[0] || len(cur) > w[1]) {
				t.Errorf("%s: %d dates, want %d-%d", cp.Key, len(cur), w[0], w[1])
			}
			prev = cur
		}
	}
}

func TestUIChartPeriods(t *testing.T) {
	saved := colour
	defer func() { colour = saved }()
	for _, colour = range []bool{false, true} {
		for _, size := range [][2]int{{130, 45}, {80, 24}} {
			w, h := size[0], size[1]
			m := newTestModel(t, []string{"Alpha", "Beta"}, 8, w, h)
			drive(t, m, "2", "enter")
			m.Update(fakeHistory(m, 1))
			for per := m.chartPer + 1; per < len(chartPeriods); per++ {
				if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}); cmd == nil || m.chartPer != per {
					t.Fatalf("] should move to %s and fetch it", chartPeriods[per].Key)
				}
				m.Update(fakeHistory(m, 1))
				v := m.View()
				checkFits(t, v, w, h)
				if h >= 45 && !strings.Contains(plain(v), "since ") {
					t.Errorf("%s: want a chart:\n%s", chartPeriods[per].Key, plain(v))
				}
				if h >= 45 && !strings.Contains(plain(v), " "+chartPeriods[per].Key+" ") {
					t.Errorf("selector should show %s", chartPeriods[per].Key)
				}
			}
			drive(t, m, "]") // already at the end
			if m.chartPer != len(chartPeriods)-1 {
				t.Error("] at the last period should stay there")
			}
			for m.chartPer > 0 {
				if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")}); cmd != nil && m.chartPer >= defaultChartPeriod {
					t.Errorf("going back to %s (already loaded) should not fetch", chartPeriods[m.chartPer].Key)
				}
				m.Update(fakeHistory(m, 1))
				checkFits(t, m.View(), w, h)
			}
			// the period sticks across holdings and tabs
			drive(t, m, "j", "esc", "3", "enter")
			if m.chartPer != 0 {
				t.Error("chart period should be kept when moving around")
			}
		}
	}
}

func TestAdjustSplits(t *testing.T) {
	pts := []pricePoint{{"2022-10-31", 600, 100}, {"2022-11-30", 610, 100}, {"2022-12-31", 41, 1500},
		{"2023-01-31", 42, 1500}, {"2023-02-28", 43, 1600}} // a 15:1 split, then a small buy
	adj, splits := adjustSplits(pts)
	if len(splits) != 1 || splits[0].Ratio != 15 || splits[0].Date != "2022-12-31" {
		t.Fatalf("want one 15:1 split, got %v", splits)
	}
	if adj[0].Price != 40 || adj[2].Price != 41 || adj[4].Price != 43 {
		t.Errorf("prices before the split should be divided by 15: %v", adj)
	}
	if pts[0].Price != 600 {
		t.Error("input should not change")
	}
	// 22% more shares in a month the price fell 10% (seen in real data) is a buy, not a split
	if _, s := adjustSplits([]pricePoint{{"a", 50, 1000}, {"b", 45, 1223}}); len(s) != 0 {
		t.Errorf("a buy is not a split: %v", s)
	}
	// a 1:10 consolidation
	if adj, s := adjustSplits([]pricePoint{{"a", 0.5, 10000}, {"b", 5.2, 1000}}); len(s) != 1 || adj[0].Price != 5 {
		t.Errorf("want a 1:10 consolidation: %v %v", adj, s)
	}
	// buying more shares with no price drop is not a split
	if _, s := adjustSplits([]pricePoint{{"a", 50, 100}, {"b", 51, 200}}); len(s) != 0 {
		t.Errorf("doubling holdings is not a split: %v", s)
	}
}
