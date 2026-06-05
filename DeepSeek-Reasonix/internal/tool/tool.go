// Package tool defines the Tool abstraction and a Registry. Built-in tools live
// in tool/builtin and self-register via init(); plugin-provided tools are added
// to a runtime Registry alongside the enabled built-ins. The agent sees only a
// *Registry, never the global built-in set directly.
package tool

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/diff"
	"reasonix/internal/provider"
)

// Tool is a capability the model can invoke.
type Tool interface {
	Name() string
	Description() string
	// Schema returns the JSON Schema for the tool's parameters.
	Schema() json.RawMessage
	// Execute parses the model-generated raw JSON args and returns result text
	// to feed back to the model.
	Execute(ctx context.Context, args json.RawMessage) (string, error)
	// ReadOnly reports whether the tool has no observable side effects on the
	// host. The agent parallelises a batch of tool calls only when every call
	// in the batch is ReadOnly; mixed batches stay sequential so write/read
	// ordering is preserved. bash and plugin tools must return false because
	// their effects can't be inferred statically from args.
	ReadOnly() bool
}

// Previewer is an optional capability a writer Tool may implement: given the
// same raw JSON args Execute would receive, compute the file change the call
// *would* make — without touching disk. A front-end uses it to show an approval
// card or a changed-files panel before the call runs (the permission gate, not
// Preview, decides whether it may proceed). Type-assert a Tool to Previewer to
// discover support; the file-writing built-ins implement it, most tools do not.
type Previewer interface {
	Preview(args json.RawMessage) (diff.Change, error)
}

// PreviewChange returns the change a writer tool would make for args, or ok=false
// when there's nothing renderable: t is read-only, doesn't implement Previewer,
// the preview errored (the edit will likely fail too), or the file is binary.
func PreviewChange(t Tool, args json.RawMessage) (diff.Change, bool) {
	if t == nil || t.ReadOnly() {
		return diff.Change{}, false
	}
	pv, ok := t.(Previewer)
	if !ok {
		return diff.Change{}, false
	}
	ch, err := pv.Preview(args)
	if err != nil || ch.Binary {
		return diff.Change{}, false
	}
	return ch, true
}

// --- process-global built-in set (populated by builtin subpackage init) ---

var builtins = map[string]Tool{}

// RegisterBuiltin registers a compile-time built-in tool. Intended for init().
// It panics on a duplicate name, which is a compile-time wiring mistake.
func RegisterBuiltin(t Tool) {
	name := t.Name()
	if _, dup := builtins[name]; dup {
		panic("tool: duplicate built-in " + name)
	}
	builtins[name] = t
}

// Builtins returns all registered built-in tools, sorted by name.
func Builtins() []Tool {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		out = append(out, builtins[n])
	}
	return out
}

// LookupBuiltin returns a registered built-in by name.
func LookupBuiltin(name string) (Tool, bool) {
	t, ok := builtins[name]
	return t, ok
}

// --- per-run registry instance ---

// Registry is a per-run set of tools: enabled built-ins plus plugin tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string
	canon map[string]json.RawMessage
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}, canon: map[string]json.RawMessage{}}
}

// Add inserts (or replaces) a tool, preserving first-seen order. The schema is
// canonicalized once here — it never changes after registration, so Schemas()
// (called every turn) reuses the result instead of re-marshaling.
func (r *Registry) Add(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := t.Name()
	if _, ok := r.tools[name]; !ok {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
	r.canon[name] = provider.CanonicalizeSchema(t.Schema())
}

// MCPNamePrefix is the namespace every MCP tool name carries: the
// model-visible name is "mcp__<server>__<tool>".
const MCPNamePrefix = "mcp__"

// SplitMCPName splits a model-visible MCP tool name "mcp__<server>__<tool>" into
// its server and tool parts. ok is false for non-MCP (built-in) names and for
// malformed names missing either part.
func SplitMCPName(name string) (server, tool string, ok bool) {
	if !strings.HasPrefix(name, MCPNamePrefix) {
		return "", "", false
	}
	rest := name[len(MCPNamePrefix):]
	parts := strings.SplitN(rest, "__", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// RemovePrefix unregisters every tool whose name starts with prefix — used to
// drop an MCP server's "mcp__<server>__" namespace when it's disconnected — and
// returns the count removed.
func (r *Registry) RemovePrefix(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	kept := r.order[:0]
	removed := 0
	for _, name := range r.order {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
			delete(r.canon, name)
			removed++
			continue
		}
		kept = append(kept, name)
	}
	r.order = kept
	return removed
}

// Get looks up a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	return t, ok
}

// Len returns the number of registered tools.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.order)
}

// Names returns the registered tool names in insertion order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Schemas exports tool definitions in stable name order for the provider.
func (r *Registry) Schemas() []provider.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, len(r.order))
	copy(names, r.order)
	sort.Strings(names)

	out := make([]provider.ToolSchema, 0, len(names))
	for _, name := range names {
		t := r.tools[name]
		if t == nil {
			continue
		}
		out = append(out, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  r.canon[name],
		})
	}
	return out
}
