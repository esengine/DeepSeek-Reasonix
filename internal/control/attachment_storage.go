package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ownedAttachmentFile struct {
	*os.File
	directory *os.Root
	name      string
	rel       string
	complete  bool
}

// Hold the directory handle through cleanup: re-resolving its path after a
// rename or symlink replacement could remove a different file.
func createOwnedAttachmentFile(base, ext string) (*ownedAttachmentFile, error) {
	directory, err := openAttachmentRootIn(base)
	if err != nil {
		return nil, err
	}

	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	for range maxAttachmentCreateAttempts {
		name := filepath.Base(attachmentPath(ext))
		f, err := directory.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			_ = directory.Close()
			return nil, err
		}
		return &ownedAttachmentFile{File: f, directory: directory, name: name,
			rel: filepath.Join(".reasonix", "attachments", name)}, nil
	}
	_ = directory.Close()
	return nil, fmt.Errorf("create unique attachment path")
}

func (f *ownedAttachmentFile) finish() error {
	if err := f.Close(); err != nil {
		return err
	}
	f.complete = true
	return nil
}

func (f *ownedAttachmentFile) cleanup() {
	_ = f.Close()
	if !f.complete {
		_ = f.directory.Remove(f.name)
	}
	_ = f.directory.Close()
}

func openAttachmentRootIn(base string) (*os.Root, error) {
	if strings.TrimSpace(base) == "" {
		base = "."
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	workspace, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer workspace.Close()
	for _, name := range []string{".reasonix", filepath.Join(".reasonix", "attachments")} {
		if err := workspace.Mkdir(name, 0o755); err != nil && !os.IsExist(err) {
			return nil, err
		}
		info, err := workspace.Lstat(name)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("attachment directory must be a directory without symlinks")
		}
	}
	return workspace.OpenRoot(filepath.Join(".reasonix", "attachments"))
}

// RemoveStoredAttachment removes a direct attachment reference without following
// a replaced workspace path outside its root.
func RemoveStoredAttachment(base, ref string) error {
	clean := filepath.Clean(filepath.FromSlash(ref))
	if filepath.Dir(clean) != filepath.Join(".reasonix", "attachments") {
		return fmt.Errorf("attachment path is outside .reasonix/attachments")
	}
	workspace, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer workspace.Close()
	directory, err := workspace.OpenRoot(filepath.Join(".reasonix", "attachments"))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Remove(filepath.Base(clean))
}
