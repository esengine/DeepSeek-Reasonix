package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	workspaceDiffTallyFileLimit = 1 << 20
	workspaceDiffTallyReadLimit = 16 << 20
)

func workspaceGitDiffTally(ctx context.Context, base string, untracked []string) (added, removed int, incomplete bool) {
	raw, err := workspaceGitCommand(ctx, "-C", base, "diff", "--numstat", "--no-textconv", "HEAD", "--", ".").Output()
	if err != nil && ctx.Err() == nil {
		incomplete = true
		raw, err = workspaceGitCommand(ctx, "-C", base, "diff", "--numstat", "--no-textconv", "--", ".").Output()
	}
	if err != nil {
		return 0, 0, true
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, _ := strconv.Atoi(fields[0])
		added += n
		n, _ = strconv.Atoi(fields[1])
		removed += n
	}
	if len(untracked) == 0 {
		return added, removed, incomplete || ctx.Err() != nil
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return added, removed, true
	}
	defer root.Close()
	remaining := int64(workspaceDiffTallyReadLimit)
	for _, rel := range untracked {
		if ctx.Err() != nil || remaining <= 0 {
			return added, removed, true
		}
		count, partial := workspaceCountFileLines(ctx, root, filepath.FromSlash(rel), &remaining)
		added += count
		incomplete = incomplete || partial
	}
	return added, removed, incomplete
}

func workspaceCountFileLines(ctx context.Context, root *os.Root, path string, remaining *int64) (int, bool) {
	if !filepath.IsLocal(path) || ctx.Err() != nil {
		return 0, true
	}
	// Check every component; Root also prevents a concurrent replacement escaping
	// the workspace. The platform open rejects final symlinks and blocking FIFOs.
	for parent := path; parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Lstat(parent)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return 0, true
		}
	}
	before, err := root.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return 0, true
	}
	f, err := openWorkspaceTallyFile(root, path)
	if err != nil {
		return 0, true
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return 0, true
	}
	limit := min(int64(workspaceDiffTallyFileLimit), *remaining)
	partial := info.Size() > limit
	reader := io.LimitReader(f, limit)
	buf := make([]byte, 64*1024)
	count, total := 0, int64(0)
	var last byte
	for {
		if ctx.Err() != nil {
			return count, true
		}
		n, readErr := reader.Read(buf)
		*remaining -= int64(n)
		total += int64(n)
		if n > 0 {
			chunk := buf[:n]
			if bytes.IndexByte(chunk, 0) >= 0 {
				return 0, false
			}
			count += bytes.Count(chunk, []byte{'\n'})
			last = chunk[n-1]
		}
		if readErr != nil {
			if readErr != io.EOF {
				return count, true
			}
			break
		}
	}
	if total > 0 && last != '\n' && !partial {
		count++
	}
	return count, partial
}
