package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"sort"

	"golang.org/x/image/bmp"
)

// Cursor describes a mouse cursor.
//
// This structure must only be created by constructors:
// NewCursorFromImages, LoadCUR
type Cursor struct {
	images []cursorImage
}

// CursorImage defines an image to import into a cursor.
//
// It is an Image with hot spot coordinates.
type CursorImage struct {
	Image   image.Image
	HotSpot HotSpot
}

// HotSpot is the coordinates of a cursor's hot spot.
type HotSpot struct {
	X uint16
	Y uint16
}

// NewCursorFromImages makes a cursor from a list of images and hot spots.
func NewCursorFromImages(images []CursorImage) (*Cursor, error) {
	cursor := &Cursor{}

	for _, img := range images {
		if err := cursor.addImage(img.Image, img.HotSpot); err != nil {
			return nil, err
		}
	}

	return cursor, nil
}

// LoadCUR loads a CUR file and returns a cursor, ready to embed in a resource set.
func LoadCUR(cur io.ReadSeeker) (*Cursor, error) {
	hdr := cursorDirHeader{}
	if err := binaryRead(cur, &hdr); err != nil {
		return nil, err
	}

	if hdr.Type != 2 || hdr.Reserved != 0 {
		return nil, errors.New(errNotCUR)
	}

	entries := make([]cursorFileDirEntry, hdr.Count)
	if err := binaryRead(cur, entries); err != nil {
		return nil, err
	}

	cursor := &Cursor{}
	for _, e := range entries {
		// Arbitrary limit: no more than 10MB per image, so we can blindly allocate bytes and try to read them.
		if e.BytesInRes > 0xA00000 {
			return nil, errors.New(errImageLengthTooBig)
		}
		if _, err := cur.Seek(int64(e.ImageOffset), io.SeekStart); err != nil {
			return nil, err
		}
		img := make([]byte, e.BytesInRes)
		if err := readFull(cur, img); err != nil {
			return nil, err
		}

		planes, bitCount, err := readDIBBitCount(img)
		if err != nil {
			return nil, err
		}

		cursor.images = append(cursor.images, cursorImage{
			info: cursorInfo{
				Width:      uint16(e.Width-1) + 1,
				Height:     uint16(e.Height-1) + 1,
				Planes:     planes,
				BitCount:   bitCount,
				BytesInRes: e.BytesInRes,
			},
			hotSpot: HotSpot{
				X: e.XHotSpot,
				Y: e.YHotSpot,
			},
			image: img,
		})
	}

	return cursor, nil
}

// SaveCUR saves a cursor as a CUR file.
func (cursor *Cursor) SaveCUR(ico io.Writer) error {
	err := binary.Write(ico, binary.LittleEndian, &cursorDirHeader{
		Type:  2,
		Count: uint16(len(cursor.images)),
	})
	if err != nil {
		return err
	}

	var (
		pos    = sizeOfCursorDirHeader
		offset = sizeOfCursorDirHeader + len(cursor.images)*sizeOfCursorFileDirEntry
	)

	cursor.order()
	for i := range cursor.images {
		err = binary.Write(ico, binary.LittleEndian, &cursorFileDirEntry{
			Width:       uint8(cursor.images[i].info.Width),
			Height:      uint8(cursor.images[i].info.Height),
			XHotSpot:    cursor.images[i].hotSpot.X,
			YHotSpot:    cursor.images[i].hotSpot.Y,
			BytesInRes:  uint32(len(cursor.images[i].image)),
			ImageOffset: uint32(offset),
		})
		if err != nil {
			return err
		}

		offset += len(cursor.images[i].image)
		pos += sizeOfCursorFileDirEntry
	}

	for i := range cursor.images {
		_, err = ico.Write(cursor.images[i].image)
		if err != nil {
			return err
		}
	}

	return nil
}

// SetCursor adds the cursor to the resource set.
func (rs *ResourceSet) SetCursor(resID Identifier, cursor *Cursor) error {
	return rs.SetCursorTranslation(resID, LCIDNeutral, cursor)
}

// SetCursorTranslation adds the cursor to a specific language in the resource set.
func (rs *ResourceSet) SetCursorTranslation(resID Identifier, langID uint16, cursor *Cursor) error {
	b := &bytes.Buffer{}
	binary.Write(b, binary.LittleEndian, cursorDirHeader{
		Type:  2,
		Count: uint16(len(cursor.images)),
	})

	cursor.order()
	for _, img := range cursor.images {
		id := rs.lastCursorID + 1

		binary.Write(b, binary.LittleEndian, cursorResDirEntry{
			cursorInfo: img.info,
			Id:         id,
		})

		if err := rs.Set(RT_CURSOR, ID(id), LCIDNeutral, img.resData()); err != nil {
			return err
		}
	}
	return rs.Set(RT_GROUP_CURSOR, resID, langID, b.Bytes())
}

// GetCursor extracts a cursor from a resource set.
func (rs *ResourceSet) GetCursor(resID Identifier) (*Cursor, error) {
	return rs.GetCursorTranslation(resID, rs.firstLang(RT_GROUP_CURSOR, resID))
}

// GetCursorTranslation extracts a cursor from a specific language of the resource set.
func (rs *ResourceSet) GetCursorTranslation(resID Identifier, langID uint16) (*Cursor, error) {
	data := rs.Get(RT_GROUP_CURSOR, resID, langID)
	if data == nil {
		return nil, errors.New(errGroupNotFound)
	}

	in := bytes.NewReader(data)
	dir := cursorDirHeader{}
	err := binaryRead(in, &dir)
	if err != nil || dir.Type != 2 || dir.Reserved != 0 {
		return nil, errors.New(errInvalidGroup)
	}

	g := &Cursor{}
	for i := 0; i < int(dir.Count); i++ {
		entry := cursorResDirEntry{}
		err := binaryRead(in, &entry)
		if err != nil {
			return nil, errors.New(errInvalidGroup)
		}
		img := rs.Get(RT_CURSOR, ID(entry.Id), rs.firstLang(RT_CURSOR, ID(entry.Id)))
		if img == nil {
			return nil, errors.New(errCursorMissing)
		}
		g.images = append(g.images, cursorImage{
			info:  entry.cursorInfo,
			image: img[4:],
			hotSpot: HotSpot{
				X: uint16(img[1])<<8 | uint16(img[0]),
				Y: uint16(img[3])<<8 | uint16(img[2]),
			},
		})
	}
	return g, nil
}

type cursorDirHeader struct {
	Reserved uint16
	Type     uint16
	Count    uint16
}

const sizeOfCursorDirHeader = 6

type cursorFileDirEntry struct {
	Width       uint8
	Height      uint8
	Reserved    uint16
	XHotSpot    uint16
	YHotSpot    uint16
	BytesInRes  uint32
	ImageOffset uint32
}

const sizeOfCursorFileDirEntry = 16

type cursorResDirEntry struct {
	cursorInfo
	Id uint16
}

type cursorInfo struct {
	Width      uint16
	Height     uint16
	Planes     uint16
	BitCount   uint16
	BytesInRes uint32
}

type cursorImage struct {
	info    cursorInfo
	hotSpot HotSpot
	image   []byte
}

func (ci *cursorImage) resData() []byte {
	// As a resource, image data includes the hot spot
	buf := bytes.NewBuffer(make([]byte, 0, len(ci.image)+4))
	binary.Write(buf, binary.LittleEndian, ci.hotSpot)
	buf.Write(ci.image)
	return buf.Bytes()
}

// This makes a testing error reporting possible
var bmpEncode = bmp.Encode

func (cursor *Cursor) addImage(img image.Image, hotSpot HotSpot) error {
	bounds := img.Bounds()
	if bounds.Empty() {
		return errors.New(errInvalidImageDimensions)
	}
	if bounds.Size().X > 256 || bounds.Size().Y > 256 {
		return errors.New(errImageTooBig)
	}

	// PNG seems to be supported, and simpler. (no need to double the height or to skip a part of the header)
	// But I've never seen PNG cursors and I would not take the risk.
	// PNG icons, on the other hand, are very common.
	buf := &bytes.Buffer{}
	curImg := imageInSquareNRGBA(img, false)
	if err := bmpEncode(buf, curImg); err != nil {
		return err
	}

	// 14 is the size of a BMPFILEHEADER, which we want to skip.
	// A BMP file is a BMPFILEHEADER followed by a DIB.
	dib := buf.Bytes()[14:]

	width, height := curImg.Bounds().Size().X, curImg.Bounds().Size().Y
	// Height must be doubled in the DIB header, as if there was an AND mask for transparency.
	// In a 32 bits DIB, the mask can be the alpha channel, therefore there is no AND mask.
	dib[8] = byte(height << 1)
	dib[9] = byte(height >> 7)

	cursor.images = append(cursor.images, cursorImage{
		info: cursorInfo{
			Width:      uint16(width),
			Height:     uint16(height),
			Planes:     1,
			BitCount:   32,
			BytesInRes: uint32(len(dib) + 4), // +4 for the hot spot
		},
		hotSpot: hotSpot,
		image:   dib,
	})

	return nil
}

func (cursor *Cursor) order() {
	sort.SliceStable(cursor.images, func(i, j int) bool {
		img1, img2 := &cursor.images[i].info, &cursor.images[j].info
		return img1.BitCount > img2.BitCount || img1.BitCount == img2.BitCount && img1.Width > img2.Width
	})
}

func readDIBBitCount(data []byte) (uint16, uint16, error) {
	// Icons and cursor may contain PNG instead of DIB
	_, s, _ := image.DecodeConfig(bytes.NewReader(data))
	if s == "png" {
		return 1, 32, nil
	}

	hdrSize := uint32(data[3])<<24 | uint32(data[2])<<16 | uint32(data[1])<<8 | uint32(data[0])

	if hdrSize != 40 && hdrSize != 108 && hdrSize != 124 {
		return 0, 0, errors.New(errUnknownImageFormat)
	}

	var (
		planes   = uint16(data[13])<<8 | uint16(data[12])
		bitCount = uint16(data[15])<<8 | uint16(data[14])
	)

	return planes, bitCount, nil
}
