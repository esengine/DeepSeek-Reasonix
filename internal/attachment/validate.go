package attachment

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode"
	"unicode/utf8"

	_ "golang.org/x/image/webp"
)

var sniffHeaders = []struct {
	mime   string
	prefix []byte
}{
	{"image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}},
	{"image/gif", []byte("GIF87a")},
	{"image/gif", []byte("GIF89a")},
	{"image/webp", []byte("RIFF")},
	{"image/jpeg", []byte{0xff, 0xd8, 0xff}},
}

func DetectMIME(raw []byte) string {
	for _, h := range sniffHeaders {
		if !bytes.HasPrefix(raw, h.prefix) {
			continue
		}
		if h.mime == "image/webp" && (len(raw) < 12 || string(raw[8:12]) != "WEBP") {
			continue
		}
		return h.mime
	}
	return ""
}

func NormalizeDisplayName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 || !utf8.ValidRune(r) || unicode.Is(unicode.C, r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "image"
	}
	return out
}

type verifiedImage struct {
	MIME   string
	Width  int
	Height int
	Bytes  []byte
}

func ValidateImage(raw []byte, declaredMIME string, policy Policy) (mime string, width, height int, err error) {
	verified, err := verifyImageBytes(raw, declaredMIME, policy)
	if err != nil {
		return "", 0, 0, err
	}
	return verified.MIME, verified.Width, verified.Height, nil
}

func verifyImageBytes(raw []byte, declaredMIME string, policy Policy) (verifiedImage, error) {
	policy = policy.withDefaults()
	if len(raw) == 0 || int64(len(raw)) > policy.MaxBytes {
		return verifiedImage{}, Error{Code: CodeSize, Message: defaultDetail(CodeSize)}
	}
	mime := DetectMIME(raw)
	if mime == "" {
		return verifiedImage{}, Error{Code: CodeUnsupported, Message: defaultDetail(CodeUnsupported)}
	}
	if declared := normalizeDeclaredMIME(declaredMIME); declared != "" && declared != mime {
		return verifiedImage{}, Error{Code: CodeUnsupported, Message: "declared MIME does not match image bytes"}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return verifiedImage{}, Error{Code: CodeCorrupt, Message: defaultDetail(CodeCorrupt), Cause: err}
	}
	if imageMIME(format) != mime {
		return verifiedImage{}, Error{Code: CodeUnsupported, Message: "decoded format does not match image bytes"}
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return verifiedImage{}, Error{Code: CodeCorrupt, Message: defaultDetail(CodeCorrupt)}
	}
	if int64(cfg.Width)*int64(cfg.Height) > policy.MaxPixels {
		return verifiedImage{}, Error{Code: CodeSize, Message: "exceeds the allowed pixel count"}
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(raw))
	if err != nil || decoded == nil {
		return verifiedImage{}, Error{Code: CodeCorrupt, Message: defaultDetail(CodeCorrupt), Cause: err}
	}
	bounds := decoded.Bounds()
	if imageMIME(decodedFormat) != mime || bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return verifiedImage{}, Error{Code: CodeCorrupt, Message: defaultDetail(CodeCorrupt)}
	}
	return verifiedImage{MIME: mime, Width: cfg.Width, Height: cfg.Height, Bytes: append([]byte(nil), raw...)}, nil
}

func normalizeDeclaredMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return mime
	case "image/jpg":
		return "image/jpeg"
	default:
		return ""
	}
}

func imageMIME(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func hasAlpha(img image.Image) bool {
	switch img.(type) {
	case *image.NRGBA, *image.NRGBA64, *image.RGBA, *image.RGBA64, *image.Alpha, *image.Alpha16:
		return true
	}
	if img == nil {
		return false
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}
