package chartspec

import (
	"fmt"
	"strconv"
	"strings"
)

const quotedRunes = 32

// Summary is the only view of a chart the model reads back: names, counts and
// numeric extremes, with every cell string quoted and truncated so data cannot
// pass for instructions. The same spec always yields the same bytes.
func (s *Spec) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "chart %s, %d rows, %d columns", quote(s.Title), len(s.Data.Rows), len(s.Data.Columns))
	writeAxis(&b, "x_axis", s.XAxis)
	writeAxis(&b, "y_axis", s.YAxis)
	idx := map[string]int{}
	for i, c := range s.Data.Columns {
		idx[c.Name] = i
	}
	for _, m := range s.Marks {
		fmt.Fprintf(&b, "\n%s x=%s", m.Type, quote(m.X))
		if m.Stacked {
			b.WriteString(" stacked")
		}
		if m.Donut {
			b.WriteString(" donut")
		}
		for _, y := range m.Y {
			s.seriesLines(&b, m, y, idx)
		}
	}
	return b.String()
}

func (s *Spec) seriesLines(b *strings.Builder, m Mark, y string, idx map[string]int) {
	yi := idx[y]
	if m.Color == "" {
		writeExtremes(b, quote(y), s.Data.Rows, yi, -1, "")
		return
	}
	ci := idx[m.Color]
	var order []string
	seen := map[string]bool{}
	for _, row := range s.Data.Rows {
		if row[ci].Kind == CellString && !seen[row[ci].Str] {
			seen[row[ci].Str] = true
			order = append(order, row[ci].Str)
		}
	}
	for _, v := range order {
		writeExtremes(b, quote(y)+"/"+quote(v), s.Data.Rows, yi, ci, v)
	}
}

func writeExtremes(b *strings.Builder, name string, rows [][]Cell, yi, ci int, only string) {
	n, nulls := 0, 0
	var lo, hi float64
	for _, row := range rows {
		if ci >= 0 && (row[ci].Kind != CellString || row[ci].Str != only) {
			continue
		}
		c := row[yi]
		if c.Kind != CellNumber {
			nulls++
			continue
		}
		if n == 0 || c.Num < lo {
			lo = c.Num
		}
		if n == 0 || c.Num > hi {
			hi = c.Num
		}
		n++
	}
	fmt.Fprintf(b, "\n  series %s n=%d", name, n)
	if n > 0 {
		fmt.Fprintf(b, " min=%s max=%s", num(lo), num(hi))
	}
	if nulls > 0 {
		fmt.Fprintf(b, " null=%d", nulls)
	}
}

func num(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func quote(s string) string {
	r := []rune(s)
	if len(r) > quotedRunes {
		return strconv.Quote(string(r[:quotedRunes])) + "..."
	}
	return strconv.Quote(s)
}

func writeAxis(b *strings.Builder, name string, a *Axis) {
	if a == nil {
		return
	}
	fmt.Fprintf(b, "\n%s title=%s unit=%s", name, quote(a.Title), quote(a.Unit))
	if a.Scale != "" {
		b.WriteString(" scale=" + string(a.Scale))
	}
}
