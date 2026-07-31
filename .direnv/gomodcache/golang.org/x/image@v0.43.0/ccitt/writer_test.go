// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ccitt

import (
	"bytes"
	"reflect"
	"testing"
)

func testEncode(t *testing.T, o Order) {
	t.Helper()
	values := []uint32{0, 1, 256, 7, 128, 3, 2560, 2240, 2368, 2048}

	decTable := whiteDecodeTable[:]
	encTableSmall := whiteEncodeTable2[:]
	encTableBig := whiteEncodeTable3[:]

	// Encode values to bit stream.
	var bb bytes.Buffer
	w := &bitWriter{w: &bb, order: o}
	for _, v := range values {
		encTable := encTableSmall
		if v < 64 {
			// No-op.
		} else if v&63 != 0 {
			t.Fatalf("writeCode: cannot encode %d: large but not a multiple of 64", v)
		} else {
			encTable = encTableBig
			v = v/64 - 1
		}
		if err := w.writeCode(encTable[v]); err != nil {
			t.Fatalf("writeCode: %v", err)
		}
	}
	if err := w.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Decode bit stream to values.
	got := []uint32(nil)
	r := &bitReader{
		r:     bytes.NewReader(bb.Bytes()),
		order: o,
	}
	finalValue := values[len(values)-1]
	for {
		v, err := decode(r, decTable)
		if err != nil {
			t.Fatalf("after got=%d: %v", got, err)
		}
		got = append(got, v)
		if v == finalValue {
			break
		}
	}

	// Check that the round-tripped values were unchanged.
	if !reflect.DeepEqual(got, values) {
		t.Fatalf("\ngot:  %v\nwant: %v", got, values)
	}
}

func TestEncodeLSB(t *testing.T) { testEncode(t, LSB) }
func TestEncodeMSB(t *testing.T) { testEncode(t, MSB) }
