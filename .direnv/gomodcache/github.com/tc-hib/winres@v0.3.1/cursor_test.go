package winres

import (
	"bytes"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadCUR(t *testing.T) {
	f, err := os.Open(filepath.Join(testDataDir, "cursor.cur"))
	if err != nil {
		t.Fatal("missing test data")
	}
	defer f.Close()

	cursor, err := LoadCUR(f)
	if err != nil {
		t.Fatal(err)
	}

	cursor.order()

	expected := []cursorImage{
		{
			info: cursorInfo{
				Width:      32,
				Height:     32,
				Planes:     1,
				BitCount:   32,
				BytesInRes: 0x10A8,
			},
			hotSpot: HotSpot{14, 8},
		},
		{
			info: cursorInfo{
				Width:      256,
				Height:     256,
				Planes:     1,
				BitCount:   24,
				BytesInRes: 0x32028,
			},
			hotSpot: HotSpot{112, 64},
		},
		{
			info: cursorInfo{
				Width:      32,
				Height:     32,
				Planes:     1,
				BitCount:   8,
				BytesInRes: 0x710,
			},
			hotSpot: HotSpot{14, 8},
		},
		{
			info: cursorInfo{
				Width:      32,
				Height:     32,
				Planes:     1,
				BitCount:   4,
				BytesInRes: 0x2E8,
			},
			hotSpot: HotSpot{21, 10},
		},
		{
			info: cursorInfo{
				Width:      32,
				Height:     32,
				Planes:     1,
				BitCount:   1,
				BytesInRes: 0x130,
			},
			hotSpot: HotSpot{21, 10},
		},
	}

	for i := range expected {
		if !reflect.DeepEqual(cursor.images[i].info, expected[i].info) {
			t.Errorf("%s - image %d: expected %v got %v", t.Name(), i, expected[i].info, cursor.images[i].info)
		}
		if cursor.images[i].hotSpot != expected[i].hotSpot {
			t.Errorf("%s - hotspot %d: expected %v got %v", t.Name(), i, expected[i].hotSpot, cursor.images[i].hotSpot)
		}
	}
}

func TestLoadCUR_ErrEOF1(t *testing.T) {
	cursor, err := LoadCUR(bytes.NewReader([]byte{0, 0, 2, 0, 1}))
	if err != io.ErrUnexpectedEOF || cursor != nil {
		t.Fail()
	}
}

func TestLoadCUR_ErrEOF2(t *testing.T) {
	cursor, err := LoadCUR(bytes.NewReader([]byte{
		0, 0, 2, 0, 0xFF, 0xFF,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}))
	if err != io.ErrUnexpectedEOF || cursor != nil {
		t.Fail()
	}
}

func TestLoadCUR_ErrImageOffset(t *testing.T) {
	temp, err := os.ReadFile(filepath.Join(testDataDir, "cursor.cur"))
	if err != nil {
		t.Fatal(err)
	}

	temp[21] = 0x04
	cursor, err := LoadCUR(bytes.NewReader(temp))
	if err != io.ErrUnexpectedEOF || cursor != nil {
		t.Fail()
	}
}

func TestLoadCUR_ErrNotCUR(t *testing.T) {
	temp, err := os.ReadFile(filepath.Join(testDataDir, "cursor.cur"))
	if err != nil {
		t.Fatal(err)
	}

	temp[0] = 1
	cursor, err := LoadCUR(bytes.NewReader(temp))
	if err == nil || cursor != nil || err.Error() != errNotCUR {
		t.Fail()
	}

	temp[0] = 0
	temp[2] = 1
	cursor, err = LoadCUR(bytes.NewReader(temp))
	if err == nil || cursor != nil || err.Error() != errNotCUR {
		t.Fail()
	}
}

func TestLoadCUR_ErrNotDIB(t *testing.T) {
	temp, err := os.ReadFile(filepath.Join(testDataDir, "cursor.cur"))
	if err != nil {
		t.Fatal(err)
	}

	temp[0x56] = 41
	cursor, err := LoadCUR(bytes.NewReader(temp))
	if err == nil || cursor != nil || err.Error() != errUnknownImageFormat {
		t.Fail()
	}
}

func TestLoadCUR_ErrSeek(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testDataDir, "cursor.cur"))
	if err != nil {
		t.Fatal("missing test data")
	}

	r := &badSeeker{br: bytes.NewReader(data)}

	cursor, err := LoadCUR(r)
	if !isExpectedSeekErr(err) || cursor != nil {
		t.Fatal("expected seek error, got", err)
	}
}

func TestLoadCUR_ImageLengthLimit(t *testing.T) {
	_, err := LoadCUR(bytes.NewReader([]byte{
		0, 0, 2, 0, 1, 0, 32, 32, 0, 0, 0, 0, 0, 0,
		0x00, 0x00, 0xA0, 0x00, // image data length = 0xA00000
		0, 0, 0, 22,
	}))
	if err == nil || err.Error() == errImageLengthTooBig {
		t.Fail()
	}

	_, err = LoadCUR(bytes.NewReader([]byte{
		0, 0, 2, 0, 1, 0, 32, 32, 0, 0, 0, 0, 0, 0,
		0x01, 0x00, 0xA0, 0x00, // image data length = 0xA00001
		0, 0, 0, 22,
	}))
	if err == nil || err.Error() != errImageLengthTooBig {
		t.Fail()
	}
}

func TestLoadCUR_PNG(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testDataDir, "png.cur"))
	if err != nil {
		t.Fatal(err)
	}

	cursor, err := LoadCUR(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if cursor.images[0].info.BitCount != 32 {
		t.Fail()
	}
}

func TestCursor_SaveCUR(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testDataDir, "cursor.cur"))
	if err != nil {
		t.Fatal(err)
	}

	cursor, err := LoadCUR(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	err = cursor.SaveCUR(buf)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestCursor_SaveCUR_ErrWrite(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testDataDir, "cursor.cur"))
	if err != nil {
		t.Fatal(err)
	}

	cursor, err := LoadCUR(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	w := newBadWriter(5)
	err = cursor.SaveCUR(w)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}

	w = newBadWriter(20)
	err = cursor.SaveCUR(w)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}

	w = newBadWriter(9190)
	err = cursor.SaveCUR(w)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func TestNewCursorFromImages(t *testing.T) {
	cursor, err := NewCursorFromImages([]CursorImage{
		{
			Image:   loadImage(t, "cur-16x8.png"),
			HotSpot: HotSpot{1, 2},
		},
		{
			Image:   shiftImage(loadImage(t, "cur-16x32.png"), -10, -100),
			HotSpot: HotSpot{3, 4},
		},
		{
			Image:   shiftImage(loadImage(t, "cur-32x64.png"), 2, -1),
			HotSpot: HotSpot{5, 6},
		},
		{
			Image:   loadImage(t, "cur-64x128.png"),
			HotSpot: HotSpot{7, 8},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, curToBinary(cursor))
}

func TestNewCursorFromImages_ErrDimensions(t *testing.T) {
	_, err := NewCursorFromImages([]CursorImage{
		{
			Image:   image.NewNRGBA(image.Rectangle{}),
			HotSpot: HotSpot{0, 0},
		},
	})
	if err == nil || err.Error() != errInvalidImageDimensions {
		t.Fail()
	}
}

func TestNewCursorFromImages_ErrEncode(t *testing.T) {
	enc := bmpEncode
	defer func() { bmpEncode = enc }()
	bmpEncode = func(w io.Writer, m image.Image) error {
		return errors.New("oops")
	}

	_, err := NewCursorFromImages([]CursorImage{
		{
			Image:   loadImage(t, "cur-32x64.png"),
			HotSpot: HotSpot{5, 6},
		},
		{
			Image:   loadImage(t, "cur-64x128.png"),
			HotSpot: HotSpot{7, 8},
		},
	})
	if err == nil || err.Error() != "oops" {
		t.Fail()
	}
}

func TestNewCursorFromImages_ErrTooBig(t *testing.T) {
	_, err := NewCursorFromImages([]CursorImage{
		{
			Image: image.NewNRGBA(image.Rectangle{
				Max: image.Point{
					X: 257,
					Y: 256,
				},
			}),
			HotSpot: HotSpot{1, 2},
		},
	})
	if err == nil || err.Error() != errImageTooBig {
		t.Fail()
	}

	_, err = NewCursorFromImages([]CursorImage{
		{
			Image: image.NewNRGBA(image.Rectangle{
				Max: image.Point{
					X: 256,
					Y: 257,
				},
			}),
			HotSpot: HotSpot{1, 2},
		},
	})
	if err == nil || err.Error() != errImageTooBig {
		t.Fail()
	}

	_, err = NewCursorFromImages([]CursorImage{
		{
			Image: image.NewNRGBA(image.Rectangle{
				Max: image.Point{
					X: 256,
					Y: 256,
				},
			}),
			HotSpot: HotSpot{1, 2},
		},
	})
	if err != nil {
		t.Fail()
	}
}

func TestResourceSet_SetCursor(t *testing.T) {
	rs := ResourceSet{}

	id := Name("CURSOR")
	cursor := loadCURFile(t, "cursor.cur")
	err := rs.SetCursor(id, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if int(rs.lastCursorID) != len(cursor.images) {
		t.Fail()
	}

	checkCursorResource(t, &rs, id, 0, cursor)
}

func TestResourceSet_SetCursor_IDOverflow(t *testing.T) {
	rs := ResourceSet{}
	rs.lastCursorID = 0xFFFD

	cursor := loadCURFile(t, "cursor.cur")
	err := rs.SetCursor(Name("CURSOR"), cursor)
	if err == nil || err.Error() != errZeroID {
		t.Fail()
	}
}

func TestResourceSet_SetCursorTranslation(t *testing.T) {
	rs := ResourceSet{}

	cursor1 := loadCURFile(t, "en.cur")
	cursor2 := loadCURFile(t, "fr.cur")
	cursor3, err := NewCursorFromImages([]CursorImage{
		{
			Image:   loadImage(t, "cur-16x8.png"),
			HotSpot: HotSpot{1, 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = rs.SetCursorTranslation(Name("LOCALCUR"), 0x409, cursor1)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.SetCursorTranslation(Name("ANOTHERCUR"), 0x409, cursor3)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.SetCursorTranslation(Name("LOCALCUR"), 0x40C, cursor2)
	if err != nil {
		t.Fatal(err)
	}

	checkCursorResource(t, &rs, Name("LOCALCUR"), 0x409, cursor1)
	checkCursorResource(t, &rs, Name("LOCALCUR"), 0x40C, cursor2)
	checkCursorResource(t, &rs, Name("ANOTHERCUR"), 0x409, cursor3)
}

func TestResourceSet_GetCursorTranslation_Err(t *testing.T) {
	rs := ResourceSet{}

	bin0 := []byte{0, 0, 2, 0, 1}
	bin1 := []byte{0, 0, 1, 0, 1, 0, 32, 0, 32, 0, 1, 0, 32, 0, 42, 0, 0, 0, 1, 0}
	bin2 := []byte{0, 0, 2, 0, 1, 0, 32, 0, 32, 0, 1, 0, 32, 0, 42, 0, 0, 0, 1}
	bin3 := []byte{0, 0, 2, 0, 1, 0, 32, 0, 32, 0, 1, 0, 32, 0, 42, 0, 0, 0, 1, 0}

	err := rs.Set(RT_GROUP_CURSOR, Name("LOCALCUR"), 0, bin0)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.Set(RT_GROUP_CURSOR, Name("LOCALCUR"), 0x401, bin1)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.Set(RT_GROUP_CURSOR, Name("LOCALCUR"), 0x402, bin2)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.Set(RT_GROUP_CURSOR, Name("LOCALCUR"), 0x403, bin3)
	if err != nil {
		t.Fatal(err)
	}

	var cursor *Cursor

	cursor, err = rs.GetCursor(ID(0))
	if err == nil || cursor != nil || err.Error() != errGroupNotFound {
		t.Fail()
	}
	cursor, err = rs.GetCursorTranslation(Name("LOCALCU"), 0x401)
	if err == nil || cursor != nil || err.Error() != errGroupNotFound {
		t.Fail()
	}
	cursor, err = rs.GetCursorTranslation(Name("LOCALCUR"), 0x409)
	if err == nil || cursor != nil || err.Error() != errGroupNotFound {
		t.Fail()
	}

	cursor, err = rs.GetCursorTranslation(Name("LOCALCUR"), 0)
	if err == nil || cursor != nil || err.Error() != errInvalidGroup {
		t.Fail()
	}
	cursor, err = rs.GetCursorTranslation(Name("LOCALCUR"), 0x401)
	if err == nil || cursor != nil || err.Error() != errInvalidGroup {
		t.Fail()
	}
	cursor, err = rs.GetCursorTranslation(Name("LOCALCUR"), 0x402)
	if err == nil || cursor != nil || err.Error() != errInvalidGroup {
		t.Fail()
	}
	cursor, err = rs.GetCursorTranslation(Name("LOCALCUR"), 0x403)
	if err == nil || cursor != nil || err.Error() != errCursorMissing {
		t.Fail()
	}
}

func checkCursorResource(t *testing.T, rs *ResourceSet, ident Identifier, langID uint16, source *Cursor) {
	cursor, err := rs.GetCursorTranslation(ident, langID)
	if err != nil {
		t.Fatal(err)
	}
	buf1, buf2 := &bytes.Buffer{}, &bytes.Buffer{}
	err = source.SaveCUR(buf1)
	if err != nil {
		t.Fatal(err)
	}
	err = cursor.SaveCUR(buf2)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fail()
	}
}

func curToBinary(cursor *Cursor) []byte {
	buf := &bytes.Buffer{}
	cursor.SaveCUR(buf)
	return buf.Bytes()
}
