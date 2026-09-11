package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"sync"
)

// OperationState is the host's lifecycle for one intended change. The model
// never writes it: every transition is a real tool result, so a completion
// claim can never move an operation the host did not observe.
type OperationState string

const (
	OperationPrepared            OperationState = "prepared"
	OperationApplied             OperationState = "applied"
	OperationVerificationPending OperationState = "verification_pending"
	OperationSettled             OperationState = "settled"
	OperationFailed              OperationState = "failed"
	OperationUnknown             OperationState = "unknown"
	OperationNeedsUser           OperationState = "needs_user"
)

// ReceiptRef kinds. They classify what a receipt proves, not which tool ran, so
// a shell verifier and a built-in checker are citable the same way.
const (
	ReceiptKindRead         = "read"
	ReceiptKindMutation     = "mutation"
	ReceiptKindVerification = "verification"
	ReceiptKindReview       = "review"
	ReceiptKindCommand      = "command"
)

// operationRecoveryBudget is how many times the host hands the model a
// machine-executable recovery for the same operation failing the same way.
// The second identical failure is not a self-correction, it is a loop: the
// operation goes to the user instead of back to the model.
const operationRecoveryBudget = 2

// Operation is the host-authoritative record of one intended change. It spans
// the read that justified it, the mutation that applied it, and the
// verification that settled it, so verification binds to the real change
// rather than to a model-maintained task item.
type Operation struct {
	ID            string         `json:"id"`
	Tool          string         `json:"tool,omitempty"`
	TargetPaths   []string       `json:"target_paths,omitempty"`
	SourceTokens  []string       `json:"source_tokens,omitempty"`
	Mutation      *ReceiptRef    `json:"mutation,omitempty"`
	Verification  *ReceiptRef    `json:"verification,omitempty"`
	State         OperationState `json:"state"`
	FailureCode   string         `json:"failure_code,omitempty"`
	RecoveryCount int            `json:"recovery_count,omitempty"`
}

func (o Operation) clone() Operation {
	o.TargetPaths = slices.Clone(o.TargetPaths)
	o.SourceTokens = slices.Clone(o.SourceTokens)
	if o.Mutation != nil {
		ref := o.Mutation.clone()
		o.Mutation = &ref
	}
	if o.Verification != nil {
		ref := o.Verification.clone()
		o.Verification = &ref
	}
	return o
}

// Terminal reports whether the operation reached a state the host will not
// leave on its own. A terminal operation never transitions twice.
func (o Operation) Terminal() bool {
	return o.State == OperationSettled || o.State == OperationNeedsUser
}

// RecoveryDecision is the host's bounded answer to one operation failure. It
// replaces open-ended "try again" prose: the model either performs the offered
// recovery or the operation belongs to the user.
type RecoveryDecision struct {
	State OperationState
	// Attempt is how many times this operation has failed this way, including
	// the failure being reported.
	Attempt int
	// Retryable is false once the budget is spent; the host then stops sending
	// the same rejection back to the model.
	Retryable bool
	// Budget is how many further automatic attempts remain.
	Budget int
}

type operationFailureKey struct{ operation, code string }

// OperationLedger owns operation identity and lifecycle for one turn. It is
// separate from the receipt list because a receipt records what happened while
// an operation records what was intended and whether it is finished.
type OperationLedger struct {
	mu        sync.Mutex
	ops       map[string]*Operation
	order     []string
	failures  map[operationFailureKey]int
	announced map[string]bool
}

func NewOperationLedger() *OperationLedger {
	return &OperationLedger{ops: map[string]*Operation{}, failures: map[operationFailureKey]int{}, announced: map[string]bool{}}
}

// OperationID derives a stable identity from what a call intends to do, never
// from the provider's per-round call ID. A retry of the same edit is the same
// operation, which is what makes repeat-failure detection possible at all.
func OperationID(toolName string, args json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(toolName)))
	h.Write([]byte{0})
	h.Write(canonicalArguments(args))
	return "op_" + hex.EncodeToString(h.Sum(nil)[:8])
}

// canonicalArguments renders arguments so cosmetic JSON differences (key order,
// whitespace) do not fork one operation into two. Unparseable arguments fall
// back to their bytes.
func canonicalArguments(args json.RawMessage) []byte {
	if len(bytes.TrimSpace(args)) == 0 {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return slices.Clone(args)
	}
	// Attempt/recovery handles identify one execution, not the intended
	// operation. Strip them so a fresh source token starts a new recovery epoch
	// for the same operation instead of bypassing its budget.
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{
			"source_token", "sourceToken",
			"expected_digest", "expectedDigest",
			"expected_snapshot", "expectedSnapshot",
			"receipt_id", "receipt_ids",
			"operation_id", "operationId",
			"call_id", "callId",
			"documentToken",
			"cursor",
		} {
			delete(object, key)
		}
		value = object
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return slices.Clone(args)
	}
	return canonical
}

func (l *OperationLedger) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = map[string]*Operation{}
	l.order = nil
	l.failures = map[operationFailureKey]int{}
	l.announced = map[string]bool{}
}

// Open records an intent. It is idempotent: resubmitting the same operation
// returns the state the host already holds instead of starting a second one.
func (l *OperationLedger) Open(id, toolName string, paths []string) Operation {
	if l == nil || id == "" {
		return Operation{ID: id, Tool: toolName, State: OperationPrepared}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op := l.mustGet(id)
	if op.Tool == "" {
		op.Tool = toolName
	}
	for _, path := range normalizePaths(paths) {
		if !slices.Contains(op.TargetPaths, path) {
			op.TargetPaths = append(op.TargetPaths, path)
		}
	}
	return op.clone()
}

// mustGet requires l.mu.
func (l *OperationLedger) mustGet(id string) *Operation {
	if l.ops == nil {
		l.ops = map[string]*Operation{}
	}
	op, ok := l.ops[id]
	if !ok {
		op = &Operation{ID: id, State: OperationPrepared}
		l.ops[id] = op
		l.order = append(l.order, id)
	}
	return op
}

// NoteSourceToken binds the read that justifies this operation, so a later
// source-version change can invalidate exactly the operations it affects.
func (l *OperationLedger) NoteSourceToken(id, token string) {
	if l == nil || id == "" || strings.TrimSpace(token) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op := l.mustGet(id)
	if !slices.Contains(op.SourceTokens, token) {
		op.SourceTokens = append(op.SourceTokens, token)
	}
}

// Apply records that the intended change really landed. A terminal operation
// is not reopened: a duplicate submission reports the state it already has.
func (l *OperationLedger) Apply(id string, ref ReceiptRef) Operation {
	if l == nil || id == "" {
		return Operation{ID: id}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op := l.mustGet(id)
	if op.Terminal() {
		return op.clone()
	}
	stored := ref.clone()
	op.Mutation = &stored
	op.FailureCode = ""
	op.State = OperationApplied
	return op.clone()
}

// AttachVerification binds a successful check to the change it covers. Only a
// successful receipt settles: a failing verifier leaves the operation applied
// and unverified, which is the honest state.
func (l *OperationLedger) AttachVerification(id string, ref ReceiptRef) Operation {
	if l == nil || id == "" {
		return Operation{ID: id}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op := l.mustGet(id)
	if op.Terminal() {
		return op.clone()
	}
	stored := ref.clone()
	op.Verification = &stored
	if ref.Success && operationCoveredByVerificationPaths(op, normalizePaths(ref.Paths)) {
		op.State = OperationSettled
		op.FailureCode = ""
	} else if op.State == OperationApplied {
		op.State = OperationVerificationPending
	}
	return op.clone()
}

// AttachLatestVerification binds a successful check to the most recent
// unsettled change it covers. Coverage is by target path, never by command
// text: a path-scoped verifier must name every target, while a pathless
// whole-suite verifier covers the latest unsettled operation.
func (l *OperationLedger) AttachLatestVerification(ref ReceiptRef) (Operation, bool) {
	if l == nil || !ref.Success {
		return Operation{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	paths := normalizePaths(ref.Paths)
	var fallback *Operation
	for _, id := range slices.Backward(l.order) {
		op, ok := l.ops[id]
		if !ok || op.Terminal() || op.Mutation == nil {
			continue
		}
		if len(paths) > 0 && operationCoveredByVerificationPaths(op, paths) {
			return l.attachLocked(op, ref), true
		}
		if fallback == nil {
			fallback = op
		}
	}
	// A verifier that names no file (a whole-suite run) covers the latest
	// unsettled change; a narrower run that named other files does not.
	if len(paths) == 0 && fallback != nil {
		return l.attachLocked(fallback, ref), true
	}
	return Operation{}, false
}

// attachLocked requires l.mu.
func (l *OperationLedger) attachLocked(op *Operation, ref ReceiptRef) Operation {
	stored := ref.clone()
	op.Verification = &stored
	op.State = OperationSettled
	op.FailureCode = ""
	return op.clone()
}

func operationCoveredByVerificationPaths(op *Operation, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	if op == nil || len(op.TargetPaths) == 0 {
		return false
	}
	for _, target := range op.TargetPaths {
		if !slices.Contains(paths, target) {
			return false
		}
	}
	return true
}

// Settle finishes an operation the host has nothing left to check. Repeat
// settles are idempotent, so a duplicate tool result cannot double-count work.
func (l *OperationLedger) Settle(id string) Operation {
	if l == nil || id == "" {
		return Operation{ID: id}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op := l.mustGet(id)
	if op.Terminal() {
		return op.clone()
	}
	op.State = OperationSettled
	op.FailureCode = ""
	return op.clone()
}

// MarkUnknown records an effect the host could not confirm either way. It is
// never settled automatically: only the user can resolve it.
func (l *OperationLedger) MarkUnknown(id, code string) Operation {
	if l == nil || id == "" {
		return Operation{ID: id}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op := l.mustGet(id)
	if op.State == OperationSettled {
		return op.clone()
	}
	op.State = OperationUnknown
	op.FailureCode = code
	return op.clone()
}

// Fail records one rejection and returns how much automatic recovery is left.
// The budget is keyed on (operation, failure code): a different failure is new
// information and gets its own budget, while the same rejection twice is a
// loop and goes to the user.
func (l *OperationLedger) Fail(id, code string) RecoveryDecision {
	if l == nil || id == "" {
		return RecoveryDecision{State: OperationFailed, Attempt: 1, Retryable: true, Budget: operationRecoveryBudget - 1}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failures == nil {
		l.failures = map[operationFailureKey]int{}
	}
	key := operationFailureKey{id, code}
	l.failures[key]++
	attempt := l.failures[key]
	op := l.mustGet(id)
	if op.State == OperationSettled {
		return RecoveryDecision{State: op.State, Attempt: attempt}
	}
	op.FailureCode = code
	op.RecoveryCount = attempt
	if attempt >= operationRecoveryBudget {
		op.State = OperationNeedsUser
		return RecoveryDecision{State: OperationNeedsUser, Attempt: attempt}
	}
	op.State = OperationFailed
	return RecoveryDecision{State: OperationFailed, Attempt: attempt, Retryable: true, Budget: operationRecoveryBudget - attempt}
}

// NewEpoch reopens automatic recovery for one operation. Only real new
// information calls it: a changed source version, fresh evidence, or the user
// explicitly asking to continue. The model cannot reach it.
func (l *OperationLedger) NewEpoch(id string) {
	if l == nil || id == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key := range l.failures {
		if key.operation == id {
			delete(l.failures, key)
		}
	}
	op, ok := l.ops[id]
	if !ok {
		return
	}
	delete(l.announced, id)
	op.RecoveryCount = 0
	op.FailureCode = ""
	if op.State == OperationNeedsUser || op.State == OperationFailed {
		op.State = OperationPrepared
		if op.Mutation != nil {
			op.State = OperationApplied
		}
	}
}

func (l *OperationLedger) Get(id string) (Operation, bool) {
	if l == nil || id == "" {
		return Operation{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.ops[id]
	if !ok {
		return Operation{}, false
	}
	return op.clone(), true
}

// NeedsUser returns the operations the host stopped automating, in the order
// they were opened, so a frontend can show them as one bounded list.
func (l *OperationLedger) NeedsUser() []Operation {
	return l.filter(func(op *Operation) bool { return op.State == OperationNeedsUser })
}

// TakeNewlyNeedsUser returns the operations that reached needs_user since the
// last call. The host announces each stop exactly once: repeating it every
// round would be the same repetition this guard exists to end.
func (l *OperationLedger) TakeNewlyNeedsUser() []Operation {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Operation
	for _, id := range l.order {
		op, ok := l.ops[id]
		if !ok || op.State != OperationNeedsUser || l.announced[id] {
			continue
		}
		if l.announced == nil {
			l.announced = map[string]bool{}
		}
		l.announced[id] = true
		out = append(out, op.clone())
	}
	return out
}

// Unsettled returns operations that changed something without reaching a
// terminal state — the Delivery gap list.
func (l *OperationLedger) Unsettled() []Operation {
	return l.filter(func(op *Operation) bool {
		return op.Mutation != nil && op.State != OperationSettled
	})
}

func (l *OperationLedger) Snapshot() []Operation {
	return l.filter(func(*Operation) bool { return true })
}

func (l *OperationLedger) filter(keep func(*Operation) bool) []Operation {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Operation
	for _, id := range l.order {
		op, ok := l.ops[id]
		if ok && keep(op) {
			out = append(out, op.clone())
		}
	}
	return out
}
