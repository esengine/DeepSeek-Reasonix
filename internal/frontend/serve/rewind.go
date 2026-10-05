package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
)

// A rewind is two calls when the plan needs consent: POST /rewind refuses a
// partial-coverage plan outright, so prepare reports the gaps and commit applies
// the plan the user saw. The CLI picker has always worked this way.

// rewindScope maps the wire's scope name. Anything unrecognised means both,
// matching what the single-shot endpoint has always done.
func rewindScope(name string) control.RewindScope {
	switch name {
	case "code":
		return control.RewindCode
	case "conversation":
		return control.RewindConversation
	}
	return control.RewindBoth
}

func (s *Server) rewindPrepare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Turn  int    `json:"turn"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Turn < 0 {
		missingField(w, "turn")
		return
	}
	plan, err := s.ctl().PrepareRewind(body.Turn, rewindScope(body.Scope))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, struct {
		checkpoint.RewindPlan
		RequiresConfirmation bool `json:"requiresConfirmation"`
	}{RewindPlan: plan, RequiresConfirmation: control.RewindPlanRequiresConfirmation(plan)})
}

func (s *Server) rewindCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"planId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PlanID == "" {
		missingField(w, "planId")
		return
	}
	result, err := s.ctl().CommitRewind(body.PlanID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.ctl().Snapshot(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, result)
}

// rewindUndo reverses a committed rewind. The commit result carries the
// transaction id and whether this is available, so a frontend can offer it while
// the id is still in hand.
func (s *Server) rewindUndo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TransactionID string `json:"transactionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TransactionID == "" {
		missingField(w, "transactionId")
		return
	}
	result, err := s.ctl().UndoRewind(body.TransactionID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.ctl().Snapshot(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, result)
}

// fileRevertPrepare works out what reverting one file would do. Separate from
// the turn-scoped prepare because the question is different: not "how far back"
// but "is this one file still the one the checkpoint captured".
func (s *Server) fileRevertPrepare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		missingField(w, "path")
		return
	}
	plan, err := s.ctl().PrepareFileRevert(strings.TrimSpace(body.Path))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, plan)
}

// fileRevertCommit applies a prepared single-file revert. A file edited since
// the checkpoint needs the caller to say which side wins — the API has always
// taken that answer, and until now nothing could ask the question.
func (s *Server) fileRevertCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID     string `json:"planId"`
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.PlanID) == "" {
		missingField(w, "planId")
		return
	}
	resolution := checkpoint.ConflictResolution(strings.TrimSpace(body.Resolution))
	if resolution != "" && resolution != checkpoint.ResolveKeepCurrent && resolution != checkpoint.ResolveOverwriteCheckpoint {
		badValue(w, "resolution", string(checkpoint.ResolveKeepCurrent), string(checkpoint.ResolveOverwriteCheckpoint))
		return
	}
	result, err := s.ctl().CommitFileRevert(strings.TrimSpace(body.PlanID), resolution)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, result)
}
