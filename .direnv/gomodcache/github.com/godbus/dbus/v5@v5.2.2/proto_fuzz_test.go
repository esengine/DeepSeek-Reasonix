package dbus

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func FuzzProto(f *testing.F) {
	for _, t := range protoTests {
		f.Add(t.bigEndian, SignatureOf(t.vs...).str)
		f.Add(t.littleEndian, SignatureOf(t.vs...).str)
	}
	f.Fuzz(func(t *testing.T, buf []byte, sigStr string) {
		sig, err := ParseSignature(sigStr)
		if err != nil {
			return
		}
		bigDec := newDecoder(bytes.NewReader(buf), binary.BigEndian, make([]int, 0))
		_, _ = bigDec.Decode(sig)
	})
}
