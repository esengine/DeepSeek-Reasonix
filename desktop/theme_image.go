package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/webp"
)

func decodeBase64Payload(payload string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(payload)
}

func validateThemeImageFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("background image must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("background image must be a regular file")
	}
	if info.Size() > themePackMaxImageBytes {
		return fmt.Errorf("background image exceeds %d bytes", themePackMaxImageBytes)
	}
	name := filepath.Base(path)
	if !themePackImageRe.MatchString(name) {
		return fmt.Errorf("unsupported background image name %q", name)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return err
	}
	head = head[:n]
	mime := sniffThemeImageMIME(head, name)
	if mime == "" {
		return fmt.Errorf("background image must be PNG, JPEG, or WebP")
	}

	// Re-open for full config decode (sniff consumed some bytes).
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	cfg, format, err := decodeThemeImageConfig(f, mime)
	if err != nil {
		return fmt.Errorf("invalid background image: %w", err)
	}
	switch format {
	case "png", "jpeg", "webp":
	default:
		return fmt.Errorf("unsupported image format %q", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid image dimensions")
	}
	if cfg.Width > themePackMaxImageEdge || cfg.Height > themePackMaxImageEdge {
		return fmt.Errorf("background image must be at most %d×%d", themePackMaxImageEdge, themePackMaxImageEdge)
	}
	return nil
}

func decodeThemeImageConfig(r io.Reader, mime string) (image.Config, string, error) {
	if mime == "image/webp" {
		cfg, err := webp.DecodeConfig(r)
		return cfg, "webp", err
	}
	return image.DecodeConfig(r)
}

func sniffThemeImageMIME(head []byte, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		if ext == ".png" || ext == "" {
			return "image/png"
		}
	}
	if len(head) >= 3 && head[0] == 0xff && head[1] == 0xd8 && head[2] == 0xff {
		if ext == ".jpg" || ext == ".jpeg" || ext == "" {
			return "image/jpeg"
		}
	}
	if len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP" {
		if ext == ".webp" || ext == "" {
			return "image/webp"
		}
	}
	// Extension/MIME mismatch or unsupported signature.
	return ""
}

func themeImageMIMEFromName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// decodeDataURLImage extracts image bytes from a data:image/...;base64,... URL.
func decodeDataURLImage(dataURL string) (filename string, data []byte, err error) {
	dataURL = strings.TrimSpace(dataURL)
	if dataURL == "" {
		return "", nil, fmt.Errorf("empty image data")
	}
	const prefix = "data:"
	if !strings.HasPrefix(dataURL, prefix) {
		return "", nil, fmt.Errorf("image must be a data URL")
	}
	// data:[<mediatype>][;base64],<data>
	rest := dataURL[len(prefix):]
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return "", nil, fmt.Errorf("invalid data URL")
	}
	meta := rest[:comma]
	payload := rest[comma+1:]
	parts := strings.Split(meta, ";")
	mime := strings.TrimSpace(parts[0])
	base64Enc := false
	for _, p := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			base64Enc = true
		}
	}
	if !base64Enc {
		return "", nil, fmt.Errorf("image data URL must be base64-encoded")
	}
	var ext string
	switch strings.ToLower(mime) {
	case "image/png":
		ext = ".png"
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	default:
		return "", nil, fmt.Errorf("unsupported image MIME %q", mime)
	}
	// Prefer stdlib base64 via encoding/base64 in caller path — import here.
	raw, err := decodeBase64Payload(payload)
	if err != nil {
		return "", nil, fmt.Errorf("decode image data: %w", err)
	}
	if int64(len(raw)) > themePackMaxImageBytes {
		return "", nil, fmt.Errorf("background image exceeds %d bytes", themePackMaxImageBytes)
	}
	return "background" + ext, raw, nil
}

func taskBackgroundImageName(decodedName string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(decodedName)))
	return "background-task" + ext
}
