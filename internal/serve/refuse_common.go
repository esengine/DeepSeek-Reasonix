// refuse_common.go — the refusals many endpoints share, so one kind of failure
// is one code rather than one per handler.
package serve

import (
	"encoding/json"
	"net/http"
	"strings"
)

// The recurring kinds. A frontend that has wording for these can say most
// failures without a code per endpoint, which is what 172 hand-written
// http.Error calls cost: the same "bad body" phrased twelve ways, none of them
// translatable.
const (
	codeBadBody           = "request.bad_body"
	codeMissingField      = "request.missing_field"
	codeNotFound          = "request.not_found"
	codeUnknownProject    = "project.unknown"
	codeSessionInUse      = "busy.session_in_use"
	codeBadValue          = "request.bad_value"
	codeSessionBusy       = "busy.session_running"
	codeSessionBadName    = "session.bad_name"
	codeSessionBadPath    = "session.bad_path"
	codeSessionOutside    = "session.outside_dir"
	codeSessionActive     = "busy.session_active"
	codeSwitchModel       = "busy.switch_model"
	codeSessionOpenFailed = "session.open_failed"
)

// badBody refuses a request whose body did not parse. The parse error itself is
// not shown: it names Go types and offsets, which tells a reader nothing and a
// translator less.
func badBody(w http.ResponseWriter) {
	refuse(w, http.StatusBadRequest, codeBadBody, "the request body could not be read", nil)
}

// missingField refuses a request that left out something required. The field
// travels as a param so the sentence around it stays the frontend's.
func missingField(w http.ResponseWriter, field string) {
	refuse(w, http.StatusBadRequest, codeMissingField, "missing "+field, map[string]any{"field": field})
}

// notFound answers 404, which is the trap in it: a name that is merely invalid
// in this request is a 400, and folding one into the other turned an unknown
// project and an unknown role into missing resources until the suite objected.
// Reach for it only when the thing could exist and does not.
func notFound(w http.ResponseWriter, kind, name string) {
	refuse(w, http.StatusNotFound, codeNotFound, "no "+kind+" named "+name,
		map[string]any{"kind": kind, "name": name})
}

// unknownProject refuses a root this runtime has not opened. It is not a 404:
// the project may well exist on disk, and asking about one that is not open
// here is a request that cannot be served, not a thing that is missing.
func unknownProject(w http.ResponseWriter, root string) {
	refuse(w, http.StatusBadRequest, codeUnknownProject, "project is not open here",
		map[string]any{"root": root})
}

// sessionInUse is the refusal a reader has to be able to act on: the session is
// held somewhere else, and the detail says where. It is a conflict, not a
// failure — nothing is broken and nothing needs reporting.
func sessionInUse(w http.ResponseWriter, err error) {
	busy(w, codeSessionInUse, sessionInUseError(err), map[string]any{"detail": sessionInUseError(err)})
}

// badValue refuses a field whose value is not one this build accepts. The
// allowed set travels with it: "mode must be one of" and then nothing is a
// refusal a reader cannot act on.
func badValue(w http.ResponseWriter, field string, allowed ...string) {
	refuse(w, http.StatusBadRequest, codeBadValue, field+" is not one of "+strings.Join(allowed, ", "),
		map[string]any{"field": field, "allowed": allowed})
}

// sessionBusy is the turn-in-flight conflict: the input was not refused because
// it was wrong, but because this session is not taking it right now. The English
// is for a log — a client that reads the code queues the words through the inbox
// instead of showing anyone this sentence, which is why it no longer carries the
// route to call. That was an instruction to a program, rendered at a person.
func sessionBusy(w http.ResponseWriter) {
	busy(w, codeSessionBusy, "a turn owns this session until it lands", nil)
}

// decodeBody reads a JSON body under a size cap and refuses as one kind when it
// does not parse. Unknown fields are rejected: a client sending a field this
// build does not have is not saying what it thinks it is saying.
func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		badBody(w)
		return false
	}
	return true
}
