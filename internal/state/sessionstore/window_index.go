package sessionstore

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/store"
)

const windowHeaderBytes = 128
const windowEntryBytes = 56

type storedWindowIndex struct {
	file           *os.File
	count          int
	size, revision int64
	digest         string
	readBytes      int
	entries        map[int]DisplayIndexEntry
}

func writeWindowIndex(path string, index *SessionDisplayIndex) error {
	if index.PageMetadata != 1 {
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".window-index-*")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	w := bufio.NewWriter(f)
	header := make([]byte, windowHeaderBytes)
	copy(header, "RXWIN001")
	binary.LittleEndian.PutUint64(header[8:], uint64(index.MessageCount))
	binary.LittleEndian.PutUint64(header[16:], uint64(index.TranscriptSize))
	binary.LittleEndian.PutUint64(header[24:], uint64(index.Revision))
	if index.RevisionKnown {
		header[32] = 1
	}
	copy(header[40:104], index.ContentDigest)
	binary.LittleEndian.PutUint32(header[104:], crc32.ChecksumIEEE(header[:104]))
	if _, err = w.Write(header); err != nil {
		return err
	}
	for _, entry := range index.Entries {
		var data [windowEntryBytes]byte
		values := []int64{entry.Offset, entry.Length, entry.WorkMs, int64(entry.CallMessage), int64(entry.CallOrdinal), 0}
		if entry.CallKnown {
			values[5] = 1
		}
		for i, value := range values {
			binary.LittleEndian.PutUint64(data[i*8:], uint64(value))
		}
		binary.LittleEndian.PutUint32(data[48:], crc32.ChecksumIEEE(data[:48]))
		if _, err = w.Write(data[:]); err != nil {
			return err
		}
	}
	if err = w.Flush(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return fileutil.ReplaceFile(f.Name(), path)
}

func openWindowIndex(path string) (*storedWindowIndex, error) {
	f, err := os.Open(store.SessionWindowIndex(path))
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*storedWindowIndex, error) { f.Close(); return nil, err }
	var header [windowHeaderBytes]byte
	if _, err = f.ReadAt(header[:], 0); err != nil {
		return fail(err)
	}
	if string(header[:8]) != "RXWIN001" || binary.LittleEndian.Uint32(header[104:]) != crc32.ChecksumIEEE(header[:104]) {
		return fail(ErrSessionDisplayReadModelDamaged)
	}
	count := binary.LittleEndian.Uint64(header[8:])
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if info.Size() < windowHeaderBytes || count > uint64(^uint(0)>>1) || count > uint64((info.Size()-windowHeaderBytes)/windowEntryBytes) || info.Size() != windowHeaderBytes+int64(count)*windowEntryBytes {
		return fail(ErrSessionDisplayReadModelDamaged)
	}
	idx := &storedWindowIndex{file: f, count: int(count), size: int64(binary.LittleEndian.Uint64(header[16:])), revision: int64(binary.LittleEndian.Uint64(header[24:])), digest: string(header[40:104]), readBytes: windowHeaderBytes, entries: map[int]DisplayIndexEntry{}}
	revision, digest, err := sessionContentRevision(path)
	if err != nil {
		return fail(err)
	}
	if digest != "" && (header[32] != 1 || idx.revision != revision || idx.digest != digest) {
		return fail(ErrHistorySnapshotChanged)
	}
	transcript, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	if transcript.Size() != idx.size || info.ModTime().Before(SessionContentModTime(path)) {
		return fail(ErrHistorySnapshotChanged)
	}
	return idx, nil
}

func (idx *storedWindowIndex) entry(at int) (DisplayIndexEntry, error) {
	if entry, ok := idx.entries[at]; ok {
		return entry, nil
	}
	if at < 0 || at >= idx.count {
		return DisplayIndexEntry{}, ErrHistoryPageRange
	}
	var data [windowEntryBytes]byte
	if _, err := idx.file.ReadAt(data[:], windowHeaderBytes+int64(at)*windowEntryBytes); err != nil {
		return DisplayIndexEntry{}, err
	}
	idx.readBytes += windowEntryBytes
	if binary.LittleEndian.Uint32(data[48:]) != crc32.ChecksumIEEE(data[:48]) {
		return DisplayIndexEntry{}, ErrSessionDisplayReadModelDamaged
	}
	v := make([]int64, 6)
	for i := range v {
		v[i] = int64(binary.LittleEndian.Uint64(data[i*8:]))
	}
	e := DisplayIndexEntry{Index: at, Offset: v[0], Length: v[1], WorkMs: v[2], CallMessage: int(v[3]), CallOrdinal: int(v[4]), CallKnown: v[5] == 1}
	if e.Offset < 0 || e.Length < 1 || e.Length > sessionDisplayIndexMaxLineBytes || e.Offset > idx.size-e.Length || v[5] < 0 || v[5] > 1 || (e.CallKnown && (e.CallMessage < 0 || e.CallMessage >= at || e.CallOrdinal < 0)) {
		return e, fmt.Errorf("invalid stored window entry: %w", ErrSessionDisplayReadModelDamaged)
	}
	idx.entries[at] = e
	return e, nil
}

func currentWindowIndex(path string) (*storedWindowIndex, error) {
	index, err := openWindowIndex(path)
	if err == nil {
		return index, nil
	}
	legacy, legacyErr := currentPageIndex(path)
	if legacyErr != nil {
		if err := repairSessionDisplayReadModelUnlocked(path); err != nil {
			return nil, err
		}
	} else if err := writeWindowIndex(store.SessionWindowIndex(path), legacy); err != nil {
		return nil, err
	}
	return openWindowIndex(path)
}
