package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
	"reasonix/internal/evidence"
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
	Graph           GraphObs            `json:"graph"`
	Artifacts       []ArtifactObs       `json:"artifacts"`
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

type GraphObs struct {
	Deltas int               `json:"deltas"`
	Nodes  []agentgraph.Node `json:"nodes,omitempty"`
	Grants map[string]string `json:"grants,omitempty"`
	Waits  map[string]string `json:"waits,omitempty"`
}

type ArtifactObs struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

func capture(phase, arm, bootSystem string, ctrl *control.Controller, sink *graphSink) Observation {
	path := ctrl.SessionPath()
	history := ctrl.History()
	system := systemText(history)
	snap := ctrl.ContextMaintenanceSnapshot()
	st, loaded, loadErr := agent.LoadCompactionState(path)
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
		Sidecar:   sidecarObs(st, loaded, loadErr),
		View:      viewObs(st, loaded, history),
		Graph:     graphObs(graph, deltas),
		Artifacts: readArtifacts(path),
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
func viewObs(st agent.CompactionState, ok bool, canonical []provider.Message) ViewObs {
	var out ViewObs
	if !ok || len(st.Projection.Messages) == 0 {
		out.Messages = len(canonical)
		out.Markers = markersIn(contents(canonical), markerPattern)
		out.Echoes = markersIn(contents(canonical), echoPattern)
		return out
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
	return out
}

func contents(msgs []provider.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func graphObs(g agentgraph.Graph, deltas int) GraphObs {
	out := GraphObs{Deltas: deltas}
	if len(g.Nodes) == 0 {
		return out
	}
	out.Nodes = g.Nodes
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
