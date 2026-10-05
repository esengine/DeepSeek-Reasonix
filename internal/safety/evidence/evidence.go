package evidence

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"reasonix/internal/contract/provider"
)

// TodoItem mirrors the todo_write item shape the host needs for step matching.
// StepID is the item's stable identity: it survives a retitle and a reorder, so
// completion attribution never has to be inferred from wording or position. It
// is optional — a list written freehand has none, and matches by text instead.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm,omitempty"`
	Level      int    `json:"level,omitempty"`
	StepID     string `json:"step_id,omitempty"`
}

// todoSegment is one serial unit of a task list: a level-0 phase header plus
// its level-1 sub-steps, or a single plain step. end is exclusive.
type todoSegment struct {
	head int
	end  int
}

// TodoStepMatch is the result of matching a complete_step citation against the
// latest successful todo_write list in this turn.
type TodoStepMatch struct {
	Found      bool
	Index      int
	Content    string
	Status     string
	ActiveForm string
	StepID     string
}

// DeliveryCheckpoint is the compact, persistence-safe state carried across
// runs of one host-owned Goal. It intentionally stores no raw tool arguments or
// output. PendingMutation means a previously observed change still needs fresh
// verification, review, and sign-off before the Goal can finalize.
type DeliveryCheckpoint struct {
	ScopeID             string `json:"scopeID,omitempty"`
	CriteriaEstablished bool   `json:"criteriaEstablished,omitempty"`
	WorkObserved        bool   `json:"workObserved,omitempty"`
	MutationObserved    bool   `json:"mutationObserved,omitempty"`
	PendingMutation     bool   `json:"pendingMutation,omitempty"`
	// BaselineChecks are the criterion identities the task began requiring. They
	// ride the checkpoint because a controller rebuild re-reads the project's
	// declaration, which is exactly when a rewritten one would otherwise win.
	BaselineChecks []string `json:"baselineChecks,omitempty"`
	// Verification is the contract this Goal was accepted under. Nil is a
	// checkpoint written before contracts existed and is never backfilled from
	// the current declaration: that would invent an acceptance nobody made.
	Verification *VerificationContract `json:"verification,omitempty"`
	// OwedReview is the structured review a change in this scope still owes. It
	// rides the checkpoint because the ledger it is derived from does not.
	OwedReview *OwedReview `json:"owedReview,omitempty"`
	// Proven is what a pending change had already proven when the checkpoint
	// was written; it stands only while every path still holds that content.
	Proven *ProvenMutation `json:"proven,omitempty"`
	// Contract names the accepted contract revision's record in Trusted Host
	// State. The record, not this checkpoint, is the authority.
	Contract string `json:"contract,omitempty"`
}

// ProvenMutation pairs the proof a pending change had earned with a content
// fingerprint of each path it changed, taken when that proof was current.
type ProvenMutation struct {
	Paths         map[string]string `json:"paths"`
	Verified      bool              `json:"verified,omitempty"`
	SignedOff     bool              `json:"signedOff,omitempty"`
	Inspected     bool              `json:"inspected,omitempty"`
	ProjectChecks bool              `json:"projectChecks,omitempty"`
}

// OwedReview names the review kinds a high-risk change owes and the paths a
// review has to cover to settle them.
type OwedReview struct {
	Kinds []ReviewKind `json:"kinds"`
	Paths []string     `json:"paths,omitempty"`
}

// Ledger stores the receipts available to complete_step for the current turn.
type Ledger struct {
	mu               sync.Mutex
	receipts         []Receipt
	backgroundLeases []BackgroundLease
	closure          ClosureTally
}

func NewLedger() *Ledger { return &Ledger{} }

// Reset clears receipts and background leases between user turns.
func (l *Ledger) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.receipts = nil
	l.backgroundLeases = nil
	l.closure = ClosureTally{}
}

// Record appends a receipt. Failed receipts are retained for auditability but
// are never accepted by the HasSuccessful* matchers.
func (l *Ledger) Record(r Receipt) {
	if l == nil {
		return
	}
	r.Command = strings.TrimSpace(r.Command)
	r.Step = strings.TrimSpace(r.Step)
	if r.MutationPaths == nil {
		r.MutationPaths = append(append([]string{}, r.Paths...), r.Created...)
	} else {
		r.MutationPaths = append(append(append([]string{}, r.MutationPaths...), r.Paths...), r.Created...)
	}
	// Every field that names a file shares one identity. Folding only Paths made
	// the matchers compare a folded path against an unfolded one, so on Windows
	// output that carried a change never counted as review of it.
	r.Paths = normalizePaths(r.Paths)
	r.Showed = normalizePaths(r.Showed)
	r.Created = normalizePaths(r.Created)
	r.Viewed = normalizePaths(r.Viewed)
	r.Todos = normalizeTodos(r.Todos)
	if r.Args != nil {
		cp := make(json.RawMessage, len(r.Args))
		copy(cp, r.Args)
		r.Args = cp
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if r.ToolName == "complete_step" && r.Step != "" && r.TodoStep == nil {
		if match := latestTodoStep(r.Step, l.receipts); match.Found {
			r.TodoStep = &match
		}
	}
	l.receipts = append(l.receipts, r)
}

// Len returns the number of receipts recorded this turn, giving callers a
// stable index to pass to the *Since matchers.
func (l *Ledger) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.receipts)
}

// ReceiptProgressSummary counts successful host-observable receipts by category
// for cross-turn progress signatures. Failed receipts and reads never count:
// repeated reads, failed bookkeeping, and reworded answers must not masquerade
// as progress. Categories are not mutually exclusive (a successful bash command
// that also writes counts in both), which is fine for a change detector.
type ReceiptProgressSummary struct {
	Writes   int // successful mutations/writes
	Commands int // successful commands (bash receipts)
	Todos    int // successful todo_write receipts
	Signoffs int // successful complete_step signoffs
	Reviews  int // successful review receipts
}

// ReceiptProgressSummary returns the current ledger's progress counts.
func (l *Ledger) ReceiptProgressSummary() ReceiptProgressSummary {
	if l == nil {
		return ReceiptProgressSummary{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out ReceiptProgressSummary
	for _, r := range l.receipts {
		if !r.Success {
			continue
		}
		if r.Mutation || r.Write {
			out.Writes++
		}
		if r.Command != "" {
			out.Commands++
		}
		if r.ToolName == "todo_write" {
			out.Todos++
		}
		if r.ToolName == "complete_step" && r.StepProof {
			out.Signoffs++
		}
		if successfulForegroundReviewReceipt(r) || completedStructuredReviewReceipt(r, nil) {
			out.Reviews++
		}
	}
	return out
}

func successfulForegroundReviewReceipt(r Receipt) bool {
	if !r.Success {
		return false
	}
	if r.ToolName == "review" {
		return true
	}
	if r.ToolName != "task" || r.Profile != "review" {
		return false
	}
	var p struct {
		RunInBackground bool `json:"run_in_background"`
	}
	return json.Unmarshal(r.Args, &p) == nil && !p.RunInBackground
}

// SuccessfulCommands returns up to limit successful bash commands from this
// turn, most recent first, for self-correction hints in rejection errors.
func (l *Ledger) SuccessfulCommands(limit int) []string {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i := len(l.receipts) - 1; i >= 0 && len(out) < limit; i-- {
		r := l.receipts[i]
		if r.Success && r.ToolName == "bash" && r.Command != "" {
			out = append(out, r.Command)
		}
	}
	return out
}

// TouchedPaths returns up to limit distinct paths from this turn's successful
// receipts, most recent first; writtenOnly restricts it to writer receipts.
func (l *Ledger) TouchedPaths(limit int, writtenOnly bool) []string {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for i := len(l.receipts) - 1; i >= 0 && len(out) < limit; i-- {
		r := l.receipts[i]
		if !r.Success || (writtenOnly && !r.Write) || (!writtenOnly && !r.Read && !r.Write) {
			continue
		}
		for _, p := range r.Paths {
			if !seen[p] && len(out) < limit {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func (l *Ledger) citedChecksAfter(after int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i := max(after+1, 0); i < len(l.receipts); i++ {
		if r := l.receipts[i]; r.Success && r.ToolName == "complete_step" {
			out = append(out, r.CitedChecks...)
		}
	}
	return out
}

func (l *Ledger) deliverySignoffAfter(after int, needReview bool) bool {
	if l == nil {
		return false
	}
	start := max(after+1, 0)

	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	for i := start; i < len(receipts); i++ {
		r := receipts[i]
		if !r.Success || r.ToolName != "complete_step" {
			continue
		}
		if needReview && after >= 0 && !receiptsReviewChanges(receipts, start, i, after) {
			continue
		}
		for _, command := range completeStepVerificationCommands(r.Args) {
			if !bashCommandIsVerification(command) {
				continue
			}
			for j := start; j < i; j++ {
				candidate := receipts[j]
				if candidate.ToolName == "bash" && CommandMatches(command, candidate.Command) && verificationPassed(candidate) {
					return true
				}
			}
		}
	}
	return false
}

// pathsAnswerFor reports whether one of the observed paths is the claimed one,
// matched on whole trailing components so a receipt's absolute path answers for
// a workspace-relative claim — and path.bak never answers for path.
func pathsAnswerFor(observed []string, needle string) bool {
	for _, path := range observed {
		candidate := strings.ToLower(filepath.ToSlash(normalizePath(path)))
		if candidate == needle || strings.HasSuffix(candidate, "/"+needle) {
			return true
		}
	}
	return false
}

// receiptsReviewChanges reports whether the change at mutationIndex was
// inspected between start and end. It asks the same question the coverage check
// asks — did an output carry the change — because a command's shape answers
// neither: `git status` prints names and `echo path/to/a.go` prints the path,
// and both used to count here while showing nothing of what changed.
func receiptsReviewChanges(receipts []Receipt, start, end, mutationIndex int) bool {
	if mutationIndex >= len(receipts) {
		return false
	}
	// A negative mutationIndex is the restored-checkpoint baseline, and a
	// mutation that named no path is the shell equivalent: what it touched is
	// unknowable either way.
	var wanted map[string]bool
	if mutationIndex >= 0 {
		wanted = pathSet(receipts[mutationIndex].Paths)
	}
	for i := start; i < end && i < len(receipts); i++ {
		r := receipts[i]
		if !r.Success {
			continue
		}
		// Nothing is known about what the change touched, so the most that can
		// honestly be required is that the turn looked at something.
		if len(wanted) == 0 {
			if r.Read || len(r.Showed) > 0 {
				return true
			}
			continue
		}
		if r.Read && pathSetHas(wanted, r.Paths) {
			return true
		}
		if pathSetHas(wanted, r.Showed) {
			return true
		}
	}
	return false
}

func pathSetHas(wanted map[string]bool, observed []string) bool {
	for _, p := range observed {
		if wanted[p] {
			return true
		}
	}
	return false
}

func (l *Ledger) LatestSuccessfulWriteIndex(paths []string) (int, bool) {
	wanted := pathSet(normalizePaths(paths))
	if l == nil || len(wanted) == 0 {
		return 0, false
	}
	latest := -1

	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.receipts {
		if !r.Success || !r.Write {
			continue
		}
		for _, p := range r.Paths {
			if wanted[p] {
				latest = i
				break
			}
		}
	}
	return latest, latest >= 0
}

func anchorRefreshRead(r Receipt) bool {
	if r.ToolName != "read_file" || !r.Read {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(r.Args, &fields); err != nil {
		return false
	}
	if limit, ok := intField(fields, "limit"); ok && limit > 0 {
		return false
	}
	if offset, ok := intField(fields, "offset"); ok && offset > 0 {
		return false
	}
	return true
}

type contextKey struct{}
type sessionMessagesKey struct{}
type deliveryProfileKey struct{}
type todoStateKey struct{}

func WithLedger(ctx context.Context, ledger *Ledger) context.Context {
	if ledger == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, ledger)
}

func FromContext(ctx context.Context) (*Ledger, bool) {
	ledger, ok := ctx.Value(contextKey{}).(*Ledger)
	return ledger, ok && ledger != nil
}

// WithDeliveryProfile marks tool execution as subject to the delivery-first
// final-readiness contract. Tools use this only for stricter evidence validation;
// it is ephemeral host state and is never serialized into sessions or prompts.
func WithDeliveryProfile(ctx context.Context) context.Context {
	return context.WithValue(ctx, deliveryProfileKey{}, true)
}

// DeliveryProfileFromContext reports whether the current tool call must produce
// evidence that the delivery final-readiness gate can accept.
func DeliveryProfileFromContext(ctx context.Context) bool {
	enabled, _ := ctx.Value(deliveryProfileKey{}).(bool)
	return enabled
}

// WithSessionMessages attaches a lazy transcript accessor so verifyStepEvidence
// can fall back to scanning the conversation when the per-turn ledger misses a
// command (cross-turn references, non-bash tool calls, truncated command
// strings). The context carries the capability, not the data: snapshot is
// called only when a consumer (complete_step) actually needs the history, so
// ordinary tool calls never pay for a full transcript copy.
func WithSessionMessages(ctx context.Context, snapshot func() []provider.Message) context.Context {
	return context.WithValue(ctx, sessionMessagesKey{}, snapshot)
}

// SessionMessagesFromContext resolves the transcript accessor attached by
// WithSessionMessages, taking the snapshot at call time.
func SessionMessagesFromContext(ctx context.Context) ([]provider.Message, bool) {
	snapshot, ok := ctx.Value(sessionMessagesKey{}).(func() []provider.Message)
	if !ok || snapshot == nil {
		return nil, false
	}
	return snapshot(), true
}

// WithTodoState attaches the host's canonical task list to a tool call. The
// per-turn ledger resets between user messages, while unfinished tasks remain
// active across those turns.
func WithTodoState(ctx context.Context, todos []TodoItem) context.Context {
	return context.WithValue(ctx, todoStateKey{}, append([]TodoItem(nil), todos...))
}

// TodoStateFromContext returns a copy of the host's canonical task list.
func TodoStateFromContext(ctx context.Context) ([]TodoItem, bool) {
	todos, ok := ctx.Value(todoStateKey{}).([]TodoItem)
	return append([]TodoItem(nil), todos...), ok
}

// PathsProvenInSession reports whether every path is covered by a successful
// (non-errored) tool call somewhere in msgs — the cross-turn fallback for diff
// and files evidence, whose per-turn ledger resets each turn. wantWrite
// restricts to writer tools (diff); false accepts either. A replay carries only
// names, so facts resolves each to the contracts its tool declares.
func PathsProvenInSession(msgs []provider.Message, paths []string, wantWrite bool, facts func(string) ToolFacts) bool {
	wanted := pathSet(normalizePaths(paths))
	if len(wanted) == 0 {
		return false
	}
	failed := failedSessionCallIDs(msgs)
	found := map[string]bool{}
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if failed[tc.ID] {
				continue
			}
			r := ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, facts(tc.Name))
			if wantWrite && !r.Write {
				continue
			}
			if !wantWrite && !r.Read && !r.Write {
				continue
			}
			for _, p := range normalizePaths(r.Paths) {
				if _, ok := wanted[p]; ok {
					found[p] = true
				}
			}
		}
	}
	return len(found) == len(wanted)
}

func failedSessionCallIDs(msgs []provider.Message) map[string]bool {
	failed := map[string]bool{}
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool || msg.ToolCallID == "" {
			continue
		}
		if strings.HasPrefix(msg.Content, "error:") || strings.HasPrefix(msg.Content, "blocked:") {
			failed[msg.ToolCallID] = true
		}
	}
	return failed
}

func ReceiptFromToolCall(toolName string, args json.RawMessage, success bool, facts ToolFacts) Receipt {
	class := ToolCallMutationClassWith(toolName, args, facts)
	r := Receipt{
		ToolName: toolName,
		Args:     args,
		Success:  success,
		Mutation: class != MutationNone,
	}
	if r.Mutation {
		r.MutationEvidence = class
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err == nil {
		if toolName == "bash" {
			r.Command = stringField(fields, "command")
		}
		if toolName == "task" {
			r.Profile = stringField(fields, "profile")
		}
		if toolName == "complete_step" {
			r.Step = completeStepIdentity(fields)
			r.StepProof = completeStepHasProof(fields)
			r.CitedChecks = completeStepCitedChecks(fields)
		}
		if toolName == "todo_write" {
			r.Todos = todoItemsField(fields, "todos")
		}
		if toolName == reviewReportTool {
			r.ReportKind = parsedReportKind(fields)
		}
		r.Paths = extractPaths(fields)
	}

	if facts.WritesNamedPaths {
		r.Write = true
	} else if isReadReceipt(toolName, facts.ReadOnly) {
		r.Read = true
	}
	return r
}

// ToolCallPaths returns the bounded, structurally declared file paths in a
// tool call. It intentionally does not attempt to parse shell scripts; callers
// must treat bash and unknown targets as allPaths when invalidation is needed.
func ToolCallPaths(args json.RawMessage) []string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil
	}
	paths := extractPaths(fields)
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

// ToolCallMutates is the delivery profile's conservative state-change
// classifier. Trusted read-only tools never mutate. Meta tools that only
// delegate (task, run_skill, review, …) never mutate by themselves — real
// writes arrive via child evidence merge. Writer-capable tools do mutate,
// except for bash commands that the host can prove are inspection or
// verification commands.
func ToolCallMutates(toolName string, args json.RawMessage, readOnly bool) bool {
	return ToolCallMutationClass(toolName, args, readOnly) != MutationNone
}

// ToolCallRequiresDeliveryCriteria reports whether a call begins execution
// work that needs an acceptance contract. Mutations always qualify; verification
// commands also qualify even though they are intentionally not mutations.
func ToolCallRequiresDeliveryCriteria(toolName string, args json.RawMessage, readOnly bool) bool {
	if ToolCallMutates(toolName, args, readOnly) {
		return true
	}
	if toolName != "bash" {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return true
	}
	return bashCommandIsVerification(stringField(fields, "command"))
}

type verificationCommandRecommendation struct {
	label    string
	examples []string
}

// writeOutputFlags are test-runner and linter flags that write snapshot,
// report, or profile files. Snapshot flags rewrite checked-in fixtures (the
// --update/-u class rejected above); the others write explicit output paths.
// A runner invoked with one of them changes workspace state, so the segment
// must not count as read-only verification.
var writeOutputFlags = map[string]bool{
	"snapshot-update":                  true, // pytest-snapshot / syrupy
	"updatesnapshot":                   true, // jest --updateSnapshot via npm/yarn wrappers
	"junitxml":                         true, // pytest
	"junit-xml":                        true, // pytest / mypy
	"junitfile":                        true, // gotestsum
	"jsonfile":                         true, // gotestsum
	"coverprofile":                     true, // go test
	"cpuprofile":                       true, // go test
	"memprofile":                       true, // go test
	"blockprofile":                     true, // go test
	"mutexprofile":                     true, // go test
	"testlogfile":                      true, // go test binary
	"gocoverdir":                       true, // go test binary
	"outputfile":                       true, // jest/vitest --outputFile (with --json)
	"report-log":                       true, // pytest-reportlog
	"xunit-output":                     true, // swift test --xunit-output writes a JUnit XML report
	"scratch-path":                     true, // swift test --scratch-path redirects the build dir
	"build-path":                       true, // swift test --build-path: legacy alias of --scratch-path
	"cache-path":                       true, // swift test --cache-path redirects the shared cache dir
	"event-stream-output-path":         true, // swift test (Swift 6.x): swift-testing JSON output
	"experimental-event-stream-output": true, // swift test (Swift 6.x): experimental event-stream output
	"attachments-path":                 true, // swift test (Swift 6.x): Swift Testing attachments dir
	"experimental-attachments-path":    true, // swift test (Swift 6.x): experimental attachments dir
}

func isReadReceipt(name string, readOnly bool) bool {
	switch name {
	case "todo_write", "complete_step":
		return false
	default:
		return isReaderTool(name) || readOnly
	}
}

func isReaderTool(name string) bool {
	switch name {
	case "read_file", "ls", "grep":
		return true
	default:
		return false
	}
}

func extractPaths(fields map[string]json.RawMessage) []string {
	var paths []string
	for _, key := range []string{"path", "file_path", "notebook_path", "source_path", "destination_path"} {
		if s := stringField(fields, key); s != "" {
			paths = append(paths, s)
		}
	}
	for _, key := range []string{"paths", "file_paths"} {
		paths = append(paths, stringSliceField(fields, key)...)
	}
	return paths
}

func stringField(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func intField(fields map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

func stringSliceField(fields map[string]json.RawMessage, key string) []string {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func pathSet(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func normalizePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = normalizePath(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NormalizePath is the identity a path has in the ledger. Anything keying its
// own map by path has to use it: Windows hands back the same file under
// different cases, and a raw key then watches it twice.
func NormalizePath(p string) string { return normalizePath(p) }

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	p = filepath.Clean(filepath.FromSlash(p))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
