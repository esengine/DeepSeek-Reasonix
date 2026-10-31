package teamroster

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"reasonix/internal/base/fileutil"
)

// AvatarDir holds the photos agents wear, each named by its content.
const AvatarDir = "avatars"

// MaxAvatarBytes bounds one photo. The window crops and compresses a picture
// to fit well inside it before sending.
const MaxAvatarBytes = 200 << 10

var avatarFile = regexp.MustCompile(`^[0-9a-f]{16}\.(png|jpg|gif|webp)$`)

// avatarKind names a picture by its leading bytes, never by what a caller
// says it is: anything that is not one of these four is not stored.
func avatarKind(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return ".png"
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return ".jpg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return ".gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return ".webp"
	}
	return ""
}

// SaveAvatar stores a photo and returns the name an Avatar carries for it.
// The same picture saved twice is one file.
func (s Store) SaveAvatar(data []byte) (string, error) {
	if len(data) > MaxAvatarBytes {
		return "", fieldErr("avatar.image", ErrTooLong)
	}
	ext := avatarKind(data)
	if ext == "" {
		return "", fieldErr("avatar.image", ErrInvalid)
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:8]) + ext
	path, err := s.AvatarPath(name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return name, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return name, fileutil.AtomicWriteFile(path, data, 0o600)
}

// AvatarPath is where a stored photo lives. A name that is not one SaveAvatar
// could have returned is refused, so no name reaches outside the folder.
func (s Store) AvatarPath(name string) (string, error) {
	if s.Dir == "" {
		return "", errNoDir
	}
	if !avatarFile.MatchString(name) {
		return "", fieldErr("avatar.image", ErrInvalid)
	}
	return filepath.Join(s.Dir, AvatarDir, name), nil
}

// PruneAvatars removes the photos no agent in r wears.
func (s Store) PruneAvatars(r Roster) error {
	worn := map[string]bool{}
	for _, a := range r.Agents {
		worn[a.Avatar.Image] = true
	}
	entries, err := os.ReadDir(filepath.Join(s.Dir, AvatarDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if avatarFile.MatchString(e.Name()) && !worn[e.Name()] {
			errs = append(errs, fileutil.Remove(filepath.Join(s.Dir, AvatarDir, e.Name())))
		}
	}
	return errors.Join(errs...)
}
