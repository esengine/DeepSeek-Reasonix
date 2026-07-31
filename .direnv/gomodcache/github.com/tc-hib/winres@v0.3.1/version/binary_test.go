package version

import (
	"io"
	"testing"
)

type eofReader struct {
	remainder int
}

func (r *eofReader) Read(data []byte) (int, error) {
	for i := range data {
		if r.remainder <= 0 {
			return i, io.EOF
		}
		data[i] = byte(i)
		r.remainder--
	}
	return len(data), nil
}

func Test_binaryRead(t *testing.T) {
	r := eofReader{3 * 4 * 5}
	s := make([]uint32, 5)
	for j := 0; j < 4; j++ {
		err := binaryRead(&r, &s)
		if (err == nil) != (j < 3) {
			t.FailNow()
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			t.Fatal(err)
		}
		for i := range s {
			if s[i] != uint32((i*4)|(i*4+1)<<8|(i*4+2)<<16|(i*4+3)<<24) {
				t.FailNow()
			}
		}
	}
}
