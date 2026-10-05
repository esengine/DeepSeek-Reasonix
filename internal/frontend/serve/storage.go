// storage.go — what the runtime keeps on disk, and moving it somewhere else.
package serve

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/storage"
)

// storageRoot is one root as the panel reads it. Sizes are the measured ones;
// pinnedBy and relocatable travel with them so the client never decides for
// itself which rows may be moved.
type storageRoot struct {
	ID          string `json:"id"`
	Dir         string `json:"dir"`
	Bytes       int64  `json:"bytes"`
	Files       int64  `json:"files"`
	Relocatable bool   `json:"relocatable"`
	PinnedBy    string `json:"pinnedBy,omitempty"`
	Missing     bool   `json:"missing,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Pending     bool   `json:"pending,omitempty"`
	Err         string `json:"err,omitempty"`
	Volume      string `json:"volume,omitempty"`
	VolumeFree  int64  `json:"volumeFree,omitempty"`
	VolumeTotal int64  `json:"volumeTotal,omitempty"`
}

// storageMove is a move in flight or the one that just finished. It is polled
// rather than pushed: a move is one operation a person is watching, and the
// panel already refreshes while it runs.
type storageMove struct {
	Root   string `json:"root"`
	To     string `json:"to"`
	Phase  string `json:"phase"`
	Bytes  int64  `json:"bytes"`
	Total  int64  `json:"total"`
	Detail string `json:"detail,omitempty"`
	Err    string `json:"err,omitempty"`
	Done   bool   `json:"done"`
}

// storageRefusal is one preflight objection, carrying both the code a client
// branches on and the sentence a person reads.
type storageRefusal struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type storagePlan struct {
	Root  string `json:"root"`
	From  string `json:"from"`
	To    string `json:"to"`
	Bytes int64  `json:"bytes"`
	Files int64  `json:"files"`
	Need  int64  `json:"need"`
	Free  int64  `json:"free"`
	// Adopt says the target already holds this root's data, so accepting the
	// plan records the location and copies nothing; Stays is what the current
	// location keeps, which an adopt does not carry across.
	Adopt    bool             `json:"adopt,omitempty"`
	Stays    int64            `json:"stays,omitempty"`
	OK       bool             `json:"ok"`
	Refusals []storageRefusal `json:"refusals,omitempty"`
}

// moveTracker holds the one move this server may be running. One at a time is
// not a simplification: two moves could name each other's targets, and the
// journal records a single operation.
type moveTracker struct {
	mu      sync.Mutex
	running bool
	state   storageMove
}

func (m *moveTracker) snapshot() (storageMove, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Phase == "" {
		return storageMove{}, false
	}
	return m.state, true
}

func (m *moveTracker) begin(state storageMove) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	m.state = state
	return true
}

func (m *moveTracker) update(fn func(*storageMove)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.state)
}

func (m *moveTracker) finish(fn func(*storageMove)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	fn(&m.state)
	m.state.Done = true
}

func (s *Server) registerStorageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /storage", s.storage)
	mux.HandleFunc("POST /storage/plan", s.storagePlan)
	mux.HandleFunc("POST /storage/move", s.storageMove)
}

// storage reports every root with its measured size, each walk bounded by
// storage.Budget. ?layout=1 answers at once with the roots unmeasured, and
// ?root=<id> measures that one alone (an undeclared id is refused), so a panel can show the fast roots
// without waiting on a slow one. The walk runs on the request's own context.
func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	layout := q.Get("layout") == "1"
	only := q.Get("root")
	if layout && only != "" {
		badValue(w, "layout", "1 (without root)")
		return
	}
	var roots []storage.Root
	switch {
	case layout:
		roots = storage.Layout()
	case only != "":
		root, ok := storage.SurveyRoot(r.Context(), config.RootID(only))
		if !ok {
			ids := config.RootIDs()
			allowed := make([]string, len(ids))
			for i, id := range ids {
				allowed[i] = string(id)
			}
			badValue(w, "root", allowed...)
			return
		}
		roots = []storage.Root{root}
	default:
		roots = storage.Survey(r.Context())
	}
	out := make([]storageRoot, 0, len(roots))
	for _, root := range roots {
		out = append(out, storageRoot{
			ID: string(root.ID), Dir: root.Dir, Bytes: root.Bytes, Files: root.Files,
			Relocatable: root.Relocatable, PinnedBy: root.PinnedBy, Missing: root.Missing,
			Truncated: root.Truncated, Pending: root.Pending,
			Err: root.Err, Volume: root.Volume.Path,
			VolumeFree: root.Volume.Free, VolumeTotal: root.Volume.Total,
		})
	}
	body := map[string]any{"roots": out, "editable": s.grants.at(r).providerEdit}
	if dir, names := storage.LeftBehind(); len(names) > 0 {
		body["leftBehind"] = map[string]any{"dir": dir, "names": names}
	}
	if move, ok := s.moves.snapshot(); ok {
		body["move"] = move
	}
	if pending, ok := storage.PendingMove(); ok {
		body["pending"] = map[string]any{
			"root": string(pending.Root), "to": pending.To, "phase": string(pending.Phase),
		}
	}
	writeJSON(w, body)
}

// storagePlan answers what a move would do without doing any of it, so the
// panel can show the cost and every objection before a person commits.
func (s *Server) storagePlan(w http.ResponseWriter, r *http.Request) {
	root, dir, ok := decodeStorageTarget(w, r)
	if !ok {
		return
	}
	writeJSON(w, planPayload(storage.PlanMove(r.Context(), root, dir)))
}

// storageMove starts one. It answers immediately with the plan it accepted and
// reports the rest through GET /storage: a move copies gigabytes, and holding
// the request open for it would leave the panel unable to say anything.
func (s *Server) storageMove(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "storage.moving_disabled", "moving data is not enabled on this server", nil)
		return
	}
	root, dir, ok := decodeStorageTarget(w, r)
	if !ok {
		return
	}
	plan := storage.PlanMove(r.Context(), root, dir)
	if !plan.OK() {
		writeJSON(w, planPayload(plan))
		return
	}
	if !s.moves.begin(storageMove{Root: string(plan.Root), To: plan.To, Phase: string(openingPhase(plan)), Total: plan.Bytes}) {
		busy(w, "storage.move_running", "a move is already running", nil)
		return
	}
	// Detached from the request: the copy outlives the client that asked for
	// it, and cancelling it is a decision this server does not offer mid-flight
	// — an abandoned copy is resumable, a half-cancelled one is a surprise.
	go func() {
		err := storage.Move(context.WithoutCancel(r.Context()), plan, func(p storage.Progress) {
			s.moves.update(func(state *storageMove) {
				state.Phase, state.Bytes, state.Total = string(p.Phase), p.Bytes, p.Total
				if p.Detail != "" {
					state.Detail = p.Detail
				}
			})
		})
		s.moves.finish(func(state *storageMove) {
			if err != nil {
				state.Err = err.Error()
				return
			}
			state.Phase = string(storage.PhaseDone)
		})
	}()
	writeJSON(w, planPayload(plan))
}

// openingPhase is what the tracker starts on, so a panel polling it is never
// told a pointer change is a copy.
func openingPhase(plan storage.Plan) storage.Phase {
	if plan.Adopt {
		return storage.PhaseAdopting
	}
	return storage.PhaseCopying
}

func planPayload(plan storage.Plan) storagePlan {
	out := storagePlan{
		Root: string(plan.Root), From: plan.From, To: plan.To,
		Bytes: plan.Bytes, Files: plan.Files, Need: plan.Need, Free: plan.Free,
		Adopt: plan.Adopt, Stays: plan.Stays, OK: plan.OK(),
	}
	for _, refusal := range plan.Refusals {
		out.Refusals = append(out.Refusals, storageRefusal{Code: refusal.Code, Detail: refusal.Detail})
	}
	return out
}

func decodeStorageTarget(w http.ResponseWriter, r *http.Request) (config.RootID, string, bool) {
	var body struct {
		Root string `json:"root"`
		Dir  string `json:"dir"`
	}
	if !decodeBody(w, r, &body) {
		return "", "", false
	}
	root := config.RootID(strings.TrimSpace(body.Root))
	if root == "" {
		missingField(w, "root")
		return "", "", false
	}
	return root, strings.TrimSpace(body.Dir), true
}
