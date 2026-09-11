package pluginpkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// PluginRootEnvVar is the variable a runtime command may use to address
// files inside its own installed package. It expands at launch time, never
// through a shell.
const PluginRootEnvVar = "${REASONIX_PLUGIN_ROOT}"

// RuntimeSpec declares a plugin-owned runtime process (Manifest v2). The
// command is exec form only: Reasonix never runs it through a shell, so
// pipes, && and ; carry no special meaning. Command may start with
// ${REASONIX_PLUGIN_ROOT} to address a binary inside the installed package;
// the expansion happens at launch time (see ExpandRuntimeCommand for the
// diagnostics-time equivalent).
type RuntimeSpec struct {
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Required     bool              `json:"required,omitempty"`
	Priority     int               `json:"priority,omitempty"`
	Intercepts   []string          `json:"intercepts,omitempty"`
	Replaces     []string          `json:"replaces,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	// TimeoutMillis optionally tunes this runtime's synchronous intercept
	// budget. Zero keeps the host's per-point defaults; the host clamps any
	// value to its 60s ceiling at dispatch time.
	TimeoutMillis int `json:"timeoutMillis,omitempty"`
}

// sniffManifestAPIVersion extracts just the apiVersion field so parseNative
// can distinguish pre-extension manifests (absent) from versioned manifests
// without a full decode.
func sniffManifestAPIVersion(b []byte) (string, error) {
	var sniff struct {
		APIVersion json.RawMessage `json:"apiVersion"`
	}
	if err := json.Unmarshal(b, &sniff); err != nil {
		return "", err
	}
	if len(sniff.APIVersion) == 0 || string(sniff.APIVersion) == "null" {
		return "", nil
	}
	var v string
	if err := json.Unmarshal(sniff.APIVersion, &v); err != nil {
		return "", fmt.Errorf("%s: apiVersion must be a string", NativeManifest)
	}
	return strings.TrimSpace(v), nil
}

// strictDecode decodes one manifest object with unknown-field rejection
// (json.Decoder.DisallowUnknownFields). path prefixes the error so a typo
// names where it happened — root keys report under the manifest name,
// nested keys under their container ("contributes", "runtime",
// "hooks.<event>[i]", "mcpServers.<name>").
func strictDecode(data []byte, v any, path string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// v1Contributes is the shared strict-decoded contributes object used by the v2
// parser. Agents, prompts, and themes exist ONLY here — the legacy top level
// has no such keys.
type v1Contributes struct {
	Skills     json.RawMessage              `json:"skills"`
	Agents     json.RawMessage              `json:"agents"`
	Commands   json.RawMessage              `json:"commands"`
	Prompts    json.RawMessage              `json:"prompts"`
	Themes     json.RawMessage              `json:"themes"`
	Hooks      map[string][]json.RawMessage `json:"hooks"`
	MCPServers map[string]json.RawMessage   `json:"mcpServers"`
}

// parseV1PathList parses a native path list. The flexible string | []string |
// [{path}] forms are kept from the legacy parser, but unknown keys inside
// the object form are rejected — a typo like {"paht": "skills"} must fail,
// not silently contribute nothing.
func parseV1PathList(raw json.RawMessage, path string) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return cleanPathList([]string{one})
	}
	var manyStrings []string
	if err := json.Unmarshal(raw, &manyStrings); err == nil {
		return cleanPathList(manyStrings)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		paths := make([]string, 0, len(items))
		for i, item := range items {
			var obj struct {
				Path string `json:"path"`
			}
			if err := strictDecode(item, &obj, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return nil, err
			}
			paths = append(paths, obj.Path)
		}
		return cleanPathList(paths)
	}
	return nil, fmt.Errorf("%s must be a path string, string array, or object array", path)
}

// parseV1HookMap strict-decodes a hooks map. Each entry is decoded
// individually so an unknown key reports its full event/index path.
func parseV1HookMap(raw map[string][]json.RawMessage, path string) (map[string][]Hook, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string][]Hook, len(raw))
	for _, event := range sortedKeys(raw) {
		entries := raw[event]
		hooks := make([]Hook, 0, len(entries))
		for i, entry := range entries {
			h, err := parseV1Hook(entry, fmt.Sprintf("%s.%s[%d]", path, event, i))
			if err != nil {
				return nil, err
			}
			hooks = append(hooks, h)
		}
		out[event] = hooks
	}
	return out, nil
}

// parseV1Hook strict-decodes one hook entry, preserving the args presence
// bit exactly like Hook.UnmarshalJSON (exec form vs shell form depends on
// it). Decoding goes through a method-free alias so the lenient legacy
// unmarshaler cannot weaken v2 strictness.
func parseV1Hook(data json.RawMessage, path string) (Hook, error) {
	type hookJSON Hook
	var decoded hookJSON
	if err := strictDecode(data, &decoded, path); err != nil {
		return Hook{}, err
	}
	h := Hook(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Hook{}, err
	}
	for name := range fields {
		if strings.EqualFold(name, "args") {
			h.ArgsSet = true
			break
		}
	}
	return h, nil
}

func parseV1MCPServerMap(raw map[string]json.RawMessage, path string) (map[string]MCPServer, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]MCPServer, len(raw))
	for _, name := range sortedKeys(raw) {
		type mcpJSON MCPServer
		var decoded mcpJSON
		if err := strictDecode(raw[name], &decoded, fmt.Sprintf("%s.%s", path, name)); err != nil {
			return nil, err
		}
		out[name] = MCPServer(decoded)
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// unionPathLists merges a legacy top-level path list with its contributes
// counterpart. Both inputs are already cleaned (slash-normalized, deduped,
// sorted); identical paths dedupe across the two and the result stays
// sorted. A path listed under both contributes.prompts and
// contributes.commands is NOT deduped across those sets — prompts and
// commands are separate semantic sets, and the path intentionally joins both.
func unionPathLists(legacy, contrib []string) []string {
	if len(legacy) == 0 {
		return contrib
	}
	if len(contrib) == 0 {
		return legacy
	}
	seen := make(map[string]bool, len(legacy)+len(contrib))
	out := make([]string, 0, len(legacy)+len(contrib))
	for _, list := range [][]string{legacy, contrib} {
		for _, p := range list {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// hookIdentity is the merge key for a hook entry: the event (applied by the
// caller) plus what the entry runs. Two entries with the same identity but
// different remaining fields are a conflict, not two hooks.
func hookIdentity(h Hook) string {
	return h.Command + "\x00" + h.ContextFile
}

// mergeV1Hooks unions legacy top-level hooks with contributes.hooks. Both
// sides are normalized first (trimmed, shell inferred, empty entries
// dropped); entries are keyed by event plus executable identity. The same
// key with a different definition is a manifest error naming the key;
// byte-identical entries dedupe.
func mergeV1Hooks(legacy, contrib map[string][]Hook) (map[string][]Hook, error) {
	legacy = normalizeHooks(legacy)
	contrib = normalizeHooks(contrib)
	if len(legacy) == 0 {
		return contrib, nil
	}
	if len(contrib) == 0 {
		return legacy, nil
	}
	out := make(map[string][]Hook, len(legacy))
	for event, hooks := range legacy {
		out[event] = append([]Hook(nil), hooks...)
	}
	for _, event := range sortedKeys(contrib) {
		for _, h := range contrib[event] {
			duplicate := false
			for _, existing := range out[event] {
				if hookIdentity(existing) != hookIdentity(h) {
					continue
				}
				if reflect.DeepEqual(existing, h) {
					duplicate = true
					break
				}
				return nil, fmt.Errorf("hook %q (event %s) is defined differently in hooks and contributes.hooks", firstNonEmpty(h.Command, h.ContextFile), event)
			}
			if !duplicate {
				out[event] = append(out[event], h)
			}
		}
	}
	return out, nil
}

// mergeV1MCPServers unions legacy top-level mcpServers with
// contributes.mcpServers, keyed by server name. The same name with a
// different definition is a manifest error naming the server; identical
// definitions dedupe.
func mergeV1MCPServers(legacy, contrib map[string]MCPServer) (map[string]MCPServer, error) {
	if len(legacy) == 0 {
		return contrib, nil
	}
	if len(contrib) == 0 {
		return legacy, nil
	}
	out := make(map[string]MCPServer, len(legacy))
	maps.Copy(out, legacy)
	for _, name := range sortedKeys(contrib) {
		server := contrib[name]
		if existing, ok := out[name]; ok {
			if reflect.DeepEqual(existing, server) {
				continue
			}
			return nil, fmt.Errorf("MCP server %q is defined differently in mcpServers and contributes.mcpServers", name)
		}
		out[name] = server
	}
	return out, nil
}

// The interceptor points, replacement slots, and priority bounds below
// duplicate internal/extension (intercept.go, replace.go). They are NOT
// imported: extension depends on pluginpkg transitively
// (extension -> hook -> pluginpkg), so pluginpkg importing extension would
// create an import cycle. Keep these lists in sync with extension — the
// adapter tests in the extension package pin them together by parsing a
// manifest that exercises every value.

const (
	minRuntimePriority = -1000 // mirrors extension.MinInterceptorPriority
	maxRuntimePriority = 1000  // mirrors extension.MaxInterceptorPriority
)

var runtimeInterceptorPoints = map[string]bool{
	"session.start":       true,
	"session.end":         true,
	"session.load":        true,
	"session.save":        true,
	"session.rotate":      true,
	"input.receive":       true,
	"agent.before_start":  true,
	"system_prompt.build": true,
	"context.prepare":     true,
	"provider.request":    true,
	"provider.response":   true,
	"tool.before":         true,
	"tool.after":          true,
	"permission.decision": true,
	"compaction.prepare":  true,
	"compaction.complete": true,
	"frontend.event":      true,
}

var runtimeNamedSlots = map[string]bool{
	"system_prompt":     true,
	"context":           true,
	"provider_request":  true,
	"provider_response": true,
	"compaction":        true,
	"session_policy":    true,
	"permission":        true,
	"frontend_events":   true,
}

var runtimeCapabilities = []string{"interceptors", "strategies", "providers", "ui"}

// validateRuntimeSlot mirrors extension.ParseSlot: bare names must be
// declared slots; tool:/provider: forms must carry a well-formed target.
// Provider targets are <name>/<model>, or plugin/<pluginID>/<name>/<model>
// for extension-hosted providers (stage 7).
func validateRuntimeSlot(s string) error {
	if runtimeNamedSlots[s] {
		return nil
	}
	if rest, ok := strings.CutPrefix(s, "tool:"); ok {
		if rest == "" || strings.ContainsAny(rest, " \t\n") {
			return fmt.Errorf("runtime.replaces: invalid tool slot %q: empty or whitespace tool name", s)
		}
		return nil
	}
	if rest, ok := strings.CutPrefix(s, "provider:"); ok {
		if !validRuntimeProviderRef(rest) {
			return fmt.Errorf("runtime.replaces: invalid provider slot %q: want provider:<name>/<model> or provider:plugin/<plugin>/<name>/<model>", s)
		}
		return nil
	}
	return fmt.Errorf("runtime.replaces: unknown slot %q", s)
}

// validRuntimeProviderRef mirrors the kernel's provider-slot target rule
// (extension.validProviderSlotTarget): an ordinary <name>/<model> ref, or an
// extension-hosted plugin/<pluginID>/<name>/<model> ref.
func validRuntimeProviderRef(ref string) bool {
	name, model, found := strings.Cut(ref, "/")
	if found && name != "" && model != "" && !strings.Contains(model, "/") {
		return true
	}
	rest, ok := strings.CutPrefix(ref, "plugin/")
	if !ok {
		return false
	}
	pluginID, nameModel, ok := strings.Cut(rest, "/")
	if !ok || pluginID == "" || strings.ContainsAny(pluginID, " \t\n") {
		return false
	}
	name, model, found = strings.Cut(nameModel, "/")
	return found && name != "" && model != "" && !strings.Contains(model, "/")
}

// parseV1Runtime strict-decodes and validates the runtime block. Every
// validation error names the offending value so a manifest author can find
// it without a second lookup.
func parseV1Runtime(raw json.RawMessage) (*RuntimeSpec, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rt RuntimeSpec
	if err := strictDecode(raw, &rt, "runtime"); err != nil {
		return nil, err
	}
	rt.Command = strings.TrimSpace(rt.Command)
	if rt.Command == "" {
		return nil, errors.New("runtime.command is required when a runtime is declared")
	}
	for i, arg := range rt.Args {
		if strings.TrimSpace(arg) == "" {
			return nil, fmt.Errorf("runtime.args[%d] must not be empty", i)
		}
	}
	for key := range rt.Env {
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("runtime.env contains an empty key")
		}
	}
	if rt.Priority < minRuntimePriority || rt.Priority > maxRuntimePriority {
		return nil, fmt.Errorf("runtime.priority %d out of range [%d, %d]", rt.Priority, minRuntimePriority, maxRuntimePriority)
	}
	if rt.TimeoutMillis < 0 {
		return nil, fmt.Errorf("runtime.timeoutMillis %d must not be negative", rt.TimeoutMillis)
	}
	for _, point := range rt.Intercepts {
		if !runtimeInterceptorPoints[point] {
			return nil, fmt.Errorf("runtime.intercepts: unknown interceptor point %q", point)
		}
	}
	for _, slot := range rt.Replaces {
		if err := validateRuntimeSlot(slot); err != nil {
			return nil, err
		}
	}
	for _, capability := range rt.Capabilities {
		known := slices.Contains(runtimeCapabilities, capability)
		if !known {
			return nil, fmt.Errorf("runtime.capabilities: unknown capability %q (want one of: %s)", capability, strings.Join(runtimeCapabilities, ", "))
		}
	}
	return &rt, nil
}

// validateV1Paths enforces the v2 on-disk path contract — stronger than the
// legacy lexical checks. Every contributed path that EXISTS must resolve
// inside the plugin root, so a symlink cannot smuggle outside content into
// the session; theme paths must be regular files. Missing paths (and
// theme globs that match nothing) are warnings, not parse failures: an
// optional asset must not disable the whole package, but doctor reports it.
func validateV1Paths(root string, m *Manifest) ([]string, error) {
	var warnings []string
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = filepath.Clean(root)
	}
	checkResidency := func(kind, rel string) error {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(abs); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				warnings = append(warnings, fmt.Sprintf("%s path %q does not exist", kind, rel))
			} else {
				warnings = append(warnings, fmt.Sprintf("%s path %q is not readable: %v", kind, rel, err))
			}
			return nil
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s path %q cannot be resolved: %v", kind, rel, err))
			return nil
		}
		if !pathWithinRoot(resolvedRoot, resolved) {
			return fmt.Errorf("%s path %q escapes the plugin root through a symlink", kind, rel)
		}
		return nil
	}
	for _, rel := range m.Skills {
		if err := checkResidency("skills", rel); err != nil {
			return warnings, err
		}
	}
	for _, rel := range m.Agents {
		if err := checkResidency("agents", rel); err != nil {
			return warnings, err
		}
	}
	for _, rel := range m.Commands {
		if err := checkResidency("commands", rel); err != nil {
			return warnings, err
		}
	}
	for _, rel := range m.Prompts {
		if err := checkResidency("prompts", rel); err != nil {
			return warnings, err
		}
	}
	for _, pattern := range m.Themes {
		if !hasGlobMeta(pattern) {
			abs := filepath.Join(root, filepath.FromSlash(pattern))
			if _, err := os.Lstat(abs); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					warnings = append(warnings, fmt.Sprintf("themes path %q does not exist", pattern))
				} else {
					warnings = append(warnings, fmt.Sprintf("themes path %q is not readable: %v", pattern, err))
				}
				continue
			}
			if err := checkThemeFile(resolvedRoot, abs, pattern); err != nil {
				return warnings, err
			}
			continue
		}
		matches, err := globThemePattern(root, pattern)
		if err != nil {
			return warnings, err
		}
		if len(matches) == 0 {
			warnings = append(warnings, fmt.Sprintf("theme glob %q matched no files", pattern))
			continue
		}
		for _, match := range matches {
			if err := checkThemeFile(resolvedRoot, match, pattern); err != nil {
				return warnings, err
			}
		}
	}
	return warnings, nil
}

func checkThemeFile(resolvedRoot, abs, pattern string) error {
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("theme %q cannot be resolved: %w", pattern, err)
	}
	if !pathWithinRoot(resolvedRoot, resolved) {
		return fmt.Errorf("theme %q escapes the plugin root through a symlink", pattern)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("theme %q is not readable: %w", pattern, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("theme %q is not a regular file", pattern)
	}
	return nil
}

func pathWithinRoot(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func hasGlobMeta(p string) bool { return strings.ContainsAny(p, "*?[") }

// globThemePattern expands a theme glob one path segment at a time, so the
// plugin root itself is never interpreted as pattern syntax. Each segment
// supports path.Match wildcards and never crosses directory boundaries.
func globThemePattern(root, pattern string) ([]string, error) {
	segs := strings.Split(pattern, "/")
	for _, seg := range segs {
		if hasGlobMeta(seg) {
			if _, err := path.Match(seg, ""); err != nil {
				return nil, fmt.Errorf("invalid theme glob %q: %w", pattern, err)
			}
		}
	}
	matches := []string{filepath.Clean(root)}
	for _, seg := range segs {
		var next []string
		if !hasGlobMeta(seg) {
			for _, base := range matches {
				next = append(next, filepath.Join(base, seg))
			}
		} else {
			for _, base := range matches {
				entries, err := os.ReadDir(base)
				if err != nil {
					continue
				}
				for _, entry := range entries {
					if ok, _ := path.Match(seg, entry.Name()); ok {
						next = append(next, filepath.Join(base, entry.Name()))
					}
				}
			}
		}
		matches = next
	}
	sort.Strings(matches)
	return matches, nil
}

// ExpandRuntimeCommand substitutes the ${REASONIX_PLUGIN_ROOT} prefix with
// the package root. Launch-time expansion lives with the runtime supervisor
// (a later stage); this exists so diagnostics can resolve the on-disk path.
func ExpandRuntimeCommand(command, root string) string {
	if rest, ok := strings.CutPrefix(command, PluginRootEnvVar); ok {
		return filepath.Join(root, filepath.FromSlash(rest))
	}
	return command
}
