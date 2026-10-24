package projectiondb

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// integrityCertificates owns reusable audits for unchanged disposable files.
// No connection or file descriptor is retained, so rebuild/rename and Windows
// deletion retain their existing lifecycle. A WAL change revokes the proof even
// before a checkpoint changes the main database.
type integrityCertificates struct {
	mu      sync.Mutex
	entries map[integrityCertificateKey]integrityCertificate
}

type integrityCertificateKey struct {
	path  string
	quick bool
}
type integrityCertificate struct {
	main    os.FileInfo
	header  string
	checked time.Time
}

var reusableIntegrityChecks integrityCertificates

func integrityFileState(path string) (integrityCertificate, bool) {
	file, err := os.Open(path)
	if err != nil {
		return integrityCertificate{}, false
	}
	defer file.Close()
	main, err := file.Stat()
	if err != nil || !main.Mode().IsRegular() {
		return integrityCertificate{}, false
	}
	wal, err := os.Stat(path + "-wal")
	if err != nil && !os.IsNotExist(err) {
		return integrityCertificate{}, false
	}
	// A zero-length WAL contains no data. Read-only connections may create
	// and remove one without changing any database page.
	if wal != nil {
		// A writer may reuse WAL frames without changing its length, and some
		// filesystems defer mtime publication until the writer closes. Do not
		// cache an audit with outstanding WAL data.
		if wal.Size() != 0 {
			return integrityCertificate{}, false
		}
	}
	var header [100]byte
	n, err := file.ReadAt(header[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return integrityCertificate{}, false
	}
	// SQLite's header includes the change counter and schema cookie. Include
	// them in addition to metadata, so a checkpoint on coarse-mtime platforms
	// cannot reuse the proof for an earlier database state.
	return integrityCertificate{main: main, header: string(header[:n])}, true
}

func sameIntegrityFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (a integrityCertificate) same(b integrityCertificate) bool {
	return sameIntegrityFile(a.main, b.main) && a.header == b.header
}

func (c *integrityCertificates) check(path string, quick bool, validate func() error) error {
	opened, known := integrityFileState(path)
	return c.checkOpened(path, quick, opened, known, validate)
}

func (c *integrityCertificates) checkOpened(path string, quick bool, opened integrityCertificate, openedKnown bool, validate func() error) error {
	key := integrityCertificateKey{path, quick}
	before, known := integrityFileState(path)
	// A path swapped while SQLite was opening must not certify the new file
	// using an audit of the old connection's inode.
	known = known && openedKnown && opened.same(before)
	c.mu.Lock()
	previous, exists := c.entries[key]
	if known && exists && before.same(previous) {
		c.mu.Unlock()
		return nil
	}
	delete(c.entries, key)
	c.mu.Unlock()
	// Do not hold a process-wide mutex across disk I/O or SQLite validation.
	// Simultaneous first readers may audit independently; neither waits for an
	// unrelated session's audit, and only a stable successful audit is retained.
	if err := validate(); err != nil {
		return err
	}
	after, stable := integrityFileState(path)
	if !known || !stable || !before.same(after) {
		return nil
	}
	after.checked = time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[integrityCertificateKey]integrityCertificate)
	}
	if len(c.entries) >= 128 {
		var oldest integrityCertificateKey
		var at time.Time
		for candidate, certificate := range c.entries {
			if at.IsZero() || certificate.checked.Before(at) {
				oldest, at = candidate, certificate.checked
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = after
	return nil
}
