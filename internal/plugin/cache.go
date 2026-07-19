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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
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
		declaredReadOnly, _, _, destructive, _ := rt.securitySnapshot()
		out = append(out, CachedTool{
			Name:         rt.rawName,
			Description:  rt.desc,
			Schema:       rt.schema,
			OutputSchema: rt.outputSchema,
			ReadOnly:     declaredReadOnly,
			Destructive:  destructive,
		})
	}
	return out
}

// cacheVersion bumps whenever CachedSchema shape changes incompatibly. Old
// files with a smaller version are treated as a miss so a stale layout never
// crashes a reader.
const cacheVersion = 2

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
// ReadOnly and Destructive (drive local approval policy).
type CachedTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Schema       json.RawMessage `json:"schema"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	ReadOnly     bool            `json:"read_only"`
	Destructive  bool            `json:"destructive,omitempty"`
}

// SpecFingerprint hashes the load-bearing, non-secret parts of a Spec. Secret
// values in env and headers are intentionally excluded: credential rotation
// must not leak through a stable digest or force an unrelated trust review.
// Their sorted key names remain identity-bearing, so adding/removing a runtime
// input still invalidates the cached schema.
func SpecFingerprint(s Spec) string {
	return specFingerprintForURL(s, normalizeIdentityURL(s.URL))
}

// legacySpecFingerprint recomputes the cache fingerprint with the
// pre-credential-aware URL normalization, or ("", false) when it cannot
// differ. LoadCachedSchemaForSpec uses it to upgrade old cache entries in
// place; remove together with legacyNormalizeIdentityURL.
func legacySpecFingerprint(s Spec) (string, bool) {
	legacyURL := legacyNormalizeIdentityURL(s.URL)
	if strings.TrimSpace(s.URL) == "" || legacyURL == normalizeIdentityURL(s.URL) {
		return "", false
	}
	return specFingerprintForURL(s, legacyURL), true
}

func specFingerprintForURL(s Spec, urlValue string) string {
	h := sha256.New()
	writeField(h, "name", s.Name)
	writeField(h, "type", s.Type)
	writeField(h, "command", s.Command)
	writeField(h, "url", urlValue)
	writeField(h, "dir", s.Dir)
	for _, a := range s.Args {
		writeField(h, "arg", a)
	}
	writeKeys(h, "env", s.Env)
	writeKeys(h, "headers", s.Headers)
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

// LoadCachedSchemaForSpec returns the cached schema matching the spec's
// current fingerprint, transparently rewriting an entry still saved under the
// legacy URL fingerprint. Without the in-place upgrade, credential rotation or
// the credential-aware normalization rollout would force a pointless
// re-handshake even though nothing observable changed.
func LoadCachedSchemaForSpec(s Spec) (*CachedSchema, bool) {
	current := SpecFingerprint(s)
	if cs, ok := LoadCachedSchema(s.Name, current); ok {
		return cs, true
	}
	legacy, ok := legacySpecFingerprint(s)
	if !ok {
		return nil, false
	}
	cs, ok := LoadCachedSchema(s.Name, legacy)
	if !ok {
		return nil, false
	}
	cs.SpecHash = current
	_ = SaveCachedSchema(s.Name, *cs)
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
	out.Tools = filterValidCachedTools(out.Tools)
	return &out, true, out.SpecHash == expectedHash
}

func filterValidCachedTools(tools []CachedTool) []CachedTool {
	out := make([]CachedTool, 0, len(tools))
	for _, t := range tools {
		schema, err := normalizeAndValidateToolSchema(t.Schema)
		if err != nil {
			continue
		}
		t.Schema = schema
		out = append(out, t)
	}
	return out
}

// SaveCachedSchema atomically writes cs under name. Best-effort: an error is
// logged at debug level and returned. The shared replacement helper preserves
// overwrite semantics on Windows as well as crash safety on Unix.
func SaveCachedSchema(name string, cs CachedSchema) error {
	p := cachePath(name)
	if p == "" {
		return nil
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
	if err := fileutil.AtomicWriteFile(p, b, 0o600); err != nil {
		slog.Debug("plugin cache: atomic write", "name", name, "err", err)
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

// windowsReservedDeviceNames are DOS device names Windows reserves as file
// stems (with or without an extension), matched case-insensitively.
var windowsReservedDeviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// slug sanitises name for use as a filename. Names changed by sanitization —
// and Windows-reserved device stems such as "con" or "com1", which would name
// a device rather than a file — get a strong suffix so confusable names cannot
// make one MCP server consume another server's cached schemas, stats, or
// private state directory. Ordinary safe names stay byte-identical.
func slug(name string) string {
	s := slugReplace.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "_"
	}
	if s != name || windowsReservedDeviceNames[s] {
		sum := sha256.Sum256([]byte(name))
		s += "-" + hex.EncodeToString(sum[:6])
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

// writeKeys hashes only sorted map keys, so Go's randomised iteration order
// cannot perturb the non-secret identity digest.
func writeKeys(h io.Writer, key string, m map[string]string) {
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
		writeField(h, key+"."+k, "present")
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
