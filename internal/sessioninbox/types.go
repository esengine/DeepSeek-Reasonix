package sessioninbox

import (
	"errors"
	"time"
)

// SchemaVersion is the on-disk format version. Unknown higher versions load
// read-only and force pause; they never auto-execute.
const SchemaVersion = 2

// Default capacity limits.
const (
	DefaultMaxItems      = 64
	DefaultMaxItemBytes  = 4 << 20  // 4 MiB
	DefaultMaxTotalBytes = 64 << 20 // 64 MiB
	DefaultPreviewRunes  = 120
)

// InboxIntent distinguishes durable follow-up turns from mid-turn steers.
type InboxIntent string

const (
	IntentFollowup InboxIntent = "followup"
	IntentSteer    InboxIntent = "steer"
)

// InboxState is the durable lifecycle of one queue item.
type InboxState string

const (
	StateQueued        InboxState = "queued"
	StateSteerAccepted InboxState = "steer_accepted"
	StateSteerConsumed InboxState = "steer_consumed"
	StateRunning       InboxState = "running"
	StateBlocked       InboxState = "blocked"
	StateUncertain     InboxState = "uncertain"
)

// Disposition reports how an admission attempt settled.
type Disposition string

const (
	DispositionStarted          Disposition = "started"
	DispositionSteerAccepted    Disposition = "steer_accepted"
	DispositionQueuedFollowup   Disposition = "queued_followup"
	DispositionRejectedBusy     Disposition = "rejected_busy"
	DispositionRejectedRotating Disposition = "rejected_rotating"
	DispositionRejectedClosed   Disposition = "rejected_closed"
	DispositionRejectedCapacity Disposition = "rejected_capacity"
	DispositionIdempotentHit    Disposition = "idempotent_hit"
)

// Sentinel errors for capacity and validation.
var (
	ErrCapacityItems       = errors.New("session inbox item limit reached")
	ErrCapacityBytes       = errors.New("session inbox byte limit reached")
	ErrItemTooLarge        = errors.New("inbox item exceeds single-item size limit")
	ErrNotFound            = errors.New("inbox item not found")
	ErrInvalidState        = errors.New("inbox item state does not allow this operation")
	ErrSchemaReadonly      = errors.New("inbox schema is newer and is read-only")
	ErrClosed              = errors.New("inbox is closed")
	ErrEmpty               = errors.New("inbox item body is empty")
	ErrPaused              = errors.New("inbox is paused")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for different input")
)

// InboxItemMeta is the durable metadata kept in the manifest (never the body).
type InboxItemMeta struct {
	ID        string      `json:"id"`
	SessionID string      `json:"sessionId,omitempty"`
	Intent    InboxIntent `json:"intent"`
	State     InboxState  `json:"state"`
	Revision  int64       `json:"revision"`
	// BlobName is the on-disk blob filename stem (without .json). Empty means
	// legacy layout where the blob is named by item ID. Updates write a new
	// immutable blob and switch this pointer after manifest commit.
	BlobName    string       `json:"blobName,omitempty"`
	Source      string       `json:"source,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
	Preview     string       `json:"preview"`
	ByteSize    int64        `json:"byteSize"`
	Checksum    string       `json:"checksum"`
	Idempotency string       `json:"idempotencyKey,omitempty"`
	Refs        []RefSummary `json:"refs,omitempty"`
	BlockReason string       `json:"blockReason,omitempty"`
	RunID       string       `json:"runId,omitempty"`
}

// RefSummary is a short reference summary stored in the manifest.
type RefSummary struct {
	Kind    string `json:"kind"` // clean_git | frozen | external | attachment
	Path    string `json:"path,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Preview string `json:"preview,omitempty"`
}

// RefSnapshot freezes a resolved @-reference at enqueue time.
type RefSnapshot struct {
	Kind         string `json:"kind"` // clean_git | frozen | external | attachment | mcp
	Path         string `json:"path,omitempty"`
	DisplayPath  string `json:"displayPath,omitempty"`
	RepoIdentity string `json:"repoIdentity,omitempty"`
	Commit       string `json:"commit,omitempty"`
	RangeStart   int    `json:"rangeStart,omitempty"`
	RangeEnd     int    `json:"rangeEnd,omitempty"`
	Content      []byte `json:"content,omitempty"`
	ContentSHA   string `json:"contentSha,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
	Server       string `json:"server,omitempty"`
	URI          string `json:"uri,omitempty"`
}

// StructuredInvocation is a frozen slash/command invocation.
type StructuredInvocation struct {
	Name    string            `json:"name,omitempty"`
	Kind    string            `json:"kind,omitempty"`
	Offset  int               `json:"offset,omitempty"`
	Args    map[string]string `json:"args,omitempty"`
	Display string            `json:"display,omitempty"`
}

// PromptEnvelope is the full durable body stored only in blobs/<id>.json.
type PromptEnvelope struct {
	DisplayText string `json:"displayText"`
	RawText     string `json:"rawText"`
	SubmitText  string `json:"submitText"`
	// Invocation is retained for schema-v1 compatibility. New writers use
	// Invocations so multiple rich-composer entities preserve visual order.
	Invocation  *StructuredInvocation  `json:"invocation,omitempty"`
	Invocations []StructuredInvocation `json:"invocations,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Attachments []string               `json:"attachments,omitempty"`
	Refs        []RefSnapshot          `json:"refs,omitempty"`
	// FrozenRefBlock is the exact typed reference context rendered at enqueue.
	// FrozenImages contains already-authorized data URLs for direct image input.
	FrozenRefBlock  string            `json:"frozenRefBlock,omitempty"`
	FrozenImages    []string          `json:"frozenImages,omitempty"`
	ReferenceErrors []string          `json:"referenceErrors,omitempty"`
	ExplicitRefs    []string          `json:"explicitRefs,omitempty"`
	Idempotency     string            `json:"idempotencyKey,omitempty"`
	Source          string            `json:"source,omitempty"`
	Extra           map[string]string `json:"extra,omitempty"`
}

// Capacity describes current usage against limits.
type Capacity struct {
	Items        int   `json:"items"`
	MaxItems     int   `json:"maxItems"`
	Bytes        int64 `json:"bytes"`
	MaxBytes     int64 `json:"maxBytes"`
	MaxItemBytes int64 `json:"maxItemBytes"`
}

// InboxSnapshot is the frontend-safe view: metadata only, never full bodies.
type InboxSnapshot struct {
	SchemaVersion int             `json:"schemaVersion"`
	Revision      int64           `json:"revision"`
	Paused        bool            `json:"paused"`
	Recovered     bool            `json:"recovered"`
	RecoveredN    int             `json:"recoveredCount,omitempty"`
	Readonly      bool            `json:"readonly,omitempty"`
	RunID         string          `json:"runId,omitempty"`
	SessionPath   string          `json:"sessionPath,omitempty"`
	Items         []InboxItemMeta `json:"items"`
	Capacity      Capacity        `json:"capacity"`
}

// InboxReceipt is returned after a durable enqueue or admission attempt.
type InboxReceipt struct {
	ItemID      string      `json:"itemId"`
	Disposition Disposition `json:"disposition"`
	Position    int         `json:"position"`
	Paused      bool        `json:"paused"`
	Capacity    Capacity    `json:"capacity"`
	Idempotent  bool        `json:"idempotent,omitempty"`
}

// EnqueueRequest is the input for durable admission.
type EnqueueRequest struct {
	Intent      InboxIntent
	Envelope    PromptEnvelope
	Source      string
	Idempotency string
	SessionID   string
}

// Limits configures capacity. Zero fields use defaults.
type Limits struct {
	MaxItems      int
	MaxItemBytes  int64
	MaxTotalBytes int64
}

func (l Limits) withDefaults() Limits {
	if l.MaxItems <= 0 {
		l.MaxItems = DefaultMaxItems
	}
	if l.MaxItemBytes <= 0 {
		l.MaxItemBytes = DefaultMaxItemBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = DefaultMaxTotalBytes
	}
	return l
}
