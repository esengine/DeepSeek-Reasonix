package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
	"reasonix/internal/evidence"
	"reasonix/internal/execjournal"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// Observation is everything one phase can read about a session without asking a
// model. Both phases capture the same shape so the matrix is a comparison and
// not two differently-shaped reports.
type Observation struct {
	Phase       string `json:"phase"`
	Arm         string `json:"arm"`
	SessionPath string `json:"session_path"`
	SystemHash  string `json:"system_hash"`
	SystemBytes int    `json:"system_bytes"`
	// BootSystemHash is what this process's assembly composed, read before any
	// resume; SystemHash is the session's own leading row. Different questions.
	BootSystemHash  string              `json:"boot_system_hash"`
	BootSystemBytes int                 `json:"boot_system_bytes"`
	Transcript      TranscriptObs       `json:"transcript"`
	Goal            GoalObs             `json:"goal"`
	Todos           []evidence.TodoItem `json:"todos"`
	Decisions       []control.Decision  `json:"decisions"`
	Context         ContextObs          `json:"context"`
	Sidecar         SidecarObs          `json:"sidecar"`
	View            ViewObs             `json:"view"`
	TodoNotes       TodoNoteObs         `json:"todo_notes"`
	Deferred        DeferredObs         `json:"deferred"`
	Obligation      ObligationObs       `json:"obligation"`
	// Interrupted is what the host derives from a barrier it found open and did
	// not open itself: evidence the wait happened, not a question to answer.
	Interrupted []control.InterruptedAdjudication `json:"interrupted"`
	// ModelSeesInterruption counts the interruption blocks the request carries,
	// which is a different surface from the derived fact behind them.
	ModelSeesInterruption int `json:"model_sees_interruption"`
	// Journal is how each barrier ended. A superseded edge is the durable
	// evidence that a turn received the interruption: nothing else writes one.
	Journal []control.AdjudicationEntry `json:"journal,omitempty"`
	Graph   GraphObs                    `json:"graph"`
	// Children is the durable side of the same fan-out the graph draws. The
	// graph has no read surface after a restart, so without this a lost graph
	// and a lost delegation would be one indistinguishable row.
	Children ChildrenObs `json:"children"`
	// FanOutTurn is whether the dispatching turn is in this phase's transcript
	// at all. A turn is appended when it ends, so a process that dies inside one
	// leaves no record of the request, and a lost graph is the smaller half.
	FanOutTurn bool `json:"fan_out_turn"`
	// Executions is the durable record of delegations this turn opened, and
	// InterruptedExecutions the host's judgement over it: open, with no owner
	// here. Written before the work starts, so it outlives an unfinished turn.
	Executions            []execjournal.Entry `json:"executions,omitempty"`
	InterruptedExecutions []execjournal.Entry `json:"interrupted_executions,omitempty"`
	// ModelSeesInterruptedExecution counts the blocks the next request carries
	// about them, which is a different surface from the fact behind them.
	ModelSeesInterruptedExecution int `json:"model_sees_interrupted_execution"`
	// WaitCauses counts the refusals the graph carries, and WaitSeries keeps
	// each node's causes in publication order. The fold keeps only the latest,
	// which cannot say whether a cause was reported once or replaced.
	WaitCauses map[string]int      `json:"wait_causes,omitempty"`
	WaitSeries map[string][]string `json:"wait_series,omitempty"`
	// SettledWorkers and QueuedWorkers are the transition arm's evidence that
	// capacity was actually freed while a refusal still stood.
	SettledWorkers int           `json:"settled_workers"`
	QueuedWorkers  []string      `json:"queued_workers,omitempty"`
	Artifacts      []ArtifactObs `json:"artifacts"`
}

type TranscriptObs struct {
	Messages int    `json:"messages"`
	Digest   string `json:"digest"`
}

type GoalObs struct {
	Goal      string `json:"goal"`
	Status    string `json:"status"`
	TurnsUsed int    `json:"turns_used"`
}

// ContextObs is the live judgement: CheckpointState is "none" whenever the
// projection did not survive validation, which is what the compaction arms are
// actually asking about.
type ContextObs struct {
	ProjectionVersion uint64 `json:"projection_version"`
	CheckpointState   string `json:"checkpoint_state"`
	CanonicalTokens   int    `json:"canonical_tokens"`
	ProjectedTokens   int    `json:"projected_tokens"`
	Blocked           bool   `json:"blocked"`
}

// SidecarObs is the stored claim, read off disk rather than through the agent,
// so a projection the host refuses to use is still visible as present.
type SidecarObs struct {
	Present           bool   `json:"present"`
	Messages          int    `json:"messages"`
	CoveredCount      int    `json:"covered_count"`
	CoveredPrefixHash string `json:"covered_prefix_hash"`
	ProjectionVersion uint64 `json:"projection_version"`
	TranscriptVersion uint64 `json:"transcript_version"`
	Generation        uint64 `json:"generation"`
	Err               string `json:"err,omitempty"`
}

// ViewObs is the model-visible context, spliced the way ContextProjection
// declares it: the stored body followed by canonical[CoveredCount:]. Markers
// are what a fold can be held to — the scripted digest carries none, so a
// marker that is gone was dropped, not summarised.
type ViewObs struct {
	Messages int `json:"messages"`
	// Markers are the user turns still shown, Echoes the assistant replies. A
	// fold takes the reply first, so the two answer different questions.
	Markers         []string `json:"markers"`
	Echoes          []string `json:"echoes"`
	BodyMarkers     []string `json:"body_markers"`
	BodyEchoes      []string `json:"body_echoes"`
	SplicedFromTail int      `json:"spliced_from_tail"`
}

// TodoNoteObs locates the host's step-identity note inside the model-visible
// view. BodyLen is where the frozen body ends, so an index below it says the
// note was frozen ahead of the live tail rather than riding it.
type TodoNoteObs struct {
	Count   int      `json:"count"`
	Indexes []int    `json:"indexes"`
	IDs     []string `json:"ids"`
	BodyLen int      `json:"body_len"`
	ViewLen int      `json:"view_len"`
	HostIDs []string `json:"host_ids"`
	// ReadableInHistory says the conversation still shows every current id, so
	// no note is owed. Without it a count of zero cannot be told apart from a
	// lost identity, and the gate would demand a note nobody needs.
	ReadableInHistory bool `json:"readable_in_history"`
}

// todoNoteMarker is the opening of todoIdentityNote. The probe reads the view
// the way a model would, so it matches the text rather than a type.
const todoNoteMarker = "Host task state."

var stepIDPattern = regexp.MustCompile(`probe_step_\d+`)

func todoNoteObs(view, history []provider.Message, todos []evidence.TodoItem) TodoNoteObs {
	out := TodoNoteObs{BodyLen: len(history), ViewLen: len(view)}
	for _, t := range todos {
		if t.StepID != "" {
			out.HostIDs = append(out.HostIDs, t.StepID)
		}
	}
	out.ReadableInHistory = len(out.HostIDs) > 0
	for _, id := range out.HostIDs {
		if !mentionsID(history, id) {
			out.ReadableInHistory = false
		}
	}
	for i, m := range view {
		if !strings.Contains(m.Content, todoNoteMarker) {
			continue
		}
		out.Count++
		out.Indexes = append(out.Indexes, i)
		out.IDs = append(out.IDs, stepIDPattern.FindAllString(m.Content, -1)...)
	}
	return out
}

// DeferredObs is the effect a pending decision is holding back. Whether the
// decision itself survives is one question; whether what it blocked stayed
// blocked is the one that cannot be answered by a design choice.
type DeferredObs struct {
	MarkerPath string `json:"marker_path"`
	Executed   bool   `json:"executed"`
}

func deferredObs(workspace string) DeferredObs {
	path := filepath.Join(workspace, deferredEffect)
	_, err := os.Stat(path)
	return DeferredObs{MarkerPath: deferredEffect, Executed: err == nil}
}

// ObligationObs is what the transcript itself still says about a turn that
// never finished: a tool call with no result is a claim the host made and did
// not settle, and an interruption record is the host saying so outright. Either
// tells a lost decision apart from a silently dropped one.
type ObligationObs struct {
	UnansweredCalls    []string `json:"unanswered_calls"`
	InterruptionMarked int      `json:"interruption_marked"`
}

func obligationObs(msgs []provider.Message) ObligationObs {
	answered := map[string]bool{}
	var out ObligationObs
	for _, m := range msgs {
		if m.ToolCallID != "" {
			answered[m.ToolCallID] = true
		}
		if m.InterruptedTurn != nil {
			out.InterruptionMarked++
		}
	}
	for _, m := range msgs {
		for _, call := range m.ToolCalls {
			if call.ID != "" && call.ID != provider.LocalOnlyToolID && !answered[call.ID] {
				out.UnansweredCalls = append(out.UnansweredCalls, call.Name+":"+call.ID)
			}
		}
	}
	return out
}

type GraphObs struct {
	Deltas int               `json:"deltas"`
	Nodes  []agentgraph.Node `json:"nodes,omitempty"`
	Edges  []agentgraph.Edge `json:"edges,omitempty"`
	Grants map[string]string `json:"grants,omitempty"`
	Waits  map[string]string `json:"waits,omitempty"`
}

// ChildFact is one delegated run as the durable store records it, which is a
// different set of facts from the node the graph drew for it. Whatever the
// graph carried and this does not is a semantic no reconstruction can recover.
type ChildFact struct {
	Ref              string `json:"ref"`
	Status           string `json:"status"`
	Kind             string `json:"kind,omitempty"`
	Name             string `json:"name,omitempty"`
	ParentToolCallID string `json:"parentToolCallId,omitempty"`
	Model            string `json:"model,omitempty"`
	Effort           string `json:"effort,omitempty"`
}

// ChildrenObs is every child the store still owns for this parent. It is the
// only durable execution evidence a restart has been shown to read, so a graph
// classification that ignores it would report a loss the host can in fact
// still speak to.
type ChildrenObs struct {
	Facts []ChildFact `json:"facts,omitempty"`
	Err   string      `json:"err,omitempty"`
}

// childrenObs reads the store the way a restart does: by parent session id,
// which is the transcript's stem. A session with no path owns nothing.
func childrenObs(root armRoot, sessionPath string) ChildrenObs {
	if strings.TrimSpace(sessionPath) == "" {
		return ChildrenObs{}
	}
	parent := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	artifacts, err := agent.ListSubagentsByParent(root.Sessions, parent)
	if err != nil {
		return ChildrenObs{Err: err.Error()}
	}
	out := ChildrenObs{}
	for _, a := range artifacts {
		out.Facts = append(out.Facts, ChildFact{
			Ref: a.Ref, Status: string(a.Meta.Status), Kind: a.Meta.Kind, Name: a.Meta.Name,
			ParentToolCallID: a.Meta.ParentToolCallID, Model: a.Meta.Model, Effort: a.Meta.Effort,
		})
	}
	sort.Slice(out.Facts, func(i, j int) bool { return out.Facts[i].Ref < out.Facts[j].Ref })
	return out
}

type ArtifactObs struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

func capture(phase, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, root armRoot) Observation {
	path := ctrl.SessionPath()
	history := ctrl.History()
	system := systemText(history)
	snap := ctrl.ContextMaintenanceSnapshot()
	st, loaded, loadErr := agent.LoadCompactionState(path)
	view, viewMsgs := viewObs(st, loaded, history)
	graph, deltas := sink.snapshot()
	return Observation{
		Phase:           phase,
		Arm:             arm,
		SessionPath:     path,
		SystemHash:      shortSum(system),
		SystemBytes:     len(system),
		BootSystemHash:  shortSum(bootSystem),
		BootSystemBytes: len(bootSystem),
		Transcript:      TranscriptObs{Messages: len(history), Digest: transcriptDigest(history)},
		Goal:            GoalObs{Goal: ctrl.Goal(), Status: ctrl.GoalStatus(), TurnsUsed: ctrl.GoalRuntime().TurnsUsed},
		Todos:           ctrl.Todos(),
		Decisions:       ctrl.Decisions(),
		Context: ContextObs{
			ProjectionVersion: snap.ProjectionVersion,
			CheckpointState:   snap.CheckpointState,
			CanonicalTokens:   snap.CanonicalTokens,
			ProjectedTokens:   snap.ProjectedTokens,
			Blocked:           snap.Blocked,
		},
		Sidecar: sidecarObs(st, loaded, loadErr),
		View:    view,
		// The note is derived for a request and stored nowhere, so the contracts
		// about it are read off the request rather than off the stored view.
		TodoNotes:             todoNoteObs(ctrl.ModelVisibleMessages(), viewMsgs, ctrl.Todos()),
		Graph:                 graphObs(graph, deltas),
		WaitCauses:            waitCauseCounts(graph),
		WaitSeries:            sink.waitSeries(),
		SettledWorkers:        settledWorkers(graph),
		QueuedWorkers:         queuedWorkers(graph),
		Children:              childrenObs(root, path),
		FanOutTurn:            fanOutTurnRecorded(history),
		Executions:            control.ExecutionHistory(path),
		InterruptedExecutions: ctrl.InterruptedExecutions(),
		ModelSeesInterruptedExecution: countBlocks(
			ctrl.ModelVisibleMessages(), "<interrupted-execution>"),
		Deferred:              deferredObs(root.Workspace),
		Obligation:            obligationObs(history),
		Interrupted:           ctrl.InterruptedAdjudications(),
		Journal:               control.AdjudicationHistory(path),
		ModelSeesInterruption: countBlocks(ctrl.ModelVisibleMessages(), "<interrupted-adjudication>"),
		Artifacts:             readArtifacts(path),
	}
}

// bootSystemText is what the freshly built assembly composed, read before a
// resume replaces the session under it.
func bootSystemText(ctrl *control.Controller) string { return systemText(ctrl.History()) }

func systemText(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// transcriptDigest covers only what reaches a provider, so a local-only marker
// appended by either process cannot read as a changed conversation.
func transcriptDigest(msgs []provider.Message) string {
	h := sha256.New()
	for _, m := range provider.ModelMessages(msgs) {
		b, _ := json.Marshal(struct {
			R, C, N string
		}{string(m.Role), m.Content, m.Name})
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func shortSum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func sidecarObs(st agent.CompactionState, ok bool, err error) SidecarObs {
	out := SidecarObs{Present: ok}
	if err != nil {
		out.Err = err.Error()
		return out
	}
	if !ok {
		return out
	}
	out.Messages = len(st.Projection.Messages)
	out.CoveredCount = st.Projection.CoveredCount
	out.CoveredPrefixHash = st.Projection.CoveredPrefixHash
	out.ProjectionVersion = st.Projection.ProjectionVersion
	out.TranscriptVersion = st.TranscriptVersion
	out.Generation = st.Generation
	return out
}

// viewObs splices the model-visible context. With no usable projection the
// view is the canonical transcript, which is what the host would send.
func viewObs(st agent.CompactionState, ok bool, canonical []provider.Message) (ViewObs, []provider.Message) {
	var out ViewObs
	if !ok || len(st.Projection.Messages) == 0 {
		out.Messages = len(canonical)
		out.Markers = markersIn(contents(canonical), markerPattern)
		out.Echoes = markersIn(contents(canonical), echoPattern)
		return out, canonical
	}
	body := st.Projection.Messages
	out.BodyMarkers = markersIn(contents(body), markerPattern)
	out.BodyEchoes = markersIn(contents(body), echoPattern)
	view := append([]provider.Message(nil), body...)
	if n := st.Projection.CoveredCount; n >= 0 && n < len(canonical) {
		view = append(view, canonical[n:]...)
		out.SplicedFromTail = len(canonical) - n
	}
	out.Messages = len(view)
	out.Markers = markersIn(contents(view), markerPattern)
	out.Echoes = markersIn(contents(view), echoPattern)
	return out, view
}

// mentionsID reads an id the way the host's own visibility check does: text or
// tool arguments, anywhere in the messages the request carries.
func mentionsID(msgs []provider.Message, id string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, id) {
			return true
		}
		for _, call := range m.ToolCalls {
			if strings.Contains(call.Arguments, id) {
				return true
			}
		}
	}
	return false
}

func countBlocks(msgs []provider.Message, marker string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Content, marker) {
			n++
		}
	}
	return n
}

func contents(msgs []provider.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

// waitCauseCounts is how many nodes the graph shows under each refusal. It
// counts nodes rather than reports: a node keeps its cause after it starts, so
// this answers what was refused, not what is refused now.
func waitCauseCounts(g agentgraph.Graph) map[string]int {
	out := map[string]int{}
	for _, n := range g.Nodes {
		if n.Wait != "" {
			out[string(n.Wait)]++
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// queuedWorkers are the items still refused admission, in a stable order.
func queuedWorkers(g agentgraph.Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind != agentgraph.KindGroup && n.State == agentgraph.StateQueued {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}

func graphObs(g agentgraph.Graph, deltas int) GraphObs {
	out := GraphObs{Deltas: deltas}
	if len(g.Nodes) == 0 {
		return out
	}
	out.Nodes = g.Nodes
	out.Edges = g.Edges
	out.Grants = map[string]string{}
	out.Waits = map[string]string{}
	for _, n := range g.Nodes {
		if n.Grant != "" {
			out.Grants[n.ID] = string(n.Grant)
		}
		if n.Wait != "" {
			out.Waits[n.ID] = string(n.Wait)
		}
	}
	return out
}

// readArtifacts lists the session sidecars that exist, which is the evidence
// behind every "persisted" claim in the matrix.
func readArtifacts(sessionPath string) []ArtifactObs {
	if sessionPath == "" {
		return nil
	}
	paths := append([]string{sessionPath}, store.SessionSidecarFiles(sessionPath)...)
	var out []ArtifactObs
	for _, p := range paths {
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, ArtifactObs{Name: filepath.Base(p), Bytes: info.Size()})
	}
	return out
}

func writeObservation(root armRoot, obs Observation) error {
	b, err := json.MarshalIndent(obs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root.Dir, "obs-"+obs.Phase+".json"), b, 0o644)
}

func readObservation(root armRoot, phase string) (Observation, error) {
	var obs Observation
	b, err := os.ReadFile(filepath.Join(root.Dir, "obs-"+phase+".json"))
	if err != nil {
		return obs, err
	}
	return obs, json.Unmarshal(b, &obs)
}
