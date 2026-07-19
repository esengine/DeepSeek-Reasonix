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

// ImageTool is an optional capability a Tool may implement when its results can
// carry images alongside text (e.g. an MCP tool returning a screenshot).
// ExecuteWithImages returns the same text Execute would — including a short
// placeholder marker where each image occurred — plus the images as data URLs
// (data:<mime>;base64,<payload>). Callers with a structural image channel (the
// agent stores them on the tool message, where vision-capable providers embed
// them) use this instead of Execute; everything else falls back to Execute and
// the placeholders alone describe the images. Keeping images out of the text
// matters: tool output text is truncated at a fixed byte budget, which would
// corrupt an embedded base64 payload.
type ImageTool interface {
	ExecuteWithImages(ctx context.Context, args json.RawMessage) (text string, images []string, err error)
}

// PlanModeClassifier is an optional capability a Tool may implement to declare
// its stance on running during the planning phase. It is deliberately distinct
// from ReadOnly(): a tool can be side-effect-free yet belong only to the
// post-approval execution phase (complete_step reports ReadOnly()==true but must
// not run while planning), or be a delegation that is safe only in a read-only
// variant (read_only_task). A false result is an explicit phase opt-out; tools
// without this interface continue to the ordinary Permissions/Sandbox path.
type PlanModeClassifier interface {
	PlanModeSafe() bool
}

// PlanModeUntrustedReadOnly marks a tool whose ReadOnly classification comes
// only from an external MCP server hint. The main Plan workflow may use that
// hint for ordinary permission classification, while planner/read-only subagent
// registries must not treat it as local trust.
type PlanModeUntrustedReadOnly interface {
	PlanModeUntrustedReadOnly() bool
}

// ReadOnlyExecutionHostMutation marks a target that is logically read-only but
// must first mutate host state to become executable, such as starting an
// on-demand MCP process. Strict read-only agents reject these targets even when
// their eventual remote operation is trusted read-only.
type ReadOnlyExecutionHostMutation interface {
	ReadOnlyExecutionHostMutation() bool
}

// ReadOnlyExecutionBlockReason lets a deferred capability explain which
// parent-session action is required when a strict read-only child cannot run
// it. The reason is host-local and never enters provider tool schemas.
type ReadOnlyExecutionBlockReason interface {
	ReadOnlyExecutionBlockReason() string
}

// MCPMetadata exposes the original MCP identity behind a model-visible
// "mcp__<server>__<tool>" adapter. The model name may be normalized for provider
// function-name rules; config such as trusted_read_only_tools must use the raw
// server-local tool name.
type MCPMetadata interface {
	MCPServerName() string
	MCPRawToolName() string
}

// MCPAnnotations exposes safety-relevant annotations reported by an installed
// MCP server. These hints do not change the provider-visible tool contract;
// execution policy consumes them locally.
type MCPAnnotations interface {
	MCPDestructiveHint() bool
}

// MCPCapabilityFingerprint exposes the host-local security fingerprint used to
// reject a cached-to-live schema drift before tools/call. It is deliberately
// separate from Schema(), so it cannot perturb provider cache prefixes.
type MCPCapabilityFingerprint interface {
	MCPCapabilityFingerprint() string
}

// ReadOnlyExecutionTrustAuthority reports whether an MCP-backed tool's reader
// classification is backed by a real host trust store (receipts), rather than
// a server hint or legacy config compatibility. Strict read-only execution
// requires positive authority; direct library embedders without a TrustManager
// keep their historical behavior outside that boundary.
type ReadOnlyExecutionTrustAuthority interface {
	ReadOnlyExecutionTrustAuthority() bool
}

// readerExecutionIntentKey carries a per-call, immutable authorization basis:
// the call was approved as a non-destructive reader. The MCP dispatcher makes
// the final, linearizable check against live security state and must never
// promote such a call into a writer lane; drift after authorization returns an
// error instead of executing.
type readerExecutionIntentKey struct{}

// ReaderExecutionIntent pins what the authorization decision saw.
type ReaderExecutionIntent struct {
	// CapabilityFingerprint, when non-empty, must still match the live tool's
	// security fingerprint at dispatch time.
	CapabilityFingerprint string
}

// WithReaderExecutionIntent marks ctx as a reader-authorized MCP invocation.
func WithReaderExecutionIntent(ctx context.Context, capabilityFingerprint string) context.Context {
	return context.WithValue(ctx, readerExecutionIntentKey{}, ReaderExecutionIntent{CapabilityFingerprint: capabilityFingerprint})
}

// ReaderExecutionIntentFrom returns the pinned reader authorization, if any.
func ReaderExecutionIntentFrom(ctx context.Context) (ReaderExecutionIntent, bool) {
	intent, ok := ctx.Value(readerExecutionIntentKey{}).(ReaderExecutionIntent)
	return intent, ok
}

const (
	MCPApprovalAuto    = "auto"
	MCPApprovalPrompt  = "prompt"
	MCPApprovalWrites  = "writes"
	MCPApprovalApprove = "approve"

	MCPApprovalReviewerUser       = "user"
	MCPApprovalReviewerAutoReview = "auto_review"
)

// MCPApprovalPolicy exposes local execution policy for one MCP tool. These
// values are intentionally not part of Schema(), so changing approval policy
// does not alter the provider-visible tool contract or prompt-cache prefix.
type MCPApprovalPolicy interface {
	MCPApprovalMode() string
	MCPApprovalReviewer() string
}

// NormalizeMCPApprovalMode returns the conservative effective MCP approval
// mode. Empty keeps annotation-driven behavior; unknown values force a prompt.
func NormalizeMCPApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return MCPApprovalAuto
	case MCPApprovalAuto, MCPApprovalPrompt, MCPApprovalWrites, MCPApprovalApprove:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return MCPApprovalPrompt
	}
}

// NormalizeMCPApprovalReviewer returns the configured reviewer. Empty preserves
// legacy behavior at the controller boundary; unknown values fail back to the
// human reviewer rather than silently enabling automatic review.
func NormalizeMCPApprovalReviewer(reviewer string) string {
	switch strings.ToLower(strings.TrimSpace(reviewer)) {
	case "":
		return ""
	case MCPApprovalReviewerAutoReview, "guardian":
		return MCPApprovalReviewerAutoReview
	default:
		return MCPApprovalReviewerUser
	}
}

// SnipHint describes how context maintenance should shorten a stale, oversized
// result this tool produced. Head/Tail are the line counts kept from each end
// when the result has many lines; HeadChars/TailChars bound the kept runes when
// the result is one giant line. A zero value is invalid — implementers return
// positive counts. The geometry lives on the tool, not in a lookup table keyed
// by name, so renaming a tool carries its snip policy with it and a new tool
// cannot silently fall back to a generic default unnoticed (the contract test
// forces every registered tool to either implement SnipHinter or opt into the
// read-only/side-effecting default explicitly).
type SnipHint struct {
	Head      int
	Tail      int
	HeadChars int
	TailChars int
}

// SnipHinter is an optional capability a Tool implements when its output has a
// known shape that a generic head/tail split would garble — e.g. read_file
// front-loads the most relevant lines, while bash output is equally meaningful
// at both ends. Type-assert a Tool to discover support; tools that omit it take
// the ReadOnly-tiered default in the maintainer.
type SnipHinter interface {
	SnipHint() SnipHint
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
	mu        sync.RWMutex
	tools     map[string]Tool
	order     []string
	canon     map[string]json.RawMessage
	suspended map[string]bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}, canon: map[string]json.RawMessage{}, suspended: map[string]bool{}}
}

// Add inserts (or replaces) a tool, preserving first-seen order. The schema is
// canonicalized once here — it never changes after registration, so Schemas()
// (called every turn) reuses the result instead of re-marshaling.
func (r *Registry) Add(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := t.Name()
	for prefix := range r.suspended {
		if strings.HasPrefix(name, prefix) {
			return
		}
	}
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

// SuspendPrefix unregisters matching tools and prevents future Add calls for
// that prefix until ResumePrefix is called. It is used for per-session MCP
// disables where an in-flight background handshake may otherwise swap tools back
// into this registry after the user turned the server off.
func (r *Registry) SuspendPrefix(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.suspended[prefix] = true
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

// ResumePrefix allows future Add calls for a previously suspended prefix.
func (r *Registry) ResumePrefix(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.suspended, prefix)
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
