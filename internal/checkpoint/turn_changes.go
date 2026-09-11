package checkpoint

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/diff"
	fileenc "reasonix/internal/fileutil/encoding"
)

// TurnChangesBudget bounds retained patches and the content processed per turn.
const TurnChangesBudget = 2 << 20

// TurnChanges contains only confirmed net changes. Partial and unknown results
// must never be presented as an exhaustive inventory of workspace changes.
type TurnChanges struct {
	ID       string     `json:"id,omitempty"`
	Turn     int        `json:"turn"`
	Coverage string     `json:"coverage"`
	Files    []TurnFile `json:"files"`
	Added    int        `json:"added"`
	Removed  int        `json:"removed"`
	Reasons  []string   `json:"reasons"`
}

type TurnFile struct {
	Path        string    `json:"path"`
	Kind        diff.Kind `json:"kind"`
	Added       int       `json:"added"`
	Removed     int       `json:"removed"`
	Binary      bool      `json:"binary,omitempty"`
	ModeOnly    bool      `json:"modeOnly,omitempty"`
	Uncounted   bool      `json:"uncounted,omitempty"`
	Unavailable string    `json:"unavailable,omitempty"`
	Patch       string    `json:"patch,omitempty"`
}

func emptyTurnChanges(turn int) *TurnChanges {
	return &TurnChanges{Turn: turn, Coverage: "unknown", Files: []TurnFile{}, Reasons: []string{}}
}

func cloneTurnChanges(r *TurnChanges) *TurnChanges {
	if r == nil {
		return nil
	}
	out := *r
	out.Files = append([]TurnFile{}, r.Files...)
	out.Reasons = append([]string{}, r.Reasons...)
	return &out
}

// Summary omits patches from transport events and transcript sidecars.
func (r *TurnChanges) Summary() *TurnChanges {
	out := cloneTurnChanges(r)
	if out != nil {
		for i := range out.Files {
			out.Files[i].Patch = ""
		}
	}
	return out
}

func (r *TurnChanges) gap(reason string) {
	r.Coverage = "partial"
	if !slices.Contains(r.Reasons, reason) {
		r.Reasons = append(r.Reasons, reason)
	}
}

// TurnChanges returns the frozen result; reading history never touches the
// workspace. A returned value is detached from the store's mutable state.
func (s *Store) TurnChanges(turn int) *TurnChanges {
	if s == nil {
		return emptyTurnChanges(turn)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.all() {
		if c.Turn == turn && c.Result != nil {
			return cloneTurnChanges(c.Result)
		}
	}
	return emptyTurnChanges(turn)
}

// FreezeTurnChanges runs while controller turn admission is closed. The shared
// mutation barrier excludes owned writes without waiting on background jobs.
// Untracked/external writes are rejected by their after-image fingerprints.
func (s *Store) FreezeTurnChanges(turn int) *TurnChanges {
	if s == nil {
		return emptyTurnChanges(turn)
	}
	owned := s.barrier.TryEnterExclusive()
	if owned {
		defer s.barrier.ExitExclusive()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var c *Checkpoint
	for _, candidate := range s.all() {
		if candidate.Turn == turn {
			c = candidate
			break
		}
	}
	if c == nil {
		return emptyTurnChanges(turn)
	}
	if c.Result != nil {
		return cloneTurnChanges(c.Result)
	}
	r := emptyTurnChanges(turn)
	r.ID = fmt.Sprintf("%d:%d", turn, c.Time.UnixNano())
	r.Coverage = "complete"
	for _, gap := range c.CoverageGaps {
		r.gap(string(gap.Reason))
	}
	if c.Legacy || c.ExpiredFilePayload {
		r.gap("snapshot_unavailable")
	}
	if !owned || len(s.activeWriters) > 0 {
		r.gap("active_writer")
	} else {
		remaining := TurnChangesBudget
		for _, f := range c.Files {
			s.addTurnFile(r, f, &remaining)
		}
	}
	c.Result = r
	if err := s.persist(c); err != nil {
		r.gap("result_not_saved")
	}
	return cloneTurnChanges(r)
}

func (s *Store) addTurnFile(r *TurnChanges, f FileSnap, remaining *int) {
	if f.PayloadExpired || (f.SHA256 != "" && f.Content == nil) {
		r.gap("snapshot_unavailable")
		return
	}
	if f.AfterExisted == nil || (*f.AfterExisted && f.AfterSHA256 == "") {
		r.gap("ownership_unknown")
		return
	}
	if f.Content != nil && (f.SHA256 == "" || Digest(v3PayloadBytes(f)) != f.SHA256) {
		r.gap("snapshot_unavailable")
		return
	}
	before := ""
	if f.Content != nil {
		before = *f.Content
	}
	if len(before) >= *remaining {
		r.gap("size_limit")
		return
	}
	after, gap, err := CapturePath(f.Path, CaptureOptions{WorkspaceRoot: s.root, ReadContent: true, MaxBytes: int64(*remaining - len(before))})
	if err != nil || gap != nil {
		r.gap("file_unavailable")
		return
	}
	if CompareIdentity(after, f.AfterSHA256, f.AfterExisted, f.AfterMode) != "" {
		r.gap("external_change")
		return
	}
	existed := f.Content != nil || f.SHA256 != ""
	if existed == after.Existed && (!existed || (f.SHA256 == after.SHA256 && f.Mode == after.Mode)) {
		return
	}
	kind := diff.Modify
	if !existed {
		kind = diff.Create
	} else if !after.Existed {
		kind = diff.Delete
	}
	enc, raw := fileenc.Detect(after.Content)
	text := string(fileenc.Decode(raw, enc))
	if len(before)+len(text) > *remaining {
		r.gap("size_limit")
		return
	}
	change := diff.Build(NormalizeRelPath(s.root, f.Path), before, text, kind)
	entry := TurnFile{Path: change.Path, Kind: kind, Binary: change.Binary, ModeOnly: existed && after.Existed && f.SHA256 == after.SHA256 && f.Mode != after.Mode}
	if strings.HasPrefix(change.Diff, "(diff omitted:") {
		entry.Uncounted, entry.Unavailable = true, "size_limit"
		r.gap("size_limit")
	} else {
		entry.Added, entry.Removed = change.Added, change.Removed
		entry.Patch = change.Diff
		if len(before)+len(text)+len(entry.Patch) > *remaining {
			entry.Patch, entry.Unavailable = "", "size_limit"
		}
	}
	*remaining -= len(before) + len(text) + len(entry.Patch)
	r.Files = append(r.Files, entry)
	r.Added += entry.Added
	r.Removed += entry.Removed
}
