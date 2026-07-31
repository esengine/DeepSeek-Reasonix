package winres

import (
	"bytes"
	"io"
	"testing"
)

func Test_ResourceSet_write_WriteErr(t *testing.T) {
	rs := ResourceSet{}
	rs.Set(Name("NAME"), Name("NAME"), 0, make([]byte, 6))
	const writeErrMsg = "expected write error, got"

	if _, err := rs.write(newBadWriter(15)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(23)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(39)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(47)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(63)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(71)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(87)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(103)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(111)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	rs.Set(ID(1), ID(1), 0, make([]byte, 6))

	if _, err := rs.write(newBadWriter(31)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}

	if _, err := rs.write(newBadWriter(79)); !isExpectedWriteErr(err) {
		t.Fatal(writeErrMsg, err)
	}
}

func Test_dataEntry_writeData(t *testing.T) {
	expected := [][]byte{
		{},
		{1, 0, 0, 0, 0, 0, 0, 0},
		{1, 2, 0, 0, 0, 0, 0, 0},
		{1, 2, 3, 0, 0, 0, 0, 0},
		{1, 2, 3, 4, 0, 0, 0, 0},
		{1, 2, 3, 4, 5, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 0},
		{1, 2, 3, 4, 5, 6, 7, 8},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 0, 0, 0, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 0, 0, 0, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 0, 0, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 0, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	}

	for i := 0; i <= 16; i++ {
		de := dataEntry{data: make([]byte, i)}
		for j := 0; j < i; j++ {
			de.data[j] = byte(j + 1)
		}
		buf := &bytes.Buffer{}

		err := de.writeData(buf)
		if err != nil || !bytes.Equal(buf.Bytes(), expected[i]) {
			t.Fail()
		}
		err = de.writeData(newBadWriter(i - 1))
		if !isExpectedWriteErr(err) {
			t.Fatal("expected write error, got", err)
		}
	}
}

func TestResourceSet_read1(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read(loadBinary(t, "rsrc1.bin"), 0xE6B000, ID(0))
	if err != nil || rs.lastIconID != 12 || rs.lastCursorID != 5 {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	rs.write(buf)
	checkBinary(t, buf.Bytes())
}

func TestResourceSet_read_RT_ICON(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read(loadBinary(t, "rsrc1.bin"), 0xE6B000, RT_ICON)
	if err != nil || rs.lastIconID != 12 || rs.lastCursorID != 0 {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	rs.write(buf)
	checkBinary(t, buf.Bytes())
}

func TestResourceSet_read_RT_GROUP_ICON(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read(loadBinary(t, "rsrc1.bin"), 0xE6B000, RT_GROUP_ICON)
	if err != nil || rs.lastIconID != 12 || rs.lastCursorID != 0 {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	rs.write(buf)
	checkBinary(t, buf.Bytes())
}

func TestResourceSet_read_RT_CURSOR(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read(loadBinary(t, "rsrc1.bin"), 0xE6B000, RT_CURSOR)
	if err != nil || rs.lastIconID != 0 || rs.lastCursorID != 5 {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	rs.write(buf)
	checkBinary(t, buf.Bytes())
}

func TestResourceSet_read_RT_GROUP_CURSOR(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read(loadBinary(t, "rsrc1.bin"), 0xE6B000, RT_GROUP_CURSOR)
	if err != nil || rs.lastIconID != 0 || rs.lastCursorID != 5 {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	rs.write(buf)
	checkBinary(t, buf.Bytes())
}

func TestResourceSet_read_PNG(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read(loadBinary(t, "rsrc1.bin"), 0xE6B000, Name("PNG"))
	if err != nil || rs.lastIconID != 0 || rs.lastCursorID != 0 {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	rs.write(buf)
	checkBinary(t, buf.Bytes())
}

func TestResourceSet_read_EOF1(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{}, 0x42, ID(0))
	if err != io.ErrUnexpectedEOF {
		t.Fail()
	}
}

func TestResourceSet_read_EOF2(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{0xC: 1, 0x16: 0}, 0x42, ID(0))
	if err != io.ErrUnexpectedEOF {
		t.Fail()
	}
}

func TestResourceSet_read_ErrNode(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{0xC: 1, 0x13: 0x80, 0x17: 0}, 0x42, ID(0))
	if err == nil || err.Error() != errInvalidResDir {
		t.Fail()
	}
}

func TestResourceSet_read_ErrLeaf(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{0xC: 1, 0x17: 0x80}, 0x42, ID(0))
	if err == nil || err.Error() != errInvalidResDir {
		t.Fail()
	}
}

func TestResourceSet_read_ErrLeafName(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{
		0xE:  1,
		0x14: 0x18,
		0x17: 0x80,
		0x26: 1,
		0x2C: 0x30,
		0x2F: 0x80,
		0x3E: 1,
		0x43: 0x80,
		0x44: 0x48,
		0x47: 0,
	}, 0x42, ID(0))
	if err == nil || err.Error() != errInvalidResDir {
		t.Fail()
	}
}

func TestResourceSet_read_ErrEOF3(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{
		0xC:  1,
		0x10: 0xFF,
		0x13: 0x80,
		0x14: 0x18,
		0x17: 0x80,
		0x26: 1,
		0x2C: 0x30,
		0x2F: 0x80,
		0x3E: 1,
		0x44: 0x48,
		0x47: 0,
	}, 0x42, ID(0))
	if err != io.ErrUnexpectedEOF {
		t.Fail()
	}
}

func TestResourceSet_read_ErrEOF4(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{
		0xC:  1,
		0x10: 0x44,
		0x13: 0x80,
		0x14: 0x18,
		0x17: 0x80,
		0x26: 1,
		0x2C: 0x30,
		0x2F: 0x80,
		0x3E: 1,
		0x44: 0x48,
		0x47: 0,
	}, 0x42, ID(0))
	if err != io.ErrUnexpectedEOF {
		t.Fail()
	}
}

func TestResourceSet_read_ErrEOF5(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{
		0xE:  1,
		0x10: 0x01,
		0x14: 0x18,
		0x17: 0x80,
		0x26: 1,
		0x2C: 0x30,
		0x2F: 0x80,
		0x3E: 1,
		0x44: 0x48,
		0x47: 0,
	}, 0x42, ID(0))
	if err != io.ErrUnexpectedEOF {
		t.Fail()
	}
}

func TestResourceSet_read_DataOutOfBounds1(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read([]byte{
		0xE:  1,
		0x10: 1,
		0x14: 0x18,
		0x17: 0x80,
		0x26: 1,
		0x2C: 0x30,
		0x2F: 0x80,
		0x3E: 1,
		0x44: 0x48,
		0x48: 0x58,
		0x49: 0x10,
		0x4C: 0x10,
		0x5D: 0,
	}, 0x1000, ID(0))
	if err == nil || err.Error() != errDataEntryOutOfBounds {
		t.Fail()
	}
}

func TestResourceSet_read_DataOutOfBounds2(t *testing.T) {
	rs := ResourceSet{}
	err := rs.read(loadBinary(t, "rsrc1.bin"), 0x42, ID(0))
	if err == nil || err.Error() != errDataEntryOutOfBounds {
		t.Fail()
	}
}
