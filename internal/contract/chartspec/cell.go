package chartspec

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

type CellKind uint8

const (
	CellNull CellKind = iota
	CellNumber
	CellString
)

// Cell is one scalar of a row. Lossy marks a number that float64 cannot carry
// exactly (an integer past 2^53, or a magnitude beyond float64); Validate refuses
// it, because the producer is told to send such values as strings.
type Cell struct {
	Kind  CellKind
	Num   float64
	Str   string
	Lossy bool
}

func (c *Cell) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*c = Cell{}
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = Cell{Kind: CellString, Str: s}
	case len(b) > 0 && (b[0] == '-' || (b[0] >= '0' && b[0] <= '9')):
		*c = numberCell(string(b))
	default:
		return &json.UnmarshalTypeError{Value: "non-scalar cell"}
	}
	return nil
}

func numberCell(lit string) Cell {
	f, err := strconv.ParseFloat(lit, 64)
	lossy := err != nil || math.IsInf(f, 0) || math.IsNaN(f)
	if !lossy && isIntegerLiteral(lit) {
		n, perr := strconv.ParseInt(lit, 10, 64)
		lossy = perr != nil || n > maxSafeInt || n < -maxSafeInt
	}
	if lossy {
		f = 0
	}
	return Cell{Kind: CellNumber, Num: f, Lossy: lossy}
}

func isIntegerLiteral(lit string) bool {
	for _, r := range lit {
		if r == '.' || r == 'e' || r == 'E' {
			return false
		}
	}
	return true
}

func (c Cell) MarshalJSON() ([]byte, error) {
	switch c.Kind {
	case CellNumber:
		return json.Marshal(c.Num)
	case CellString:
		return json.Marshal(c.Str)
	}
	return []byte("null"), nil
}
