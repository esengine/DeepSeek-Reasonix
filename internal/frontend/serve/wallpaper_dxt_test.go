package serve

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"
)

func dxtTestTEX(format, width, height int, block []byte) []byte {
	var b bytes.Buffer
	b.WriteString("TEXV0005\x00TEXI0001\x00")
	for _, value := range []uint32{uint32(format), 0, uint32(width), uint32(height), uint32(width), uint32(height), 0} {
		if err := binary.Write(&b, binary.LittleEndian, value); err != nil {
			panic(err)
		}
	}
	b.WriteString("TEXB0004\x00")
	for _, value := range []uint32{1, 1, uint32(width), uint32(height), 0, uint32(len(block)), uint32(len(block))} {
		if err := binary.Write(&b, binary.LittleEndian, value); err != nil {
			panic(err)
		}
	}
	b.Write(block)
	return b.Bytes()
}

func TestDXT1TextureBecomesAPNG(t *testing.T) {
	block := []byte{0x00, 0xf8, 0x1f, 0x00, 0x00, 0x00, 0x00, 0x00}
	raw, mime, err := imageFromTEX(dxtTestTEX(7, 4, 4, block))
	if err != nil || mime != "image/png" {
		t.Fatalf("DXT1 = %q, %v", mime, err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r != 0xffff || g != 0 || b != 0 || a != 0xffff {
		t.Fatalf("first pixel = %04x %04x %04x %04x, want red", r, g, b, a)
	}
}

func TestDXT3AndDXT5CarryAlpha(t *testing.T) {
	color := []byte{0x00, 0xf8, 0x1f, 0x00, 0x00, 0x00, 0x00, 0x00}
	dxt3 := append([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, color...)
	out, err := decodeDXT(dxt3, 4, 4, 16, 1)
	if err != nil || out[3] != 0xff {
		t.Fatalf("DXT3 alpha = %v, %v", out, err)
	}
	alpha := []byte{0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	dxt5 := append(alpha, color...)
	out, err = decodeDXT(dxt5, 4, 4, 16, 4)
	if err != nil || out[3] != 0xff {
		t.Fatalf("DXT5 alpha = %v, %v", out, err)
	}
}
