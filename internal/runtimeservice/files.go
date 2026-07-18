package runtimeservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"reasonix/internal/runtimeapi"
)

var workspaceNoiseNames = map[string]bool{
	".codex":       true,
	".DS_Store":    true,
	".git":         true,
	".npm":         true,
	".pnpm-store":  true,
	"node_modules": true,
	"Thumbs.db":    true,
}

var workspaceNoiseDirs = map[string]bool{
	"bin":                      true,
	"desktop/build":            true,
	"desktop/frontend/dist":    true,
	"desktop/frontend/wailsjs": true,
	"dist":                     true,
	"npm/.stage":               true,
	"site/.astro":              true,
	"site/dist":                true,
	"stage":                    true,
	"tmp":                      true,
}

var imageExtensions = map[string]bool{
	".bmp": true, ".gif": true, ".jpeg": true, ".jpg": true,
	".png": true, ".svg": true, ".webp": true,
}

func normalizeRelative(raw string, allowEmpty bool) (string, error) {
	if strings.IndexByte(raw, 0) >= 0 || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "\\") || strings.Contains(raw, "\\") || strings.Contains(raw, ":") {
		return "", ErrInvalidPath
	}
	cleaned := path.Clean(raw)
	if cleaned == "." {
		if allowEmpty {
			return "", nil
		}
		return "", ErrInvalidPath
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrPathEscapesRoot
	}
	if cleaned == "" {
		if allowEmpty {
			return "", nil
		}
		return "", ErrInvalidPath
	}
	return cleaned, nil
}

func (s *FileGitService) resolveExisting(rel string) (string, os.FileInfo, error) {
	candidate := filepath.Join(s.root, filepath.FromSlash(rel))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, ErrPathNotFound
		}
		if errors.Is(err, os.ErrPermission) {
			return "", nil, os.ErrPermission
		}
		return "", nil, ErrQueryFailed
	}
	resolved = filepath.Clean(resolved)
	within, err := filepath.Rel(s.root, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return "", nil, ErrPathEscapesRoot
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, ErrPathNotFound
		}
		return "", nil, err
	}
	return resolved, info, nil
}

func workspaceEntryPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func skipWorkspaceEntry(parent, name string, isDir bool) bool {
	if workspaceNoiseNames[name] {
		return true
	}
	return isDir && workspaceNoiseDirs[workspaceEntryPath(parent, name)]
}

func fileEntryLess(left, right runtimeapi.FileEntry) bool {
	if left.IsDir != right.IsDir {
		return left.IsDir
	}
	lowerLeft, lowerRight := strings.ToLower(left.Name), strings.ToLower(right.Name)
	if lowerLeft != lowerRight {
		return lowerLeft < lowerRight
	}
	return left.Path < right.Path
}

func (s *FileGitService) ListFiles(ctx context.Context, input runtimeapi.FileListInput) (runtimeapi.FileListResult, error) {
	result := runtimeapi.FileListResult{Entries: []runtimeapi.FileEntry{}}
	if err := requireSession(input.Session); err != nil {
		return result, err
	}
	limit, err := normalizedPageLimit(input.Limit)
	if err != nil {
		return result, err
	}
	rel, err := normalizeRelative(input.Path, true)
	if err != nil {
		return result, err
	}
	resolved, info, err := s.resolveExisting(rel)
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, ErrNotDirectory
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return result, sanitizeFileError(err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entryRel := workspaceEntryPath(rel, entry.Name())
		_, targetInfo, err := s.resolveExisting(entryRel)
		if err != nil {
			// Escaping, broken, or concurrently removed symlinks are not part of
			// the primary workspace view.
			if errors.Is(err, ErrPathEscapesRoot) || errors.Is(err, ErrPathNotFound) {
				continue
			}
			return result, err
		}
		isDir := targetInfo.IsDir()
		if skipWorkspaceEntry(rel, entry.Name(), isDir) {
			continue
		}
		if !isDir && !targetInfo.Mode().IsRegular() {
			continue
		}
		result.Entries = append(result.Entries, runtimeapi.FileEntry{Name: entry.Name(), Path: entryRel, IsDir: isDir})
	}
	sort.Slice(result.Entries, func(i, j int) bool { return fileEntryLess(result.Entries[i], result.Entries[j]) })
	full := result.Entries
	revision := snapshotRevision(full, "file/list", rel)
	session := sessionBinding(input.Session)
	offset, err := s.pageOffset(input.Cursor, "file/list", session, rel, revision, len(full))
	if err != nil {
		return runtimeapi.FileListResult{Entries: []runtimeapi.FileEntry{}}, err
	}
	end := offset + limit
	if end > len(full) {
		end = len(full)
	}
	result.Entries = append([]runtimeapi.FileEntry(nil), full[offset:end]...)
	result.HasMore = end < len(full)
	if result.HasMore {
		result.Next = s.encodeCursor(cursorPayload{
			Method: "file/list", Session: session, Filter: rel,
			Revision: revision, Offset: end,
		})
	}
	return result, nil
}

type searchWalk struct {
	basename []runtimeapi.FileEntry
	segment  []runtimeapi.FileEntry
	dirs     []runtimeapi.FileEntry
	visited  int
	limited  bool
	seenDirs map[string]bool
}

func (s *FileGitService) SearchFiles(ctx context.Context, input runtimeapi.FileSearchInput) (runtimeapi.FileSearchResult, error) {
	result := runtimeapi.FileSearchResult{Entries: []runtimeapi.FileEntry{}}
	if err := requireSession(input.Session); err != nil {
		return result, err
	}
	limit, err := normalizedSearchLimit(input.Limit)
	if err != nil {
		return result, err
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))
	if query == "" || strings.ContainsAny(query, `/\\`) {
		return result, ErrInvalidPath
	}
	walk := &searchWalk{seenDirs: map[string]bool{s.root: true}}
	if err := s.searchDirectory(ctx, "", s.root, query, strings.HasPrefix(query, "."), walk); err != nil {
		return result, err
	}
	for _, group := range [][]runtimeapi.FileEntry{walk.dirs, walk.basename, walk.segment} {
		sort.Slice(group, func(i, j int) bool { return group[i].Path < group[j].Path })
	}
	all := make([]runtimeapi.FileEntry, 0, len(walk.dirs)+len(walk.basename)+len(walk.segment))
	// Reserve a small navigation tier, then fill relevant files, then any
	// remaining matching directories. This preserves current Desktop ranking
	// without artificially returning fewer than the requested limit.
	dirHead := len(walk.dirs)
	if dirHead > 5 {
		dirHead = 5
	}
	all = append(all, walk.dirs[:dirHead]...)
	all = append(all, walk.basename...)
	all = append(all, walk.segment...)
	all = append(all, walk.dirs[dirHead:]...)
	total := len(all)
	if total > limit {
		result.Entries = append(result.Entries, all[:limit]...)
	} else {
		result.Entries = append(result.Entries, all...)
	}
	result.ReturnedItems = len(result.Entries)
	if walk.limited {
		result.Truncated = true
		result.TruncationReason = runtimeapi.SearchScanLimit
	} else {
		result.TotalItems = &total
		if total > limit {
			result.Truncated = true
			result.TruncationReason = runtimeapi.SearchResultLimit
		}
	}
	return result, nil
}

func (s *FileGitService) searchDirectory(ctx context.Context, rel, resolved, query string, showHidden bool, state *searchWalk) error {
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return sanitizeFileError(err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		state.visited++
		if state.visited > runtimeapi.SearchMaxVisitedItems {
			state.limited = true
			return nil
		}
		entryRel := workspaceEntryPath(rel, entry.Name())
		entryResolved, info, err := s.resolveExisting(entryRel)
		if err != nil {
			if errors.Is(err, ErrPathEscapesRoot) || errors.Is(err, ErrPathNotFound) {
				continue
			}
			return err
		}
		isDir := info.IsDir()
		if skipWorkspaceEntry(rel, entry.Name(), isDir) || (!showHidden && strings.HasPrefix(entry.Name(), ".")) {
			continue
		}
		if isDir {
			if strings.Contains(strings.ToLower(entry.Name()), query) {
				state.dirs = append(state.dirs, runtimeapi.FileEntry{Name: entry.Name(), Path: entryRel, IsDir: true})
			}
			if !state.seenDirs[entryResolved] {
				state.seenDirs[entryResolved] = true
				if err := s.searchDirectory(ctx, entryRel, entryResolved, query, showHidden, state); err != nil || state.limited {
					return err
				}
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file := runtimeapi.FileEntry{Name: entry.Name(), Path: entryRel}
		if strings.Contains(strings.ToLower(entry.Name()), query) {
			state.basename = append(state.basename, file)
		} else if pathSegmentContains(entryRel, query) {
			state.segment = append(state.segment, file)
		}
	}
	return nil
}

func pathSegmentContains(rel, query string) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		if strings.Contains(strings.ToLower(part), query) {
			return true
		}
	}
	return false
}

func (s *FileGitService) PreviewFile(ctx context.Context, input runtimeapi.FilePreviewInput) (runtimeapi.FilePreview, error) {
	result := runtimeapi.FilePreview{}
	if err := requireSession(input.Session); err != nil {
		return result, err
	}
	rel, err := normalizeRelative(input.Path, false)
	if err != nil {
		return result, err
	}
	resolved, info, err := s.resolveExisting(rel)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, ErrNotFile
	}
	result.Name = path.Base(rel)
	result.Path = rel
	result.SizeBytes = info.Size()
	extension := strings.ToLower(path.Ext(rel))
	switch {
	case extension == ".pdf":
		result.Kind, result.Binary = runtimeapi.FilePDF, true
		return result, nil
	case imageExtensions[extension]:
		result.Kind, result.Binary = runtimeapi.FileImage, true
		return result, nil
	}
	file, err := os.Open(resolved)
	if err != nil {
		return result, sanitizeFileError(err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(runtimeapi.PreviewBytes)))
	if err != nil {
		return result, sanitizeFileError(err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if current, err := file.Stat(); err == nil {
		result.SizeBytes = current.Size()
	}
	if bytes.IndexByte(data, 0) >= 0 {
		result.Kind, result.Binary = runtimeapi.FileBinary, true
		return result, nil
	}
	textBytes := data
	if result.SizeBytes > int64(len(data)) {
		textBytes = trimUTF8Boundary(data)
	}
	if !utf8.Valid(textBytes) {
		result.Kind, result.Binary = runtimeapi.FileBinary, true
		return result, nil
	}
	body := string(textBytes)
	result.Kind = runtimeapi.FileText
	result.Body = &body
	result.ReturnedBytes = int64(len(textBytes))
	result.Truncated = result.SizeBytes > result.ReturnedBytes
	if result.Truncated {
		result.TruncationReason = runtimeapi.ByteLimit
	}
	return result, result.Validate()
}

func trimUTF8Boundary(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	start := len(data) - utf8.UTFMax
	if start < 0 {
		start = 0
	}
	for candidate := len(data) - 1; candidate >= start; candidate-- {
		if !utf8.Valid(data[:candidate]) {
			continue
		}
		if validIncompleteUTF8(data[candidate:]) {
			return data[:candidate]
		}
	}
	return data
}

func validIncompleteUTF8(suffix []byte) bool {
	if len(suffix) == 0 {
		return false
	}
	first := suffix[0]
	want := 0
	switch {
	case first >= 0xC2 && first <= 0xDF:
		want = 2
	case first >= 0xE0 && first <= 0xEF:
		want = 3
	case first >= 0xF0 && first <= 0xF4:
		want = 4
	default:
		return false
	}
	if len(suffix) >= want {
		return false
	}
	for _, value := range suffix[1:] {
		if value&0xC0 != 0x80 {
			return false
		}
	}
	if len(suffix) >= 2 {
		second := suffix[1]
		switch first {
		case 0xE0:
			if second < 0xA0 {
				return false
			}
		case 0xED:
			if second > 0x9F {
				return false
			}
		case 0xF0:
			if second < 0x90 {
				return false
			}
		case 0xF4:
			if second > 0x8F {
				return false
			}
		}
	}
	return true
}

func sanitizeFileError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ErrPathNotFound
	case errors.Is(err, os.ErrPermission):
		return os.ErrPermission
	default:
		return ErrQueryFailed
	}
}
