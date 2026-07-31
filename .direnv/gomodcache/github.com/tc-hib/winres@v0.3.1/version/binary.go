package version

// In this file are functions to convert an Info structure to/from its binary representation.
// https://docs.microsoft.com/en-us/windows/win32/menurc/vs-versioninfo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"unicode/utf16"
)

const codePageUTF16LE = 1200

type nodeHeader struct {
	Length      uint16
	ValueLength uint16
	Type        uint16
}

const sizeOfNodeHeader = 6

// https://docs.microsoft.com/en-us/windows/win32/api/verrsrc/ns-verrsrc-vs_fixedfileinfo
type _VS_FIXEDFILEINFO struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
	FileFlagsMask    uint32
	FileFlags        uint32
	FileOS           uint32
	FileType         uint32
	FileSubtype      uint32
	FileDateMS       uint32
	FileDateLS       uint32
}

const sizeOfFixedFileInfo = 52

const fixedFileInfoSignature = 0xFEEF04BD
const fixedFileInfoVersion = 0x10000

const (
	_VS_FF_DEBUG        = 0x01
	_VS_FF_PRERELEASE   = 0x02
	_VS_FF_PATCHED      = 0x04
	_VS_FF_PRIVATEBUILD = 0x08
	_VS_FF_SPECIALBUILD = 0x20
	_VS_FF_MASK         = 0x3F
)

const (
	_VOS_NT_WINDOWS32 = 0x040004
)

const (
	_VFT_UNKNOWN = 0
	_VFT_APP     = 1
	_VFT_DLL     = 2
)

const (
	vsVersionInfo  = "VS_VERSION_INFO"
	stringFileInfo = "StringFileInfo"
	varFileInfo    = "VarFileInfo"
	translation    = "Translation"
)

func (vi *Info) bytes() []byte {
	buf := &bytes.Buffer{}
	writeStructAligned(buf, vi.fixedFileInfo())
	sfi := stringFileInfoBytes(&vi.lt)
	writeAligned(buf, sfi)
	vfi := varFileInfoBytes(&vi.lt)
	writeAligned(buf, vfi)
	return nodeBytes(false, vsVersionInfo, buf.Bytes(), sizeOfFixedFileInfo)
}

func (vi *Info) fixedFileInfo() _VS_FIXEDFILEINFO {
	ffi := _VS_FIXEDFILEINFO{
		Signature:        fixedFileInfoSignature,
		StrucVersion:     fixedFileInfoVersion,
		FileVersionMS:    uint32(vi.FileVersion[0])<<16 | uint32(vi.FileVersion[1]),
		FileVersionLS:    uint32(vi.FileVersion[2])<<16 | uint32(vi.FileVersion[3]),
		ProductVersionMS: uint32(vi.ProductVersion[0])<<16 | uint32(vi.ProductVersion[1]),
		ProductVersionLS: uint32(vi.ProductVersion[2])<<16 | uint32(vi.ProductVersion[3]),
		FileFlagsMask:    _VS_FF_MASK,
		FileOS:           _VOS_NT_WINDOWS32,
	}
	if vi.Flags.Debug {
		ffi.FileFlags |= _VS_FF_DEBUG
	}
	if vi.Flags.Patched {
		ffi.FileFlags |= _VS_FF_PATCHED
	}
	if vi.Flags.Prerelease {
		ffi.FileFlags |= _VS_FF_PRERELEASE
	}
	if vi.Flags.PrivateBuild {
		ffi.FileFlags |= _VS_FF_PRIVATEBUILD
	}
	if vi.Flags.SpecialBuild {
		ffi.FileFlags |= _VS_FF_SPECIALBUILD
	}
	switch vi.Type {
	case App:
		ffi.FileType = _VFT_APP
	case DLL:
		ffi.FileType = _VFT_DLL
	default:
		ffi.FileType = _VFT_UNKNOWN
	}
	ffi.FileDateMS, ffi.FileDateLS = timeToTimestamp(vi.Timestamp)
	return ffi
}

func stringFileInfoBytes(lt *LangTable) []byte {
	buf := &bytes.Buffer{}
	for _, langID := range lt.sortedKeys() {
		b := stringTableBytes(langID, (*lt)[langID])
		writeAligned(buf, b)
	}
	return nodeBytes(true, stringFileInfo, buf.Bytes(), 0)
}

func stringTableBytes(langID uint16, strings *StringTable) []byte {
	buf := &bytes.Buffer{}
	for _, k := range strings.sortedKeys() {
		b := stringBytes(k, (*strings)[k])
		writeAligned(buf, b)
	}
	return nodeBytes(true, fmt.Sprintf("%08x", uint32(langID)<<16|codePageUTF16LE), buf.Bytes(), 0)
}

func stringBytes(key string, value string) []byte {
	wValue := utf16.Encode([]rune(value + "\x00"))
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, wValue)
	return nodeBytes(true, key, buf.Bytes(), len(wValue))
}

func varFileInfoBytes(lt *LangTable) []byte {
	buf := &bytes.Buffer{}
	var langs []uint32
	for _, langID := range lt.sortedKeys() {
		langs = append(langs, codePageUTF16LE<<16|uint32(langID))
	}
	b := varBytes(langs)
	writeAligned(buf, b)
	return nodeBytes(true, varFileInfo, buf.Bytes(), 0)
}

func varBytes(langs []uint32) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, langs)
	return nodeBytes(false, translation, buf.Bytes(), buf.Len())
}

func nodeBytes(text bool, key string, value []byte, valueLength int) []byte {
	wKey := utf16.Encode([]rune(key + "\x00"))
	hdr := nodeHeader{}
	hdr.Length = uint16(sizeOfNodeHeader + len(wKey)*2 + len(value))
	if len(wKey)&1 == 0 {
		hdr.Length += 2
	}
	hdr.ValueLength = uint16(valueLength)
	if text {
		hdr.Type = 1
	}

	buf := bytes.NewBuffer(make([]byte, 0, hdr.Length))
	binary.Write(buf, binary.LittleEndian, hdr)
	binary.Write(buf, binary.LittleEndian, wKey)
	writeAligned(buf, value)
	return buf.Bytes()
}

func align(s int) int {
	return (s + 3) &^ 3
}

func writeAligned(buffer *bytes.Buffer, data []byte) {
	var pad [4]byte
	s := buffer.Len()
	p := align(s) - s
	buffer.Write(pad[:p])
	buffer.Write(data)
}

func writeStructAligned(buffer *bytes.Buffer, data interface{}) {
	var pad [4]byte
	s := buffer.Len()
	p := align(s) - s
	buffer.Write(pad[:p])
	binary.Write(buffer, binary.LittleEndian, data)
}

func (st *StringTable) sortedKeys() []string {
	keys := make([]string, 0, len(*st))
	for k := range *st {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (lt *LangTable) sortedKeys() []uint16 {
	keys := make([]int, 0, len(*lt))
	for k := range *lt {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	keysU16 := make([]uint16, len(*lt))
	for i, v := range keys {
		keysU16[i] = uint16(v)
	}
	return keysU16
}

func fromBytes(data []byte) (*Info, error) {
	vi := &Info{}
	_, err := vi.readNode(nil, data)
	if err != nil {
		return nil, err
	}
	return vi, nil
}

// Returns the number of bytes read
func (vi *Info) readNode(parent interface{}, data []byte) (int, error) {
	n, pos, err := readNodeHeader(data)
	if err != nil {
		return 0, err
	}
	if n.Length < sizeOfNodeHeader || int(n.Length) > len(data) {
		return 0, errors.New(errInvalidLength)
	}
	data = data[:n.Length]

	key, err := readString(data, &pos)
	if err != nil {
		return 0, err
	}

	if n.ValueLength > 0 && align(pos)+int(n.ValueLength) > len(data) {
		return 0, io.ErrUnexpectedEOF
	}

	switch true {
	case parent == nil && key == vsVersionInfo:
		if n.ValueLength >= sizeOfFixedFileInfo {
			err := vi.readFixedStruct(data[align(pos):])
			if err != nil {
				return 0, err
			}
		}
		pos += int(n.ValueLength)
		err := vi.readChildren(key, data, &pos)
		if err != nil {
			return 0, err
		}

	case parent == vsVersionInfo && key == varFileInfo:
		// skip these nodes
		break

	case parent == vsVersionInfo && key == stringFileInfo:
		err := vi.readChildren(key, data, &pos)
		if err != nil {
			return 0, err
		}

	case parent == stringFileInfo:
		var langID, codePage uint16
		_, err := fmt.Sscanf(key, "%04x%04x", &langID, &codePage)
		if err != nil {
			return 0, errors.New(errInvalidLangID)
		}
		if codePage != codePageUTF16LE {
			return 0, errors.New(errUnhandledCodePage)
		}
		err = vi.readChildren(langID, data, &pos)
		if err != nil {
			return 0, err
		}

	default:
		// Under a language ID, this is a key/value pair
		if id, ok := parent.(uint16); ok {
			pos = align(pos)
			value, err := readStringWithLength(data, &pos, int(n.ValueLength))
			if err != nil {
				return 0, err
			}
			vi.Set(id, key, value)
		}
	}

	return len(data), nil
}

func (vi *Info) readChildren(parent interface{}, data []byte, pos *int) error {
	for align(*pos) < len(data) {
		*pos = align(*pos)
		offset, err := vi.readNode(parent, data[*pos:])
		if err != nil {
			return err
		}
		*pos += offset
	}
	return nil
}

func (vi *Info) readFixedStruct(data []byte) error {
	fixed := _VS_FIXEDFILEINFO{}
	// No error handling because the caller guaranteed "data" is long enough
	_ = binaryRead(bytes.NewReader(data), &fixed)
	if fixed.Signature != fixedFileInfoSignature {
		return errors.New(errInvalidSignature)
	}
	switch fixed.FileType {
	case _VFT_APP:
		vi.Type = App
	case _VFT_DLL:
		vi.Type = DLL
	default:
		vi.Type = Unknown
	}
	flags := fixed.FileFlags & _VS_FF_MASK
	vi.Flags.Debug = flags&_VS_FF_DEBUG != 0
	vi.Flags.Prerelease = flags&_VS_FF_PRERELEASE != 0
	vi.Flags.Patched = flags&_VS_FF_PATCHED != 0
	vi.Flags.PrivateBuild = flags&_VS_FF_PRIVATEBUILD != 0
	vi.Flags.SpecialBuild = flags&_VS_FF_SPECIALBUILD != 0
	vi.FileVersion[0] = uint16(fixed.FileVersionMS >> 16)
	vi.FileVersion[1] = uint16(fixed.FileVersionMS)
	vi.FileVersion[2] = uint16(fixed.FileVersionLS >> 16)
	vi.FileVersion[3] = uint16(fixed.FileVersionLS)
	vi.ProductVersion[0] = uint16(fixed.ProductVersionMS >> 16)
	vi.ProductVersion[1] = uint16(fixed.ProductVersionMS)
	vi.ProductVersion[2] = uint16(fixed.ProductVersionLS >> 16)
	vi.ProductVersion[3] = uint16(fixed.ProductVersionLS)
	vi.Timestamp = timestampToTime(fixed.FileDateMS, fixed.FileDateLS)
	return nil
}

func readString(data []byte, pos *int) (string, error) {
	data = data[*pos:]

	var length int

	for i := 1; i < len(data); i += 2 {
		if data[i-1] == 0 && data[i] == 0 {
			length = (i - 1) / 2
			break
		}
	}
	if length == 0 {
		*pos = len(data)
		return "", io.ErrUnexpectedEOF
	}

	wKey := make([]uint16, length)
	for i := 0; i < length; i++ {
		wKey[i] = uint16(data[i*2+1])<<8 | uint16(data[i*2])
	}
	*pos += (length + 1) * 2
	return string(utf16.Decode(wKey)), nil
}

func readStringWithLength(data []byte, pos *int, length int) (string, error) {
	data = data[*pos:]

	if length == 0 {
		return "", nil
	}
	if length < 0 || length > len(data)/2 {
		return "", errors.New(errInvalidStringLength)
	}

	length--
	wString := make([]uint16, length)
	for i := 0; i < length; i++ {
		wString[i] = uint16(data[i*2+1])<<8 | uint16(data[i*2])
	}
	*pos += (length + 1) * 2

	return string(utf16.Decode(wString)), nil
}

func readNodeHeader(data []byte) (nodeHeader, int, error) {
	if len(data) < sizeOfNodeHeader {
		return nodeHeader{}, 0, io.ErrUnexpectedEOF
	}
	n := nodeHeader{
		Length:      uint16(data[1])<<8 | uint16(data[0]),
		ValueLength: uint16(data[3])<<8 | uint16(data[2]),
		Type:        uint16(data[5])<<8 | uint16(data[4]),
	}
	return n, sizeOfNodeHeader, nil
}

// binaryRead is like binary.Read, except it always returns io.ErrUnexpectedEOF instead of io.EOF.
// Furthermore, it always uses binary.LittleEndian.
func binaryRead(r io.Reader, v interface{}) error {
	err := binary.Read(r, binary.LittleEndian, v)
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}
