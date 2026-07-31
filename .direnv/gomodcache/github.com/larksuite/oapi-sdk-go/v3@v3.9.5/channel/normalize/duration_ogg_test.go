package normalize

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseOpusDuration(t *testing.T) {
	buf := make([]byte, 100)
	copy(buf[60:64], []byte{0x4f, 0x67, 0x67, 0x53})

	granuleBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(granuleBytes, 48000) // 1000ms
	copy(buf[66:74], granuleBytes)

	reader := bytes.NewReader(buf)
	duration, err := ParseOpusDuration(reader)
	if err != nil {
		t.Fatalf("ParseOpusDuration failed: %v", err)
	}
	if duration != 1000 {
		t.Errorf("expected 1000, got %d", duration)
	}
}

func TestParseOpusDuration_NoOggS(t *testing.T) {
	buf := make([]byte, 100)
	reader := bytes.NewReader(buf)
	_, err := ParseOpusDuration(reader)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
