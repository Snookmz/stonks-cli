package main

import (
	"fmt"
	"strings"
	"testing"

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
	m := &model{cursor: map[int]int{}, offset: map[int]int{}}
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
