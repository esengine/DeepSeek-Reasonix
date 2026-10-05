// Package encodingtest writes test fixtures in a file encoding.
package encodingtest

import "reasonix/internal/base/fileutil/encoding"

// MustEncode is encoding.Encode for fixture text known to be representable.
func MustEncode(text string, enc encoding.Kind) []byte {
	out, err := encoding.Encode(text, enc)
	if err != nil {
		panic(err)
	}
	return out
}
