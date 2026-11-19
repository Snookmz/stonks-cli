package main

// areaChart: a filled price chart for the terminal. The y axis starts at 0 with the scale on the
// right; months and years run along the bottom. Eighth-height blocks give 8 levels per row, so the
// shape still shows when the price only moves within a few rows of the top.

import (
	"math"
	"strings"
	"time"
)

var eighths = []rune(" ▁▂▃▄▅▆▇█")

// tickUnit is what the timeline under a chart marks.
type tickUnit int

const (
	tickWeek    tickUnit = iota // Mondays, labelled "17 Nov"
	tickMonth                   // month starts, "Nov"
	tickQuarter                 // quarter starts, "Jan" "Apr" "Jul" "Oct"
	tickYear                    // year starts, "2026"
)

// unitStart is the start of the unit containing d.
func unitStart(u tickUnit, d time.Time) time.Time {
	switch u {
	case tickWeek:
		return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	case tickMonth:
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	case tickQuarter:
		return time.Date(d.Year(), (d.Month()-1)/3*3+1, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(d.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
}

func unitLabel(u tickUnit, d time.Time) string {
	switch u {
	case tickWeek:
		return d.Format("2 Jan")
	case tickYear:
		return d.Format("2006")
	}
	return d.Format("Jan")
}

// niceStep picks a tick step (1, 2, 2.5 or 5 × 10ⁿ) giving about n ticks up to max.
func niceStep(max float64, n int) float64 {
	raw := max / float64(n)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, f := range []float64{1, 2, 2.5, 5, 10} {
		if f*mag >= raw {
			return f * mag
		}
	}
	return 10 * mag
}

// areaChart draws vals (one per date, oldest first) in a w-wide block using at most rows lines,
// with a timeline marking unit. paintFn colours the filled area. It returns nil if there isn't room.
func areaChart(vals []float64, dates []string, w, rows int, unit tickUnit, paintFn func(string) string) []string {
	plotRows := rows - 3 // baseline, labels, years
	if len(vals) < 2 || plotRows < 3 {
		return nil
	}
	hi := 0.0
	for _, v := range vals {
		hi = math.Max(hi, v)
	}
	if hi <= 0 {
		return nil
	}
	// ticks every k rows; the chart is a whole number of ticks tall
	nTicks := max(2, min(5, plotRows/3))
	step := niceStep(hi, nTicks)
	nTicks = int(math.Ceil(hi/step - 1e-9))
	k := max(1, plotRows/nTicks)
	R := nTicks * k
	if R > plotRows {
		return nil
	}
	top := float64(nTicks) * step
	dec := 0
	if step < 1 {
		dec = 2
	} else if step != math.Trunc(step) {
		dec = 1
	}
	labels := make([]string, nTicks+1)
	lw := 0
	for j := range labels {
		labels[j] = commas(float64(j)*step, dec)
		lw = max(lw, len(labels[j]))
	}
	W := w - 2 - 2 - lw // left gutter, " ┤" or similar, label
	if W < 20 {
		return nil
	}

	// value at each column, interpolated between the weekly points
	col := make([]float64, W)
	for x := range col {
		t := float64(x) * float64(len(vals)-1) / float64(W-1)
		i := int(t)
		if i >= len(vals)-1 {
			col[x] = vals[len(vals)-1]
			continue
		}
		f := t - float64(i)
		col[x] = vals[i]*(1-f) + vals[i+1]*f
	}

	var out []string
	for r := 0; r < R; r++ {
		b := R - 1 - r // row from the bottom
		var sb strings.Builder
		for _, v := range col {
			fill := int(math.Round(v/top*float64(R)*8)) - b*8
			sb.WriteRune(eighths[max(0, min(8, fill))])
		}
		axis := "│"
		if (R-r)%k == 0 { // the top edge of this row is a tick
			axis = "├ " + labels[(R-r)/k]
		}
		out = append(out, "  "+paintFn(sb.String())+axis)
	}

	// baseline with a tick at each unit start, then labels and (below a year) the years
	pd := make([]time.Time, len(dates))
	for i, d := range dates {
		t, _ := time.Parse("2006-01-02", d)
		pd[i] = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	base := []rune(strings.Repeat("─", W))
	marks := []rune(strings.Repeat(" ", W+lw+2))
	years := []rune(strings.Repeat(" ", W+lw+2))
	put := func(line []rune, at int, s string) {
		for i, c := range []rune(s) {
			if at+i < len(line) {
				line[at+i] = c
			}
		}
	}
	if unit != tickYear {
		put(years, 0, pd[0].Format("2006"))
	}
	lastEnd, lastYearEnd := -1, 4
	for i := 1; i < len(pd); i++ {
		s := unitStart(unit, pd[i])
		if !s.After(pd[i-1]) {
			continue // no unit boundary between these two points
		}
		frac := s.Sub(pd[i-1]).Hours() / pd[i].Sub(pd[i-1]).Hours()
		x := int(math.Round((float64(i-1) + frac) * float64(W-1) / float64(len(vals)-1)))
		if x < 0 || x >= W {
			continue
		}
		base[x] = '┬'
		name := unitLabel(unit, s)
		if x <= lastEnd || x+len([]rune(name)) > W+lw+2 {
			continue
		}
		put(marks, x, name)
		lastEnd = x + len([]rune(name))
		if unit != tickYear && s.Year() != pd[i-1].Year() && x > lastYearEnd {
			put(years, x, s.Format("2006"))
			lastYearEnd = x + 4
		}
	}
	out = append(out, "  "+dim(string(base)+"┘ "+labels[0]), "  "+strings.TrimRight(string(marks), " "))
	if unit != tickYear {
		out = append(out, "  "+dim(strings.TrimRight(string(years), " ")))
	}
	return out
}
