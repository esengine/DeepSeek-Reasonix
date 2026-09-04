package serve

import (
	"net/http"
	"strings"

	"reasonix/internal/control"
)

const executionGraphSchemaVersion = 1

// executionGraphView is what a client bootstraps from. Watermark is the frame
// the snapshot is at least as new as, so the client resumes the delta stream
// after it: everything the snapshot already shows and the stream repeats folds
// idempotently, and nothing between the two is skipped.
type executionGraphView struct {
	SchemaVersion int    `json:"schemaVersion"`
	SessionID     string `json:"sessionId"`
	Watermark     int64  `json:"watermark"`
	control.ExecutionGraphSnapshot
}

func (s *Server) registerExecutionGraphRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /execution-graph", s.executionGraph)
}

// executionGraph answers with the rebuilt graph and the frame to resume after.
// The watermark is read first, and that order is the contract: read after the
// rebuild, a transition landing in between is numbered below a watermark the
// client resumes past, and the frame is lost. Read first, it is merely replayed
// onto a snapshot that may hold it — duplicates are safe, gaps are not.
func (s *Server) executionGraph(w http.ResponseWriter, r *http.Request) {
	watermark := s.bc.Watermark()
	ctrl := s.Controller()
	view := executionGraphView{
		SchemaVersion:          executionGraphSchemaVersion,
		SessionID:              executionGraphSessionID(ctrl.SessionPath()),
		Watermark:              watermark,
		ExecutionGraphSnapshot: ctrl.ExecutionGraph(),
	}
	writeJSONCached(w, r, view)
}

// executionGraphSessionID names which conversation this snapshot belongs to.
// Frame numbers keep climbing across a session switch while the replay tail is
// cleared, so a client that merged a new snapshot with the previous session's
// deltas would be folding two conversations into one picture.
func executionGraphSessionID(path string) string {
	base := strings.TrimSpace(path)
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".jsonl")
}
