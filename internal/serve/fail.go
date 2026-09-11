// fail.go — how a refusal reaches a frontend: as a code, not as a sentence.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/control"
)

// Reason is a machine-readable refusal: the kernel decides what happened, a
// frontend decides how to say it. Message is English fallback for logs and for
// clients with no wording for this code — never the translation. Params carry
// the pieces a sentence needs, so grammar stays the frontend's problem.
type Reason struct {
	Code    string         `json:"code"`
	Message string         `json:"error"`
	Params  map[string]any `json:"params,omitempty"`
}

// refuse writes a Reason. Codes are dotted and stable: renaming one is a
// breaking change to every frontend, the same as renaming a route.
func refuse(w http.ResponseWriter, status int, code, message string, params map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Reason{Code: code, Message: message, Params: params})
}

// busy is the refusal a frontend has to phrase most carefully: nothing is
// wrong, the answer is "not while this is running". It is one helper because
// every caller of it means the same thing. Params carry what the reader needs
// in order to act — "close the pane first" is only useful alongside which one.
func busy(w http.ResponseWriter, code, message string, params map[string]any) {
	refuse(w, http.StatusConflict, code, message, params)
}

// coded carries a Reason out through call sites that only return `error`.
// "A turn is running" is known where the controller is, not at the mux, and
// this is what gets that decision to the boundary without the layers between
// learning about HTTP.
type coded struct {
	reason Reason
	status int
	err    error
}

func (c *coded) Error() string { return c.err.Error() }
func (c *coded) Unwrap() error { return c.err }

// refusal builds an error that writeErr will render as a Reason.
func refusal(status int, code string, err error, params map[string]any) error {
	return &coded{reason: Reason{Code: code, Message: err.Error(), Params: params}, status: status, err: err}
}

// Refusal wraps err so it reaches the frontend as a dotted code rather than a
// sentence. Exported for a host whose link layer can tell apart what this
// package cannot see: a changed host key is not a network hiccup, and only the
// side that verified the key knows which it was.
func Refusal(status int, code string, err error, params map[string]any) error {
	return refusal(status, code, err, params)
}

// busyErr is the "not while this is running" refusal in error form.
func busyErr(code, message string) error {
	return refusal(http.StatusConflict, code, errors.New(message), nil)
}

// keepRefusalStatus carries the status its producer chose onto an error with no
// code of its own, so a caller cannot fold it into whatever fallback it passes.
// resumeInto answers with "the status a refusal deserves" so the hub and the
// /resume handler refuse alike; dropping it let a transcript that no longer
// exists be reported as one another window was holding.
func keepRefusalStatus(status int, err error) error {
	var c *coded
	if err == nil || errors.As(err, &c) {
		return err
	}
	return refusal(status, codeSessionOpenFailed, err, nil)
}

// writeErr turns an error into a response. One carrying a code reaches the
// frontend as one, a config file the kernel could not read keeps its own code
// wherever it surfaces, and anything else keeps the old shape, so adoption is
// incremental rather than a flag day.
func writeErr(w http.ResponseWriter, fallback int, err error) {
	var c *coded
	if errors.As(err, &c) {
		refuse(w, c.status, c.reason.Code, c.reason.Message, c.reason.Params)
		return
	}
	if refuseUnparsedConfig(w, err) {
		return
	}
	http.Error(w, err.Error(), fallback)
}

// refuseUnparsedConfig answers when the failure is the config file itself. It
// is one condition with one code across every panel that writes settings, and
// it carries where to look rather than a parser's sentence about columns.
func refuseUnparsedConfig(w http.ResponseWriter, err error) bool {
	problem := control.ConfigProblemFromError(err)
	if problem == nil {
		return false
	}
	refuse(w, http.StatusConflict, "config.unparsed", err.Error(), map[string]any{
		"path": problem.Path, "line": problem.Line, "excerpt": problem.Excerpt,
		"repair": problem.Repair, "recovered": problem.Recovered,
	})
	return true
}

// saveFailed refuses a settings write with an identity. A config file the
// kernel could not read is that same condition wherever it surfaces, so it
// keeps one code across every panel; anything else is that panel's own
// rejection, carrying the kernel's words as the detail a reader acts on.
func saveFailed(w http.ResponseWriter, status int, code string, err error) {
	if refuseUnparsedConfig(w, err) {
		return
	}
	refuse(w, status, code, err.Error(), map[string]any{"detail": err.Error()})
}

// codedRefusal is the identity a coded error carries, for the caller that has to
// tell two of them apart. Empty for an error that carries none — which is the
// answer, not a reason to start reading the sentence instead.
func codedRefusal(err error) string {
	var c *coded
	if errors.As(err, &c) {
		return c.reason.Code
	}
	return ""
}

// rebuildFailed is the one answer to "the settings were written and the runtime
// could not be rebuilt on them", which is not the same failure as the write.
func rebuildFailed(w http.ResponseWriter, err error) {
	refuse(w, http.StatusConflict, "runtime.rebuild_failed", err.Error(), map[string]any{"detail": err.Error()})
}
