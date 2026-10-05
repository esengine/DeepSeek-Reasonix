package feedback

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"

	"reasonix/internal/model/visionimage"
)

const (
	maxSide        = 1600
	maxDecodePixel = 40_000_000
)

// Attachment is an image ready to send: re-encoded from pixels, which drops
// every metadata block (EXIF, GPS, embedded thumbnails) the original carried.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Data        []byte `json:"-"`
}

// Normalize checks the images against the limits and returns what will be
// sent. Only PNG and JPEG pass, judged by content and never by file name.
func Normalize(images []Image, lim Limits) ([]Attachment, error) {
	if len(images) > lim.Images {
		return nil, invalid(FieldImages, ReasonTooMany)
	}
	out := make([]Attachment, 0, len(images))
	for _, in := range images {
		if len(in.Data) == 0 {
			return nil, invalid(FieldImages, ReasonUndecodable)
		}
		if len(in.Data) > lim.UploadBytes {
			return nil, invalid(FieldImages, ReasonTooLarge)
		}
		a, err := normalizeOne(in, lim.ImageBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// clean is a variable so a test can force the gate to fail.
var clean = metadataFree

func normalizeOne(in Image, budget int) (Attachment, error) {
	a, err := reencode(in, budget)
	if err == nil && !clean(a.Data, a.ContentType) {
		return Attachment{}, ErrImageMetadata
	}
	return a, err
}

func reencode(in Image, budget int) (Attachment, error) {
	mime := visionimage.DetectMime(in.Data)
	if mime != "image/png" && mime != "image/jpeg" {
		return Attachment{}, invalid(FieldImages, ReasonFormat)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(in.Data))
	if err != nil || int64(cfg.Width)*int64(cfg.Height) > maxDecodePixel {
		return Attachment{}, invalid(FieldImages, ReasonUndecodable)
	}
	src, _, err := image.Decode(bytes.NewReader(in.Data))
	if err != nil {
		return Attachment{}, invalid(FieldImages, ReasonUndecodable)
	}
	if mime == "image/jpeg" {
		src = orient(src, jpegOrientation(in.Data))
	}
	name := attachmentName(in.Name, mime)
	side := maxSide
	for range 8 {
		img := fit(src, side)
		if mime == "image/png" {
			if data := encodePNG(img); len(data) <= budget {
				return Attachment{name, mime, data}, nil
			}
		}
		for _, q := range []int{85, 70, 55} {
			if data := encodeJPEG(img, q); len(data) <= budget {
				return Attachment{jpegName(name), "image/jpeg", data}, nil
			}
		}
		side = side * 3 / 4
	}
	return Attachment{}, invalid(FieldImages, ReasonTooLarge)
}

func fit(src image.Image, side int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return src
	}
	nw, nh := side, max(h*side/w, 1)
	if h > w {
		nw, nh = max(w*side/h, 1), side
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// encodeJPEG flattens onto white first: JPEG has no alpha, and a transparent
// pixel would otherwise turn black.
func encodeJPEG(img image.Image, quality int) []byte {
	flat := image.NewRGBA(img.Bounds())
	draw.Draw(flat, flat.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: quality}); err != nil {
		return nil
	}
	return buf.Bytes()
}

func attachmentName(name, mime string) string {
	base := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(name, `\`, "/")), filepath.Ext(name))
	base = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, base)
	if base == "" || base == "." {
		base = "screenshot"
	}
	if r := []rune(base); len(r) > 60 {
		base = string(r[:60])
	}
	return base + visionimage.Ext(mime)
}

func jpegName(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name)) + ".jpg"
}
