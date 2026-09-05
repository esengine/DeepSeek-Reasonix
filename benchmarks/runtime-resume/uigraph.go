package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/execjournal"
	"reasonix/internal/provider"
	"reasonix/internal/serve"
)

// armUIGraphMixed is the only arm that reads the frontend rather than the host.
// Everything below it has been shown to rebuild the graph exactly; this asks
// the question a value comparison cannot: through which door did each fact
// reach the view? A history replayed as live deltas settles on the same state
// and tells the user work is starting that ended in a process now dead.
const armUIGraphMixed = "ui-graph-mixed"

func uiArm(name string) bool { return name == armUIGraphMixed }

// The two extra fleets this arm's turn opens. The settled one ends before the
// death, which is the only way a skip exists at all — a fan-out publishes one in
// its closing delta — and the live one is dispatched by the resumed process.
const (
	fleetSettledSentinel = "PROBE-FLEET-SETTLED"
	fleetLiveSentinel    = "PROBE-FLEET-LIVE"
)

// envProbeSabotage names a deliberate defect to run under. The arm must go red
// with it and green without: a probe nobody has watched fail is a decoration.
const envProbeSabotage = "REASONIX_PROBE_SABOTAGE"

// envProbeRepo carries the checkout to the child that has to build the observer.
// A child's working directory is its own workspace, so nothing else in the
// process can find the frontend.
const envProbeRepo = "REASONIX_PROBE_REPO"

const (
	sabotagePublish    = "publish"
	sabotageTrajectory = "trajectory"
)

// settledFleet runs to completion with its dependent skipped, so the death
// inherits a terminal the mid-flight fleet cannot produce.
func settledFleet() []provider.Chunk {
	return fleetCall("probe_fleet_settled", []map[string]any{
		{"id": "up", "prompt": childFail + " settled upstream", "description": "fails", "read_only": true},
		{"id": "down", "prompt": childDone + " settled downstream", "description": "skipped by up",
			"read_only": true, "depends_on": []string{"up"}},
	})
}

// uiMixedFleet is the density fleet plus one item the session ceiling refuses,
// so the death also holds a node that was queued and never admitted.
func uiMixedFleet(adoptRef string) []provider.Chunk {
	return fleetCall("probe_fleet_mixed", []map[string]any{
		{"id": "m1", "prompt": childDone + " mixed one", "description": "completes", "read_only": true},
		{"id": "m2", "prompt": childFail + " mixed two", "description": "fails", "read_only": true},
		{"id": "m3", "prompt": childDone + " mixed three", "description": "blocked by m2",
			"read_only": true, "depends_on": []string{"m2"}},
		{"id": "m4", "adopt_ref": adoptRef, "description": "adopted"},
		{"id": "m5", "prompt": childHang + " mixed five", "description": "running at death",
			"write_paths": []string{"probe-fanout-out"}},
		{"id": "m6", "prompt": childHang + " mixed six", "description": "queued at death", "read_only": true},
	})
}

// liveFleet is what the resumed process dispatches while the view is watching.
// Without it a frontend that had stopped folding deltas entirely would pass
// every other row here.
func liveFleet() []provider.Chunk {
	return fleetCall("probe_fleet_live", []map[string]any{
		{"id": "l1", "prompt": childDone + " live one", "description": "starts after the resume", "read_only": true},
		{"id": "l2", "prompt": childDone + " live two", "description": "starts after the resume", "read_only": true},
	})
}

// livePrompt opens that fleet from the frontend, through /submit.
func livePrompt() string { return fanOutTurn(200, fleetLiveSentinel) }

// uiBus is the broadcaster the one arm with a frontend needs beside the probe's
// own fold: in the dying process it is what records the wire log a window would
// have recorded, and in the resumed one it is what the client reads.
func uiBus(arm string, sink *graphSink) (*serve.Broadcaster, event.Sink) {
	if !uiArm(arm) {
		return nil, sink
	}
	bc := serve.NewBroadcaster()
	return bc, teeSink{to: []event.Sink{sink, bc}}
}

// recordLikeAWindow attaches the wire log the trajectory is read from. The
// server is never served: the constructor is what subscribes the recorder, and
// the dying process needs the record, not a socket.
func recordLikeAWindow(ctrl control.SessionAPI, bc *serve.Broadcaster) {
	if bc != nil {
		_ = serve.New(ctrl, bc, config.ServeConfig{})
	}
}

// teeSink lets one runtime's events reach both the probe's fold and the server
// the frontend reads. Nothing production does this; the arm needs the same
// stream on two surfaces.
type teeSink struct{ to []event.Sink }

func (t teeSink) Emit(e event.Event) {
	for _, s := range t.to {
		s.Emit(e)
	}
}

// UIEntry is one state the view passed through, as the frontend's own store
// reports it. States is what the graph showed, keyed by node.
type UIEntry struct {
	Phase           string            `json:"phase"`
	Origin          string            `json:"origin"`
	SessionID       string            `json:"sessionId"`
	States          map[string]string `json:"states"`
	Interruptions   []UIInterruption  `json:"interruptions"`
	IdentityUnknown []string          `json:"identityUnknown"`
}

type UIInterruption struct {
	Execution string `json:"execution"`
	Kind      string `json:"kind"`
}

// UIObs is the whole observation sequence, not a final value: the defect this
// arm exists to catch agrees with the host on every final value.
type UIObs struct {
	Trace []UIEntry `json:"trace,omitempty"`
	// Deltas is what each publication the view folded named. A republished
	// history moves no visible state and is still the kernel saying that work
	// which ended in a dead process is happening now.
	Deltas [][]string `json:"deltas,omitempty"`
	Err    string     `json:"err,omitempty"`
}

// observeUI serves the resumed session on a loopback listener and runs the real
// client against it. The server is the production one and the client is the
// production one; only the shell between them is the probe's, which is what
// lets the trace be read as what a user could have seen.
func observeUI(root armRoot, ctrl control.SessionAPI, bc *serve.Broadcaster, obs Observation) UIObs {
	ln, err := serve.ListenLoopback()
	if err != nil {
		return UIObs{Err: "listen: " + err.Error()}
	}
	defer ln.Close()
	srv := serve.New(ctrl, bc, config.ServeConfig{})
	handler := srv.Handler()
	if os.Getenv(envProbeSabotage) == sabotagePublish {
		handler = republishOnce(handler, func() { publishRebuiltGraph(bc, obs) })
	}
	go func() { _ = http.Serve(ln, handler) }()

	bundle, err := bundleObserver(root)
	if err != nil {
		return UIObs{Err: err.Error()}
	}
	out, runErr := runObserver(bundle, serve.LoopbackOrigin(ln))
	seen := parseTrace(out)
	if runErr != nil && seen.Err == "" {
		seen.Err = runErr.Error()
	}
	return seen
}

// bundleObserver builds the observer with the frontend's own toolchain. It is
// the client's code, imported the way the app imports it — a hand-written stand
// -in would be measuring the probe.
func bundleObserver(root armRoot) (string, error) {
	repo := strings.TrimSpace(os.Getenv(envProbeRepo))
	if repo == "" {
		return "", fmt.Errorf("no checkout named in %s, so the frontend could not be built", envProbeRepo)
	}
	dir := filepath.Join(repo, "desktop", "frontend-next")
	esbuild := filepath.Join(dir, "node_modules", ".bin", "esbuild")
	if _, err := os.Stat(esbuild); err != nil {
		return "", fmt.Errorf("the frontend's toolchain is not installed: %w", err)
	}
	out := filepath.Join(root.Dir, "graph-observer.mjs")
	cmd := exec.Command(esbuild, filepath.Join("src", "e2e", "graph_observer.ts"),
		"--bundle", "--platform=node", "--format=esm", "--outfile="+out)
	cmd.Dir = dir
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("bundle the observer: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return out, nil
}

func runObserver(bundle, base string) ([]byte, error) {
	cmd := exec.Command("node", bundle, "--base", base, "--prompt", livePrompt(),
		"--sabotage", os.Getenv(envProbeSabotage))
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

// parseTrace reads the observer's last line. Anything it printed before that is
// the frontend's own noise, which belongs on the terminal and not in the row.
func parseTrace(out []byte) UIObs {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var obs UIObs
	if len(lines) == 0 {
		return UIObs{Err: "the observer printed nothing"}
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &obs); err != nil {
		return UIObs{Err: "the observer's answer did not parse: " + err.Error()}
	}
	return obs
}

// republishOnce fires the sabotage after a client has read the snapshot, which
// is the only window where it can be seen at all: what a resume publishes before
// that is numbered below the watermark the client then resumes past, and the
// bootstrap absorbs it. The defect this models is the Studio path — a pane that
// is already attached when a session is resumed under it.
func republishOnce(next http.Handler, fire func()) http.Handler {
	var once sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.URL.Path == "/execution-graph" {
			once.Do(func() { go fire() })
		}
	})
}

// publishRebuiltGraph is the sabotage itself: a resume that helpfully republishes
// what it rebuilt, which is how a view gets told that finished work is starting
// now. Production must never do this, and the arm's own rows are what says so.
func publishRebuiltGraph(sink event.Sink, obs Observation) {
	rebuilt := rebuiltGraph(obs)
	for _, n := range rebuilt.Graph.Nodes {
		sink.Emit(event.Event{Kind: event.GraphDelta, Graph: &agentgraph.Delta{Nodes: []agentgraph.Node{n}}})
	}
	if len(rebuilt.Graph.Edges) > 0 {
		sink.Emit(event.Event{Kind: event.GraphDelta, Graph: &agentgraph.Delta{Edges: rebuilt.Graph.Edges}})
	}
	// The publication has to reach the socket before the client attaches, or the
	// sabotage would be measuring a race instead of a defect.
	time.Sleep(200 * time.Millisecond)
}

// historicalIDs is every node the dying process had drawn, groups included: a
// group redrawn as running is the same lie about the same dead process.
func historicalIDs(o Observation) map[string]bool {
	out := map[string]bool{}
	for _, n := range o.Graph.Nodes {
		out[n.ID] = true
	}
	return out
}

// firstSight is the origin each node carried the first time the view held it.
func firstSight(trace []UIEntry) map[string]string {
	out := map[string]string{}
	for _, e := range trace {
		for id := range e.States {
			if _, seen := out[id]; !seen {
				out[id] = e.Origin
			}
		}
	}
	return out
}

var liveStates = map[string]bool{"pending": true, "queued": true, "running": true}

// ghostSightings are the historical nodes the view ever drew as someone's work
// in progress. One render tick is enough: the user saw it.
func ghostSightings(trace []UIEntry, historical map[string]bool) []string {
	seen := map[string]bool{}
	for _, e := range trace {
		for id, state := range e.States {
			if historical[id] && liveStates[state] {
				seen[id+":"+state] = true
			}
		}
	}
	return sortedKeys(seen)
}

// rebirths are nodes the view lost and introduced again. A snapshot replace is
// not a rebirth; leaving a state and coming back inside one session is.
func rebirths(trace []UIEntry) []string {
	gone, twice := map[string]bool{}, map[string]bool{}
	held := map[string]bool{}
	for _, e := range trace {
		if e.Phase == "loading" {
			gone, held = map[string]bool{}, map[string]bool{}
			continue
		}
		for id := range held {
			if _, still := e.States[id]; !still {
				gone[id] = true
			}
		}
		for id := range e.States {
			if gone[id] {
				twice[id] = true
			}
		}
		held = map[string]bool{}
		for id := range e.States {
			held[id] = true
		}
	}
	return sortedKeys(twice)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// wantedInterruptions is what the host says, formatted the way the view reports
// it. The row compares the two rather than a hand-written expectation.
func wantedInterruptions(o Observation) string {
	var out []string
	for _, i := range rebuiltGraph(o).Interrupted {
		kind := execjournal.InterruptedBeforeStart
		if i.Started {
			kind = execjournal.InterruptedDuringExecution
		}
		out = append(out, i.Execution+":"+kind)
	}
	sort.Strings(out)
	return join(out)
}

func shownInterruptions(trace []UIEntry) string {
	if len(trace) == 0 {
		return ""
	}
	var out []string
	for _, i := range trace[len(trace)-1].Interruptions {
		out = append(out, i.Execution+":"+i.Kind)
	}
	sort.Strings(out)
	return join(out)
}
