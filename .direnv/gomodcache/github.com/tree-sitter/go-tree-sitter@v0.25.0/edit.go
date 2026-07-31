package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
*/
import "C"

type InputEdit struct {
	StartByte      uint
	OldEndByte     uint
	NewEndByte     uint
	StartPosition  Point
	OldEndPosition Point
	NewEndPosition Point
}

func (i *InputEdit) toTSInputEdit() *C.TSInputEdit {
	return &C.TSInputEdit{
		start_byte:    C.uint(i.StartByte),
		old_end_byte:  C.uint(i.OldEndByte),
		new_end_byte:  C.uint(i.NewEndByte),
		start_point:   i.StartPosition.toTSPoint(),
		old_end_point: i.OldEndPosition.toTSPoint(),
		new_end_point: i.NewEndPosition.toTSPoint(),
	}
}
