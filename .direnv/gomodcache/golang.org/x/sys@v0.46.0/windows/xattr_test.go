// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package windows_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestFdXattr(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "test_fd_xattr.txt")
	wantContent := "I am an xattr testing file. I will get some xattrs attached."
	wantXa := map[string]string{
		"xattr-key-1": "Value for xattr-key-1",
		"xattr-key-2": "Also value, but for xattr-key-2",
		"xattr-key-3": "xattr-key-3 needs a value too",
		"xattr-key-4": "xattr-key-4 never wanted any value but got one anyway",
	}

	xattrSet(t, fp, wantContent, wantXa)

	fr, err := os.Open(fp)
	if err != nil {
		t.Fatalf("Open for read error: %v", err)
	}
	defer fr.Close()

	haveContent, err := io.ReadAll(fr)

	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(haveContent) != wantContent {
		t.Fatalf("File content mismatch: want %q, have %q", wantContent, string(haveContent))
	}

	haveXa, err := winGetEa(fr)
	if err != nil {
		t.Fatalf("Windows get EA error: %v", err)
	}

	for k, v := range wantXa {
		if haveXa[k] != v {
			t.Fatalf("XAttr mismatch for key %q: want %q, have %q", k, v, haveXa[k])
		}
	}
}

func xattrSet(t *testing.T, fp string, content string, xa map[string]string) {
	fw, err := os.OpenFile(fp, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("Open for write error: %v", err)
	}
	defer fw.Close()

	_, err = fw.WriteString(content)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	err = winSetEa(fw, xa)
	if err != nil {
		if err == windows.STATUS_EAS_NOT_SUPPORTED {
			t.Skip("filesystem does not support extended attributes, skipping test")
		}
		t.Fatalf("Windows set EA error: %v", err)
	}
}

// ExtendedAttribute represents a single Windows EA.
type extendedAttribute struct {
	Name  string
	Value []byte
	Flags uint8
}

type fileFullEaInformation struct {
	NextEntryOffset uint32
	Flags           uint8
	NameLength      uint8
	ValueLength     uint16
}

var fileFullEaInformationSize = binary.Size(&fileFullEaInformation{})

// Windows just cannot keep its hands off letter case
func keyToAttrName(k string) string {
	return strings.ToUpper(k)
}

func attrNameToKey(k string) string {
	return strings.ToLower(k)
}

func winSetEa(f *os.File, xattrs map[string]string) error {
	eas := make([]extendedAttribute, 0, len(xattrs))

	for k, v := range xattrs {
		eas = append(eas, extendedAttribute{
			Name:  keyToAttrName(k),
			Value: []byte(v),
		})
	}

	eaBuf, err := encodeExtendedAttributes(eas)
	if err != nil {
		return err
	}

	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtSetEaFile(
		windows.Handle(f.Fd()), &iosb, &eaBuf[0], uint32(len(eaBuf)),
	)

	if err != nil {
		return err
	}

	return nil
}

func winGetEa(f *os.File) (map[string]string, error) {
	sz, err := getEaBlockSize(f)
	if err != nil || sz == 0 {
		return nil, err
	}

	var iosb windows.IO_STATUS_BLOCK
	eaBuf := make([]byte, int(sz))

	err = windows.NtQueryEaFile(
		windows.Handle(f.Fd()),
		&iosb,
		&eaBuf[0],
		uint32(len(eaBuf)),
		false,
		nil,
		0,
		nil,
		true,
	)

	if err != nil {
		return nil, err
	}

	eas, err := decodeExtendedAttributes(eaBuf)
	if err != nil || len(eas) == 0 {
		return nil, err
	}

	m := make(map[string]string, len(eas))
	for _, ea := range eas {
		m[attrNameToKey(ea.Name)] = string(ea.Value)
	}

	return m, nil
}

func getEaBlockSize(f *os.File) (uint, error) {
	var iosb windows.IO_STATUS_BLOCK
	var rv uint32

	err := windows.NtQueryInformationFile(
		windows.Handle(f.Fd()),
		&iosb,
		(*byte)(unsafe.Pointer(&rv)),
		uint32(unsafe.Sizeof(rv)),
		windows.FileEaInformation,
	)

	if err != nil {
		return 0, err
	}

	return uint(rv), nil
}

func encodeExtendedAttributes(eas []extendedAttribute) ([]byte, error) {
	var buf bytes.Buffer
	for i := range eas {
		last := false
		if i == len(eas)-1 {
			last = true
		}

		err := writeExtendedAttributes(&buf, &eas[i], last)
		if err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func decodeExtendedAttributes(b []byte) (eas []extendedAttribute, err error) {
	for len(b) != 0 {
		ea, nb, err := parseExtendedAttributes(b)
		if err != nil {
			return nil, err
		}

		eas = append(eas, ea)
		b = nb
	}
	return
}

func writeExtendedAttributes(buf *bytes.Buffer, ea *extendedAttribute, last bool) error {
	if int(uint8(len(ea.Name))) != len(ea.Name) {
		return fmt.Errorf(
			"Extended attribute name is too long (limited to 255 bytes): name %q, value %q",
			ea.Name, string(ea.Value),
		)
	}

	if int(uint16(len(ea.Value))) != len(ea.Value) {
		return fmt.Errorf(
			"Extended attribute value is too long (limited to 65535 bytes): name %q, value %q",
			ea.Name, string(ea.Value),
		)
	}

	entrySize := uint32(fileFullEaInformationSize + len(ea.Name) + 1 + len(ea.Value))
	withPadding := (entrySize + 3) &^ 3
	nextOffset := uint32(0)
	if !last {
		nextOffset = withPadding
	}
	info := fileFullEaInformation{
		NextEntryOffset: nextOffset,
		Flags:           ea.Flags,
		NameLength:      uint8(len(ea.Name)),
		ValueLength:     uint16(len(ea.Value)),
	}

	err := binary.Write(buf, binary.LittleEndian, &info)
	if err != nil {
		return err
	}

	_, err = buf.Write([]byte(ea.Name))
	if err != nil {
		return err
	}

	err = buf.WriteByte(0)
	if err != nil {
		return err
	}

	_, err = buf.Write(ea.Value)
	if err != nil {
		return err
	}

	_, err = buf.Write([]byte{0, 0, 0}[0 : withPadding-entrySize])
	if err != nil {
		return err
	}

	return nil
}

func parseExtendedAttributes(b []byte) (ea extendedAttribute, nb []byte, err error) {
	var info fileFullEaInformation
	err = binary.Read(bytes.NewReader(b), binary.LittleEndian, &info)
	if err != nil {
		return
	}

	nameOffset := fileFullEaInformationSize
	nameLen := int(info.NameLength)
	valueOffset := nameOffset + int(info.NameLength) + 1
	valueLen := int(info.ValueLength)
	nextOffset := int(info.NextEntryOffset)
	if valueLen+valueOffset > len(b) || nextOffset < 0 || nextOffset > len(b) {
		err = fmt.Errorf("Invalid extended attribute buffer offset")
		return
	}

	ea.Name = string(b[nameOffset : nameOffset+nameLen])
	ea.Value = b[valueOffset : valueOffset+valueLen]
	ea.Flags = info.Flags
	if info.NextEntryOffset != 0 {
		nb = b[info.NextEntryOffset:]
	}
	return
}
