package chartspec

import (
	"strconv"
	"strings"
)

// Table is a spec's rows as display text, for a frontend that cannot draw.
type Table struct {
	Header  []string
	Rows    [][]string
	Numeric []bool // per column: right-align
	More    int    // rows past the limit
}

// Table lists the first limit rows (all of them when limit is not positive).
func (s *Spec) Table(limit int) Table {
	tb := Table{Header: make([]string, len(s.Data.Columns)), Numeric: make([]bool, len(s.Data.Columns))}
	for i, c := range s.Data.Columns {
		tb.Header[i], tb.Numeric[i] = c.Name, c.Type == TypeNumber
	}
	rows := s.Data.Rows
	if limit > 0 && len(rows) > limit {
		tb.More, rows = len(rows)-limit, rows[:limit]
	}
	for _, row := range rows {
		out := make([]string, len(row))
		for i, c := range row {
			switch c.Kind {
			case CellNumber:
				out[i] = strconv.FormatFloat(c.Num, 'f', -1, 64)
			case CellString:
				out[i] = c.Str
			}
		}
		tb.Rows = append(tb.Rows, out)
	}
	return tb
}

// Describe names what each mark plots, one clause per mark.
func (s *Spec) Describe() string {
	parts := make([]string, len(s.Marks))
	for i, m := range s.Marks {
		parts[i] = string(m.Type) + " " + m.X + " × " + strings.Join(m.Y, ",")
		if m.Color != "" {
			parts[i] += " by " + m.Color
		}
	}
	return strings.Join(parts, "; ")
}
