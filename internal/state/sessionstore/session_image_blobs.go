package sessionstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// Image payloads are held beside the event log, one file per distinct data URL
// named by its SHA-256, so the log every save replays grows with text alone.
// The .jsonl checkpoint keeps them inline: it is what export and paging read,
// and the copy a missing blob is restored from.
const (
	sessionImageBlobScheme = "sha256:"
	// sessionImageBlobMaxBytes bounds one read; no provider takes an image
	// anywhere near it, so a larger file is damage, not an attachment.
	sessionImageBlobMaxBytes = int64(64 << 20)
	// sessionImageBlobTempMaxAge spares a temp file a reader restoring a blob
	// outside the save lock may still be about to rename.
	sessionImageBlobTempMaxAge = time.Hour
)

var sessionImageBlobName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ErrSessionImagesUnavailable identifies a save that cannot proceed because
// an image the transcript holds has no readable stored copy: the blob could
// not be restored, or the checkpoint still holds the only copy.
var ErrSessionImagesUnavailable = errors.New("session image payload unavailable")

// sessionImageSlot is one entry of a message's Images as written: a stored
// blob, read back on replay, or a URL kept as written. A blob that cannot be
// read replays as a provider.UnavailableImage naming the same slot.
type sessionImageSlot struct {
	Blob string `json:"blob,omitempty"`
	URL  string `json:"url,omitempty"`
}

// sessionEventImages restores the images of the record's message at Message,
// which the log stores with none.
type sessionEventImages struct {
	Message int                `json:"message"`
	Slots   []sessionImageSlot `json:"slots"`
}

func isDataURL(url string) bool { return strings.HasPrefix(url, "data:") }

func carriesStoredImage(m provider.Message) bool {
	return len(m.UnavailableImages) > 0 || slices.ContainsFunc(m.Images, isDataURL)
}

func sessionImageKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return sessionImageBlobScheme + hex.EncodeToString(sum[:])
}

// sessionImageBlobPath is the only way a key becomes a path: anything but a
// bare lowercase SHA-256 names nothing.
func sessionImageBlobPath(dir, key string) (string, bool) {
	name, ok := strings.CutPrefix(key, sessionImageBlobScheme)
	if !ok || dir == "" || !sessionImageBlobName.MatchString(name) || filepath.Base(name) != name {
		return "", false
	}
	return filepath.Join(dir, name), true
}

// messageImageSlots lays a message's images out as written: unavailable ones
// back at their positions, the readable ones filling the rest in order.
func messageImageSlots(m provider.Message) []sessionImageSlot {
	slots := make([]sessionImageSlot, len(m.Images)+len(m.UnavailableImages))
	taken := make([]bool, len(slots))
	var stray []provider.UnavailableImage
	for _, u := range m.UnavailableImages {
		if u.Index >= 0 && u.Index < len(slots) && !taken[u.Index] {
			slots[u.Index], taken[u.Index] = sessionImageSlot{Blob: u.Ref}, true
			continue
		}
		stray = append(stray, u)
	}
	next := 0
	fill := func(slot sessionImageSlot) {
		for taken[next] {
			next++
		}
		slots[next], taken[next] = slot, true
	}
	for _, url := range m.Images {
		fill(sessionImageSlot{URL: url})
	}
	for _, u := range stray {
		fill(sessionImageSlot{Blob: u.Ref})
	}
	return slots
}

// externalizeSessionImages writes every data-URL image in msgs to the session's
// blob directory and returns msgs with those messages' images moved into the
// side table. Each blob is fsynced before this returns, so the record naming it
// can never reach disk first. msgs itself is not modified.
func externalizeSessionImages(sessionPath string, msgs []provider.Message) ([]provider.Message, []sessionEventImages, error) {
	var out []provider.Message
	var refs []sessionEventImages
	dir := store.SessionBlobsDir(sessionPath)
	for i, m := range msgs {
		if !carriesStoredImage(m) {
			continue
		}
		if dir == "" {
			return nil, nil, fmt.Errorf("session image blobs: empty session path")
		}
		if out == nil {
			out = slices.Clone(msgs)
		}
		slots := messageImageSlots(m)
		for j, slot := range slots {
			if !isDataURL(slot.URL) {
				continue
			}
			key, err := writeSessionImageBlob(dir, slot.URL)
			if err != nil {
				return nil, nil, err
			}
			slots[j] = sessionImageSlot{Blob: key}
		}
		out[i].Images, out[i].UnavailableImages = nil, nil
		refs = append(refs, sessionEventImages{Message: i, Slots: slots})
	}
	if out == nil {
		return msgs, nil, nil
	}
	return out, refs, nil
}

// writeSessionImageBlob publishes url under its hash unless a file there
// already hashes to it; one that does not is damage and is replaced.
func writeSessionImageBlob(dir, url string) (string, error) {
	key := sessionImageKey(url)
	if _, ok := readSessionImageBlob(dir, key); ok {
		return key, nil
	}
	path, ok := sessionImageBlobPath(dir, key)
	if !ok {
		return "", fmt.Errorf("session image blobs: empty blob directory")
	}
	_, statErr := os.Stat(dir)
	if err := fileutil.AtomicWriteFileStrict(path, []byte(url), 0o600); err != nil {
		return "", fmt.Errorf("write session image blob: %w", err)
	}
	if os.IsNotExist(statErr) {
		syncSessionDir(filepath.Dir(dir))
	}
	return key, nil
}

// syncSessionDir makes a new blob directory's entry durable. Best effort, like
// the parent sync AtomicWriteFileStrict performs: some platforms cannot.
func syncSessionDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// readSessionImageBlob returns the data URL a key names, or false when the key
// is malformed or the blob is missing, oversized or does not hash to its name.
func readSessionImageBlob(dir, key string) (string, bool) {
	path, ok := sessionImageBlobPath(dir, key)
	if !ok {
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, sessionImageBlobMaxBytes+1))
	if err != nil || int64(len(b)) > sessionImageBlobMaxBytes || sessionImageKey(string(b)) != key {
		return "", false
	}
	return string(b), true
}

// resolveSessionImages puts each referenced image back on its message. A blob
// that cannot be read becomes an UnavailableImage: the transcript keeps its
// identity and position, and nothing about it is written into the text.
func resolveSessionImages(logPath string, msgs []provider.Message, refs []sessionEventImages) error {
	dir := store.SessionBlobsDirForEventLog(logPath)
	for _, ref := range refs {
		if ref.Message < 0 || ref.Message >= len(msgs) {
			return fmt.Errorf("image reference to message %d outside its record", ref.Message)
		}
		m := &msgs[ref.Message]
		m.Images, m.UnavailableImages = nil, nil
		for j, slot := range ref.Slots {
			if slot.Blob == "" {
				m.Images = append(m.Images, slot.URL)
				continue
			}
			if url, ok := readSessionImageBlob(dir, slot.Blob); ok {
				m.Images = append(m.Images, url)
				continue
			}
			m.UnavailableImages = append(m.UnavailableImages, provider.UnavailableImage{Index: j, Ref: slot.Blob})
		}
	}
	return nil
}

func sessionImageSlotCount(refs []sessionEventImages) int {
	n := 0
	for _, ref := range refs {
		n += len(ref.Slots)
	}
	return n
}

func unavailableImageKeys(msgs []provider.Message) map[string]bool {
	var keys map[string]bool
	for _, m := range msgs {
		for _, u := range m.UnavailableImages {
			if keys == nil {
				keys = map[string]bool{}
			}
			keys[u.Ref] = true
		}
	}
	return keys
}

// restoreUnavailableImage puts url back where the image named by ref sat.
func restoreUnavailableImage(m *provider.Message, ref, url string) {
	for {
		at := slices.IndexFunc(m.UnavailableImages, func(u provider.UnavailableImage) bool { return u.Ref == ref })
		if at < 0 {
			return
		}
		u := m.UnavailableImages[at]
		m.UnavailableImages = slices.Delete(m.UnavailableImages, at, at+1)
		pos := u.Index
		for _, other := range m.UnavailableImages {
			if other.Index < u.Index {
				pos--
			}
		}
		pos = max(0, min(pos, len(m.Images)))
		m.Images = slices.Insert(m.Images, pos, url)
		if len(m.UnavailableImages) == 0 {
			m.UnavailableImages = nil
		}
	}
}

// backfillUnavailableImages restores images whose blob is gone from the copy
// the checkpoint still inlines, and republishes the blob so the next replay
// needs no checkpoint. A blob that cannot be written still restores the image
// in memory: the bytes are in hand, and the next save retries the write.
func backfillUnavailableImages(logPath string, msgs []provider.Message) {
	wanted := unavailableImageKeys(msgs)
	if len(wanted) == 0 {
		return
	}
	found := checkpointImages(store.SessionPathForEventLog(logPath), wanted)
	dir := store.SessionBlobsDirForEventLog(logPath)
	for key, url := range found {
		if _, err := writeSessionImageBlob(dir, url); err != nil {
			slog.Warn("session: could not restore image blob from checkpoint", "log", logPath, "err", err)
		}
		for i := range msgs {
			restoreUnavailableImage(&msgs[i], key, url)
		}
	}
	if missing := len(wanted) - len(found); missing > 0 {
		slog.Warn("session: image payloads unavailable", "log", logPath, "missing", missing)
	}
}

// checkpointImages finds the data URLs in the checkpoint whose keys are wanted.
// It reads raw bytes rather than decoding messages, so a copy past a torn or
// foreign line is still found: encoding/json writes a data URL verbatim.
func checkpointImages(checkpoint string, wanted map[string]bool) map[string]string {
	found := map[string]string{}
	info, err := os.Stat(checkpoint)
	if err != nil || !info.Mode().IsRegular() || info.Size() > sessionEventReplayMaxBytes {
		return found
	}
	b, err := os.ReadFile(checkpoint)
	if err != nil {
		return found
	}
	marker := []byte(`"data:`)
	for len(found) < len(wanted) {
		at := bytes.Index(b, marker)
		if at < 0 {
			break
		}
		b = b[at+1:]
		end := bytes.IndexByte(b, '"')
		if end < 0 {
			break
		}
		raw := b[:end]
		b = b[end+1:]
		url := string(raw)
		if bytes.IndexByte(raw, '\\') >= 0 {
			quoted := append(append([]byte{'"'}, raw...), '"')
			if json.Unmarshal(quoted, &url) != nil {
				continue
			}
		}
		if key := sessionImageKey(url); wanted[key] {
			found[key] = url
		}
	}
	return found
}

// refuseDegradedCheckpointRewrite keeps a transcript missing an image from
// replacing a checkpoint that still inlines it: that file would then be the
// only copy, and the rewrite would destroy it.
func refuseDegradedCheckpointRewrite(path string, msgs []provider.Message) error {
	wanted := unavailableImageKeys(msgs)
	if len(wanted) == 0 {
		return nil
	}
	if held := len(checkpointImages(path, wanted)); held > 0 {
		return fmt.Errorf("%w: the checkpoint holds the only copy of %d image(s); it was left unchanged",
			ErrSessionImagesUnavailable, held)
	}
	return nil
}

// loadSnapshotBaseline loads the transcript a save is judged against. When it
// is missing images this runtime still holds, their blobs are written back
// first, so a save is not refused as a divergence the store itself caused.
// The reverse, a runtime missing images the store still has, is refused under
// ErrSessionImagesUnavailable: saving it would drop them.
func loadSnapshotBaseline(path string, next []provider.Message) (*Session, error) {
	current, err := loadSessionUnlocked(path)
	if err != nil || current == nil {
		return current, err
	}
	stored := current.Snapshot()
	for i, m := range next {
		if len(m.UnavailableImages) > 0 && i < len(stored) && holdsAnyImage(stored[i], m.UnavailableImages) {
			return nil, fmt.Errorf("%w: this runtime lost images the stored session still holds; reopen it",
				ErrSessionImagesUnavailable)
		}
	}
	dir := store.SessionBlobsDir(path)
	restored := false
	for i, m := range stored {
		if len(m.UnavailableImages) == 0 || i >= len(next) {
			continue
		}
		for _, url := range next[i].Images {
			if !isDataURL(url) || !slices.ContainsFunc(m.UnavailableImages, func(u provider.UnavailableImage) bool {
				return u.Ref == sessionImageKey(url)
			}) {
				continue
			}
			if _, err := writeSessionImageBlob(dir, url); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrSessionImagesUnavailable, err)
			}
			restored = true
		}
	}
	if !restored {
		return current, nil
	}
	return loadSessionUnlocked(path)
}

func holdsAnyImage(m provider.Message, lost []provider.UnavailableImage) bool {
	for _, url := range m.Images {
		if isDataURL(url) && slices.ContainsFunc(lost, func(u provider.UnavailableImage) bool { return u.Ref == sessionImageKey(url) }) {
			return true
		}
	}
	return false
}

var sessionImageKeyPattern = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

// salvagedImageKeys is every blob key the session's salvage and recovery
// sidecars name: records a repair set aside still point at their images, and
// the only way back to those images is the blob. ok is false when a sidecar
// could not be read whole, and then nothing may be swept.
func salvagedImageKeys(sessionPath string) (map[string]bool, bool) {
	keys := map[string]bool{}
	for _, sidecar := range []string{
		store.SessionEventLogDamaged(sessionPath),
		store.SessionSuperseded(sessionPath),
		store.SessionContext(sessionPath),
	} {
		info, err := os.Stat(sidecar)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.Size() > sessionEventReplayMaxBytes {
			return nil, false
		}
		b, err := os.ReadFile(sidecar)
		if err != nil {
			return nil, false
		}
		for _, key := range sessionImageKeyPattern.FindAll(b, -1) {
			keys[string(key)] = true
		}
	}
	return keys, true
}

// sweepSessionImageBlobs removes blobs that neither refs nor a salvage sidecar
// names. Callers run it only after the log naming exactly refs is published.
func sweepSessionImageBlobs(sessionPath string, refs []sessionEventImages) {
	dir := store.SessionBlobsDir(sessionPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keep, ok := salvagedImageKeys(sessionPath)
	if !ok {
		return
	}
	for _, ref := range refs {
		for _, slot := range ref.Slots {
			if slot.Blob != "" {
				keep[slot.Blob] = true
			}
		}
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() {
			continue
		}
		if path, ok := sessionImageBlobPath(dir, sessionImageBlobScheme+name); ok {
			if !keep[sessionImageBlobScheme+name] {
				_ = os.Remove(path)
			}
			continue
		}
		if strings.HasPrefix(name, ".atomic-") && strings.HasSuffix(name, ".tmp") && filepath.Base(name) == name {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > sessionImageBlobTempMaxAge {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
	}
}

// inlineImageBytes is how much of a transcript's encoded size is data-URL
// images, which a folded log holds outside itself.
func inlineImageBytes(msgs []provider.Message) int64 {
	var n int64
	for _, m := range msgs {
		for _, url := range m.Images {
			if isDataURL(url) {
				n += int64(len(url))
			}
		}
	}
	return n
}
