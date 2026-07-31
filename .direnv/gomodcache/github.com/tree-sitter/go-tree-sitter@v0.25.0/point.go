package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
*/
import "C"

// A position in a multi-line text document, in terms of rows and columns.
//
// Rows and columns are zero-based.
type Point struct {
	Row    uint
	Column uint
}

func NewPoint(row, column uint) Point {
	return Point{Row: row, Column: column}
}

func (p *Point) toTSPoint() C.TSPoint {
	return C.TSPoint{
		row:    C.uint32_t(p.Row),
		column: C.uint32_t(p.Column),
	}
}

func (p *Point) fromTSPoint(tp C.TSPoint) {
	p.Row = uint(tp.row)
	p.Column = uint(tp.column)
}
