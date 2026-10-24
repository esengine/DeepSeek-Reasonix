package checkenv

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/safety/evidence"
)

type capturedInput struct {
	path     string
	body     []byte
	relative string
}

// WithCaptured substitutes host-held criterion bytes in the frozen source only.
// Its receipt cannot authorize ordinary integration checks.
func (s Spec) WithCaptured(path string, body []byte) Spec {
	relative, err := filepath.Rel(s.SourceRoot, path)
	if err != nil || !filepath.IsAbs(path) || !filepath.IsLocal(relative) || relative == "." {
		relative = ""
	}
	s.capture = &capturedInput{path: path, body: bytes.Clone(body), relative: relative}
	return s
}

func VerifyCaptured(input evidence.CheckInputs, executionID, path string, body []byte) bool {
	return verifyReceipt(input, executionID, digest(capturedInputIdentity{path, body}))
}

type capturedInputIdentity struct {
	Path string
	Body []byte
}

func (s Spec) captureIdentity() string {
	if s.capture == nil {
		return ""
	}
	return digest(capturedInputIdentity{s.capture.path, s.capture.body})
}

func (s Spec) applyCapture(ctx context.Context, frozen string, policy treePolicy) (string, error) {
	if s.capture != nil {
		path := s.capture.relative
		if path == "" {
			return "", errors.New("captured criterion is outside verification source")
		}
		path = filepath.ToSlash(path)
		if deniedPath(path, append([]string{".git"}, policy.denied...)) {
			return "", errors.New("captured criterion intersects a forbidden input")
		}
		if err := replaceCaptured(frozen, path, s.capture.body); err != nil {
			return "", err
		}
	}
	entries, err := scanTree(ctx, frozen, treePolicy{})
	return digest(entries), err
}

func replaceCaptured(frozen, path string, body []byte) error {
	root, err := os.OpenRoot(frozen)
	if err != nil {
		return err
	}
	defer root.Close()
	parts := strings.Split(path, "/")
	for index := range parts {
		name := filepath.Join(parts[:index+1]...)
		info, err := root.Lstat(name)
		last := index == len(parts)-1
		if last && os.IsNotExist(err) {
			break
		}
		if err != nil {
			return err
		}
		if (!last && !info.IsDir()) || (last && !info.Mode().IsRegular()) {
			return errors.New("captured criterion path contains an alias or special file")
		}
	}
	if err := root.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body)
	return errors.Join(writeErr, file.Close())
}
