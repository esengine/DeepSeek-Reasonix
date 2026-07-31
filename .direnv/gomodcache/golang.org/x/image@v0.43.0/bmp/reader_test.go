// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bmp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"os"
	"testing"

	_ "image/png"
)

const testdataDir = "../testdata/"

func compare(img0, img1 image.Image) error {
	b := img1.Bounds()
	if !b.Eq(img0.Bounds()) {
		return fmt.Errorf("wrong image size: want %s, got %s", img0.Bounds(), b)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c0 := img0.At(x, y)
			c1 := img1.At(x, y)
			r0, g0, b0, a0 := c0.RGBA()
			r1, g1, b1, a1 := c1.RGBA()
			if r0 != r1 || g0 != g1 || b0 != b1 || a0 != a1 {
				return fmt.Errorf("pixel at (%d, %d) has wrong color: want %v, got %v", x, y, c0, c1)
			}
		}
	}
	return nil
}

// TestDecode tests that decoding a PNG image and a BMP image result in the
// same pixel data.
func TestDecode(t *testing.T) {
	testCases := []string{
		"colormap",
		"colormap-0",
		"colormap-251",
		"video-001",
		"yellow_rose-small",
		"yellow_rose-small-v5",
		"bmp_1bpp",
		"bmp_4bpp",
		"bmp_8bpp",
	}

	for _, tc := range testCases {
		f0, err := os.Open(testdataDir + tc + ".png")
		if err != nil {
			t.Errorf("%s: Open PNG: %v", tc, err)
			continue
		}
		defer f0.Close()
		img0, _, err := image.Decode(f0)
		if err != nil {
			t.Errorf("%s: Decode PNG: %v", tc, err)
			continue
		}

		f1, err := os.Open(testdataDir + tc + ".bmp")
		if err != nil {
			t.Errorf("%s: Open BMP: %v", tc, err)
			continue
		}
		defer f1.Close()
		img1, _, err := image.Decode(f1)
		if err != nil {
			t.Errorf("%s: Decode BMP: %v", tc, err)
			continue
		}

		if err := compare(img0, img1); err != nil {
			t.Errorf("%s: %v", tc, err)
			continue
		}
	}
}

func TestDecodeConstructed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		b       []byte
		wantErr error
	}{{
		name: "1x1 paletted",
		b: bmpBuilder{
			width:        1,
			height:       1,
			planes:       1,
			bitsPerPixel: 1,
			colorsUsed:   2,
			colorTable: []colorTableEntry{
				{0, 0, 0},
				{0xff, 0xff, 0xff},
			},
			data: []byte{0, 0, 0, 0},
		}.Bytes(),
		wantErr: nil, // successful base case
	}, {
		name: "1x1 rgb",
		b: bmpBuilder{
			width:        1,
			height:       1,
			planes:       1,
			bitsPerPixel: 24,
			data: []byte{
				0, 0, 0, 0,
			},
		}.Bytes(),
		wantErr: nil, // successful base case
	}, {
		name: "invalid palette index",
		b: bmpBuilder{
			width:        1,
			height:       1,
			planes:       1,
			bitsPerPixel: 8,
			colorsUsed:   2,
			colorTable: []colorTableEntry{
				{0, 0, 0},
				{0xff, 0xff, 0xff},
			},
			data: []byte{
				2, 0, 0, 0, // index 2
			},
		}.Bytes(),
		wantErr: errInvalidPaletteIndex,
	}, {
		name: "0x1",
		b: bmpBuilder{
			width:        1,
			height:       0,
			planes:       1,
			bitsPerPixel: 24,
			data: []byte{
				0, 0, 0, 0,
			},
		}.Bytes(),
		wantErr: ErrUnsupported,
	}, {
		name: "excessive width and height",
		b: bmpBuilder{
			width:        2147483647,
			height:       2147483647,
			planes:       1,
			bitsPerPixel: 24,
		}.Bytes(),
		wantErr: ErrUnsupported,
	}} {
		img, _, err := image.Decode(bytes.NewReader(tc.b))
		if err != tc.wantErr {
			t.Errorf("%v: Decode error %v; want %v", tc.name, err, tc.wantErr)
		}
		if err != nil {
			continue
		}
		_ = img.At(0, 0) // try accessing a pixel
	}
}

// TestEOF tests that decoding a BMP image returns io.ErrUnexpectedEOF
// when there are no headers or data is empty
func TestEOF(t *testing.T) {
	_, err := Decode(bytes.NewReader(nil))
	if err != io.ErrUnexpectedEOF {
		t.Errorf("Error should be io.ErrUnexpectedEOF on nil but got %v", err)
	}
}

type bmpBuilder struct {
	width           int32
	height          int32
	planes          uint16
	bitsPerPixel    uint16
	compression     uint32
	imageSize       uint32
	xppm            uint32
	yppm            uint32
	colorsUsed      uint32
	colorsImportant uint32
	colorTable      []colorTableEntry
	data            []byte
}

type colorTableEntry struct {
	r, g, b byte
}

func (b bmpBuilder) Bytes() []byte {
	buf := []byte{
		0x42, 0x4d, // 'BM'
		0x00, 0x00, 0x00, 0x00, // file size
		0x00, 0x00, 0x00, 0x00, // reserved
		0x00, 0x00, 0x00, 0x00, // data offset
		0x28, 0x00, 0x00, 0x00, // header size (40)
	}
	buf = binary.LittleEndian.AppendUint32(buf, uint32(b.width))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(b.height))
	buf = binary.LittleEndian.AppendUint16(buf, b.planes)
	buf = binary.LittleEndian.AppendUint16(buf, b.bitsPerPixel)
	buf = binary.LittleEndian.AppendUint32(buf, b.compression)
	buf = binary.LittleEndian.AppendUint32(buf, b.imageSize)
	buf = binary.LittleEndian.AppendUint32(buf, b.xppm)
	buf = binary.LittleEndian.AppendUint32(buf, b.yppm)
	buf = binary.LittleEndian.AppendUint32(buf, b.colorsUsed)
	buf = binary.LittleEndian.AppendUint32(buf, b.colorsImportant)
	for _, e := range b.colorTable {
		buf = append(buf, e.r, e.g, e.b, 0)
	}
	binary.LittleEndian.PutUint32(buf[10:], uint32(len(buf))) // data offset
	buf = append(buf, b.data...)
	binary.LittleEndian.PutUint32(buf[2:], uint32(len(buf))) // file size
	return buf
}
