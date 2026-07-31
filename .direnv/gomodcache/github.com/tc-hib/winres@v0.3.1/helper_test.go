package winres

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const testDataDir = "testdata"

func checkResourceSet(t *testing.T, rs *ResourceSet, arch Arch) {
	buf := &bytes.Buffer{}
	if err := rs.WriteObject(buf, arch); err != nil {
		t.Fatal(err)
	}
	checkBinary(t, buf.Bytes())
}

func golden(t *testing.T) string {
	return filepath.Join(testDataDir, t.Name()+".golden")
}

func checkBinary(t *testing.T, data []byte) {
	refFile := golden(t)
	ref, _ := os.ReadFile(refFile)

	if !bytes.Equal(ref, data) {
		t.Error(t.Name() + " output is different")
		bugFile := refFile[:len(refFile)-7] + ".bug"
		err := os.WriteFile(bugFile, data, 0666)
		if err != nil {
			t.Error(err)
			return
		}
		t.Log("dumped output to", bugFile)
	}
}

func loadBinary(t *testing.T, filename string) []byte {
	data, err := os.ReadFile(filepath.Join(testDataDir, filename))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func loadPNGFileAsIcon(t *testing.T, name string, sizes []int) *Icon {
	f, err := os.Open(filepath.Join(testDataDir, name))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	icon, err := NewIconFromResizedImage(img, sizes)
	if err != nil {
		t.Fatal(err)
	}
	return icon
}

func loadPNGFileAsCursor(t *testing.T, name string, spotX, spotY uint16) *Cursor {
	f, err := os.Open(filepath.Join(testDataDir, name))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := NewCursorFromImages([]CursorImage{{img, HotSpot{spotX, spotY}}})
	if err != nil {
		t.Fatal(err)
	}
	return cursor
}

func loadICOFile(t *testing.T, name string) *Icon {
	f, err := os.Open(filepath.Join(testDataDir, name))
	if err != nil {
		t.Fatal(err)
	}
	icon, err := LoadICO(f)
	if err != nil {
		t.Fatal(err)
	}
	return icon
}

func loadCURFile(t *testing.T, name string) *Cursor {
	f, err := os.Open(filepath.Join(testDataDir, name))
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := LoadCUR(f)
	if err != nil {
		t.Fatal(err)
	}
	return cursor
}

func loadImage(t *testing.T, name string) image.Image {
	f, err := os.Open(filepath.Join(testDataDir, name))
	if err != nil {
		t.Fatal(err)
	}

	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}

	return img
}

func shiftImage(img image.Image, x, y int) image.Image {
	shifted := image.NewNRGBA(image.Rectangle{
		Min: image.Point{
			X: img.Bounds().Min.X + x,
			Y: img.Bounds().Min.Y + y,
		},
		Max: image.Point{
			X: img.Bounds().Max.X + x,
			Y: img.Bounds().Max.Y + y,
		},
	})

	for srcY := img.Bounds().Min.Y; srcY < img.Bounds().Max.Y; srcY++ {
		for srcX := img.Bounds().Min.X; srcX < img.Bounds().Max.X; srcX++ {
			shifted.Set(srcX+x, srcY+y, img.At(srcX, srcY))
		}
	}

	return shifted
}

type badReader struct {
	br       *bytes.Reader
	errPos   int64
	returned bool
}

type badSeeker struct {
	br       *bytes.Reader
	errIter  int
	returned bool
}

type badWriter struct {
	badLen   int
	returned bool
}

func newBadWriter(badLen int) *badWriter {
	return &badWriter{badLen: badLen}
}

const (
	errRead    = "expected read error"
	errReadOn  = "reading on after error"
	errSeek    = "expected seek error"
	errSeekOn  = "seeking on after error"
	errWrite   = "expected write error"
	errWriteOn = "writing on after error"
)

func (r *badReader) Read(b []byte) (int, error) {
	if r.returned {
		return 0, errors.New(errReadOn)
	}
	p, _ := r.br.Seek(0, io.SeekCurrent)
	if p <= r.errPos && r.errPos < p+int64(len(b)) {
		n, _ := r.br.Read(b[:r.errPos-p])
		r.returned = true
		return n, errors.New(errRead)
	}
	return r.br.Read(b)
}

func (r *badReader) Seek(offset int64, whence int) (int64, error) {
	if r.returned {
		return 0, errors.New(errSeekOn)
	}
	return r.br.Seek(offset, whence)
}

func isExpectedReadErr(err error) bool {
	return err != nil && err.Error() == errRead
}

func (s *badSeeker) Read(b []byte) (int, error) {
	if s.returned {
		return 0, errors.New(errReadOn)
	}
	return s.br.Read(b)
}

func (s *badSeeker) Seek(offset int64, whence int) (int64, error) {
	if s.returned {
		return 0, errors.New(errSeekOn)
	}
	if s.errIter <= 0 {
		s.returned = true
		return 0, errors.New(errSeek)
	}
	s.errIter--
	return s.br.Seek(offset, whence)
}

func isExpectedSeekErr(err error) bool {
	return err != nil && err.Error() == errSeek
}

func (w *badWriter) Write(b []byte) (n int, err error) {
	if w.returned {
		return 0, errors.New(errWriteOn)
	}
	w.badLen -= len(b)
	if w.badLen <= 0 {
		w.returned = true
		return 0, errors.New(errWrite)
	}
	return len(b), nil
}

func isExpectedWriteErr(err error) bool {
	return err != nil && err.Error() == errWrite
}
