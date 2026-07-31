package winres

import (
	"io"
	"testing"
)

func Test_writeObject_WriteErrFileHeader(t *testing.T) {
	bw := newBadWriter(19)
	rs := &ResourceSet{}

	err := writeObject(bw, rs, ArchAMD64)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func Test_writeObject_WriteErrSectionHeader(t *testing.T) {
	bw := newBadWriter(58)
	rs := &ResourceSet{}

	err := writeObject(bw, rs, ArchAMD64)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func Test_writeObject_WriteErrReloc(t *testing.T) {
	bw := newBadWriter(165)
	rs := &ResourceSet{}
	rs.Set(RT_RCDATA, ID(1), 0, make([]byte, 1))

	err := writeObject(bw, rs, ArchAMD64)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func Test_writeObject_WriteErrSymbol(t *testing.T) {
	bw := newBadWriter(182)
	rs := &ResourceSet{}
	rs.Set(RT_RCDATA, ID(1), 0, make([]byte, 1))

	err := writeObject(bw, rs, ArchAMD64)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func Test_writeObject_WriteErrStringTable(t *testing.T) {
	bw := newBadWriter(185)
	rs := &ResourceSet{}
	rs.Set(RT_RCDATA, ID(1), 0, make([]byte, 1))

	err := writeObject(bw, rs, ArchAMD64)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func Test_writeObject_WriteErrSection(t *testing.T) {
	bw := newBadWriter(61)
	rs := &ResourceSet{}
	rs.Set(RT_RCDATA, ID(1), 0, make([]byte, 1))

	err := writeObject(bw, rs, ArchAMD64)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func Test_writeRelocTable_UnknownArch(t *testing.T) {
	err := writeRelocTable(io.Discard, 1, "*", []int{1, 2, 3, 4})
	if err == nil || err.Error() != errUnknownArch {
		t.Fail()
	}
}
