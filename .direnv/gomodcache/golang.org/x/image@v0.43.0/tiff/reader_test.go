// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tiff

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"testing"

	_ "image/png"
)

const testdataDir = "../testdata/"

// Read makes *buffer implements io.Reader, so that we can pass one to Decode.
func (*buffer) Read([]byte) (int, error) {
	panic("unimplemented")
}

func load(name string) (image.Image, error) {
	f, err := os.Open(testdataDir + name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// TestNoRPS tests decoding an image that has no RowsPerStrip tag. The tag is
// mandatory according to the spec but some software omits it in the case of a
// single strip.
func TestNoRPS(t *testing.T) {
	_, err := load("no_rps.tiff")
	if err != nil {
		t.Fatal(err)
	}
}

// TestNoCompression tests decoding an image that has no Compression tag. This
// tag is mandatory, but most tools interpret a missing value as no
// compression.
func TestNoCompression(t *testing.T) {
	_, err := load("no_compress.tiff")
	if err != nil {
		t.Fatal(err)
	}
}

// TestUnpackBits tests the decoding of PackBits-encoded data.
func TestUnpackBits(t *testing.T) {
	var unpackBitsTests = []struct {
		compressed   string
		uncompressed string
	}{{
		// Example data from Wikipedia.
		"\xfe\xaa\x02\x80\x00\x2a\xfd\xaa\x03\x80\x00\x2a\x22\xf7\xaa",
		"\xaa\xaa\xaa\x80\x00\x2a\xaa\xaa\xaa\xaa\x80\x00\x2a\x22\xaa\xaa\xaa\xaa\xaa\xaa\xaa\xaa\xaa\xaa",
	}}
	for _, u := range unpackBitsTests {
		buf, err := unpackBits(strings.NewReader(u.compressed), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf) != u.uncompressed {
			t.Fatalf("unpackBits: want %x, got %x", u.uncompressed, buf)
		}
	}
}

func TestShortBlockData(t *testing.T) {
	b, err := ioutil.ReadFile("../testdata/bw-uncompressed.tiff")
	if err != nil {
		t.Fatal(err)
	}
	// The bw-uncompressed.tiff image is a 153x55 bi-level image. This is 1 bit
	// per pixel, or 20 bytes per row, times 55 rows, or 1100 bytes of pixel
	// data. 1100 in hex is 0x44c, or "\x4c\x04" in little-endian. We replace
	// that byte count (StripByteCounts-tagged data) by something less than
	// that, so that there is not enough pixel data.
	old := []byte{0x4c, 0x04}
	new := []byte{0x01, 0x01}
	i := bytes.Index(b, old)
	if i < 0 {
		t.Fatal(`could not find "\x4c\x04" byte count`)
	}
	if bytes.Contains(b[i+len(old):], old) {
		t.Fatal(`too many occurrences of "\x4c\x04"`)
	}
	b[i+0] = new[0]
	b[i+1] = new[1]
	if _, err = Decode(bytes.NewReader(b)); err == nil {
		t.Fatal("got nil error, want non-nil")
	}
}

func TestDecodeInvalidDataType(t *testing.T) {
	b, err := ioutil.ReadFile("../testdata/bw-uncompressed.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// off is the offset of the ImageWidth tag. It is the offset of the overall
	// IFD block (0x00000454), plus 2 for the uint16 number of IFD entries, plus 12
	// to skip the first entry.
	const off = 0x00000454 + 2 + 12*1

	if v := binary.LittleEndian.Uint16(b[off : off+2]); v != tImageWidth {
		t.Fatal(`could not find ImageWidth tag`)
	}
	binary.LittleEndian.PutUint16(b[off+2:], uint16(len(lengths))) // invalid datatype

	if _, err = Decode(bytes.NewReader(b)); err == nil {
		t.Fatal("got nil error, want non-nil")
	}
}

func compare(t *testing.T, img0, img1 image.Image) {
	t.Helper()
	b0 := img0.Bounds()
	b1 := img1.Bounds()
	if b0.Dx() != b1.Dx() || b0.Dy() != b1.Dy() {
		t.Fatalf("wrong image size: want %s, got %s", b0, b1)
	}
	x1 := b1.Min.X - b0.Min.X
	y1 := b1.Min.Y - b0.Min.Y
	for y := b0.Min.Y; y < b0.Max.Y; y++ {
		for x := b0.Min.X; x < b0.Max.X; x++ {
			c0 := img0.At(x, y)
			c1 := img1.At(x+x1, y+y1)
			r0, g0, b0, a0 := c0.RGBA()
			r1, g1, b1, a1 := c1.RGBA()
			if r0 != r1 || g0 != g1 || b0 != b1 || a0 != a1 {
				t.Fatalf("pixel at (%d, %d) has wrong color: want %v, got %v", x, y, c0, c1)
			}
		}
	}
}

// TestDecode tests that decoding a PNG image and a TIFF image result in the
// same pixel data.
func TestDecode(t *testing.T) {
	img0, err := load("video-001.png")
	if err != nil {
		t.Fatal(err)
	}
	img1, err := load("video-001.tiff")
	if err != nil {
		t.Fatal(err)
	}
	img2, err := load("video-001-strip-64.tiff")
	if err != nil {
		t.Fatal(err)
	}
	img3, err := load("video-001-tile-64x64.tiff")
	if err != nil {
		t.Fatal(err)
	}
	img4, err := load("video-001-16bit.tiff")
	if err != nil {
		t.Fatal(err)
	}

	compare(t, img0, img1)
	compare(t, img0, img2)
	compare(t, img0, img3)
	compare(t, img0, img4)
}

// TestDecodeLZW tests that decoding a PNG image and a LZW-compressed TIFF
// image result in the same pixel data.
func TestDecodeLZW(t *testing.T) {
	img0, err := load("blue-purple-pink.png")
	if err != nil {
		t.Fatal(err)
	}
	img1, err := load("blue-purple-pink.lzwcompressed.tiff")
	if err != nil {
		t.Fatal(err)
	}

	compare(t, img0, img1)
}

// TestEOF tests that decoding a TIFF image returns io.ErrUnexpectedEOF
// when there are no headers or data is empty
func TestEOF(t *testing.T) {
	_, err := Decode(bytes.NewReader(nil))
	if err != io.ErrUnexpectedEOF {
		t.Errorf("Error should be io.ErrUnexpectedEOF on nil but got %v", err)
	}
}

// TestDecodeCCITT tests that decoding a PNG image and a CCITT compressed TIFF
// image result in the same pixel data.
func TestDecodeCCITT(t *testing.T) {
	// TODO Add more tests.
	for _, fn := range []string{
		"bw-gopher",
	} {
		img0, err := load(fn + ".png")
		if err != nil {
			t.Fatal(err)
		}

		img1, err := load(fn + "_ccittGroup3.tiff")
		if err != nil {
			t.Fatal(err)
		}
		compare(t, img0, img1)

		img2, err := load(fn + "_ccittGroup4.tiff")
		if err != nil {
			t.Fatal(err)
		}
		compare(t, img0, img2)
	}
}

// TestDecodeTagOrder tests that a malformed image with unsorted IFD entries is
// correctly rejected.
func TestDecodeTagOrder(t *testing.T) {
	data, err := ioutil.ReadFile("../testdata/video-001.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// Swap the first two IFD entries.
	ifdOffset := int64(binary.LittleEndian.Uint32(data[4:8]))
	for i := ifdOffset + 2; i < ifdOffset+14; i++ {
		data[i], data[i+12] = data[i+12], data[i]
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err == nil {
		t.Fatal("got nil error, want non-nil")
	}
}

// TestDecompress tests that decoding some TIFF images that use different
// compression formats result in the same pixel data.
func TestDecompress(t *testing.T) {
	var decompressTests = []string{
		"bw-uncompressed.tiff",
		"bw-deflate.tiff",
		"bw-packbits.tiff",
	}
	var img0 image.Image
	for _, name := range decompressTests {
		img1, err := load(name)
		if err != nil {
			t.Fatalf("decoding %s: %v", name, err)
		}
		if img0 == nil {
			img0 = img1
			continue
		}
		compare(t, img0, img1)
	}
}

func replace(src []byte, find, repl string) ([]byte, error) {
	removeSpaces := func(r rune) rune {
		if r != ' ' {
			return r
		}
		return -1
	}

	f, err := hex.DecodeString(strings.Map(removeSpaces, find))
	if err != nil {
		return nil, err
	}
	r, err := hex.DecodeString(strings.Map(removeSpaces, repl))
	if err != nil {
		return nil, err
	}
	dst := bytes.Replace(src, f, r, 1)
	if bytes.Equal(dst, src) {
		return nil, errors.New("replacement failed")
	}
	return dst, nil
}

// TestZeroBitsPerSample tests that an IFD with a bitsPerSample of 0 does not
// cause a crash.
// Issue 10711.
func TestZeroBitsPerSample(t *testing.T) {
	b0, err := ioutil.ReadFile(testdataDir + "bw-deflate.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the loaded image to have the problem.
	// 02 01: tag number (tBitsPerSample)
	// 03 00: data type (short, or uint16)
	// 01 00 00 00: count
	// ?? 00 00 00: value (1 -> 0)
	b1, err := replace(b0,
		"02 01 03 00 01 00 00 00 01 00 00 00",
		"02 01 03 00 01 00 00 00 00 00 00 00",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decode(bytes.NewReader(b1))
	if err == nil {
		t.Fatal("Decode with 0 bits per sample: got nil error, want non-nil")
	}
}

// TestTileTooBig tests that we do not panic when a tile is too big compared to
// the data available.
// Issue 10712
func TestTileTooBig(t *testing.T) {
	b0, err := ioutil.ReadFile(testdataDir + "video-001-tile-64x64.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the loaded image to have the problem.
	//
	// 42 01: tag number (tTileWidth)
	// 03 00: data type (short, or uint16)
	// 01 00 00 00: count
	// xx 00 00 00: value (0x40 -> 0x44: a wider tile consumes more data
	// than is available)
	b1, err := replace(b0,
		"42 01 03 00 01 00 00 00 40 00 00 00",
		"42 01 03 00 01 00 00 00 44 00 00 00",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Turn off the predictor, which makes it possible to hit the
	// place with the defect. Without this patch to the image, we run
	// out of data too early, and do not hit the part of the code where
	// the original panic was.
	//
	// 3d 01: tag number (tPredictor)
	// 03 00: data type (short, or uint16)
	// 01 00 00 00: count
	// xx 00 00 00: value (2 -> 1: 2 = horizontal, 1 = none)
	b2, err := replace(b1,
		"3d 01 03 00 01 00 00 00 02 00 00 00",
		"3d 01 03 00 01 00 00 00 01 00 00 00",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decode(bytes.NewReader(b2))
	if err == nil {
		t.Fatal("did not expect nil error")
	}
}

// TestZeroSizedImages tests that decoding does not panic when image dimensions
// are zero, and returns a zero-sized image instead.
// Issue 10393.
func TestZeroSizedImages(t *testing.T) {
	testsizes := []struct {
		w, h int
	}{
		{0, 0},
		{1, 0},
		{0, 1},
	}
	for _, r := range testsizes {
		img := image.NewRGBA(image.Rect(0, 0, r.w, r.h))
		if err := Encode(io.Discard, img, nil); err == nil {
			t.Errorf("encode w=%d h=%d: success (want error)", r.w, r.h)
		}

		enc, err := os.ReadFile(fmt.Sprintf("testdata/%vx%v.tiff", r.w, r.h))
		if err != nil {
			t.Error(err)
			continue
		}
		if _, err := Decode(bytes.NewReader(enc)); err == nil {
			t.Errorf("decode w=%d h=%d: %v", r.w, r.h, err)
		}
	}
}

// TestLargeIFDEntry tests that a large IFD entry does not cause Decode to
// panic.
// Issue 10596.
func TestLargeIFDEntry(t *testing.T) {
	testdata := "II*\x00\x08\x00\x00\x00\f\x000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"00000000000000\x17\x01\x04\x00\x01\x00" +
		"\x00\xc0000000000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"000000"
	_, err := Decode(strings.NewReader(testdata))
	if err == nil {
		t.Fatal("Decode with large IFD entry: got nil error, want non-nil")
	}
}

func TestInvalidPaletteRef(t *testing.T) {
	contents, err := ioutil.ReadFile(testdataDir + "invalid-palette-ref.tiff")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(contents)); err == nil {
		t.Fatal("Decode with invalid palette index: got nil error, want non-nil")
	}
}

// benchmarkDecode benchmarks the decoding of an image.
func benchmarkDecode(b *testing.B, filename string) {
	b.Helper()
	contents, err := ioutil.ReadFile(testdataDir + filename)
	if err != nil {
		b.Fatal(err)
	}
	benchmarkDecodeData(b, contents)
}

func benchmarkDecodeData(b *testing.B, data []byte) {
	b.Helper()
	r := &buffer{buf: data}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Decode(r)
		if err != nil {
			b.Fatal("Decode:", err)
		}
	}
}

func BenchmarkDecodeCompressed(b *testing.B)   { benchmarkDecode(b, "video-001.tiff") }
func BenchmarkDecodeUncompressed(b *testing.B) { benchmarkDecode(b, "video-001-uncompressed.tiff") }

func BenchmarkZeroHeightTile(b *testing.B) {
	enc := binary.BigEndian
	data := newTIFF(enc)
	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:  uint32(4294967295),
		tImageLength: uint32(0),
		tTileWidth:   uint32(1),
		tTileLength:  uint32(0),
	})
	benchmarkDecodeData(b, data)
}

func BenchmarkRepeatedOversizedTileData(b *testing.B) {
	const (
		imageWidth  = 256
		imageHeight = 256
		tileWidth   = 8
		tileLength  = 8
		numTiles    = (imageWidth * imageHeight) / (tileWidth * tileLength)
	)

	// Create a chunk of tile data that decompresses to a large size.
	zdata := func() []byte {
		var zbuf bytes.Buffer
		zw := zlib.NewWriter(&zbuf)
		zeros := make([]byte, 1024)
		for i := 0; i < 1<<16; i++ {
			zw.Write(zeros)
		}
		zw.Close()
		return zbuf.Bytes()
	}()

	enc := binary.BigEndian
	data := newTIFF(enc)

	zoff := len(data)
	data = append(data, zdata...)

	// Each tile refers to the same compressed data chunk.
	var tileoffs []uint32
	var tilesizes []uint32
	for i := 0; i < numTiles; i++ {
		tileoffs = append(tileoffs, uint32(zoff))
		tilesizes = append(tilesizes, uint32(len(zdata)))
	}

	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:                uint32(imageWidth),
		tImageLength:               uint32(imageHeight),
		tTileWidth:                 uint32(tileWidth),
		tTileLength:                uint32(tileLength),
		tTileOffsets:               tileoffs,
		tTileByteCounts:            tilesizes,
		tCompression:               uint16(cDeflate),
		tBitsPerSample:             []uint16{16, 16, 16},
		tPhotometricInterpretation: uint16(pRGB),
	})
	benchmarkDecodeData(b, data)
}

type byteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// newTIFF returns the TIFF header.
func newTIFF(enc byteOrder) []byte {
	b := []byte{0, 0, 0, 42, 0, 0, 0, 0}
	switch enc.Uint16([]byte{1, 0}) {
	case 0x1:
		b[0], b[1] = 'I', 'I'
	case 0x100:
		b[0], b[1] = 'M', 'M'
	default:
		panic("odd byte order")
	}
	return b
}

// appendIFD appends an IFD to the TIFF in b,
// updating the IFD location in the header.
func appendIFD(b []byte, enc byteOrder, entries map[uint16]interface{}) []byte {
	var tags []uint16
	for tag := range entries {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i] < tags[j]
	})

	var ifd []byte
	for _, tag := range tags {
		ifd = enc.AppendUint16(ifd, tag)
		switch v := entries[tag].(type) {
		case uint16:
			ifd = enc.AppendUint16(ifd, dtShort)
			ifd = enc.AppendUint32(ifd, 1)
			ifd = enc.AppendUint16(ifd, v)
			ifd = enc.AppendUint16(ifd, v)
		case uint32:
			ifd = enc.AppendUint16(ifd, dtLong)
			ifd = enc.AppendUint32(ifd, 1)
			ifd = enc.AppendUint32(ifd, v)
		case []uint16:
			ifd = enc.AppendUint16(ifd, dtShort)
			ifd = enc.AppendUint32(ifd, uint32(len(v)))
			switch len(v) {
			case 0:
				ifd = enc.AppendUint32(ifd, 0)
			case 1:
				ifd = enc.AppendUint16(ifd, v[0])
				ifd = enc.AppendUint16(ifd, v[1])
			default:
				ifd = enc.AppendUint32(ifd, uint32(len(b)))
				for _, e := range v {
					b = enc.AppendUint16(b, e)
				}
			}
		case []uint32:
			ifd = enc.AppendUint16(ifd, dtLong)
			ifd = enc.AppendUint32(ifd, uint32(len(v)))
			switch len(v) {
			case 0:
				ifd = enc.AppendUint32(ifd, 0)
			case 1:
				ifd = enc.AppendUint32(ifd, v[0])
			default:
				ifd = enc.AppendUint32(ifd, uint32(len(b)))
				for _, e := range v {
					b = enc.AppendUint32(b, e)
				}
			}
		default:
			panic(fmt.Errorf("unhandled type %T", v))
		}
	}

	enc.PutUint32(b[4:8], uint32(len(b)))
	b = enc.AppendUint16(b, uint16(len(entries)))
	b = append(b, ifd...)
	b = enc.AppendUint32(b, 0)
	return b
}

// ioReader wraps an io.Reader to hide any io.ReaderAt implementation,
// forcing the tiff package to use the buffer code path.
type ioReader struct {
	io.Reader
}

// TestDecodeOOMIFDOffset tests that a TIFF with an IFD offset of 0xFFFFFFFF
// does not cause an out-of-memory panic in buffer.fill.
func TestDecodeOOMIFDOffset(t *testing.T) {
	for _, endian := range []struct {
		name   string
		header []byte
	}{
		{"little-endian", []byte{'I', 'I', 42, 0, 0xff, 0xff, 0xff, 0xff}},
		{"big-endian", []byte{'M', 'M', 0, 42, 0xff, 0xff, 0xff, 0xff}},
	} {
		t.Run(endian.name, func(t *testing.T) {
			r := ioReader{bytes.NewReader(endian.header)}
			_, err := Decode(r)
			if err == nil {
				t.Error("Decode with IFD offset 0xFFFFFFFF: got nil error, want non-nil")
			}
			r = ioReader{bytes.NewReader(endian.header)}
			_, err = DecodeConfig(r)
			if err == nil {
				t.Error("DecodeConfig with IFD offset 0xFFFFFFFF: got nil error, want non-nil")
			}
		})
	}
}

func TestDecodeTooLarge(t *testing.T) {
	var tiffHeader = []byte{
		'I', 'I', 42, 0, // Header
		8, 0, 0, 0, // Offset of first IFD
		4, 0, // Number of IFD entries (4)
		// Entry 1: ImageWidth (256)
		0, 1, // Tag 256
		4, 0, // dtLong (4)
		1, 0, 0, 0, // Count 1
		0x00, 0x00, 0x00, 0x7f, // Value: 0x7f000000
		// Entry 2: ImageLength (257)
		1, 1, // Tag 257
		4, 0, // dtLong (4)
		1, 0, 0, 0, // Count 1
		0x00, 0x00, 0x00, 0x7f, // Value: 0x7f000000
		// Entry 3: StripOffsets (273)
		0x11, 0x01, // Tag 273
		4, 0, // dtLong (4)
		1, 0, 0, 0, // Count 1
		0, 0, 0, 0, // Value: 0
		// Entry 4: StripByteCounts (279)
		0x17, 0x01, // Tag 279
		4, 0, // dtLong (4)
		1, 0, 0, 0, // Count 1
		0, 0, 0, 0, // Value: 0
		0, 0, 0, 0, // Next IFD offset
	}

	_, err := DecodeConfig(bytes.NewReader(tiffHeader))
	if err == nil {
		t.Error("DecodeConfig: got nil error, want non-nil")
	}
	_, err = Decode(bytes.NewReader(tiffHeader))
	if err == nil {
		t.Fatal("Decode: got nil error, want non-nil")
	}
}

// TestDecodeSliceOverflow tests that decoding a TIFF with a large strip offset
// does not trigger a slice bounds panic in buffer.Slice.
func TestDecodeSliceOverflow(t *testing.T) {
	var tiffHeader = []byte{
		'I', 'I', 42, 0, // Header
		8, 0, 0, 0, // Offset of first IFD
		4, 0, // Number of IFD entries (4)
		// Entry 1: ImageWidth (256)
		0, 1, // Tag 256
		3, 0, // dtShort (3)
		1, 0, 0, 0, // Count 1
		1, 0, 0, 0, // Value: 1
		// Entry 2: ImageLength (257)
		1, 1, // Tag 257
		3, 0, // dtShort (3)
		1, 0, 0, 0, // Count 1
		1, 0, 0, 0, // Value: 1
		// Entry 3: StripOffsets (273)
		0x11, 0x01, // Tag 273
		4, 0, // dtLong (4)
		1, 0, 0, 0, // Count 1
		0xff, 0xff, 0xff, 0xff, // Value: 0xFFFFFFFF
		// Entry 4: StripByteCounts (279)
		0x17, 0x01, // Tag 279
		4, 0, // dtLong (4)
		1, 0, 0, 0, // Count 1
		10, 0, 0, 0, // Value: 10
		0, 0, 0, 0, // Next IFD offset
	}

	// Use ioReader to force the use of the buffer code path.
	r := ioReader{bytes.NewReader(tiffHeader)}
	_, err := Decode(r)
	if err == nil {
		t.Error("Decode: got nil error, want non-nil")
	}
}

func TestTileSizeError(t *testing.T) {
	enc := binary.BigEndian
	data := newTIFF(enc)
	data = appendIFD(data, enc, map[uint16]any{
		tImageWidth:                uint32(100),
		tImageLength:               uint32(100),
		tTileWidth:                 uint32(1024),
		tTileLength:                uint32(1025),
		tTileOffsets:               []uint32{8},
		tTileByteCounts:            []uint32{0},
		tPhotometricInterpretation: uint16(pBlackIsZero),
		tBitsPerSample:             uint16(8),
	})

	_, err := Decode(bytes.NewReader(data))
	if want := "tile size exceeds image size"; err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Decode: got %v, want error containing %q", err, want)
	}
}

func TestDecodeBlockDataSizeTooLarge(t *testing.T) {
	t.Run("strip", func(t *testing.T) {
		// 1x1 image, blockMaxDataSize = 1 * 1 * 8 = 8.
		// StripByteCounts is set to 9.
		enc := binary.BigEndian
		data := newTIFF(enc)
		data = appendIFD(data, enc, map[uint16]any{
			tImageWidth:                uint32(1),
			tImageLength:               uint32(1),
			tStripOffsets:              []uint32{8},
			tStripByteCounts:           []uint32{9},
			tPhotometricInterpretation: uint16(pBlackIsZero),
			tBitsPerSample:             uint16(8),
		})

		_, err := Decode(bytes.NewReader(data))
		if want := "block data size too large"; err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Decode: got %v, want error containing %q", err, want)
		}
	})

	t.Run("tile", func(t *testing.T) {
		// 16x16 image with 16x16 tiles.
		// blockMaxDataSize = 16 * 16 * 8 = 2048.
		// TileByteCounts is set to 2049.
		enc := binary.BigEndian
		data := newTIFF(enc)
		data = appendIFD(data, enc, map[uint16]any{
			tImageWidth:                uint32(16),
			tImageLength:               uint32(16),
			tTileWidth:                 uint32(16),
			tTileLength:                uint32(16),
			tTileOffsets:               []uint32{8},
			tTileByteCounts:            []uint32{2049},
			tPhotometricInterpretation: uint16(pBlackIsZero),
			tBitsPerSample:             uint16(8),
		})

		_, err := Decode(bytes.NewReader(data))
		if want := "block data size too large"; err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Decode: got %v, want error containing %q", err, want)
		}
	})
}
