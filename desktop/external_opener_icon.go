package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxExternalOpenerIconBytes = 768 << 10

var externalOpenerIconCache sync.Map

func externalOpenerIconDataURL(spec externalOpenerSpec) string {
	source := strings.TrimSpace(spec.IconSource)
	if source == "" {
		return ""
	}
	cacheKey := source
	if info, err := os.Stat(source); err == nil {
		cacheKey = fmt.Sprintf("%s:%d:%d", source, info.ModTime().UnixNano(), info.Size())
	}
	if cached, ok := externalOpenerIconCache.Load(cacheKey); ok {
		return cached.(string)
	}
	icon := platformExternalOpenerIconDataURL(spec)
	if icon != "" {
		externalOpenerIconCache.Store(cacheKey, icon)
	}
	return icon
}

func externalOpenerIconFileDataURL(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > maxExternalOpenerIconBytes {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > maxExternalOpenerIconBytes {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return externalOpenerPNGDataURL(data)
	case ".jpg", ".jpeg":
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
	case ".webp":
		return "data:image/webp;base64," + base64.StdEncoding.EncodeToString(data)
	case ".svg":
		return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(data)
	default:
		return ""
	}
}

func externalOpenerPNGDataURL(data []byte) string {
	if len(data) == 0 || len(data) > maxExternalOpenerIconBytes {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

func externalOpenerPNGFromBGRAComposites(black, white []byte, width, height int) []byte {
	if width <= 0 || height <= 0 || len(black) != width*height*4 || len(white) != len(black) {
		return nil
	}
	clampByte := func(value int) uint8 {
		if value < 0 {
			return 0
		}
		if value > 255 {
			return 255
		}
		return uint8(value)
	}
	imageData := image.NewNRGBA(image.Rect(0, 0, width, height))
	for source := 0; source < len(black); source += 4 {
		blackB, blackG, blackR := int(black[source]), int(black[source+1]), int(black[source+2])
		whiteB, whiteG, whiteR := int(white[source]), int(white[source+1]), int(white[source+2])
		diff := func(light, dark int) int {
			if light <= dark {
				return 0
			}
			return int(clampByte(light - dark))
		}
		alpha := 255 - (diff(whiteR, blackR)+diff(whiteG, blackG)+diff(whiteB, blackB))/3
		destination := source
		if alpha > 0 {
			imageData.Pix[destination] = clampByte(blackR * 255 / alpha)
			imageData.Pix[destination+1] = clampByte(blackG * 255 / alpha)
			imageData.Pix[destination+2] = clampByte(blackB * 255 / alpha)
		}
		imageData.Pix[destination+3] = uint8(alpha)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageData); err != nil {
		return nil
	}
	return encoded.Bytes()
}
