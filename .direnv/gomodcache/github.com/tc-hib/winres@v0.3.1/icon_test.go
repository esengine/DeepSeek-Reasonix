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

func TestLoadICO(t *testing.T) {
	f, err := os.Open(filepath.Join(testDataDir, "icon.ico"))
	if err != nil {
		t.Fatal("missing test data")
	}
	defer f.Close()

	cursor, err := LoadICO(f)
	if err != nil {
		t.Fatal(err)
	}

	cursor.order()

	expected := []iconImage{
		{
			info: iconInfo{
				Width:      0,
				Height:     0,
				ColorCount: 0,
				Planes:     1,
				BitCount:   32,
				BytesInRes: 0x7D30,
			},
		},
		{
			info: iconInfo{
				Width:      48,
				Height:     48,
				ColorCount: 0,
				Planes:     1,
				BitCount:   32,
				BytesInRes: 0x25A8,
			},
		},
		{
			info: iconInfo{
				Width:      32,
				Height:     32,
				ColorCount: 0,
				Planes:     1,
				BitCount:   32,
				BytesInRes: 0x10A8,
			},
		},
		{
			info: iconInfo{
				Width:      16,
				Height:     16,
				ColorCount: 0,
				Planes:     1,
				BitCount:   32,
				BytesInRes: 0x468,
			},
		},
		{
			info: iconInfo{
				Width:      0,
				Height:     0,
				ColorCount: 0,
				Planes:     1,
				BitCount:   8,
				BytesInRes: 0x5824,
			},
		},
		{
			info: iconInfo{
				Width:      48,
				Height:     48,
				ColorCount: 0,
				Planes:     1,
				BitCount:   8,
				BytesInRes: 0xEA8,
			},
		},
		{
			info: iconInfo{
				Width:      32,
				Height:     32,
				ColorCount: 0,
				Planes:     1,
				BitCount:   8,
				BytesInRes: 0x8A8,
			},
		},
		{
			info: iconInfo{
				Width:      16,
				Height:     16,
				ColorCount: 0,
				Planes:     1,
				BitCount:   8,
				BytesInRes: 0x568,
			},
		},
		{
			info: iconInfo{
				Width:      0,
				Height:     0,
				ColorCount: 16,
				Planes:     1,
				BitCount:   4,
				BytesInRes: 0x55FC,
			},
		},
		{
			info: iconInfo{
				Width:      48,
				Height:     48,
				ColorCount: 16,
				Planes:     1,
				BitCount:   4,
				BytesInRes: 0x668,
			},
		},
		{
			info: iconInfo{
				Width:      32,
				Height:     32,
				ColorCount: 16,
				Planes:     1,
				BitCount:   4,
				BytesInRes: 0x2E8,
			},
		},
		{
			info: iconInfo{
				Width:      16,
				Height:     16,
				ColorCount: 16,
				Planes:     1,
				BitCount:   4,
				BytesInRes: 0x128,
			},
		},
	}

	for i := range expected {
		if !reflect.DeepEqual(cursor.images[i].info, expected[i].info) {
			t.Errorf("%s - image %d: expected %v got %v", t.Name(), i, expected[i].info, cursor.images[i].info)
		}
	}
}

func TestLoadICO_ErrEOF1(t *testing.T) {
	cursor, err := LoadICO(bytes.NewReader([]byte{0, 0, 1, 0, 1}))
	if err != io.ErrUnexpectedEOF || cursor != nil {
		t.Fail()
	}
}

func TestLoadICO_ErrEOF2(t *testing.T) {
	icon, err := LoadICO(bytes.NewReader([]byte{
		0, 0, 1, 0, 0xFF, 0xFF,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}))
	if err != io.ErrUnexpectedEOF || icon != nil {
		t.Fail()
	}
}

func TestLoadICO_ErrImageOffset(t *testing.T) {
	temp, err := os.ReadFile(filepath.Join(testDataDir, "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}

	temp[21] = 0x01
	icon, err := LoadICO(bytes.NewReader(temp))
	if err != io.ErrUnexpectedEOF || icon != nil {
		t.Fail()
	}
}

func TestLoadCUR_ErrNotICO(t *testing.T) {
	temp, err := os.ReadFile(filepath.Join(testDataDir, "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}

	temp[0] = 1
	icon, err := LoadICO(bytes.NewReader(temp))
	if err == nil || icon != nil || err.Error() != errNotICO {
		t.Fail()
	}

	temp[0] = 0
	temp[2] = 2
	icon, err = LoadICO(bytes.NewReader(temp))
	if err == nil || icon != nil || err.Error() != errNotICO {
		t.Fail()
	}
}

func TestLoadICO_ErrSeek(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testDataDir, "icon.ico"))
	if err != nil {
		t.Fatal("missing test data")
	}

	r := &badSeeker{br: bytes.NewReader(data)}

	icon, err := LoadICO(r)
	if !isExpectedSeekErr(err) || icon != nil {
		t.Fatal("expected seek error, got", err)
	}
}

func TestLoadICO_ImageLengthLimit(t *testing.T) {
	_, err := LoadICO(bytes.NewReader([]byte{
		0, 0, 1, 0, 1, 0, 32, 32, 0, 0, 1, 0, 32, 0,
		0x00, 0x00, 0xA0, 0x00, // image data length = 0xA00000
		0, 0, 0, 22,
	}))
	if err == nil || err.Error() == errImageLengthTooBig {
		t.Fail()
	}

	_, err = LoadICO(bytes.NewReader([]byte{
		0, 0, 1, 0, 1, 0, 32, 32, 0, 0, 0, 0, 0, 0,
		0x01, 0x00, 0xA0, 0x00, // image data length = 0xA00001
		0, 0, 0, 22,
	}))
	if err == nil || err.Error() != errImageLengthTooBig {
		t.Fail()
	}
}

func TestIcon_SaveICO(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testDataDir, "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}

	icon, err := LoadICO(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	err = icon.SaveICO(buf)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestIcon_SaveICO_ErrWrite(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testDataDir, "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}

	icon, err := LoadICO(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	w := newBadWriter(5)
	err = icon.SaveICO(w)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}

	w = newBadWriter(20)
	err = icon.SaveICO(w)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}

	w = newBadWriter(41885)
	err = icon.SaveICO(w)
	if !isExpectedWriteErr(err) {
		t.Fatal("expected write error, got", err)
	}
}

func TestNewIconFromImages(t *testing.T) {
	icon, err := NewIconFromImages([]image.Image{
		shiftImage(loadImage(t, "cur-16x8.png"), 30, 0),
		loadImage(t, "cur-16x32.png"),
		loadImage(t, "cur-32x64.png"),
		loadImage(t, "cur-64x128.png"),
		shiftImage(loadImage(t, "cur-16x8.png"), 1, -100),
	})
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, icoToBinary(icon))
}

func TestNewIconFromImages_ErrDimensions(t *testing.T) {
	_, err := NewIconFromImages([]image.Image{image.NewNRGBA(image.Rectangle{})})
	if err == nil || err.Error() != errInvalidImageDimensions {
		t.Fail()
	}
}

func TestNewIconFromImages_ErrEncode(t *testing.T) {
	enc := pngEncode
	defer func() { pngEncode = enc }()
	pngEncode = func(w io.Writer, m image.Image) error {
		return errors.New("oops")
	}

	_, err := NewIconFromImages([]image.Image{
		loadImage(t, "cur-32x64.png"),
		loadImage(t, "cur-64x128.png"),
	})
	if err == nil || err.Error() != "oops" {
		t.Fail()
	}
}

func TestNewIconFromImages_ErrTooBig(t *testing.T) {
	_, err := NewIconFromImages([]image.Image{
		image.NewNRGBA(image.Rectangle{
			Max: image.Point{
				X: 257,
				Y: 256,
			},
		}),
	})
	if err == nil || err.Error() != errImageTooBig {
		t.Fail()
	}

	_, err = NewIconFromImages([]image.Image{
		image.NewNRGBA(image.Rectangle{
			Max: image.Point{
				X: 256,
				Y: 257,
			},
		}),
	})
	if err == nil || err.Error() != errImageTooBig {
		t.Fail()
	}

	_, err = NewIconFromImages([]image.Image{
		image.NewNRGBA(image.Rectangle{
			Max: image.Point{
				X: 256,
				Y: 256,
			},
		}),
	})
	if err != nil {
		t.Fail()
	}
}

func TestNewIconFromResizedImage(t *testing.T) {
	icon, err := NewIconFromResizedImage(shiftImage(loadImage(t, "img.png"), -100, -10), nil)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, icoToBinary(icon))
}

func TestNewIconFromResizedImage_Ratio1(t *testing.T) {
	icon, err := NewIconFromResizedImage(shiftImage(loadImage(t, "cur-32x64.png"), 5, -3), []int{32, 64})
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, icoToBinary(icon))
}

func TestNewIconFromResizedImage_Ratio2(t *testing.T) {
	icon, err := NewIconFromResizedImage(shiftImage(loadImage(t, "cur-16x8.png"), -2, 8), []int{8, 16})
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, icoToBinary(icon))
}

func TestNewIconFromResizedImage_InvalidSize(t *testing.T) {
	icon, err := NewIconFromResizedImage(shiftImage(loadImage(t, "img.png"), 100, 100), []int{16, 257})
	if err == nil || icon != nil || err.Error() != errImageTooBig {
		t.Fail()
	}
}

func TestNewIconFromResizedImage_TooManySizes(t *testing.T) {
	s := make([]int, 30)
	for i := range s {
		s[i] = i + 1
	}

	_, err := NewIconFromResizedImage(shiftImage(loadImage(t, "img.png"), 100, 100), s)
	if err != nil {
		t.Fail()
	}

	s = append(s, len(s)+1)

	icon, err := NewIconFromResizedImage(shiftImage(loadImage(t, "img.png"), 100, 100), s)
	if err == nil || icon != nil || err.Error() != errTooManyIconSizes {
		t.Fail()
	}
}

func TestResourceSet_SetIcon(t *testing.T) {
	rs := ResourceSet{}

	id := Name("ICON")
	icon := loadICOFile(t, "icon.ico")
	err := rs.SetIcon(id, icon)
	if err != nil {
		t.Fatal(err)
	}
	if int(rs.lastIconID) != len(icon.images) {
		t.Fail()
	}

	checkIconResource(t, &rs, id, 0, icon)
}

func TestResourceSet_SetIcon_IDOverflow(t *testing.T) {
	rs := ResourceSet{}
	rs.lastIconID = 0xFFF4

	icon := loadICOFile(t, "icon.ico")
	err := rs.SetIcon(Name("ICON"), icon)
	if err == nil || err.Error() != errZeroID {
		t.Fail()
	}
}

func TestResourceSet_SetIconTranslation(t *testing.T) {
	rs := ResourceSet{}

	icon1 := loadICOFile(t, "en.ico")
	icon2 := loadICOFile(t, "fr.ico")
	icon3, err := NewIconFromImages([]image.Image{loadImage(t, "cur-16x8.png")})
	if err != nil {
		t.Fatal(err)
	}

	err = rs.SetIconTranslation(Name("LOCALICO"), 0x409, icon1)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.SetIconTranslation(Name("ANOTHERICO"), 0x409, icon3)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.SetIconTranslation(Name("LOCALICO"), 0x40C, icon2)
	if err != nil {
		t.Fatal(err)
	}

	checkIconResource(t, &rs, Name("LOCALICO"), 0x409, icon1)
	checkIconResource(t, &rs, Name("LOCALICO"), 0x40C, icon2)
	checkIconResource(t, &rs, Name("ANOTHERICO"), 0x409, icon3)
}

func TestResourceSet_GetIconTranslation_Err(t *testing.T) {
	rs := ResourceSet{}

	bin0 := []byte{0, 0, 1, 0, 1}
	bin1 := []byte{0, 0, 2, 0, 1, 0, 32, 32, 0, 0, 1, 0, 32, 0, 42, 0, 0, 0, 1, 0}
	bin2 := []byte{0, 0, 1, 0, 1, 0, 32, 32, 0, 0, 1, 0, 32, 0, 42, 0, 0, 0, 1}
	bin3 := []byte{0, 0, 1, 0, 1, 0, 32, 32, 0, 0, 1, 0, 32, 0, 42, 0, 0, 0, 1, 0}

	err := rs.Set(RT_GROUP_ICON, Name("LOCALICO"), 0, bin0)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.Set(RT_GROUP_ICON, Name("LOCALICO"), 0x401, bin1)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.Set(RT_GROUP_ICON, Name("LOCALICO"), 0x402, bin2)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.Set(RT_GROUP_ICON, Name("LOCALICO"), 0x403, bin3)
	if err != nil {
		t.Fatal(err)
	}

	var icon *Icon

	icon, err = rs.GetIcon(ID(0))
	if err == nil || icon != nil || err.Error() != errGroupNotFound {
		t.Fail()
	}
	icon, err = rs.GetIconTranslation(Name("LOCALIC"), 0x401)
	if err == nil || icon != nil || err.Error() != errGroupNotFound {
		t.Fail()
	}
	icon, err = rs.GetIconTranslation(Name("LOCALICO"), 0x409)
	if err == nil || icon != nil || err.Error() != errGroupNotFound {
		t.Fail()
	}

	icon, err = rs.GetIconTranslation(Name("LOCALICO"), 0)
	if err == nil || icon != nil || err.Error() != errInvalidGroup {
		t.Fail()
	}
	icon, err = rs.GetIconTranslation(Name("LOCALICO"), 0x401)
	if err == nil || icon != nil || err.Error() != errInvalidGroup {
		t.Fail()
	}
	icon, err = rs.GetIconTranslation(Name("LOCALICO"), 0x402)
	if err == nil || icon != nil || err.Error() != errInvalidGroup {
		t.Fail()
	}
	icon, err = rs.GetIconTranslation(Name("LOCALICO"), 0x403)
	if err == nil || icon != nil || err.Error() != errIconMissing {
		t.Fail()
	}
}

func checkIconResource(t *testing.T, rs *ResourceSet, ident Identifier, langID uint16, source *Icon) {
	icon, err := rs.GetIconTranslation(ident, langID)
	if err != nil {
		t.Fatal(err)
	}
	buf1, buf2 := &bytes.Buffer{}, &bytes.Buffer{}
	err = source.SaveICO(buf1)
	if err != nil {
		t.Fatal(err)
	}
	err = icon.SaveICO(buf2)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fail()
	}
}

func icoToBinary(icon *Icon) []byte {
	buf := &bytes.Buffer{}
	icon.SaveICO(buf)
	return buf.Bytes()
}
