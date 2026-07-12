// Package-internal cache for MCP handshake results. The handshake (initialize +
// listTools, plus optional listPrompts/listResources) costs hundreds of ms to a
// few seconds per server on cold start. We persist the tool schema +
// capabilities under the user cache dir keyed by a fingerprint of the load-
// bearing Spec fields, so the next launch can register tools optimistically
// without waiting for the network/subprocess. Caching is purely an
// optimisation: any failure (missing dir, bad JSON, hash mismatch) silently
// degrades to a fresh handshake.
package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/tool"
)

// cacheableToolsOf extracts the persistable subset of remote tools so Start()
// can hand them to SaveCachedSchema. Non-remote tools are skipped — Start
// only feeds remote ones at the call site, but the type-assert is defensive.
func cacheableToolsOf(tools []tool.Tool) []CachedTool {
	out := make([]CachedTool, 0, len(tools))
	for _, t := range tools {
		rt, ok := t.(*remoteTool)
		if !ok {
			continue
		}
		out = append(out, CachedTool{
			Name:        rt.rawName,
			Description: rt.desc,
			Schema:      rt.schema,
			ReadOnly:    rt.readOnly,
		})
	}
	return out
}

// cacheVersion bumps whenever CachedSchema shape changes incompatibly. Old
// files with a smaller version are treated as a miss so a stale layout never
// crashes a reader.
const cacheVersion = 1

// CachedSchema is the persisted snapshot of one server's handshake result.
// SpecHash gates reuse — Capabilities/Tools are only trusted when the
// caller's expectedHash (from SpecFingerprint of the current Spec) matches,
// so renaming env vars or swapping a command never serves stale tools.
type CachedSchema struct {
	Version       int             `json:"version"`
	SpecHash      string          `json:"spec_hash"`
	Capabilities  map[string]bool `json:"capabilities"`
	Tools         []CachedTool    `json:"tools"`
	LastValidated time.Time       `json:"last_validated"`
}

// CachedTool mirrors the subset of an MCP tool definition we need to register
// a placeholder before the real handshake completes: Name (raw, server-local),
// Description (model-visible), Schema (raw JSON for input validation),
// ReadOnly (drives confirmation prompts).
type CachedTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	ReadOnly    bool            `json:"read_only"`
}

// SpecFingerprint hashes the load-bearing parts of a Spec so changing the
// command/url/args/env (not just renaming) invalidates the cache. Env map
// keys are sorted so ordering doesn't perturb the hash — Go map iteration
// order is randomised, so we'd otherwise get fingerprint churn on every
// launch.
func SpecFingerprint(s Spec) string {
	h := sha256.New()
	writeField(h, "type", s.Type)
	writeField(h, "command", s.Command)
	writeField(h, "url", s.URL)
	writeField(h, "dir", s.Dir)
	for _, a := range s.Args {
		writeField(h, "arg", a)
	}
	writeKV(h, "env", s.Env)
	writeKV(h, "headers", s.Headers)
	if len(s.ReadOnlyToolNames) > 0 {
		writeBoolKV(h, "read_only_tool", s.ReadOnlyToolNames)
	}
	if len(s.ReadOnlyModelToolNames) > 0 {
		writeBoolKV(h, "read_only_model_tool", s.ReadOnlyModelToolNames)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// LoadCachedSchema returns the cached schema for name iff it exists, parses,
// and matches expectedHash. Any error → (nil, false): cache is best-effort,
// a corrupt file just means we re-handshake. Returning an error here would
// only invite callers to log it on every launch — silence is intentional.
func LoadCachedSchema(name, expectedHash string) (*CachedSchema, bool) {
	cs, ok, hashOK := LoadCachedSchemaAny(name, expectedHash)
	if !ok || !hashOK {
		return nil, false
	}
	return cs, true
}

// LoadCachedSchemaAny returns the cached schema regardless of spec-hash match,
// plus whether the hash matched expectedHash. Catalog building uses it so a
// fingerprint-mismatched cache can still surface tools as stale candidates;
// execution paths must keep using LoadCachedSchema, which refuses mismatches.
func LoadCachedSchemaAny(name, expectedHash string) (cs *CachedSchema, ok bool, hashOK bool) {
	p := cachePath(name)
	if p == "" {
		return nil, false, false
	}
	b, err := fileencoding.ReadFileUTF8(p)
	if err != nil {
		return nil, false, false
	}
	var out CachedSchema
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, false, false
	}
	if out.Version != cacheVersion {
		return nil, false, false
	}
	return &out, true, out.SpecHash == expectedHash
}

// SaveCachedSchema atomically writes cs under name. Best-effort: an error
// is logged at debug level and dropped. Uses tmpfile + os.Rename in the
// parent dir so a crash mid-write can't leave a half-written JSON behind
// that the next Load would mis-parse.
func SaveCachedSchema(name string, cs CachedSchema) error {
	p := cachePath(name)
	if p == "" {
		return nil
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Debug("plugin cache: mkdir", "name", name, "err", err)
		return err
	}
	cs.Version = cacheVersion
	if cs.LastValidated.IsZero() {
		cs.LastValidated = time.Now().UTC()
	}
	b, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		slog.Debug("plugin cache: marshal", "name", name, "err", err)
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mcp-*.tmp")
	if err != nil {
		slog.Debug("plugin cache: tempfile", "name", name, "err", err)
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		slog.Debug("plugin cache: write", "name", name, "err", err)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		slog.Debug("plugin cache: close", "name", name, "err", err)
		return err
	}
	if err := os.Rename(tmpPath, p); err != nil {
		os.Remove(tmpPath)
		slog.Debug("plugin cache: rename", "name", name, "err", err)
		return err
	}
	return nil
}

// cachePath returns "<config.CacheDir()>/mcp/<slug(name)>.json". Returns ""
// when CacheDir is unavailable (no-op caching).
func cachePath(name string) string {
	base := config.CacheDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "mcp", slug(name)+".json")
}

// slugReplace strips characters that aren't safe in a filename across the
// OSes we target. We lowercase first so the slug is stable regardless of
// the user's display capitalisation.
var slugReplace = regexp.MustCompile(`[^a-z0-9_-]+`)

// slug sanitises name for use as a filename: lowercase, only [a-z0-9_-]. An
// empty result (all chars stripped) falls back to "_" so cachePath stays
// valid for any input.
func slug(name string) string {
	s := slugReplace.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "_"
	}
	return s
}

// writeField feeds a single tagged field into h with explicit separators so
// the boundary between (key, value) and the next field can't collide via
// concatenation (e.g. "command" + "foo" vs "comm" + "andfoo").
func writeField(h io.Writer, key, val string) {
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(val))
	_, _ = h.Write([]byte{1})
}

// writeKV hashes a map deterministically by sorting keys, so Go's randomised
// map iteration doesn't churn the fingerprint between launches.
func writeKV(h io.Writer, key string, m map[string]string) {
	if len(m) == 0 {
		writeField(h, key, "")
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeField(h, key+"."+k, m[k])
	}
}

func writeBoolKV(h io.Writer, key string, m map[string]bool) {
	if len(m) == 0 {
		writeField(h, key, "")
		return
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeField(h, key+"."+k, "true")
	}
}
