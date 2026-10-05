package trustedstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Digest names an object by content: "sha256:" followed by 64 hex digits.
type Digest string

const digestPrefix = "sha256:"

// DigestOf is the name an object with these bytes has in any store.
func DigestOf(data []byte) Digest {
	sum := sha256.Sum256(data)
	return Digest(digestPrefix + hex.EncodeToString(sum[:]))
}

func (d Digest) hex() (string, bool) {
	h, ok := strings.CutPrefix(string(d), digestPrefix)
	if !ok || len(h) != sha256.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", false
	}
	return h, true
}

func (s *Store) objectPath(d Digest) (string, error) {
	h, ok := d.hex()
	if !ok {
		return "", fmt.Errorf("%w: malformed digest %q", ErrTampered, d)
	}
	return filepath.Join(s.root, "objects", h[:2], h), nil
}

// PutObject stores data durably and returns its digest. Storing bytes that are
// already present is a no-op, except that an existing file whose bytes do not
// match its name is reported rather than silently trusted.
func (s *Store) PutObject(data []byte) (Digest, error) { return s.putObject(data, true) }

// PutIndexObject stores data without waiting for it to reach the disk. It is
// for objects no record seals, such as snapshot tree nodes: a crash may lose
// one, and a reader then finds it missing rather than trusting a torn file.
func (s *Store) PutIndexObject(data []byte) (Digest, error) { return s.putObject(data, false) }

func (s *Store) putObject(data []byte, durable bool) (Digest, error) {
	d := DigestOf(data)
	path, err := s.objectPath(d)
	if err != nil {
		return "", err
	}
	if exists, err := checkObjectFile(path, data, d); exists || err != nil {
		if err != nil {
			return "", err
		}
		return d, nil
	}
	// Linking publishes immutable bytes without replacing another writer's object.
	if err := writeFilePublished(path, data, durable, os.Link); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if exists, checkErr := checkObjectFile(path, data, d); checkErr != nil {
			return "", checkErr
		} else if !exists {
			return "", err
		}
	}
	return d, nil
}

func checkObjectFile(path string, data []byte, d Digest) (bool, error) {
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	if !bytes.Equal(existing, data) {
		return false, fmt.Errorf("%w: object %s does not hold the bytes it is named for", ErrTampered, d)
	}
	return true, nil
}

// Object returns the bytes named by d after checking they still hash to d.
func (s *Store) Object(d Digest) ([]byte, error) {
	path, err := s.objectPath(d)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: object %s", ErrNotFound, d)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	if DigestOf(data) != d {
		return nil, fmt.Errorf("%w: object %s does not hold the bytes it is named for", ErrTampered, d)
	}
	return data, nil
}

// writeFileAtomic writes through a temporary sibling and renames it into place,
// so a reader sees the previous file or the complete new one, never a prefix.
func writeFileAtomic(path string, data []byte, durable bool) error {
	return writeFilePublished(path, data, durable, os.Rename)
}

func writeFilePublished(path string, data []byte, durable bool, publish func(string, string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, werr := tmp.Write(data)
	var serr error
	if durable {
		serr = tmp.Sync()
	}
	cerr := tmp.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		return fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	if err := publish(name, path); err != nil {
		return fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	return nil
}
