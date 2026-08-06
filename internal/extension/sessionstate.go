package extension

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"reasonix/internal/fileutil"
)

// SessionExtensionsVersion is the on-disk version of the sidecar state file.
const SessionExtensionsVersion = 1

// SessionExtensionsFileSuffix is appended to the session JSONL path to form
// the per-session extension state path. The store NEVER reads or writes the
// session JSONL itself; old readers simply ignore this additive file.
const SessionExtensionsFileSuffix = ".extensions.json"

// MaxPluginStateBytes caps one plugin's state blob at 1 MiB. Writes beyond
// the cap fail — truncating would silently corrupt the extension's state.
const MaxPluginStateBytes = 1 << 20

// ErrPluginStateTooLarge rejects a per-plugin blob beyond MaxPluginStateBytes.
var ErrPluginStateTooLarge = errors.New("extension: plugin session state exceeds 1 MiB")

// sessionExtensionsFile is the JSON shape at
// <sessionPath>.extensions.json: a versioned map of per-plugin blobs.
type sessionExtensionsFile struct {
	Version int                        `json:"version"`
	Plugins map[string]json.RawMessage `json:"plugins"`
}

// SessionExtensions is the per-session state store extension sidecars use to
// persist their plugin-scoped state across reloads. It lives beside the
// session file at <sessionPath>.extensions.json and is deliberately separate:
// the session JSONL keeps its exact byte stability, and readers that predate
// extensions ignore the new file.
type SessionExtensions struct {
	path    string
	plugins map[string]json.RawMessage
}

// SessionExtensionsPath returns the store path for one session file, or ""
// when the session has no path yet.
func SessionExtensionsPath(sessionPath string) string {
	trimmed := strings.TrimSpace(sessionPath)
	if trimmed == "" {
		return ""
	}
	return trimmed + SessionExtensionsFileSuffix
}

// LoadSessionExtensions reads the store for sessionPath. A missing or
// corrupt file yields an empty store and no error — extension state must
// never crash a session load — and only I/O failures beyond those surface.
func LoadSessionExtensions(sessionPath string) (*SessionExtensions, error) {
	store := &SessionExtensions{
		path:    SessionExtensionsPath(sessionPath),
		plugins: make(map[string]json.RawMessage),
	}
	if store.path == "" {
		return store, nil
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, fmt.Errorf("extension: load session extensions: %w", err)
	}
	var file sessionExtensionsFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		// Corrupt state must not fail the session: start empty. The next
		// Save atomically replaces the bad file.
		return store, nil
	}
	for pluginID, blob := range file.Plugins {
		if len(blob) > MaxPluginStateBytes {
			// An over-cap blob is dropped, not truncated: partial JSON is
			// worse than none.
			continue
		}
		store.plugins[pluginID] = append(json.RawMessage(nil), blob...)
	}
	return store, nil
}

// Path returns the store's file path.
func (s *SessionExtensions) Path() string { return s.path }

// Get returns the stored blob for pluginID.
func (s *SessionExtensions) Get(pluginID string) (json.RawMessage, bool) {
	blob, ok := s.plugins[pluginID]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), blob...), true
}

// Set stages pluginID's blob for the next Save. The blob must be valid JSON
// within the 1 MiB cap; over-cap writes fail with ErrPluginStateTooLarge and
// are never truncated.
func (s *SessionExtensions) Set(pluginID string, blob json.RawMessage) error {
	if strings.TrimSpace(pluginID) == "" {
		return errors.New("extension: plugin ID is required for session state")
	}
	if len(blob) > MaxPluginStateBytes {
		return fmt.Errorf("%w (plugin %q: %d bytes)", ErrPluginStateTooLarge, pluginID, len(blob))
	}
	trimmed := bytes.TrimSpace(blob)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return fmt.Errorf("extension: plugin %q session state must be valid JSON", pluginID)
	}
	s.plugins[pluginID] = append(json.RawMessage(nil), trimmed...)
	return nil
}

// Delete removes pluginID's blob. A missing ID is a no-op.
func (s *SessionExtensions) Delete(pluginID string) {
	delete(s.plugins, pluginID)
}

// Plugins returns the IDs with staged state, sorted for determinism.
func (s *SessionExtensions) Plugins() []string {
	out := make([]string, 0, len(s.plugins))
	for pluginID := range s.plugins {
		out = append(out, pluginID)
	}
	sort.Strings(out)
	return out
}

// Save atomically persists the staged blobs (tmpfile + rename via
// fileutil.AtomicWriteFile), so a crash mid-save never leaves a half-written
// store. It writes only the .extensions.json sibling — never the session
// JSONL itself.
func (s *SessionExtensions) Save() error {
	if s.path == "" {
		return errors.New("extension: session extensions store has no path")
	}
	file := sessionExtensionsFile{Version: SessionExtensionsVersion, Plugins: s.plugins}
	data, err := json.Marshal(file)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.AtomicWriteFile(s.path, data, 0o644)
}
