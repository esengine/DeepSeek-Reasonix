package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
*/
import "C"
import "fmt"

// A range of positions in a multi-line text document, both in terms of bytes
// and of rows and columns.
type Range struct {
	StartByte  uint
	EndByte    uint
	StartPoint Point
	EndPoint   Point
}

// An error that occurred in [Parser.SetIncludedRanges].
type IncludedRangesError struct {
	Index uint32
}

func (r *Range) ToTSRange() C.TSRange {
	return C.TSRange{
		start_byte:  C.uint32_t(r.StartByte),
		end_byte:    C.uint32_t(r.EndByte),
		start_point: r.StartPoint.toTSPoint(),
		end_point:   r.EndPoint.toTSPoint(),
	}
}

func (r *Range) FromTSRange(tr C.TSRange) {
	r.StartByte = uint(tr.start_byte)
	r.EndByte = uint(tr.end_byte)
	r.StartPoint.fromTSPoint(tr.start_point)
	r.EndPoint.fromTSPoint(tr.end_point)
}

func (i *IncludedRangesError) Error() string {
	return fmt.Sprintf("Incorrect range by index: %d", i.Index)
}
