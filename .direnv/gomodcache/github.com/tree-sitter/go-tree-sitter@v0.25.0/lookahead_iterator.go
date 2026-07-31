package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
*/
import "C"

import (
	"unsafe"
)

type LookaheadIterator struct {
	_inner *C.TSLookaheadIterator
}

func newLookaheadIterator(ptr *C.TSLookaheadIterator) *LookaheadIterator {
	return &LookaheadIterator{_inner: ptr}
}

func (l *LookaheadIterator) Close() {
	C.ts_lookahead_iterator_delete(l._inner)
}

func (l *LookaheadIterator) Language() *Language {
	return NewLanguage(unsafe.Pointer(C.ts_lookahead_iterator_language(l._inner)))
}

// Get the current symbol of the lookahead iterator.
func (l *LookaheadIterator) Symbol() uint16 {
	return uint16(C.ts_lookahead_iterator_current_symbol(l._inner))
}

// Get the current symbol name of the lookahead iterator.
func (l *LookaheadIterator) SymbolName() string {
	return C.GoString(C.ts_lookahead_iterator_current_symbol_name(l._inner))
}

// Reset the lookahead iterator.
//
// This returns `true` if the language was set successfully and `false`
// otherwise.
func (l *LookaheadIterator) Reset(language *Language, state uint16) bool {
	return bool(C.ts_lookahead_iterator_reset(l._inner, language.Inner, C.TSStateId(state)))
}

// Reset the lookahead iterator to another state.
//
// This returns `true` if the iterator was reset to the given state and
// `false` otherwise.
func (l *LookaheadIterator) ResetState(state uint16) bool {
	return bool(C.ts_lookahead_iterator_reset_state(l._inner, C.TSStateId(state)))
}

// Iterate symbols.
func (l *LookaheadIterator) Iter() []uint16 {
	var symbols []uint16
	for C.ts_lookahead_iterator_next(l._inner) {
		symbols = append(symbols, l.Symbol())
	}
	return symbols
}

// Iterate symbol names.
func (l *LookaheadIterator) IterNames() []string {
	var names []string
	for C.ts_lookahead_iterator_next(l._inner) {
		names = append(names, l.SymbolName())
	}
	return names
}
