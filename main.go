// stonks: your Sharesight portfolios in the terminal. Read-only (GET requests only).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/guptarohit/asciigraph"
)

const usage = `stonks: your Sharesight portfolios in the terminal

  stonks                     interactive: tabs per portfolio, drill into each holding
                             (prints the summary instead when not run in a terminal)
  stonks summary             summary: value and gains for every period
  stonks holdings [-s KEY]   every holding; sort by value (default), today, 1m, 1y, all, code
  stonks show CODE           one holding in detail (e.g. stonks show CBA)
  stonks chart [PERIOD]      performance chart; PERIOD = 1m 1y (default) 3y 5y all
  stonks history [N]         daily snapshots saved by 'stonks pull' (last N, default 20)
  stonks pull                save today's snapshot (cron runs this on weekdays)

  -p NAME    only this portfolio (e.g. -p NAB)
  --fresh    ignore the 10-minute cache

Data from Sharesight. Figures only, not advice.`

func main() {
	var args []string
	pf, sortKey := "", "value"
	a := os.Args[1:]
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "-p", "--portfolio":
			if i+1 < len(a) {
				i++
				pf = a[i]
			}
		case "-s", "--sort":
			if i+1 < len(a) {
				i++
				sortKey = strings.ToLower(a[i])
			}
		case "--fresh":
			fresh = true
		case "-h", "--help", "help":
			fmt.Println(usage)
			return
		default:
			args = append(args, a[i])
		}
	}
	cmd := "summary"
	if interactive() {
		cmd = "ui"
	}
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "summary", "s":
		err = cmdSummary(pf)
	case "holdings", "h":
		err = cmdHoldings(pf, sortKey)
	case "show":
		if len(args) == 0 {
			err = fmt.Errorf("which holding? e.g. stonks show CBA")
		} else {
			err = cmdShow(pf, args[0])
		}
	case "chart", "c":
		per := "1y"
		if len(args) > 0 {
			per = args[0]
		}
		err = cmdChart(pf, per)
	case "history":
		n := 20
		if len(args) > 0 {
			fmt.Sscan(args[0], &n)
		}
		err = cmdHistory(pf, n)
	case "ui", "i":
		err = cmdUI(pf)
	case "pull":
		err = cmdPull()
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "stonks:", err)
		os.Exit(1)
	}
}

func periodKeys() []string {
	var k []string
	for _, p := range periods {
		k = append(k, p.Key)
	}
	return k
}

func label(per Period) string {
	if per.Key == "today" {
		return dayLabel()
	}
	return per.Label
}

// ---- summary ----

func cmdSummary(pf string) error {
	ps, err := portfolios(pf)
	if err != nil {
		return err
	}
	data, err := fetchAll(ps, periodKeys())
	if err != nil {
		return err
	}
	fmt.Println()
	var total float64
	for _, p := range ps {
		all := data[p.ID]["all"].Report
		total += all.Value
		fmt.Printf("  %s   %s   %s\n\n", bold(p.Name), bold(money(all.Value)), dim("as of "+all.EndDate))
		summaryTable(func(k string) (Gains, bool) {
			return data[p.ID][k].Report.Gains, false
		})
		a := all.Gains
		fmt.Printf("  %s  capital %s · dividends %s · currency %s\n\n", dim("All time:"),
			signed(a.CapitalGain, moneySigned(a.CapitalGain)), signed(a.PayoutGain, moneySigned(a.PayoutGain)),
			signed(a.CurrencyGain, moneySigned(a.CurrencyGain)))
	}
	if len(ps) > 1 {
		fmt.Printf("  %s   %s\n\n", bold("Combined"), bold(money(total)))
		summaryTable(func(k string) (Gains, bool) { return combinedGains(ps, data, k, total) })
		fmt.Println()
	}
	fmt.Println(dim("  Sharesight's returns; over a year they're per annum (≈ = value-weighted average). Not advice."))
	fmt.Println()
	return nil
}

// combinedGains adds up period k across portfolios (total = their combined value). The bool is true
// when the % is approximate: p.a. figures can't be added up, so they're a value-weighted average.
func combinedGains(ps []Portfolio, data map[int]map[string]*PeriodData, k string, total float64) (Gains, bool) {
	per, _ := periodByKey(k)
	var g Gains
	var base, wsum float64
	for _, p := range ps {
		r := data[p.ID][k].Report
		g.TotalGain += r.TotalGain
		base += impliedBase(r)
		wsum += r.TotalGainPercent * r.Value
	}
	if per.Annualise {
		if total != 0 {
			g.TotalGainPercent = wsum / total
		}
		return g, true
	}
	if base != 0 {
		g.TotalGainPercent = g.TotalGain / base * 100
	}
	return g, false
}

// impliedBase backs out the capital a % was measured against, so portfolios can be combined.
func impliedBase(r *Report) float64 {
	if math.Abs(r.TotalGainPercent) > 0.005 {
		return r.TotalGain / r.TotalGainPercent * 100
	}
	return r.Value - r.TotalGain
}

// summaryTable prints one row per period. get returns the gains and whether the % is approximate.
func summaryTable(get func(k string) (Gains, bool)) { fmt.Print(summaryString(get)) }

func summaryString(get func(k string) (Gains, bool)) string {
	var rows [][]string
	for _, per := range periods {
		g, approx := get(per.Key)
		p := pct(g.TotalGainPercent)
		if approx {
			p = "≈" + p
		}
		pa := ""
		if per.Annualise {
			pa = dim("p.a.")
		}
		rows = append(rows, []string{label(per), signed(g.TotalGain, moneySigned(g.TotalGain)),
			signed(g.TotalGainPercent, p), pa})
	}
	return tableString([]string{"", "gain", "return", ""}, rows, []bool{false, true, true, false})
}

// ---- holdings ----

type hrow struct {
	pf   string
	h    Holding
	per  map[string]float64 // period key -> total gain %
	gain float64            // all-time $
}

// holdingKeys are the periods the holdings table shows.
var holdingKeys = []string{"today", "1m", "1y", "all"}

// holdingRows lists every holding in ps with its % for each holdingKeys period, plus the total value.
func holdingRows(ps []Portfolio, data map[int]map[string]*PeriodData) ([]hrow, float64) {
	var rows []hrow
	var total float64
	for _, p := range ps {
		all := data[p.ID]["all"].Report
		total += all.Value
		for _, h := range all.Holdings {
			r := hrow{pf: p.Name, h: h, per: map[string]float64{}, gain: h.TotalGain}
			for _, k := range holdingKeys {
				for _, x := range data[p.ID][k].Report.Holdings {
					if x.ID == h.ID {
						r.per[k] = x.TotalGainPercent
					}
				}
			}
			rows = append(rows, r)
		}
	}
	return rows, total
}

var sortKeys = []string{"value", "today", "1m", "1y", "all", "code"}

func sortHoldings(rows []hrow, sortKey string) {
	sort.SliceStable(rows, func(i, j int) bool {
		switch sortKey {
		case "code", "name":
			return rows[i].h.Instrument.Code < rows[j].h.Instrument.Code
		case "today", "1m", "1y", "all":
			return rows[i].per[sortKey] > rows[j].per[sortKey]
		}
		return rows[i].h.Value > rows[j].h.Value
	})
}

// holdingTable builds the holdings table; weight is relative to total. multi adds a portfolio column.
func holdingTable(rows []hrow, total float64, multi bool) (hdr []string, out [][]string, align []bool) {
	hdr = []string{"code", "name", "value", "weight", dayLabel(), "1 month", "1 year", "all time p.a.", "all-time gain"}
	align = []bool{false, false, true, true, true, true, true, true, true}
	if multi {
		hdr = append(hdr, "portfolio")
		align = append(align, false)
	}
	for _, r := range rows {
		c := []string{bold(r.h.Instrument.Code), truncate(r.h.Instrument.Name, 24), money(r.h.Value),
			fmt.Sprintf("%.1f%%", r.h.Value/total*100)}
		for _, k := range holdingKeys {
			c = append(c, signed(r.per[k], pct(r.per[k])))
		}
		c = append(c, signed(r.gain, moneySigned(r.gain)))
		if multi {
			c = append(c, dim(r.pf))
		}
		out = append(out, c)
	}
	return hdr, out, align
}

func cmdHoldings(pf, sortKey string) error {
	ps, err := portfolios(pf)
	if err != nil {
		return err
	}
	data, err := fetchAll(ps, holdingKeys)
	if err != nil {
		return err
	}
	rows, total := holdingRows(ps, data)
	sortHoldings(rows, sortKey)
	hdr, out, align := holdingTable(rows, total, len(ps) > 1)
	fmt.Println()
	table(hdr, out, align)
	fmt.Printf("\n  %d holdings, %s\n\n", len(rows), bold(money(total)))
	return nil
}

// ---- show ----

func cmdShow(pf, code string) error {
	ps, err := portfolios(pf)
	if err != nil {
		return err
	}
	data, err := fetchAll(ps, periodKeys())
	if err != nil {
		return err
	}
	found := false
	for _, p := range ps {
		all := data[p.ID]["all"].Report
		for _, h := range all.Holdings {
			if !strings.EqualFold(h.Instrument.Code, code) {
				continue
			}
			found = true
			fmt.Print(holdingDetail(p, data[p.ID], h))
		}
	}
	if !found {
		return fmt.Errorf("no holding %q (try: stonks holdings)", strings.ToUpper(code))
	}
	return nil
}

// holdingDetail renders one holding: what it is, the position, and its gains for every period.
func holdingDetail(p Portfolio, d map[string]*PeriodData, h Holding) string {
	var b strings.Builder
	all := d["all"].Report
	in := h.Instrument
	fmt.Fprintf(&b, "\n  %s  %s  %s\n", bold(in.Code), in.Name, dim(in.Market+" · "+p.Name))
	if in.Sector != "" {
		fmt.Fprintf(&b, "  %s\n", dim(in.Sector+" · "+in.Industry))
	}
	fmt.Fprintf(&b, "\n  %s × %s = %s   %s\n\n", commas(h.Quantity, 0), "$"+commas(h.Price, 3), bold(money(h.Value)),
		dim(fmt.Sprintf("%.1f%% of %s", h.Value/all.Value*100, p.Name)))
	b.WriteString(summaryString(func(k string) (Gains, bool) {
		for _, x := range d[k].Report.Holdings {
			if x.ID == h.ID {
				return x.Gains, false
			}
		}
		return Gains{}, false
	}))
	fmt.Fprintf(&b, "  %s  capital %s · dividends %s · currency %s\n\n", dim("All time:"),
		signed(h.CapitalGain, moneySigned(h.CapitalGain)), signed(h.PayoutGain, moneySigned(h.PayoutGain)),
		signed(h.CurrencyGain, moneySigned(h.CurrencyGain)))
	return b.String()
}

// ---- chart ----

func cmdChart(pf, perKey string) error {
	per, ok := periodByKey(strings.ToLower(perKey))
	if !ok || per.Key == "today" {
		return fmt.Errorf("period must be one of 1w 1m ytd 1y 3y 5y all")
	}
	ps, err := portfolios(pf)
	if err != nil {
		return err
	}
	for _, p := range ps {
		c, err := indexChart(p, periodStart(p, per))
		if err != nil {
			return err
		}
		for _, l := range c.Lines {
			if l.Type != "PORTFOLIO" || len(l.Values) < 2 {
				continue
			}
			vals := make([]float64, len(l.Values))
			for i, v := range l.Values {
				vals[i] = (v/l.Values[0] - 1) * 100
			}
			last := vals[len(vals)-1]
			fmt.Printf("\n  %s  %s  %s\n\n", bold(p.Name), label(per), signed(last, pct(last)))
			g := asciigraph.Plot(vals, asciigraph.Height(14), asciigraph.Width(termWidth()-14), asciigraph.Precision(1),
				asciigraph.Offset(4), asciigraph.Caption(fmt.Sprintf("%% return, %s to %s", c.Dates[0], c.Dates[len(c.Dates)-1])))
			fmt.Println(g)
		}
	}
	fmt.Println()
	return nil
}

// ---- snapshots ----

type snapHolding struct {
	Code     string  `json:"code"`
	Market   string  `json:"market"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
	Value    float64 `json:"value"`
}

type snapshot struct {
	Date      string        `json:"date"`
	Portfolio string        `json:"portfolio"`
	Value     float64       `json:"value"`
	Gain      float64       `json:"all_time_gain"`
	Holdings  []snapHolding `json:"holdings"`
}

var snapPath = filepath.Join(dataDir, "snapshots.jsonl")

func readSnaps() ([]snapshot, error) {
	f, err := os.Open(snapPath)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var s snapshot
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			out = append(out, s)
		}
	}
	return out, sc.Err()
}

func cmdPull() error {
	fresh = true
	ps, err := portfolios("")
	if err != nil {
		return err
	}
	data, err := fetchAll(ps, []string{"all"})
	if err != nil {
		return err
	}
	old, err := readSnaps()
	if err != nil {
		return err
	}
	date := today().Format("2006-01-02")
	var keep []snapshot
	for _, s := range old {
		if s.Date != date {
			keep = append(keep, s)
		}
	}
	for _, p := range ps {
		r := data[p.ID]["all"].Report
		s := snapshot{Date: date, Portfolio: p.Name, Value: r.Value, Gain: r.TotalGain}
		for _, h := range r.Holdings {
			s.Holdings = append(s.Holdings, snapHolding{h.Instrument.Code, h.Instrument.Market, h.Quantity, h.Price, h.Value})
		}
		keep = append(keep, s)
	}
	os.MkdirAll(dataDir, 0o700)
	tmp := snapPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, s := range keep {
		enc.Encode(s)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, snapPath); err != nil {
		return err
	}
	fmt.Printf("saved %s snapshot for %d portfolios\n", date, len(ps))
	return nil
}

func cmdHistory(pf string, n int) error {
	snaps, err := readSnaps()
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		return fmt.Errorf("no snapshots yet; 'stonks pull' saves one (cron runs it on weekdays)")
	}
	byDate := map[string]map[string]float64{}
	var names []string
	seen := map[string]bool{}
	for _, s := range snaps {
		if pf != "" && !strings.EqualFold(s.Portfolio, pf) {
			continue
		}
		if byDate[s.Date] == nil {
			byDate[s.Date] = map[string]float64{}
		}
		byDate[s.Date][s.Portfolio] = s.Value
		if !seen[s.Portfolio] {
			seen[s.Portfolio] = true
			names = append(names, s.Portfolio)
		}
	}
	var dates []string
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	hdr := append([]string{"date"}, names...)
	if len(names) > 1 {
		hdr = append(hdr, "total")
	}
	hdr = append(hdr, "change")
	align := make([]bool, len(hdr))
	for i := 1; i < len(align); i++ {
		align[i] = true
	}
	var rows [][]string
	prev := math.NaN()
	for _, d := range dates {
		row := []string{d}
		var t float64
		for _, nm := range names {
			v, ok := byDate[d][nm]
			t += v
			if ok {
				row = append(row, money(v))
			} else {
				row = append(row, dim("-"))
			}
		}
		if len(names) > 1 {
			row = append(row, bold(money(t)))
		}
		ch := ""
		if !math.IsNaN(prev) {
			ch = signed(t-prev, moneySigned(t-prev))
		}
		prev = t
		rows = append(rows, append(row, ch))
	}
	if len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	fmt.Println()
	table(hdr, rows, align)
	fmt.Println()
	return nil
}
