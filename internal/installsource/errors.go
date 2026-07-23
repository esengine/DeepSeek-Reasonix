package installsource

import (
	"errors"
	"fmt"
)

// RiskLevel classifies how dangerous an action is. The install-capability skill
// prompt tells the model to call apply=true only when every action is low or
// medium, or to ask the user first when any action is high.
type RiskLevel string

const (
	// RiskLow is read-mostly safe: copy/link of a single skill file, or
	// connecting an MCP endpoint whose URL the user already trusts.
	RiskLow RiskLevel = "low"
	// RiskMedium is a write that mutates the active config (new MCP server,
	// new skill registered into a project root the user already shares).
	RiskMedium RiskLevel = "medium"
	// RiskHigh is a write the user almost certainly wants to see first: a
	// symlink target outside any expected scope, a remote URL with auth
	// headers, a package name that triggers an out-of-process fetch, or a
	// replace of an existing MCP server.
	RiskHigh RiskLevel = "high"
)

// Sentinel errors. Callers use errors.Is to map a failure to a remediation
// hint without scraping error messages.
var (
	// ErrAuthRequired: the upstream demanded credentials that the request
	// did not carry. Surface a hint to set the relevant env var or header.
	ErrAuthRequired = errors.New("install_source: authentication required")
	// ErrBinaryMissing: a stdio MCP server references a command that is not
	// on PATH or not present at the given path.
	ErrBinaryMissing = errors.New("install_source: command or runtime not found")
	// ErrAlreadyExists: a target file / config entry already exists and the
	// call did not opt into replace=true.
	ErrAlreadyExists = errors.New("install_source: target already exists")
	// ErrUnsafeLinkTarget: a link-mode skill install would create a symlink
	// that escapes the expected skill roots — typically an attempt to
	// read arbitrary host files.
	ErrUnsafeLinkTarget = errors.New("install_source: link target escapes skill roots")
	// ErrSourceUnreadable: a URL did not respond, returned non-2xx, or a
	// local path was not readable.
	ErrSourceUnreadable = errors.New("install_source: source is not readable")
	// ErrManifestMissing: a path was reachable but contained no installable
	// artifact (no SKILL.md, no .mcp.json, no executable, etc.).
	ErrManifestMissing = errors.New("install_source: no installable manifest")
	// ErrInvalidManifest: a manifest existed but did not validate (missing
	// required fields, unknown transport, etc.).
	ErrInvalidManifest = errors.New("install_source: manifest did not validate")
	// ErrNoCompatibleCapabilities: a plugin manifest was valid but none of its
	// capabilities can run in Reasonix. Preview returns a structured block.
	ErrNoCompatibleCapabilities = errors.New("install_source: plugin has no compatible capabilities")
	// ErrUnsupportedKind: kind was set explicitly to something the resolver
	// cannot satisfy (e.g. kind=skill for a remote MCP endpoint).
	ErrUnsupportedKind = errors.New("install_source: kind does not match source")
	// ErrApprovalDenied: the host's ApprovalFunc returned a non-nil error,
	// or the call set apply=true while the host requires explicit consent.
	ErrApprovalDenied = errors.New("install_source: host denied the install")
)

// errKind wraps a sentinel with a human-readable detail so logs and the
// `next` field stay useful.
type errKind struct {
	sentinel error
	detail   string
}

func (e *errKind) Error() string { return fmt.Sprintf("%s: %s", e.sentinel, e.detail) }
func (e *errKind) Unwrap() error { return e.sentinel }

func newErr(sentinel error, format string, args ...any) error {
	return &errKind{sentinel: sentinel, detail: fmt.Sprintf(format, args...)}
}
