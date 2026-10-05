package feedback

import (
	"encoding/binary"
	"image"
)

// jpegOrientation reads the EXIF orientation (1-8) from a JPEG's APP1 segment,
// or 1 when there is none. Metadata is dropped on re-encode, so the rotation it
// asked for has to be applied to the pixels first.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for pos := 2; pos+4 <= len(data) && data[pos] == 0xFF; {
		marker := data[pos+1]
		size := int(binary.BigEndian.Uint16(data[pos+2:]))
		if marker == 0xDA || size < 2 || pos+2+size > len(data) {
			return 1
		}
		if marker == 0xE1 {
			if o := exifOrientation(data[pos+4 : pos+2+size]); o != 0 {
				return o
			}
		}
		pos += 2 + size
	}
	return 1
}

func exifOrientation(seg []byte) int {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 0
	}
	tiff := seg[6:]
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifd := int(bo.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	n := int(bo.Uint16(tiff[ifd:]))
	for i := range n {
		at := ifd + 2 + i*12
		if at+12 > len(tiff) {
			return 0
		}
		if bo.Uint16(tiff[at:]) == 0x0112 {
			if v := int(bo.Uint16(tiff[at+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
	}
	return 0
}

// orient returns img with EXIF orientation o applied (1 leaves it alone).
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
