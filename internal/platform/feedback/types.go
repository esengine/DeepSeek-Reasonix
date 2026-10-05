package feedback

import (
	"strconv"
	"strings"
	"time"
)

type Category string

const (
	Bug      Category = "bug"
	Idea     Category = "idea"
	Question Category = "question"
	Other    Category = "other"
)

func (c Category) valid() bool {
	switch c {
	case Bug, Idea, Question, Other:
		return true
	}
	return false
}

type Surface string

const (
	SurfaceStudio Surface = "studio"
	SurfaceTUI    Surface = "tui"
	SurfaceACP    Surface = "acp"
)

// Valid reports whether s names a surface a report may come from.
func (s Surface) Valid() bool { return s == SurfaceStudio || s == SurfaceTUI || s == SurfaceACP }

// Status is what the worker lets a person see. A submission waiting for triage
// reads as received; one the maintainers declined reads as closed, with no
// reason.
type Status string

const (
	StatusReceived   Status = "received"
	StatusNeedsInfo  Status = "needs_info"
	StatusAnswered   Status = "answered"
	StatusClosed     Status = "closed"
	StatusRecorded   Status = "recorded"
	StatusInProgress Status = "in_progress"
	StatusFixed      Status = "fixed"
	StatusWontFix    Status = "wontfix"
	StatusDuplicate  Status = "duplicate"
)

// Limits are the ceilings the worker enforces, held here so a frontend states
// them from one place.
type Limits struct {
	BodyBytes    int `json:"bodyBytes"`
	NameChars    int `json:"nameChars"`
	ContactChars int `json:"contactChars"`
	Images       int `json:"images"`
	ImageBytes   int `json:"imageBytes"`
	UploadBytes  int `json:"uploadBytes"`
	ReplyBytes   int `json:"replyBytes"`
}

// DefaultLimits is what this build enforces.
var DefaultLimits = Limits{
	BodyBytes: 8192, NameChars: 40, ContactChars: 120,
	Images: 3, ImageBytes: 2 << 20, UploadBytes: 10 << 20, ReplyBytes: 4096,
}

// Env is what every report carries without a checkbox. It holds no key, no base
// URL and no path.
type Env struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	Surface      string `json:"surface"`
	OS           string `json:"os"`
	OSVersion    string `json:"osVersion"`
	Arch         string `json:"arch"`
	Locale       string `json:"locale"`
	Channel      string `json:"channel"`
	ProviderKind string `json:"providerKind"`
}

// EnvContext is what only the caller knows about itself.
type EnvContext struct {
	Surface      Surface
	Locale       string
	ProviderKind string
}

// Image is one picked, pasted or dropped file, as the person supplied it.
type Image struct {
	Name string
	Data []byte
}

// Draft is one report to send. IdempotencyKey is minted when empty; a caller
// retrying after a failure passes the key of the attempt that failed.
type Draft struct {
	IdempotencyKey string
	Category       Category
	Body           string
	DisplayName    string
	Contact        string
	Images         []Image
	Env            EnvContext
	// TurnstileToken answers a feedback.challenge_required refusal.
	TurnstileToken string
}

// Receipt is the service's acknowledgement. Redacted reports that local
// redaction changed the text before it left the machine.
type Receipt struct {
	Receipt   string    `json:"receipt"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Redacted  bool      `json:"redacted"`
}

// ReplyAuthor says who wrote one message of a thread.
type ReplyAuthor string

const (
	AuthorMaintainer ReplyAuthor = "maintainer"
	AuthorUser       ReplyAuthor = "user"
)

// ReplyID orders the messages of a thread. The service sends it as a number; a
// numeric string is read the same, so a change of spelling cannot blank the list.
type ReplyID int64

func (id *ReplyID) UnmarshalJSON(b []byte) error {
	n, err := strconv.ParseInt(strings.Trim(string(b), `"`), 10, 64)
	if err != nil {
		return err
	}
	*id = ReplyID(n)
	return nil
}

// Reply is one message of a report's thread. Body is plain text: no frontend
// may link, format or execute it.
type Reply struct {
	ID        ReplyID     `json:"id"`
	Author    ReplyAuthor `json:"author"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"createdAt"`
}

// ReplyReceipt acknowledges a reply the service accepted.
type ReplyReceipt struct {
	ReplyID   ReplyID   `json:"replyId"`
	CreatedAt time.Time `json:"createdAt"`
}

// Item is one of the caller's own reports.
type Item struct {
	Receipt         string    `json:"receipt"`
	Category        Category  `json:"category"`
	TitleSnippet    string    `json:"titleSnippet"`
	Status          Status    `json:"status"`
	IssueNumber     *int      `json:"issueNumber,omitempty"`
	IssueURL        string    `json:"issueUrl,omitempty"`
	ResolvedVersion string    `json:"resolvedVersion,omitempty"`
	DuplicateOf     *int      `json:"duplicateOf,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	// StatusUnavailable marks a report sent under an install identity this
	// machine no longer holds: its status can no longer be read.
	StatusUnavailable bool `json:"statusUnavailable,omitempty"`
	// NeedsInput is the maintainers waiting on the reporter.
	NeedsInput bool    `json:"needsInput"`
	Replies    []Reply `json:"replies"`
	// UnreadReplies counts maintainer replies newer than the last one this
	// machine marked seen; it is computed on read, never stored.
	UnreadReplies int `json:"unreadReplies"`
}

// Mine is the caller's reports. Offline means the list is what this machine
// remembers, and statuses may have moved since. Unread counts REPORTS, not
// replies, that want attention (an unseen maintainer reply or a question to
// answer); reports whose identity is gone are never counted. HasNew is whether
// any report does.
type Mine struct {
	Items   []Item `json:"items"`
	Offline bool   `json:"offline"`
	Unread  int    `json:"unread"`
	HasNew  bool   `json:"hasNew"`
}
