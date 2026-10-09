package feedback

import (
	"errors"
	"time"
)

// Sentinels are the identities a caller branches on with errors.Is. The service
// codes come from the worker's own error codes; Offline and Unavailable are the
// two ways a dependency can fail without saying anything.
var (
	ErrInvalid       = errors.New("feedback: invalid")
	ErrTooLarge      = errors.New("feedback: too large")
	ErrRateLimited   = errors.New("feedback: rate limited")
	ErrDisabled      = errors.New("feedback: disabled")
	ErrDuplicate     = errors.New("feedback: duplicate")
	ErrBadToken      = errors.New("feedback: install token rejected")
	ErrBusy          = errors.New("feedback: service is at capacity")
	ErrImageMetadata = errors.New("feedback: image carries metadata")
	// ErrReplyLimit: the report's thread is full or the install replied too
	// often. ErrNotReplyable: the report is in a state that takes no reply.
	// ErrChallengeRequired: the service wants a verification token with the
	// submit.
	ErrReplyLimit        = errors.New("feedback: reply limit reached")
	ErrNotReplyable      = errors.New("feedback: report takes no reply")
	ErrChallengeRequired = errors.New("feedback: verification required")
	ErrOffline           = errors.New("feedback: service unreachable")
	ErrUnavailable       = errors.New("feedback: service failed")
)

// Field names an InvalidError can carry.
const (
	FieldBody        = "body"
	FieldDisplayName = "displayName"
	FieldContact     = "contact"
	FieldCategory    = "category"
	FieldImages      = "images"
	FieldReceipt     = "receipt"
	FieldReplyID     = "replyId"
)

// Reasons an InvalidError can carry.
const (
	ReasonEmpty       = "empty"
	ReasonTooLong     = "too_long"
	ReasonBadValue    = "bad_value"
	ReasonTooMany     = "too_many"
	ReasonFormat      = "format"
	ReasonTooLarge    = "too_large"
	ReasonUndecodable = "undecodable"
)

// InvalidError is a request the client refused before sending. It is an
// ErrInvalid that also says which field and why, so a frontend picks its words
// from the pair rather than from a sentence.
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string    { return "feedback: invalid " + e.Field + " (" + e.Reason + ")" }
func (e *InvalidError) Is(t error) bool  { return t == ErrInvalid }
func invalid(field, reason string) error { return &InvalidError{Field: field, Reason: reason} }

// Limit names the window a refusal came from, exactly as the service says it.
// An identifier this build does not know still passes through; a frontend with
// no words for it says only that a limit was reached.
type Limit string

const (
	LimitIPHourly      Limit = "ip_hourly"
	LimitInstallHourly Limit = "install_hourly"
	LimitInstallDaily  Limit = "install_daily"
	LimitReplyHourly   Limit = "reply_hourly"
	LimitReplyItem     Limit = "reply_item"
	LimitGlobalDaily   Limit = "global_daily"
	LimitGlobalBurst   Limit = "global_burst"
)

// LimitError is a refusal that names its window: the sentinel it satisfies
// errors.Is for, which window, when it resets and how long to wait. ResetsAt is
// zero and After is 0 where the service sent none (the per-report cap never
// resets); Limit is empty from a service that sends no window at all.
type LimitError struct {
	Kind     error
	Limit    Limit
	ResetsAt time.Time
	After    time.Duration
}

func (e *LimitError) Error() string   { return e.Kind.Error() }
func (e *LimitError) Is(t error) bool { return t == e.Kind }

// LimitOf is the window a refused call carries, or false for any other error.
func LimitOf(err error) (*LimitError, bool) {
	var l *LimitError
	if errors.As(err, &l) {
		return l, true
	}
	return nil, false
}

// RetryAfter is the wait a refusal carries, or 0.
func RetryAfter(err error) time.Duration {
	if l, ok := LimitOf(err); ok {
		return l.After
	}
	return 0
}
