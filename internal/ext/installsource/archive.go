// archive.go — a plugin package handed over as a local .zip.
package installsource

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
)

// looksLikeZip reports a local archive source. Exporting a package produces
// one, and installing it is the same act as installing the folder it unpacks
// to — there is no separate import path for a package to arrive through.
func looksLikeZip(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// unpackPluginZip extracts path into a temporary directory and returns the
// package root inside it: the single directory every entry shares, or the
// extraction directory itself when the archive was packed flat.
func unpackPluginZip(path string) (string, func(), error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", func() {}, newErr(ErrSourceUnreadable, "read %s: %v", filepath.Base(path), err)
	}
	defer zr.Close()

	dir, err := os.MkdirTemp("", "reasonix-plugin-zip-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	files := 0
	var total int64
	root := ""
	rooted := true
	for _, entry := range zr.File {
		// Only regular files are recreated. A symlink inside an archive from
		// somewhere else is a path onto this machine, not package content.
		if entry.FileInfo().IsDir() || !entry.Mode().IsRegular() {
			continue
		}
		rel := filepath.FromSlash(entry.Name)
		if !filepath.IsLocal(rel) {
			cleanup()
			return "", func() {}, newErr(ErrUnsupportedKind, "zip entry %q escapes the extraction directory", entry.Name)
		}
		head, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		if root == "" {
			root = head
		}
		if head != root || !strings.Contains(filepath.ToSlash(rel), "/") {
			rooted = false
		}
		files++
		if files > tarballFileLimit {
			cleanup()
			return "", func() {}, newErr(ErrSourceUnreadable, "zip holds more than %d files", tarballFileLimit)
		}
		dest := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			cleanup()
			return "", func() {}, newErr(ErrSourceUnreadable, "create %s: %v", filepath.Dir(rel), err)
		}
		rc, err := entry.Open()
		if err != nil {
			cleanup()
			return "", func() {}, newErr(ErrSourceUnreadable, "read %s: %v", entry.Name, err)
		}
		written, err := writeTarEntry(dest, rc, int64(entry.Mode().Perm()))
		rc.Close()
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		total += written
		if total > tarballTotalLimit {
			cleanup()
			return "", func() {}, newErr(ErrSourceUnreadable, "zip expands past %d bytes", tarballTotalLimit)
		}
	}
	if files == 0 {
		cleanup()
		return "", func() {}, newErr(ErrSourceUnreadable, "zip is empty")
	}
	if rooted && root != "" {
		return filepath.Join(dir, root), cleanup, nil
	}
	return dir, cleanup, nil
}

// planZipPlugin plans from the archive's unpacked contents while keeping the
// archive itself as the action's source, so apply unpacks the same file the
// user approved rather than a temporary directory that is already gone.
func (t *Tool) planZipPlugin(req request, path string) ([]action, []string, error) {
	if req.Kind != "auto" && req.Kind != "plugin" {
		return nil, nil, newErr(ErrUnsupportedKind, "%s is a plugin package archive, not a %s source", filepath.Base(path), req.Kind)
	}
	root, cleanup, err := unpackPluginZip(path)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	act, warnings, err := t.localPluginPackageAction(req, root)
	if err != nil {
		return nil, warnings, err
	}
	act.Source = path
	return []action{act}, warnings, nil
}

func (t *Tool) preparePluginZip(path, mode string) (string, string, func(), error) {
	// A link is a live pointer at a directory the author keeps editing. An
	// archive has no such directory — linking one would point at a temporary
	// copy this call is about to delete.
	if mode == "link" {
		return "", "", func() {}, newErr(ErrUnsupportedKind, "a .zip cannot be linked; install it without link mode")
	}
	root, cleanup, err := unpackPluginZip(path)
	if err != nil {
		return "", "", func() {}, err
	}
	return root, "", cleanup, nil
}
