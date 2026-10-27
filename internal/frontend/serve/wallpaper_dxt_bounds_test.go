package serve

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"
)

func dxtSizedTEX(format, texW, texH, imgW, imgH int, block []byte) []byte {
	var b bytes.Buffer
	b.WriteString("TEXV0005")
	b.WriteByte(0)
	b.WriteString("TEXI0001")
	b.WriteByte(0)
	for _, value := range []uint32{uint32(format), 0, uint32(texW), uint32(texH), uint32(imgW), uint32(imgH), 0} {
		if err := binary.Write(&b, binary.LittleEndian, value); err != nil {
			panic(err)
		}
	}
	b.WriteString("TEXB0004")
	b.WriteByte(0)
	for _, value := range []uint32{1, 1, uint32(texW), uint32(texH), 0, uint32(len(block)), uint32(len(block))} {
		if err := binary.Write(&b, binary.LittleEndian, value); err != nil {
			panic(err)
		}
	}
	b.Write(block)
	return b.Bytes()
}

func TestDXTTextureRejectsAnImageLargerThanItsTexture(t *testing.T) {
	block := make([]byte, 8)
	for _, dims := range [][2]int{{8, 4}, {2, 8}} {
		if _, _, err := imageFromTEX(dxtSizedTEX(7, 4, 4, dims[0], dims[1], block)); err == nil {
			t.Errorf("DXT with texture 4x4 and image %dx%d was accepted", dims[0], dims[1])
		}
	}
}

func TestLZ4BlockDoesNotAllocateItsDeclaredSize(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := lz4Block([]byte{0x00}, 256<<20); err == nil {
		t.Fatal("a tiny LZ4 block with a huge declared size was accepted")
	}
	runtime.ReadMemStats(&after)
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 16<<20 {
		t.Fatalf("refusing the LZ4 block allocated %d bytes", delta)
	}
}
