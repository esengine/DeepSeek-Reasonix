package normalize

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseMP4Duration(t *testing.T) {
	buf := new(bytes.Buffer)

	// moov header
	moovHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(moovHeader[0:4], 40)
	copy(moovHeader[4:8], "moov")
	buf.Write(moovHeader)

	// mvhd header
	mvhdHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(mvhdHeader[0:4], 32)
	copy(mvhdHeader[4:8], "mvhd")
	buf.Write(mvhdHeader)

	// mvhd payload version 0
	mvhdPayload := make([]byte, 24)
	mvhdPayload[0] = 0                                   // version
	binary.BigEndian.PutUint32(mvhdPayload[12:16], 1000) // timescale
	binary.BigEndian.PutUint32(mvhdPayload[16:20], 5000) // duration
	buf.Write(mvhdPayload)

	reader := bytes.NewReader(buf.Bytes())
	duration, err := ParseMP4Duration(reader)
	if err != nil {
		t.Fatalf("ParseMP4Duration failed: %v", err)
	}
	if duration != 5000 {
		t.Errorf("expected 5000, got %d", duration)
	}
}

func TestParseMP4Duration_V1(t *testing.T) {
	buf := new(bytes.Buffer)

	// moov header
	moovHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(moovHeader[0:4], 52)
	copy(moovHeader[4:8], "moov")
	buf.Write(moovHeader)

	// mvhd header
	mvhdHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(mvhdHeader[0:4], 44)
	copy(mvhdHeader[4:8], "mvhd")
	buf.Write(mvhdHeader)

	// mvhd payload version 1
	mvhdPayload := make([]byte, 36)
	mvhdPayload[0] = 1                                   // version
	binary.BigEndian.PutUint32(mvhdPayload[20:24], 1000) // timescale
	binary.BigEndian.PutUint64(mvhdPayload[24:32], 6000) // duration
	buf.Write(mvhdPayload)

	reader := bytes.NewReader(buf.Bytes())
	duration, err := ParseMP4Duration(reader)
	if err != nil {
		t.Fatalf("ParseMP4Duration failed: %v", err)
	}
	if duration != 6000 {
		t.Errorf("expected 6000, got %d", duration)
	}
}
