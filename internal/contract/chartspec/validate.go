package chartspec

import (
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"
)

var dateLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02", "2006-01", "2006"}

// Validate checks a spec's structure, types and caps and returns the first
// refusal as an *Error. A nil result means a frontend can draw it.
func (s *Spec) Validate() error {
	if s.SpecVersion == 0 {
		return fail(CodeSchemaInvalid, "/spec_version")
	}
	if s.SpecVersion != SpecVersion {
		return fail(CodeSpecVersionUnsupported, "/spec_version")
	}
	if err := checkLabel(s.Title, "/title"); err != nil {
		return err
	}
	for _, a := range []struct {
		ax   *Axis
		path string
	}{{s.XAxis, "/x_axis"}, {s.YAxis, "/y_axis"}} {
		if err := a.ax.validate(a.path); err != nil {
			return err
		}
	}
	cols, err := s.Data.validate()
	if err != nil {
		return err
	}
	if len(s.Marks) == 0 {
		return fail(CodeSchemaInvalid, "/marks")
	}
	if len(s.Marks) > MaxMarks {
		return exceeded(CapMarks, "/marks")
	}
	for i := range s.Marks {
		if err := s.Marks[i].validate("/marks/"+strconv.Itoa(i), cols, &s.Data, s.YAxis); err != nil {
			return err
		}
	}
	return nil
}

func (a *Axis) validate(path string) error {
	if a == nil {
		return nil
	}
	if err := checkLabel(a.Title, path+"/title"); err != nil {
		return err
	}
	if err := checkLabel(a.Unit, path+"/unit"); err != nil {
		return err
	}
	switch a.Scale {
	case "", ScaleLinear, ScaleLog:
	default:
		return fail(CodeSchemaInvalid, path+"/scale")
	}
	switch a.Format {
	case "", FormatInteger, FormatDecimal, FormatPercent:
	default:
		return fail(CodeSchemaInvalid, path+"/format")
	}
	return nil
}

func (d *Data) validate() (map[string]int, error) {
	if len(d.Columns) == 0 {
		return nil, fail(CodeSchemaInvalid, "/data/columns")
	}
	if len(d.Columns) > MaxColumns {
		return nil, exceeded(CapColumns, "/data/columns")
	}
	if len(d.Rows) > MaxRows {
		return nil, exceeded(CapRows, "/data/rows")
	}
	index := make(map[string]int, len(d.Columns))
	for i, c := range d.Columns {
		path := "/data/columns/" + strconv.Itoa(i)
		if c.Name == "" {
			return nil, fail(CodeSchemaInvalid, path+"/name")
		}
		if err := checkLabel(c.Name, path+"/name"); err != nil {
			return nil, err
		}
		if _, dup := index[c.Name]; dup {
			return nil, fail(CodeSchemaInvalid, path+"/name")
		}
		switch c.Type {
		case TypeNumber, TypeString, TypeDate:
		default:
			return nil, fail(CodeSchemaInvalid, path+"/type")
		}
		index[c.Name] = i
	}
	for r, row := range d.Rows {
		rowPath := "/data/rows/" + strconv.Itoa(r)
		if len(row) != len(d.Columns) {
			return nil, fail(CodeSchemaInvalid, rowPath)
		}
		for c, cell := range row {
			if err := cell.validate(d.Columns[c].Type, rowPath+"/"+strconv.Itoa(c)); err != nil {
				return nil, err
			}
		}
	}
	return index, nil
}

func (c Cell) validate(t ColumnType, path string) error {
	switch c.Kind {
	case CellNull:
		return nil
	case CellNumber:
		if t != TypeNumber || c.Lossy {
			return fail(CodeTypeMismatch, path)
		}
	case CellString:
		if t == TypeNumber {
			return fail(CodeTypeMismatch, path)
		}
		if err := checkLabel(c.Str, path); err != nil {
			return err
		}
		if t == TypeDate && !isDate(c.Str) {
			return fail(CodeTypeMismatch, path)
		}
	}
	return nil
}

func isDate(s string) bool {
	for _, l := range dateLayouts {
		if _, err := time.Parse(l, s); err == nil {
			return true
		}
	}
	return false
}

func (m *Mark) validate(path string, cols map[string]int, d *Data, yAxis *Axis) error {
	switch m.Type {
	case MarkBar, MarkLine, MarkPie:
	default:
		return fail(CodeSchemaInvalid, path+"/type")
	}
	if m.Stacked && m.Type != MarkBar || m.Donut && m.Type != MarkPie {
		return fail(CodeSchemaInvalid, path)
	}
	if _, ok := cols[m.X]; !ok {
		return fail(CodeColumnUnknown, path+"/x")
	}
	if len(m.Y) == 0 || m.Type == MarkPie && (len(m.Y) != 1 || m.Color != "") {
		return fail(CodeSchemaInvalid, path+"/y")
	}
	for i, y := range m.Y {
		idx, ok := cols[y]
		if !ok {
			return fail(CodeColumnUnknown, path+"/y/"+strconv.Itoa(i))
		}
		if d.Columns[idx].Type != TypeNumber {
			return fail(CodeTypeMismatch, path+"/y/"+strconv.Itoa(i))
		}
	}
	series := len(m.Y)
	if m.Color != "" {
		idx, ok := cols[m.Color]
		if !ok {
			return fail(CodeColumnUnknown, path+"/color")
		}
		if d.Columns[idx].Type != TypeString {
			return fail(CodeTypeMismatch, path+"/color")
		}
		series *= distinct(d, idx)
	}
	if series > MaxSeries {
		return exceeded(CapSeries, path)
	}
	if len(d.Rows)*len(m.Y) > MaxPoints {
		return exceeded(CapPoints, path)
	}
	return m.checkValues(path, cols, d, yAxis)
}

func (m *Mark) checkValues(path string, cols map[string]int, d *Data, yAxis *Axis) error {
	logScale := yAxis != nil && yAxis.Scale == ScaleLog
	if m.Type == MarkPie && logScale {
		return fail(CodeSchemaInvalid, "/y_axis/scale")
	}
	for _, y := range m.Y {
		idx := cols[y]
		for r, row := range d.Rows {
			c := row[idx]
			if c.Kind != CellNumber {
				continue
			}
			p := "/data/rows/" + strconv.Itoa(r) + "/" + strconv.Itoa(idx)
			if m.Type == MarkPie && c.Num < 0 || logScale && c.Num <= 0 {
				return fail(CodeSchemaInvalid, p)
			}
		}
	}
	return nil
}

func distinct(d *Data, col int) int {
	seen := map[string]struct{}{}
	for _, row := range d.Rows {
		if row[col].Kind == CellString {
			seen[row[col].Str] = struct{}{}
		}
	}
	return len(seen)
}

func checkLabel(s, path string) error {
	if utf8.RuneCountInString(s) > MaxLabelRunes {
		return exceeded(CapLabel, path)
	}
	if !utf8.ValidString(s) {
		return fail(CodeSchemaInvalid, path)
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return fail(CodeSchemaInvalid, path)
		}
	}
	return nil
}
