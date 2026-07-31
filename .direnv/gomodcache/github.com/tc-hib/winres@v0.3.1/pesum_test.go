package winres

import "testing"

func Test_peCheckSum_Write(t *testing.T) {
	w := peCheckSum{}
	w.Write(nil)
	w.Write([]byte{0x42})
	w.Write([]byte{0xAF, 0xE7, 0x50})
	if w.sum != 42 {
		t.FailNow()
	}
	w.Write([]byte{0x10, 0xFF, 0xFF, 0xCD, 0x00, 0x50, 0xF0})
	w.Write([]byte{0xFF, 0xFF, 0x01, 0x82, 0xBF, 0x51})
	if w.sum != 0xDEAD || w.rem != 0x51 || w.Sum() != 0xDF0F {
		t.FailNow()
	}
	w.Write([]byte{0})
	if w.Sum() != 0xDF10 {
		t.FailNow()
	}
	w.Write([]byte{0x10, 0x20, 0xF7})
	if w.Sum() != 0x1B {
		t.FailNow()
	}
	b := make([]byte, 0x12345)
	for i := 0; i < 0x100; i++ {
		w.Write(b)
	}
	if w.Sum() != 0x123451B {
		t.FailNow()
	}
}
