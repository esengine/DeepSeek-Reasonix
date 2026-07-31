// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ccitt

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func compareImages(t *testing.T, img0 image.Image, img1 image.Image) {
	t.Helper()

	b0 := img0.Bounds()
	b1 := img1.Bounds()
	if b0 != b1 {
		t.Fatalf("bounds differ: %v vs %v", b0, b1)
	}

	for y := b0.Min.Y; y < b0.Max.Y; y++ {
		for x := b0.Min.X; x < b0.Max.X; x++ {
			c0 := img0.At(x, y)
			c1 := img1.At(x, y)
			if c0 != c1 {
				t.Fatalf("pixel at (%d, %d) differs: %v vs %v", x, y, c0, c1)
			}
		}
	}
}

func decodePNG(fileName string) (image.Image, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// simpleHB is a simple implementation of highBits.
func simpleHB(dst []byte, src []byte, invert bool) (d int, s int) {
	for d < len(dst) {
		numToPack := len(src) - s
		if numToPack <= 0 {
			break
		} else if numToPack > 8 {
			numToPack = 8
		}

		byteValue := byte(0)
		if invert {
			byteValue = 0xFF >> uint(numToPack)
		}
		for n := 0; n < numToPack; n++ {
			byteValue |= (src[s] & 0x80) >> uint(n)
			s++
		}
		dst[d] = byteValue
		d++
	}
	return d, s
}

func TestHighBits(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	dst0 := make([]byte, 3)
	dst1 := make([]byte, 3)
	src := make([]byte, 20)

	for r := 0; r < 1000; r++ {
		numDst := rng.Intn(len(dst0) + 1)
		randomByte := byte(rng.Intn(256))
		for i := 0; i < numDst; i++ {
			dst0[i] = randomByte
			dst1[i] = randomByte
		}

		numSrc := rng.Intn(len(src) + 1)
		for i := 0; i < numSrc; i++ {
			src[i] = byte(rng.Intn(256))
		}

		invert := rng.Intn(2) == 0

		d0, s0 := highBits(dst0[:numDst], src[:numSrc], invert)
		d1, s1 := simpleHB(dst1[:numDst], src[:numSrc], invert)

		if (d0 != d1) || (s0 != s1) || !bytes.Equal(dst0[:numDst], dst1[:numDst]) {
			srcHighBits := make([]byte, numSrc)
			for i := range srcHighBits {
				srcHighBits[i] = src[i] >> 7
			}

			t.Fatalf("r=%d, numDst=%d, numSrc=%d, invert=%t:\nsrcHighBits=%d\n"+
				"got  d=%d, s=%d, bytes=[% 02X]\n"+
				"want d=%d, s=%d, bytes=[% 02X]",
				r, numDst, numSrc, invert, srcHighBits,
				d0, s0, dst0[:numDst],
				d1, s1, dst1[:numDst],
			)
		}
	}
}

func BenchmarkHighBits(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	dst := make([]byte, 1024)
	src := make([]byte, 7777)
	for i := range src {
		src[i] = uint8(rng.Intn(256))
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		highBits(dst, src, false)
		highBits(dst, src, true)
	}
}

func TestMaxCodeLength(t *testing.T) {
	br := bitReader{}
	size := unsafe.Sizeof(br.bits)
	size *= 8 // Convert from bytes to bits.

	// Check that the size of the bitReader.bits field is large enough to hold
	// nextBitMaxNBits bits.
	if size < nextBitMaxNBits {
		t.Fatalf("size: got %d, want >= %d", size, nextBitMaxNBits)
	}

	// Check that bitReader.nextBit will always leave enough spare bits in the
	// bitReader.bits field such that the decode function can unread up to
	// maxCodeLength bits.
	if want := size - nextBitMaxNBits; maxCodeLength > want {
		t.Fatalf("maxCodeLength: got %d, want <= %d", maxCodeLength, want)
	}

	// The decode function also assumes that, when saving bits to possibly
	// unread later, those bits fit inside a uint32.
	if maxCodeLength > 32 {
		t.Fatalf("maxCodeLength: got %d, want <= %d", maxCodeLength, 32)
	}
}

func testDecodeTable(t *testing.T, decodeTable [][2]int16, codes []code, values []uint32) {
	// Build a map from values to codes.
	m := map[uint32]string{}
	for _, code := range codes {
		m[code.val] = code.str
	}

	// Build the encoded form of those values in MSB order.
	enc := []byte(nil)
	bits := uint8(0)
	nBits := uint32(0)
	for _, v := range values {
		code := m[v]
		if code == "" {
			panic("unmapped code")
		}
		for _, c := range code {
			bits |= uint8(c&1) << (7 - nBits)
			nBits++
			if nBits == 8 {
				enc = append(enc, bits)
				bits = 0
				nBits = 0
				continue
			}
		}
	}
	if nBits > 0 {
		enc = append(enc, bits)
	}

	// Decode that encoded form.
	got := []uint32(nil)
	r := &bitReader{
		r:     bytes.NewReader(enc),
		order: MSB,
	}
	finalValue := values[len(values)-1]
	for {
		v, err := decode(r, decodeTable)
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

func TestModeDecodeTable(t *testing.T) {
	testDecodeTable(t, modeDecodeTable[:], modeCodes, []uint32{
		modePass,
		modeV0,
		modeV0,
		modeVL1,
		modeVR3,
		modeVL2,
		modeExt,
		modeVL1,
		modeH,
		modeVL1,
		modeVL1,
		// The exact value of this final slice element doesn't really matter,
		// except that testDecodeTable assumes that it (the finalValue) is
		// different from every previous element.
		modeVL3,
	})
}

func TestWhiteDecodeTable(t *testing.T) {
	testDecodeTable(t, whiteDecodeTable[:], whiteCodes, []uint32{
		0, 1, 256, 7, 128, 3, 2560,
	})
}

func TestBlackDecodeTable(t *testing.T) {
	testDecodeTable(t, blackDecodeTable[:], blackCodes, []uint32{
		63, 64, 63, 64, 64, 63, 22, 1088, 2048, 7, 6, 5, 4, 3, 2, 1, 0,
	})
}

func TestDecodeInvalidCode(t *testing.T) {
	// The bit stream is:
	// 1 010 000000011011
	// Packing that LSB-first gives:
	// 0b_1101_1000_0000_0101
	src := []byte{0x05, 0xD8}

	decodeTable := modeDecodeTable[:]
	r := &bitReader{
		r: bytes.NewReader(src),
	}

	// "1" decodes to the value 2.
	if v, err := decode(r, decodeTable); v != 2 || err != nil {
		t.Fatalf("decode #0: got (%v, %v), want (2, nil)", v, err)
	}

	// "010" decodes to the value 6.
	if v, err := decode(r, decodeTable); v != 6 || err != nil {
		t.Fatalf("decode #0: got (%v, %v), want (6, nil)", v, err)
	}

	// "00000001" is an invalid code.
	if v, err := decode(r, decodeTable); v != 0 || err != errInvalidCode {
		t.Fatalf("decode #0: got (%v, %v), want (0, %v)", v, err, errInvalidCode)
	}

	// The bitReader should not have advanced after encountering an invalid
	// code. The remaining bits should be "000000011011".
	remaining := []byte(nil)
	for {
		bit, err := r.nextBit()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("nextBit: %v", err)
		}
		remaining = append(remaining, uint8('0'+bit))
	}
	if got, want := string(remaining), "000000011011"; got != want {
		t.Fatalf("remaining bits: got %q, want %q", got, want)
	}
}

func testRead(t *testing.T, fileName string, sf SubFormat, align, invert, truncated bool) {
	t.Helper()

	const width, height = 153, 55
	opts := &Options{
		Align:  align,
		Invert: invert,
	}

	got := ""
	{
		f, err := os.Open(fileName)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer f.Close()
		gotBytes, err := ioutil.ReadAll(NewReader(f, MSB, sf, width, height, opts))
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		got = string(gotBytes)
	}

	want := ""
	{
		img, err := decodePNG("testdata/bw-gopher.png")
		if err != nil {
			t.Fatalf("decodePNG: %v", err)
		}
		gray, ok := img.(*image.Gray)
		if !ok {
			t.Fatalf("decodePNG: got %T, want *image.Gray", img)
		}
		bounds := gray.Bounds()
		if w := bounds.Dx(); w != width {
			t.Fatalf("width: got %d, want %d", w, width)
		}
		if h := bounds.Dy(); h != height {
			t.Fatalf("height: got %d, want %d", h, height)
		}

		// Prepare to extend each row's width to a multiple of 8, to simplify
		// packing from 1 byte per pixel to 1 bit per pixel.
		extended := make([]byte, (width+7)&^7)

		wantBytes := []byte(nil)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			rowPix := gray.Pix[(y-bounds.Min.Y)*gray.Stride:]
			rowPix = rowPix[:width]
			copy(extended, rowPix)

			// Pack from 1 byte per pixel to 1 bit per pixel, MSB first.
			byteValue := uint8(0)
			for x, pixel := range extended {
				byteValue |= (pixel & 0x80) >> uint(x&7)
				if (x & 7) == 7 {
					wantBytes = append(wantBytes, byteValue)
					byteValue = 0
				}
			}
		}
		want = string(wantBytes)
	}

	// We expect a width of 153 pixels, which is 20 bytes per row (at 1 bit per
	// pixel, plus 7 final bits of padding). Check that want is 20 * height
	// bytes long, and if got != want, format them to split at every 20 bytes.

	if n := len(want); n != 20*height {
		t.Fatalf("len(want): got %d, want %d", n, 20*height)
	}

	format := func(s string) string {
		b := []byte(nil)
		for row := 0; len(s) >= 20; row++ {
			b = append(b, fmt.Sprintf("row%02d: %02X\n", row, s[:20])...)
			s = s[20:]
		}
		if len(s) > 0 {
			b = append(b, fmt.Sprintf("%02X\n", s)...)
		}
		return string(b)
	}

	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", format(got), format(want))
	}

	// Passing AutoDetectHeight should produce the same output, provided that
	// the input hasn't truncated the trailing sequence of consecutive EOL's
	// that marks the end of the image.
	if !truncated {
		f, err := os.Open(fileName)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer f.Close()
		adhBytes, err := ioutil.ReadAll(NewReader(f, MSB, sf, width, AutoDetectHeight, opts))
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if s := string(adhBytes); s != want {
			t.Fatalf("AutoDetectHeight produced different output.\n"+
				"height=%d:\n%s\nheight=%d:\n%s",
				AutoDetectHeight, format(s), height, format(want))
		}
	}
}

func TestRead(t *testing.T) {
	for _, fileName := range []string{
		"testdata/bw-gopher.ccitt_group3",
		"testdata/bw-gopher-aligned.ccitt_group3",
		"testdata/bw-gopher-inverted.ccitt_group3",
		"testdata/bw-gopher-inverted-aligned.ccitt_group3",
		"testdata/bw-gopher.ccitt_group4",
		"testdata/bw-gopher-aligned.ccitt_group4",
		"testdata/bw-gopher-inverted.ccitt_group4",
		"testdata/bw-gopher-inverted-aligned.ccitt_group4",
		"testdata/bw-gopher-truncated0.ccitt_group3",
		"testdata/bw-gopher-truncated0.ccitt_group4",
		"testdata/bw-gopher-truncated1.ccitt_group3",
		"testdata/bw-gopher-truncated1.ccitt_group4",
	} {
		subFormat := Group3
		if strings.HasSuffix(fileName, "group4") {
			subFormat = Group4
		}
		align := strings.Contains(fileName, "aligned")
		invert := strings.Contains(fileName, "inverted")
		truncated := strings.Contains(fileName, "truncated")
		testRead(t, fileName, subFormat, align, invert, truncated)
	}
}

func TestDecodeIntoGray(t *testing.T) {
	for _, tt := range []struct {
		fileName string
		sf       SubFormat
		w, h     int
	}{
		{"testdata/bw-gopher.ccitt_group3", Group3, 153, 55},
		{"testdata/bw-gopher.ccitt_group4", Group4, 153, 55},
		{"testdata/bw-gopher-truncated0.ccitt_group3", Group3, 153, 55},
		{"testdata/bw-gopher-truncated0.ccitt_group4", Group4, 153, 55},
		{"testdata/bw-gopher-truncated1.ccitt_group3", Group3, 153, 55},
		{"testdata/bw-gopher-truncated1.ccitt_group4", Group4, 153, 55},
	} {
		t.Run(tt.fileName, func(t *testing.T) {
			testDecodeIntoGray(t, tt.fileName, MSB, tt.sf, tt.w, tt.h, nil)
		})
	}
}

func testDecodeIntoGray(t *testing.T, fileName string, order Order, sf SubFormat, width int, height int, opts *Options) {
	t.Helper()

	f, err := os.Open(filepath.FromSlash(fileName))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	got := image.NewGray(image.Rect(0, 0, width, height))
	if err := DecodeIntoGray(got, f, order, sf, opts); err != nil {
		t.Fatalf("DecodeIntoGray: %v", err)
	}

	want, err := decodePNG("testdata/bw-gopher.png")
	if err != nil {
		t.Fatal(err)
	}

	compareImages(t, got, want)
}
