package main

// stonks ui: an interactive view. One tab for all portfolios together, one per portfolio with its
// holdings, and a detail page per holding. Everything comes from a single fetch (the same requests
// `stonks` makes), so moving around never touches the network; only `r` fetches again.

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

type loadedMsg struct {
	ps   []Portfolio
	data map[int]map[string]*PeriodData
	err  error
}

// load fetches every period for every portfolio. Only one runs at a time (fetchAll's limit of 2
// concurrent requests is per call), so the model never starts a second while one is in flight.
func load(pf string, refresh bool) tea.Cmd {
	return func() tea.Msg {
		fresh = refresh
		defer func() { fresh = false }()
		ps, err := portfolios(pf)
		if err != nil {
			return loadedMsg{err: err}
		}
		data, err := fetchAll(ps, periodKeys())
		return loadedMsg{ps: ps, data: data, err: err}
	}
}

type model struct {
	pf       string
	fresh    bool // --fresh on the command line: skip the cache on the first load
	ps       []Portfolio
	data     map[int]map[string]*PeriodData
	loading  bool
	err      error
	loadedAt time.Time

	tab     int         // 0 = Combined when there's more than one portfolio
	cursor  map[int]int // per tab
	offset  map[int]int // first visible holding row, per tab
	sortIdx int
	detail  bool
	w, h    int
}

// interactive is true when stonks is attached to a terminal both ways (not a pipe or cron).
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func cmdUI(pf string) error {
	if !interactive() {
		return fmt.Errorf("stonks ui needs a terminal (try 'stonks summary' instead)")
	}
	m := &model{pf: pf, fresh: fresh, loading: true, cursor: map[int]int{}, offset: map[int]int{}, w: 80, h: 24}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) Init() tea.Cmd { return load(m.pf, m.fresh) }

func (m *model) combined() bool { return len(m.ps) > 1 }

func (m *model) tabCount() int {
	if m.combined() {
		return len(m.ps) + 1
	}
	return len(m.ps)
}

// portfolio returns the portfolio shown on the current tab, or nil on the Combined tab.
func (m *model) portfolio() *Portfolio {
	i := m.tab
	if m.combined() {
		i--
	}
	if i < 0 || i >= len(m.ps) {
		return nil
	}
	return &m.ps[i]
}

// rows are the selectable rows on the current tab: portfolios on Combined, else holdings.
func (m *model) rows() []hrow {
	p := m.portfolio()
	if p == nil {
		return nil
	}
	rows, _ := holdingRows([]Portfolio{*p}, m.data)
	sortHoldings(rows, sortKeys[m.sortIdx])
	return rows
}

func (m *model) rowCount() int {
	if m.portfolio() == nil {
		return len(m.ps)
	}
	return len(m.rows())
}

func (m *model) move(d int) {
	n := m.rowCount()
	if n == 0 {
		m.cursor[m.tab] = 0
		return
	}
	m.cursor[m.tab] = max(0, min(n-1, m.cursor[m.tab]+d))
}

func (m *model) setTab(t int) {
	if n := m.tabCount(); n > 0 {
		m.tab = (t%n + n) % n
		m.detail = false
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err // keep whatever loaded before
			return m, nil
		}
		m.err = nil
		m.ps, m.data, m.loadedAt = msg.ps, msg.data, time.Now()
		if m.tab >= m.tabCount() {
			m.tab = 0
		}
		m.move(0) // holdings can change on a refresh; keep the cursor in range (views clamp the other tabs)
		if m.rowCount() == 0 {
			m.detail = false
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, load(m.pf, true)
			}
		}
		if m.data == nil {
			return m, nil
		}
		switch k := msg.String(); k {
		case "tab", "right", "l":
			m.setTab(m.tab + 1)
		case "shift+tab", "left", "h":
			m.setTab(m.tab - 1)
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if t := int(k[0] - '1'); t < m.tabCount() {
				m.setTab(t)
			}
		case "down", "j":
			m.move(1)
		case "up", "k":
			m.move(-1)
		case "pgdown", "ctrl+d", " ":
			m.move(10)
		case "pgup", "ctrl+u":
			m.move(-10)
		case "home", "g":
			m.move(-1 << 20)
		case "end", "G":
			m.move(1 << 20)
		case "enter":
			if m.portfolio() == nil {
				m.setTab(m.cursor[m.tab] + 1)
			} else if m.rowCount() > 0 {
				m.detail = true
			}
		case "esc", "backspace":
			m.detail = false
		case "s":
			if m.portfolio() != nil && !m.detail {
				// keep the same holding selected after re-sorting
				rows := m.rows()
				id := -1
				if c := m.cursor[m.tab]; c < len(rows) {
					id = rows[c].h.ID
				}
				m.sortIdx = (m.sortIdx + 1) % len(sortKeys)
				for i, r := range m.rows() {
					if r.h.ID == id {
						m.cursor[m.tab] = i
					}
				}
			}
		}
	}
	return m, nil
}

// ---- view ----

func (m *model) View() string {
	if m.data == nil {
		if m.err != nil && !m.loading {
			return "\n  " + paint("31", "Couldn't load from Sharesight:") + "\n\n  " + m.err.Error() +
				"\n\n  " + dim("r to try again · q to quit") + "\n"
		}
		return "\n  Loading your portfolios from Sharesight…\n"
	}
	var body string
	switch p := m.portfolio(); {
	case p == nil:
		body = m.viewCombined()
	case m.detail:
		body = m.viewDetail(*p)
	default:
		body = m.viewPortfolio(*p)
	}
	return m.tabBar() + body + m.footer()
}

func (m *model) tabBar() string {
	var names []string
	if m.combined() {
		names = append(names, "Combined")
	}
	for _, p := range m.ps {
		names = append(names, p.Name)
	}
	var parts []string
	for i, n := range names {
		t := fmt.Sprintf(" %d  %s ", i+1, n)
		if i == m.tab {
			parts = append(parts, paint("1;7", t))
		} else {
			parts = append(parts, dim(t))
		}
	}
	return "\n  " + strings.Join(parts, "  ") + "\n"
}

func (m *model) footer() string {
	keys := "←→ tabs   ↑↓ move   enter open   s sort   r refresh   q quit"
	if m.detail {
		keys = "↑↓ next holding   esc back   ←→ tabs   r refresh   q quit"
	} else if m.portfolio() == nil {
		keys = "←→ tabs   ↑↓ move   enter open portfolio   r refresh   q quit"
	}
	status := "updated " + m.loadedAt.Format("15:04") + " · Sharesight figures, not advice"
	switch {
	case m.loading:
		status = paint("33", "refreshing from Sharesight…")
	case m.err != nil:
		status = paint("31", "refresh failed: "+truncate(m.err.Error(), max(20, m.w-24))) + dim(" (showing earlier figures)")
	}
	return "\n  " + dim(keys) + "\n  " + status
}

// chromeLines is what the tab bar and footer take up.
const chromeLines = 2 + 3

func (m *model) viewCombined() string {
	var total float64
	for _, p := range m.ps {
		total += m.data[p.ID]["all"].Report.Value
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", bold("All portfolios"), bold(money(total)),
		dim("as of "+m.data[m.ps[0].ID]["all"].Report.EndDate))

	hdr := []string{"portfolio", "value", "weight", dayLabel(), "1 month", "1 year", "all time p.a.", "all-time gain"}
	align := []bool{false, true, true, true, true, true, true, true}
	var rows [][]string
	for _, p := range m.ps {
		d := m.data[p.ID]
		all := d["all"].Report
		r := []string{bold(p.Name), money(all.Value), fmt.Sprintf("%.1f%%", all.Value/total*100)}
		for _, k := range holdingKeys {
			v := d[k].Report.TotalGainPercent
			r = append(r, signed(v, pct(v)))
		}
		rows = append(rows, append(r, signed(all.TotalGain, moneySigned(all.TotalGain))))
	}
	b.WriteString(m.cursorTable(hdr, rows, align, []int{2, 4, 7}, len(rows)))

	fmt.Fprintf(&b, "\n  %s\n\n", bold("Combined"))
	b.WriteString(summaryString(func(k string) (Gains, bool) { return combinedGains(m.ps, m.data, k, total) }))
	b.WriteString(dim("  Over a year Sharesight's returns are per annum; ≈ is a value-weighted average.") + "\n")
	return b.String()
}

func (m *model) viewPortfolio(p Portfolio) string {
	d := m.data[p.ID]
	all := d["all"].Report
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", bold(p.Name), bold(money(all.Value)), dim("as of "+all.EndDate))

	// periods across the page so the holdings get the height
	hdr := []string{""}
	align := []bool{false}
	gain, ret := []string{dim("gain")}, []string{dim("return")}
	for _, per := range periods {
		h := label(per)
		if per.Annualise {
			h += " p.a."
		}
		g := d[per.Key].Report.Gains
		hdr, align = append(hdr, h), append(align, true)
		gain = append(gain, signed(g.TotalGain, moneySigned(g.TotalGain)))
		ret = append(ret, signed(g.TotalGainPercent, pct(g.TotalGainPercent)))
	}
	b.WriteString(fitTable(hdr, [][]string{gain, ret}, align, []int{2, 7, 4, 6}, m.w))
	a := all.Gains
	fmt.Fprintf(&b, "  %s  capital %s · dividends %s · currency %s\n\n", dim("All time:"),
		signed(a.CapitalGain, moneySigned(a.CapitalGain)), signed(a.PayoutGain, moneySigned(a.PayoutGain)),
		signed(a.CurrencyGain, moneySigned(a.CurrencyGain)))

	rows := m.rows()
	fmt.Fprintf(&b, "  %s   %s\n", bold(fmt.Sprintf("%d holdings", len(rows))), dim("sorted by "+sortName(sortKeys[m.sortIdx])))
	th, tr, ta := holdingTable(rows, all.Value, false)
	used := strings.Count(b.String(), "\n") + 1 // + table header
	visible := max(3, m.h-chromeLines-used-1)
	b.WriteString(m.cursorTable(th, tr, ta, []int{8, 5, 3, 1}, visible))
	return b.String()
}

func (m *model) viewDetail(p Portfolio) string {
	rows := m.rows()
	c := min(m.cursor[m.tab], len(rows)-1)
	s := holdingDetail(p, m.data[p.ID], rows[c].h)
	return s + dim(fmt.Sprintf("  %d of %d in %s, sorted by %s", c+1, len(rows), p.Name, sortName(sortKeys[m.sortIdx]))) + "\n"
}

func sortName(k string) string {
	switch k {
	case "today":
		return dayLabel() + "'s return"
	case "1m":
		return "1 month return"
	case "1y":
		return "1 year return"
	case "all":
		return "all-time return p.a."
	}
	return k
}

// cursorTable renders a table with the current tab's cursor row highlighted, showing at most
// visible rows and scrolling to keep the cursor in view. drop lists columns to leave out, in order,
// when the table is wider than the terminal.
func (m *model) cursorTable(hdr []string, rows [][]string, align []bool, drop []int, visible int) string {
	lines := strings.Split(strings.TrimSuffix(fitTable(hdr, rows, align, drop, m.w), "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	head, body := lines[0], lines[1:]
	c := min(m.cursor[m.tab], max(0, len(body)-1))
	off := m.offset[m.tab]
	if c < off {
		off = c
	}
	if c >= off+visible {
		off = c - visible + 1
	}
	off = max(0, min(off, len(body)-visible))
	m.offset[m.tab] = off

	var b strings.Builder
	b.WriteString(head + "\n")
	end := min(len(body), off+visible)
	for i := off; i < end; i++ {
		l := body[i]
		if i == c {
			l = highlight(l, m.w)
		}
		b.WriteString(l + "\n")
	}
	if len(body) > visible {
		b.WriteString(dim(fmt.Sprintf("  %d–%d of %d", off+1, end, len(body))) + "\n")
	}
	return b.String()
}

// highlight marks the selected row: an arrow in the gutter and reversed colours across the row.
func highlight(line string, w int) string {
	line = "▸" + strings.TrimPrefix(line, " ")
	if !colour {
		return line
	}
	const rev = "\x1b[7m"
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+rev)
	return rev + line + strings.Repeat(" ", max(0, min(w-1, width(line)+2)-width(line))) + "\x1b[0m"
}

// fitTable is tableString, leaving out columns (in drop order) until it fits in w.
func fitTable(hdr []string, rows [][]string, align []bool, drop []int, w int) string {
	out := tableString(hdr, rows, align)
	gone := map[int]bool{}
	for _, d := range drop {
		if maxWidth(out) <= w {
			break
		}
		gone[d] = true
		keep := func(cells []string) []string {
			var r []string
			for i, c := range cells {
				if !gone[i] {
					r = append(r, c)
				}
			}
			return r
		}
		var al []bool
		for i, a := range align {
			if !gone[i] {
				al = append(al, a)
			}
		}
		var rs [][]string
		for _, r := range rows {
			rs = append(rs, keep(r))
		}
		out = tableString(keep(hdr), rs, al)
	}
	return out
}

func maxWidth(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		n = max(n, width(l))
	}
	return n
}
