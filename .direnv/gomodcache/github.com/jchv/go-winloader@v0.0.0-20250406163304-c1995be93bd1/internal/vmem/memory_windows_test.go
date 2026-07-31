package vmem

import (
	"io"
	"testing"
)

func TestBasicAlloc(t *testing.T) {
	mem := Alloc(0, 0x1000, MemCommit|MemReserve, PageReadWrite)
	mem.Write([]byte("Test!"))
	mem.Seek(0, io.SeekStart)
	b := make([]byte, 5)
	mem.Read(b)
	if string(b) != "Test!" {
		t.Errorf(`expected "Test!" got %q`, string(b))
	}
}
