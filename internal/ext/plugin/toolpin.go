package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/textutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

const toolPinsVersion = 1

// HeldTool is one tool withheld from the model because its definition is not
// the one the user approved for its server.
type HeldTool struct {
	RawName     string
	VisibleName string
	Class       tool.HeldMCPClass
	// Digest is the definition the server offers now; Approved is the one on
	// record, empty for an added tool.
	Digest   string
	Approved string
}

// Binding names the tool the way the registry's policies do.
func (h HeldTool) Binding(s Spec) tool.MCPBinding {
	return tool.MCPBinding{
		Package: s.Package, Server: s.Name, RawName: h.RawName, VisibleName: h.VisibleName,
		CallableName: ModelToolName(s.Name, h.VisibleName),
		CapabilityID: "mcp-tool:" + s.Name + "/" + h.RawName,
	}
}

// ApplyHeldMCPPolicy replaces the registry's held set for one server with what
// its live client withheld. A server with no client leaves the set unchanged.
func ApplyHeldMCPPolicy(reg *tool.Registry, host *Host, s Spec) {
	if reg == nil || host == nil {
		return
	}
	held, ok := host.HeldTools(s.Name)
	if !ok {
		return
	}
	out := make([]tool.HeldMCP, 0, len(held))
	for _, h := range held {
		out = append(out, tool.HeldMCP{Binding: h.Binding(s), Class: h.Class})
	}
	reg.ReplaceHeldMCP(s.Name, out)
}

// HeldTools reports the tools a connected server's client withheld.
func (h *Host) HeldTools(server string) ([]HeldTool, bool) {
	c := h.client(server)
	if c == nil {
		return nil, false
	}
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	return append([]HeldTool(nil), c.held...), true
}

type toolPinFile struct {
	Version int               `json:"version"`
	Tools   map[string]string `json:"tools"`
}

// toolPinsPath is a server's approval record, named by a digest of its name. A
// user-level server has one record for all workspaces. It guards against remote
// servers and unattended upgrades: an unconfined local server can rewrite it.
func toolPinsPath(s Spec) string {
	if strings.TrimSpace(s.StateDir) == "" {
		return ""
	}
	return toolPinsPathIn(s.StateDir, s.Name, config.MCPConfigSource(strings.TrimSpace(s.ConfigSource)).UserOwned())
}

func toolPinsPathIn(stateDir, name string, user bool) string {
	root := filepath.Dir(stateDir)
	if user {
		root = filepath.Join(filepath.Dir(root), ".user")
	}
	sum := sha256.Sum256([]byte(name))
	return pinFileIn(filepath.Join(root, ".tool-pins"), hex.EncodeToString(sum[:]))
}

var pinKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// pinFileIn joins a record under dir only for a 64-hex key, and only when the
// result stays inside dir; anything else has no path.
func pinFileIn(dir, key string) string {
	if !pinKeyPattern.MatchString(key) {
		return ""
	}
	dir = filepath.Clean(dir)
	p := filepath.Join(dir, key+".json")
	if rel, err := filepath.Rel(dir, p); err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return p
}

// ForgetToolPins drops a removed server's approval records in both scopes, so
// adding it again is a new approval.
func ForgetToolPins(stateDir, name string) error {
	if strings.TrimSpace(stateDir) == "" {
		return nil
	}
	var errs []error
	for _, user := range []bool{false, true} {
		p := toolPinsPathIn(stateDir, name, user)
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ErrHeldDigestMismatch is an acceptance bound to a held set that is no longer
// the one the server now holds.
var ErrHeldDigestMismatch = errors.New("the held MCP tool definitions changed since they were shown")

// HeldDigest identifies a held set by its tool names and definition digests, so
// an acceptance can be bound to exactly what the user was shown.
func HeldDigest(held []HeldTool) string {
	lines := make([]string, len(held))
	for i, h := range held {
		lines[i] = h.RawName + "\x00" + h.Digest
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// ErrNoHeldTools is accepting a server's held tools when it has none.
var ErrNoHeldTools = errors.New("MCP server has no held tool definitions")

// AcceptHeldTools records the definitions a server's live connection withheld
// as approved, provided they are the set digest names, and returns their raw
// names. The tools reach the model once the server lists them again.
func (h *Host) AcceptHeldTools(server, digest string) ([]string, error) {
	c := h.client(server)
	if c == nil {
		return nil, ErrNoHeldTools
	}
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	if len(c.held) == 0 {
		return nil, ErrNoHeldTools
	}
	if digest != HeldDigest(c.held) {
		return nil, ErrHeldDigestMismatch
	}
	path := toolPinsPath(c.spec)
	if path == "" {
		return nil, errors.New("no state directory to record the approval in")
	}
	pins := toolPinFile{Version: toolPinsVersion, Tools: map[string]string{}}
	if raw, err := os.ReadFile(path); err == nil {
		var old toolPinFile
		if json.Unmarshal(raw, &old) == nil && old.Version == toolPinsVersion && old.Tools != nil {
			pins = old
		}
	}
	names := make([]string, 0, len(c.held))
	for _, held := range c.held {
		pins.Tools[held.RawName] = held.Digest
		names = append(names, held.RawName)
	}
	if err := writeToolPins(path, pins.Tools); err != nil {
		return nil, err
	}
	return names, nil
}

func (h *Host) announceHeld(c *Client) {
	c.toolsMu.Lock()
	held := append([]HeldTool(nil), c.held...)
	c.toolsMu.Unlock()
	if len(held) == 0 {
		return
	}
	h.statusMu.RLock()
	sink := h.statusSink
	h.statusMu.RUnlock()
	if sink == nil {
		return
	}
	names := make([]string, len(held))
	for i, t := range held {
		names[i] = t.RawName
	}
	server := textutil.ShownLocator(c.spec.Name)
	sink.Emit(event.Event{
		Kind: event.Notice, Audience: event.NoticeAudienceOperator, Level: event.LevelWarn,
		Code:   event.NoticeCodeMCPToolsHeld,
		Text:   fmt.Sprintf("MCP server %q: %d tool(s) are held out of the model's tools because their definitions are new or differ from the ones you approved. Review the server, then run %s to accept them.", server, len(held), trustCommand(c.spec.Name)),
		Detail: textutil.ShownLocator(strings.Join(names, ", ")) + "\ndigest: " + HeldDigest(held),
	})
}

// mcpToolDigest is the digest of every definition field the server controls,
// read from the decoded structure with keys sorted and array order kept. It
// depends on nothing Reasonix rewrites, so upgrading Reasonix never moves it.
func mcpToolDigest(t mcpTool) string {
	def := struct {
		Name         string          `json:"name"`
		Title        string          `json:"title"`
		Description  string          `json:"description"`
		InputSchema  json.RawMessage `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema"`
		Annotations  json.RawMessage `json:"annotations"`
	}{t.Name, t.Title, t.Description, canonicalJSON(t.InputSchema), canonicalJSON(t.OutputSchema), canonicalJSON(t.Annotations)}
	b, err := json.Marshal(def)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return b
}

// judgeToolPins compares a live listing with the server's approval record. A
// server with no record that the user has authorized is recorded as listed; a
// record that cannot be read holds every tool, since nothing can be compared.
func judgeToolPins(s Spec, tools []mcpTool) []HeldTool {
	path := toolPinsPath(s)
	if path == "" {
		return nil
	}
	current := make(map[string]string, len(tools))
	for _, t := range tools {
		current[t.Name] = mcpToolDigest(t)
	}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if s.ServerAuthorized() && len(current) > 0 {
			_ = writeToolPins(path, current)
		}
		return nil
	case err != nil:
		return holdAll(s, current, tool.HeldMCPUnverifiable)
	}
	var pins toolPinFile
	if json.Unmarshal(raw, &pins) != nil || pins.Version != toolPinsVersion || pins.Tools == nil {
		return holdAll(s, current, tool.HeldMCPUnverifiable)
	}
	var held []HeldTool
	for name, digest := range current {
		approved, known := pins.Tools[name]
		switch {
		case !known:
			held = append(held, newHeld(s, name, tool.HeldMCPAdded, digest, ""))
		case approved != digest:
			held = append(held, newHeld(s, name, tool.HeldMCPChanged, digest, approved))
		}
	}
	sort.Slice(held, func(i, j int) bool { return held[i].RawName < held[j].RawName })
	return held
}

func holdAll(s Spec, current map[string]string, class tool.HeldMCPClass) []HeldTool {
	held := make([]HeldTool, 0, len(current))
	for name, digest := range current {
		held = append(held, newHeld(s, name, class, digest, ""))
	}
	sort.Slice(held, func(i, j int) bool { return held[i].RawName < held[j].RawName })
	return held
}

func newHeld(s Spec, raw string, class tool.HeldMCPClass, digest, approved string) HeldTool {
	visible := raw
	if s.StripRawPrefix != "" {
		visible = strings.TrimPrefix(visible, s.StripRawPrefix)
	}
	return HeldTool{RawName: raw, VisibleName: visible, Class: class, Digest: digest, Approved: approved}
}

func writeToolPins(path string, digests map[string]string) error {
	b, err := json.MarshalIndent(toolPinFile{Version: toolPinsVersion, Tools: digests}, "", "  ")
	if err == nil {
		err = fileutil.AtomicWriteFile(path, b, 0o600)
	}
	if err != nil {
		slog.Warn("plugin: could not record the approved tool definitions", "path", path, "err", err)
	}
	return err
}

// trustCommand is the suggested acceptance command. A name that is not a valid
// server name is never embedded: it could carry terminal or shell syntax.
func trustCommand(name string) string {
	if config.IsValidMCPServerName(name) {
		return "`reasonix mcp trust " + name + "`"
	}
	return "`reasonix mcp trust` with this server's name"
}
