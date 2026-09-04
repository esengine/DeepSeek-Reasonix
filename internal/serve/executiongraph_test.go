package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// settlingController fires a transition while the rebuild is running, which is
// the instant a bootstrap has to survive: durable facts change and a frame is
// numbered between the handler's two reads. It wraps a real controller so the
// handler under test is the production one.
type settlingController struct {
	control.SessionAPI
	onRebuild func()
	once      sync.Once
}

func (c *settlingController) ExecutionGraph() control.ExecutionGraphSnapshot {
	c.once.Do(c.onRebuild)
	return c.SessionAPI.ExecutionGraph()
}

// TestBootstrapMissesNothingAcrossTheCut is the synchronization contract, read
// off the real handler. A transition landing between its two reads must reach
// the client — through the snapshot, the replayed frames, or both. Reading the
// watermark first allows a duplicate; reading it second loses the frame, and
// nothing later reveals that it is gone.
func TestBootstrapMissesNothingAcrossTheCut(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := &settlingController{
		SessionAPI: control.New(control.Options{Sink: bc}),
		onRebuild: func() {
			bc.Emit(event.Event{Kind: event.GraphDelta, Graph: &agentgraph.Delta{
				Nodes: []agentgraph.Node{{ID: "call/item", Kind: agentgraph.KindWorker, State: agentgraph.StateCompleted}},
			}})
		},
	}
	s := &Server{ctrl: ctrl, bc: bc}

	rec := httptest.NewRecorder()
	s.executionGraph(rec, httptest.NewRequest(http.MethodGet, "/execution-graph", nil))
	var view executionGraphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}

	frames, complete := bc.Replay(view.Watermark)
	if !complete {
		t.Fatal("replay could not account for the frames after the watermark")
	}
	graph := view.Graph
	for _, f := range frames {
		graph.Apply(deltaOf(t, f))
	}

	node, ok := graph.Node("call/item")
	if !ok {
		t.Fatal("the transition that landed during the rebuild reached the client through neither path")
	}
	if node.State != agentgraph.StateCompleted {
		t.Fatalf("state = %q, want completed", node.State)
	}
}

// deltaOf pulls the graph delta out of a wire frame the way a client does.
func deltaOf(t *testing.T, frame json.RawMessage) agentgraph.Delta {
	t.Helper()
	var wire struct {
		Kind  string            `json:"kind"`
		Graph *agentgraph.Delta `json:"graph"`
	}
	if err := json.Unmarshal(frame, &wire); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if wire.Kind != "graph_delta" || wire.Graph == nil {
		return agentgraph.Delta{}
	}
	return *wire.Graph
}

// TestSnapshotNamesItsSession: frame numbers keep climbing across a session
// switch while the replay tail is cleared, so a snapshot that did not say which
// conversation it belongs to could be merged with the previous one's deltas.
func TestSnapshotNamesItsSession(t *testing.T) {
	for path, want := range map[string]string{
		"/home/u/.reasonix/sessions/abc.jsonl": "abc",
		"abc.jsonl":                            "abc",
		"":                                     "",
	} {
		if got := executionGraphSessionID(path); got != want {
			t.Errorf("sessionID(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestExecutionGraphRouteAnswers checks the shape a client bootstraps from,
// including that the watermark is carried rather than left for the client to
// guess from whatever frame it happens to see first.
func TestExecutionGraphRouteAnswers(t *testing.T) {
	bc := NewBroadcaster()
	s := &Server{ctrl: control.New(control.Options{Sink: bc}), bc: bc}
	rec := httptest.NewRecorder()
	s.executionGraph(rec, httptest.NewRequest(http.MethodGet, "/execution-graph", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var view executionGraphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.SchemaVersion != executionGraphSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", view.SchemaVersion, executionGraphSchemaVersion)
	}
	if view.Watermark < 0 {
		t.Errorf("watermark = %d, want the current frame number", view.Watermark)
	}
}
