package tui

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/chartspec"
	"reasonix/internal/frontend/termrender"
)

const (
	chartPreviewRows = 10
	minText          = 4
)

// chartOf returns the spec of a settled chart call. A running or refused call
// has nothing to show yet, and the card says why instead.
func chartOf(it *Item) (*chartspec.Spec, bool) {
	t := it.Tool
	if t == nil || it.Running || t.Err != "" || t.Output == "" {
		return nil, false
	}
	return chartspec.FromCall(t.Name, t.ResolvedName, t.Args)
}

// chartRows draws a chart as what a terminal can show: its title and marks,
// then the data as an aligned table, numbers on the right.
func chartRows(spec *chartspec.Spec, width int, f outputFold) []string {
	limit := chartPreviewRows
	if f == foldOpen {
		limit = shellExpandLines
	}
	tb := spec.Table(limit)
	avail := max(width-len([]rune(connector)), 1)
	kept, widths := fitColumns(tb, avail)
	gutter := strings.Repeat(" ", len([]rune(connector)))
	rows := []string{termrender.Dim(connector + oneLine(spec.Title+" · "+spec.Describe(), avail))}
	line := func(cells []string) string {
		cells = cells[:kept]
		parts := make([]string, len(cells))
		for i, c := range cells {
			c = termrender.Truncate(oneLine(c, widths[i]), widths[i], "…")
			pad := strings.Repeat(" ", max(widths[i]-termrender.VisibleWidth(c), 0))
			if tb.Numeric[i] {
				parts[i] = pad + c
			} else {
				parts[i] = c + pad
			}
		}
		return termrender.Truncate(gutter+strings.TrimRight(strings.Join(parts, "  "), " "), width, "")
	}
	rows = append(rows, termrender.Dim(line(tb.Header)))
	for _, r := range tb.Rows {
		rows = append(rows, line(r))
	}
	if kept < len(tb.Header) {
		rows = append(rows, termrender.Dim(termrender.Truncate(gutter+fmt.Sprintf(i18n.M.TUIChartMoreColsFmt, len(tb.Header)-kept), width, "")))
	}
	if tb.More > 0 {
		hint := ""
		if f == foldShut {
			hint = " (Ctrl+B)"
		}
		rows = append(rows, termrender.Dim(termrender.Truncate(gutter+fmt.Sprintf(i18n.M.TUIChartMoreRowsFmt, tb.More)+hint, width, "")))
	}
	return rows
}

// fitColumns keeps as many leading columns as can be read: numbers at their
// whole width, text shrunk only as far as minText. It returns the kept count
// and each kept column's width; the widest text gives way first.
func fitColumns(tb chartspec.Table, avail int) (int, []int) {
	nat := make([]int, len(tb.Header))
	measure := func(i int, s string) { nat[i] = max(nat[i], termrender.VisibleWidth(oneLine(s, 0))) }
	for i, h := range tb.Header {
		measure(i, h)
	}
	for _, r := range tb.Rows {
		for i, c := range r {
			measure(i, c)
		}
	}
	floor := func(i int) int {
		if tb.Numeric[i] {
			return nat[i]
		}
		return min(nat[i], minText)
	}
	kept := len(nat)
	for kept > 1 {
		need := 2 * (kept - 1)
		for i := range kept {
			need += floor(i)
		}
		if need <= avail {
			break
		}
		kept--
	}
	widths := slices.Clone(nat[:kept])
	total := func() int { return 2*(kept-1) + sumInts(widths) }
	for total() > avail {
		widest := -1
		for i, w := range widths {
			if w > floor(i) && (widest < 0 || w > widths[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
	}
	return kept, widths
}

func sumInts(v []int) int {
	n := 0
	for _, x := range v {
		n += x
	}
	return n
}
