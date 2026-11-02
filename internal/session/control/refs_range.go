package control

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var refLineRangeSuffix = regexp.MustCompile(`:([0-9]+)-([0-9]+)$`)

var errRefLineRange = errors.New("invalid file line range")

type refLineRange struct {
	start int
	end   int
}

func splitRefLineRange(token string) (string, *refLineRange) {
	match := refLineRangeSuffix.FindStringSubmatchIndex(token)
	if match == nil {
		return token, nil
	}
	start, startErr := strconv.Atoi(token[match[2]:match[3]])
	end, endErr := strconv.Atoi(token[match[4]:match[5]])
	if startErr != nil || endErr != nil {
		start, end = 0, 0
	}
	return token[:match[0]], &refLineRange{start: start, end: end}
}

func (c *Controller) detectFileRef(token string, scopedOnly bool) (ref, bool) {
	if r, ok := c.externalFolderRef(token); ok {
		_, ok := workspaceRefPath(r.path, r.baseDir)
		return r, ok
	}
	if c.workspaceRoot != "" {
		rel, ok := workspaceRefPath(token, c.workspaceRoot)
		if !ok {
			return ref{}, false
		}
		kind := refFile
		if isAttachmentRef(rel) && isImageAttachmentRef(rel) {
			kind = refImage
		}
		return ref{kind: kind, path: rel, raw: token}, true
	}
	if scopedOnly {
		return ref{}, false
	}
	return classifyRef(token, nil, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func readFileRefLines(ctx context.Context, path, baseDir string, lines refLineRange) (string, error) {
	if lines.start < 1 || lines.end < lines.start {
		return "", fmt.Errorf("%w: use positive, ascending line numbers", errRefLineRange)
	}
	absPath, absBase, ok := resolveAbsRef(path, baseDir)
	if !ok {
		return "", os.ErrNotExist
	}
	if absBase == "" {
		var err error
		absPath, err = filepath.Abs(absPath)
		if err != nil {
			return "", err
		}
		absPath, err = filepath.EvalSymlinks(absPath)
		if err != nil {
			return "", err
		}
		// Rootless CLI references retain filesystem-wide access, including symlinks.
		absBase = filepath.VolumeName(absPath) + string(filepath.Separator)
	}
	root, err := os.OpenRoot(absBase)
	if err != nil {
		return "", err
	}
	defer root.Close()
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return "", err
	}
	info, err := root.Stat(rel)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: ranges require a regular text file", errRefLineRange)
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: ranges require a regular text file", errRefLineRange)
	}
	return streamFileRefLines(ctx, f, path, lines)
}

func streamFileRefLines(ctx context.Context, f *os.File, path string, lines refLineRange) (string, error) {
	r := bufio.NewReaderSize(f, 8192)
	prefix, err := r.Peek(8192)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if bytes.IndexByte(prefix, 0) >= 0 || imageMime(prefix, path) != "" || bytes.HasPrefix(prefix, []byte("%PDF-")) {
		return "", fmt.Errorf("%w: ranges require a text file", errRefLineRange)
	}
	var selected strings.Builder
	lineBytes := 0
	for line := 1; ; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fragment, readErr := r.ReadSlice('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, bufio.ErrBufferFull) {
			return "", readErr
		}
		lineBytes += len(fragment)
		if line >= lines.start {
			if bytes.IndexByte(fragment, 0) >= 0 {
				return "", fmt.Errorf("%w: ranges require a text file", errRefLineRange)
			}
			if selected.Len()+len(fragment) > maxFileRefBytes {
				return "", fmt.Errorf("%w: selected text exceeds %d bytes; choose fewer lines", errRefLineRange, maxFileRefBytes)
			}
			selected.Write(fragment)
		}
		if !errors.Is(readErr, bufio.ErrBufferFull) {
			if line == lines.end && lineBytes > 0 {
				return selected.String(), nil
			}
			if errors.Is(readErr, io.EOF) {
				return "", fmt.Errorf("%w: requested lines %d-%d extend past the end of the file", errRefLineRange, lines.start, lines.end)
			}
			line++
			lineBytes = 0
		}
	}
}
