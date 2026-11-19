package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

var colour = term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""

func paint(code, s string) string {
	if !colour {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string { return paint("1", s) }
func dim(s string) string  { return paint("2", s) }

// signed colours s green/red by the sign of v.
func signed(v float64, s string) string {
	switch {
	case v > 0.004:
		return paint("32", s)
	case v < -0.004:
		return paint("31", s)
	}
	return s
}

func commas(v float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, math.Abs(v))
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if frac != "" {
		b.WriteString("." + frac)
	}
	return b.String()
}

func money(v float64) string { return "$" + commas(v, 0) }

func moneySigned(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "-"
	}
	return sign + "$" + commas(v, 0)
}

func pct(v float64) string { return fmt.Sprintf("%+.2f%%", v) }

// visible width (ignores ANSI escapes)
func width(s string) int {
	n, esc := 0, false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc:
			if r == 'm' {
				esc = false
			}
		default:
			n++
		}
	}
	return n
}

func pad(s string, w int, right bool) string {
	gap := w - width(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// table prints rows; align[i] true = right-align column i.
func table(header []string, rows [][]string, align []bool) {
	fmt.Print(tableString(header, rows, align))
}

// tableString renders rows as text, one line each; align[i] true = right-align column i.
func tableString(header []string, rows [][]string, align []bool) string {
	w := make([]int, len(header))
	for i, h := range header {
		w[i] = width(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if width(c) > w[i] {
				w[i] = width(c)
			}
		}
	}
	var b strings.Builder
	line := func(cells []string, hdr bool) {
		var parts []string
		for i, c := range cells {
			if hdr {
				c = dim(c)
			}
			parts = append(parts, pad(c, w[i], align[i]))
		}
		b.WriteString("  " + strings.TrimRight(strings.Join(parts, "   "), " ") + "\n")
	}
	line(header, true)
	for _, r := range rows {
		line(r, false)
	}
	return b.String()
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return 80
}
