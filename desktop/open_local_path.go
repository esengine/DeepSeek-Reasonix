package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// normalizeLocalOpenPath validates and normalizes a user-clicked local path
// before it is handed to the OS opener. It accepts either a plain absolute
// path (D:\a\b.md or D:/a/b.md) or a file:/// URL produced by the markdown
// linkifier; the frontend decodes percent-escapes before calling
// OpenLocalPath, so no URL decoding happens here.
func normalizeLocalOpenPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", os.ErrInvalid
	}
	path = strings.TrimPrefix(path, "file:///")
	// Normalize forward slashes (file URLs, slash-form UNC "//nas/share")
	// to the platform-native separators the opener expects.
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path is not absolute: %q", path)
	}
	return path, nil
}

// openTargetAllowed reports whether a resolved path may be handed to the OS
// "open" verb. Directories and documents open normally; executable targets
// are refused because OpenLocalPath is fed by AI-generated chat content —
// a prompt-injected or hallucinated ".bat" path must not run on click.
// openWorkspacePath itself stays untouched: it is also used by
// RevealWorkspacePathForTab with trusted workspace inputs.
var executableOpenSuffixes = map[string]bool{
	".bat": true, ".cmd": true, ".com": true, ".exe": true,
	".ps1": true, ".vbs": true, ".jse": true, ".js": true,
	".lnk": true, ".url": true, ".scr": true, ".msi": true,
	".reg": true, ".pif": true, ".hta": true, ".wsf": true,
}

func openTargetAllowed(path string, isDir bool) bool {
	if isDir {
		return true
	}
	return !executableOpenSuffixes[strings.ToLower(filepath.Ext(path))]
}

// OpenLocalPath opens an arbitrary local absolute path (file or directory)
// with the OS default application. It backs clicking a local path rendered in
// chat markdown (issue #7426) — Windows drive paths, UNC paths and file:///
// URLs included.
func (a *App) OpenLocalPath(path string) error {
	path, err := normalizeLocalOpenPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !openTargetAllowed(path, info.IsDir()) {
		return fmt.Errorf("refusing to open executable target %q", path)
	}
	return openWorkspacePath(path)
}
