package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/webp"
)

const markdownImageMaxPixels int64 = 40_000_000

var errMarkdownImageTooLarge = errors.New("markdown image exceeds the decode budget")

func validateOpenMarkdownImageFile(f *os.File, mimeType string, size int64) error {
	if size <= 0 {
		return fmt.Errorf("empty image")
	}
	if size > remoteMarkdownImageMaxBytes {
		return errMarkdownImageTooLarge
	}
	return validateMarkdownImageConfig(f, mimeType)
}

func validateMarkdownImageBytes(body []byte, mimeType string) error {
	if len(body) == 0 {
		return fmt.Errorf("empty image")
	}
	if len(body) > remoteMarkdownImageMaxBytes {
		return errMarkdownImageTooLarge
	}
	return validateMarkdownImageConfig(bytes.NewReader(body), mimeType)
}

func validateMarkdownImageConfig(r io.Reader, mimeType string) error {
	// SVG is sanitized by the remote proxy and does not allocate a source-sized
	// raster during Go-side validation. ICO remains byte-bounded; Go has no
	// decoder for it in the supported dependency set.
	if mimeType == "image/svg+xml" || mimeType == "image/x-icon" {
		return nil
	}

	var (
		cfg    image.Config
		format string
		err    error
	)
	if mimeType == "image/webp" {
		cfg, err = webp.DecodeConfig(r)
		format = "webp"
	} else {
		cfg, format, err = image.DecodeConfig(r)
	}
	if err != nil {
		return fmt.Errorf("decode image config: %w", err)
	}
	wantFormat := map[string]string{
		"image/png":  "png",
		"image/jpeg": "jpeg",
		"image/gif":  "gif",
		"image/bmp":  "bmp",
		"image/webp": "webp",
	}[mimeType]
	if wantFormat == "" || format != wantFormat {
		return fmt.Errorf("image MIME/format mismatch: %s/%s", mimeType, format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid image dimensions")
	}
	if int64(cfg.Width) > markdownImageMaxPixels/int64(cfg.Height) {
		return errMarkdownImageTooLarge
	}
	return nil
}
