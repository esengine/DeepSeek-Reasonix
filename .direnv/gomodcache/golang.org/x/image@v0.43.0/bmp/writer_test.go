// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bmp

import (
	"bytes"
	"image"
	"image/draw"
	"io/ioutil"
	"os"
	"testing"
)

func openImage(filename string) (image.Image, error) {
	f, err := os.Open(testdataDir + filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Decode(f)
}

func convertToRGBA(in image.Image) image.Image {
	b := in.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, in, b.Min, draw.Src)
	return out
}

func convertToNRGBA(in image.Image) image.Image {
	b := in.Bounds()
	out := image.NewNRGBA(b)
	draw.Draw(out, b, in, b.Min, draw.Src)
	return out
}

func TestEncode(t *testing.T) {
	testCases := []string{
		"video-001.bmp",
		"yellow_rose-small.bmp",
	}

	for _, tc := range testCases {
		img0, err := openImage(tc)
		if err != nil {
			t.Errorf("%s: Open BMP: %v", tc, err)
			continue
		}

		buf := new(bytes.Buffer)
		err = Encode(buf, img0)
		if err != nil {
			t.Errorf("%s: Encode BMP: %v", tc, err)
			continue
		}

		img1, err := Decode(buf)
		if err != nil {
			t.Errorf("%s: Decode BMP: %v", tc, err)
			continue
		}

		err = compare(img0, img1)
		if err != nil {
			t.Errorf("%s: Compare BMP: %v", tc, err)
			continue
		}

		buf2 := new(bytes.Buffer)
		rgba := convertToRGBA(img0)
		err = Encode(buf2, rgba)
		if err != nil {
			t.Errorf("%s: Encode pre-multiplied BMP: %v", tc, err)
			continue
		}

		img2, err := Decode(buf2)
		if err != nil {
			t.Errorf("%s: Decode pre-multiplied BMP: %v", tc, err)
			continue
		}

		// We need to do another round trip to NRGBA to compare to, since
		// the conversion process is lossy.
		img3 := convertToNRGBA(rgba)

		err = compare(img3, img2)
		if err != nil {
			t.Errorf("%s: Compare pre-multiplied BMP: %v", tc, err)
		}
	}
}

// BenchmarkEncode benchmarks the encoding of an image.
func BenchmarkEncode(b *testing.B) {
	img, err := openImage("video-001.bmp")
	if err != nil {
		b.Fatal(err)
	}
	s := img.Bounds().Size()
	b.SetBytes(int64(s.X * s.Y * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Encode(ioutil.Discard, img)
	}
}
