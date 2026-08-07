// Package flywheel implements the data-flywheel 清洗/复用 layers
// (docs/DATA_FLYWHEEL.md §2.3–§2.5): structured trajectories, quality
// labels, and the per-project memory directory. It is host-agnostic and
// dependency-free (JSONL + stdlib + internal/retrieval BM25 primitives).
package flywheel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/retrieval"
)

// Step is one tool-call step of a trajectory (summary form — never embeds
// full tool outputs; those live in the session .events.jsonl).
type Step struct {
	Kind   string `json:"kind"` // tool_call | tool_result | verify | judge
	Tool   string `json:"tool,omitempty"`
	DurMs  int64  `json:"dur_ms,omitempty"`
	OK     bool   `json:"ok,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Verify is the validation result attached to a trajectory.
type Verify struct {
	Kind   string `json:"kind"` // go_test | build | smoke | manual
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Label is a quality label (docs/DATA_FLYWHEEL.md §2.4).
type Label struct {
	Score  float64 `json:"score"`
	Name   string  `json:"name"` // excellent | good | partial | failed
	Reason string  `json:"reason"`
}

// Trajectory is the task-level asset (docs §2.3).
type Trajectory struct {
	ID      string   `json:"id"`
	Task    string   `json:"task"`
	Session string   `json:"session"`
	TS      string   `json:"ts"`
	Repo    string   `json:"repo,omitempty"`
	Steps   []Step   `json:"steps"`
	Verify  *Verify  `json:"verify,omitempty"`
	Judge   *Label   `json:"judge,omitempty"`
}

// Store persists trajectories under a root directory:
//
//	<root>/trajectories/<id>.jsonl
//	<root>/memory/NOTES.md
//	<root>/memory/failures/<id>.md
type Store struct {
	root string
}

// NewStore creates the flywheel store root (idempotent).
func NewStore(root string) (*Store, error) {
	for _, d := range []string{root, filepath.Join(root, "trajectories"),
		filepath.Join(root, "memory"), filepath.Join(root, "memory", "failures")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{root: root}, nil
}

// Root returns the store root directory.
func (s *Store) Root() string { return s.root }

// SaveTrajectory writes one trajectory (append-only, one JSON line).
func (s *Store) SaveTrajectory(t *Trajectory) error {
	if t.ID == "" {
		t.ID = fmt.Sprintf("traj_%d", time.Now().UnixNano())
	}
	if t.TS == "" {
		t.TS = time.Now().UTC().Format(time.RFC3339)
	}
	f, err := os.OpenFile(filepath.Join(s.root, "trajectories", t.ID+".jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(t)
}

// LoadTrajectories reads all trajectories (sorted by TS).
func (s *Store) LoadTrajectories() ([]*Trajectory, error) {
	dir := filepath.Join(s.root, "trajectories")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []*Trajectory
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line == "" {
				continue
			}
			var t Trajectory
			if json.Unmarshal([]byte(line), &t) == nil {
				out = append(out, &t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, nil
}

// NOTESPath returns the project memory notes file path.
func (s *Store) NOTESPath() string { return filepath.Join(s.root, "memory", "NOTES.md") }

// AppendNote appends one line to NOTES.md (creates the file if absent).
func (s *Store) AppendNote(line string) error {
	f, err := os.OpenFile(s.NOTESPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, err = f.WriteString(line)
	return err
}

// ReadNotes returns the current NOTES.md content.
func (s *Store) ReadNotes() (string, error) {
	b, err := os.ReadFile(s.NOTESPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

// SaveFailure writes a failure-lesson file (few-shot reuse candidate).
func (s *Store) SaveFailure(t *Trajectory) error {
	if t.Judge == nil {
		t.Judge = &Label{Score: 0, Name: "failed", Reason: "no judge label"}
	}
	body := fmt.Sprintf("# Failure: %s\n\n- task: %s\n- session: %s\n- ts: %s\n- label: %s (%0.2f)\n- reason: %s\n\n## lesson\n\n(记录教训：什么失败、为什么、下次如何避免)\n",
		t.ID, t.Task, t.Session, t.TS, t.Judge.Name, t.Judge.Score, t.Judge.Reason)
	return os.WriteFile(filepath.Join(s.root, "memory", "failures", t.ID+".md"), []byte(body), 0o644)
}

// SearchTrajectories ranks stored trajectories by BM25 relevance to query.
// Returns best-matching trajectories with scores ≥1.0, best first.
func (s *Store) SearchTrajectories(query string, limit int) ([]struct {
	Trajectory *Trajectory
	Score      float64
}, error) {
	trajs, err := s.LoadTrajectories()
	if err != nil {
		return nil, err
	}
	docs := make([]map[string]int, len(trajs))
	for i, t := range trajs {
		docs[i] = retrieval.Counts(retrieval.Tokens(t.Task + " " + t.Repo + " " + labelText(t)))
	}
	df := retrieval.DocumentFrequency(docs)
	qTerms := retrieval.Tokens(strings.ToLower(query))
	totalDocs := len(docs)
	avgLen := 1.0
	if totalDocs > 0 {
		var sum int
		for _, d := range docs {
			sum += len(d)
		}
		avgLen = float64(sum) / float64(totalDocs)
	}
	type hit struct {
		t *Trajectory
		s float64
	}
	var hits []hit
	for i, d := range docs {
		if sc := retrieval.BM25Score(d, len(d), qTerms, df, totalDocs, avgLen); sc >= 1.0 {
			hits = append(hits, hit{trajs[i], sc})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	if limit <= 0 || len(hits) < limit {
		limit = len(hits)
	}
	out := make([]struct {
		Trajectory *Trajectory
		Score      float64
	}, limit)
	for i := 0; i < limit; i++ {
		out[i] = struct {
			Trajectory *Trajectory
			Score      float64
		}{hits[i].t, hits[i].s}
	}
	return out, nil
}

func labelText(t *Trajectory) string {
	if t.Judge == nil {
		return ""
	}
	return t.Judge.Name + " " + t.Judge.Reason
}
