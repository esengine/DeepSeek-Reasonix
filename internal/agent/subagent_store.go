package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/tool"
)

type SubagentStatus string

const (
	SubagentRunning   SubagentStatus = "running"
	SubagentCompleted SubagentStatus = "completed"
	SubagentFailed    SubagentStatus = "failed"
)

// SubagentMeta is the sidecar for a persisted sub-agent transcript. It captures
// the execution identity that must stay stable for continuation/fork.
type SubagentMeta struct {
	Ref              string         `json:"ref"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	Status           SubagentStatus `json:"status"`
	Kind             string         `json:"kind"` // task | skill
	Name             string         `json:"name"`
	WorkspaceRoot    string         `json:"workspaceRoot"`
	ParentSession    string         `json:"parentSession,omitempty"`
	ParentToolCallID string         `json:"parentToolCallId,omitempty"`
	SystemPromptHash string         `json:"systemPromptHash"`
	ToolScope        []string       `json:"toolScope"`
	ToolSchemaHash   string         `json:"toolSchemaHash"`
	Model            string         `json:"model"`
	Effort           string         `json:"effort"`
}

// SubagentSpec describes the current invocation identity.
type SubagentSpec struct {
	Kind             string
	Name             string
	WorkspaceRoot    string
	ParentSession    string
	ParentToolCallID string
	SystemPrompt     string
	Registry         *tool.Registry
	Model            string
	Effort           string
}

// SubagentRun is a prepared transcript run. Call Release exactly once.
type SubagentRun struct {
	Ref     string
	Session *Session
	Meta    SubagentMeta

	store   *SubagentStore
	release func()
}

func (r *SubagentRun) Release() {
	if r != nil && r.release != nil {
		r.release()
		r.release = nil
	}
}

// SubagentStore persists sub-agent transcripts under config.SessionDir()/subagents.
// Its locks are process-local; cross-process mutation is intentionally out of v1.
type SubagentStore struct {
	dir string

	mu     sync.Mutex
	locked map[string]bool
}

func NewSubagentStore(dir string) *SubagentStore {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	return &SubagentStore{dir: dir, locked: map[string]bool{}}
}

func (s *SubagentStore) PrepareFresh(spec SubagentSpec) (*SubagentRun, error) {
	if s == nil {
		return nil, fmt.Errorf("subagent transcript store is required")
	}
	ref, err := s.newRef()
	if err != nil {
		return nil, err
	}
	release, err := s.lock(ref)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	meta := metaFromSpec(ref, SubagentRunning, now, now, spec)
	return &SubagentRun{Ref: ref, Session: NewSession(spec.SystemPrompt), Meta: meta, store: s, release: release}, nil
}

func (s *SubagentStore) PrepareContinue(ref string, spec SubagentSpec) (*SubagentRun, error) {
	if s == nil {
		return nil, fmt.Errorf("subagent continuation is not available in this session")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("continue_from requires a subagent reference")
	}
	release, err := s.lock(ref)
	if err != nil {
		return nil, err
	}
	meta, err := s.LoadMeta(ref)
	if err != nil {
		release()
		return nil, err
	}
	if err := validateMeta(meta, spec); err != nil {
		release()
		return nil, err
	}
	sess, err := LoadSession(s.sessionPath(ref))
	if err != nil {
		release()
		return nil, fmt.Errorf("load subagent transcript %q: %w", ref, err)
	}
	meta.ParentSession = spec.ParentSession
	meta.ParentToolCallID = spec.ParentToolCallID
	return &SubagentRun{Ref: ref, Session: sess, Meta: meta, store: s, release: release}, nil
}

func (s *SubagentStore) PrepareFork(ref string, spec SubagentSpec) (*SubagentRun, error) {
	if s == nil {
		return nil, fmt.Errorf("subagent continuation is not available in this session")
	}
	sourceRef := strings.TrimSpace(ref)
	if sourceRef == "" {
		return nil, fmt.Errorf("fork_from requires a subagent reference")
	}
	sourceRelease, err := s.lock(sourceRef)
	if err != nil {
		return nil, err
	}
	meta, err := s.LoadMeta(sourceRef)
	if err != nil {
		sourceRelease()
		return nil, err
	}
	if err := validateMeta(meta, spec); err != nil {
		sourceRelease()
		return nil, err
	}
	sess, err := LoadSession(s.sessionPath(sourceRef))
	if err != nil {
		sourceRelease()
		return nil, fmt.Errorf("load subagent transcript %q: %w", sourceRef, err)
	}
	newRef, err := s.newRef()
	if err != nil {
		sourceRelease()
		return nil, err
	}
	newRelease, err := s.lock(newRef)
	if err != nil {
		sourceRelease()
		return nil, err
	}
	now := time.Now().UTC()
	newMeta := metaFromSpec(newRef, SubagentRunning, now, now, spec)
	release := func() {
		newRelease()
		sourceRelease()
	}
	return &SubagentRun{Ref: newRef, Session: sess, Meta: newMeta, store: s, release: release}, nil
}

func (s *SubagentStore) MarkRunning(run *SubagentRun) error {
	if s == nil || run == nil || run.Ref == "" {
		return nil
	}
	meta := run.Meta
	meta.Status = SubagentRunning
	meta.UpdatedAt = time.Now().UTC()
	return s.saveMeta(meta)
}

func (s *SubagentStore) SaveCompleted(run *SubagentRun) error {
	if s == nil || run == nil || run.Ref == "" {
		return nil
	}
	if err := run.Session.Save(s.sessionPath(run.Ref)); err != nil {
		return err
	}
	meta := run.Meta
	meta.Status = SubagentCompleted
	meta.UpdatedAt = time.Now().UTC()
	run.Meta = meta
	return s.saveMeta(meta)
}

func (s *SubagentStore) SaveFailed(run *SubagentRun) error {
	if s == nil || run == nil || run.Ref == "" {
		return nil
	}
	meta := run.Meta
	meta.Status = SubagentFailed
	meta.UpdatedAt = time.Now().UTC()
	run.Meta = meta
	return s.saveMeta(meta)
}

func (s *SubagentStore) LoadMeta(ref string) (SubagentMeta, error) {
	var meta SubagentMeta
	if !validSubagentRef(ref) {
		return meta, fmt.Errorf("invalid subagent reference %q", ref)
	}
	data, err := os.ReadFile(s.metaPath(ref))
	if err != nil {
		return meta, fmt.Errorf("load subagent metadata %q: %w", ref, err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, fmt.Errorf("decode subagent metadata %q: %w", ref, err)
	}
	return meta, nil
}

func metaFromSpec(ref string, status SubagentStatus, created, updated time.Time, spec SubagentSpec) SubagentMeta {
	scope, schemaHash := toolIdentity(spec.Registry)
	return SubagentMeta{
		Ref:              ref,
		CreatedAt:        created,
		UpdatedAt:        updated,
		Status:           status,
		Kind:             strings.TrimSpace(spec.Kind),
		Name:             strings.TrimSpace(spec.Name),
		WorkspaceRoot:    strings.TrimSpace(spec.WorkspaceRoot),
		ParentSession:    strings.TrimSpace(spec.ParentSession),
		ParentToolCallID: strings.TrimSpace(spec.ParentToolCallID),
		SystemPromptHash: bytesHash([]byte(spec.SystemPrompt)),
		ToolScope:        scope,
		ToolSchemaHash:   schemaHash,
		Model:            strings.TrimSpace(spec.Model),
		Effort:           strings.TrimSpace(spec.Effort),
	}
}

func validateMeta(meta SubagentMeta, spec SubagentSpec) error {
	if meta.Status == SubagentRunning {
		return fmt.Errorf("subagent reference %q is still in progress", meta.Ref)
	}
	if meta.Status == SubagentFailed {
		return fmt.Errorf("subagent reference %q failed and cannot be continued", meta.Ref)
	}
	want := metaFromSpec(meta.Ref, meta.Status, meta.CreatedAt, meta.UpdatedAt, spec)
	switch {
	case meta.Kind != want.Kind:
		return fmt.Errorf("subagent reference %q has kind %q, want %q", meta.Ref, meta.Kind, want.Kind)
	case meta.Name != want.Name:
		return fmt.Errorf("subagent reference %q has name %q, want %q", meta.Ref, meta.Name, want.Name)
	case meta.WorkspaceRoot != want.WorkspaceRoot:
		return fmt.Errorf("subagent reference %q belongs to workspace %q, current workspace is %q", meta.Ref, meta.WorkspaceRoot, want.WorkspaceRoot)
	case meta.SystemPromptHash != want.SystemPromptHash:
		return fmt.Errorf("subagent reference %q uses a different subagent persona; run a fresh subagent to use the current persona", meta.Ref)
	case !sameStrings(meta.ToolScope, want.ToolScope):
		return fmt.Errorf("subagent reference %q uses a different tool scope", meta.Ref)
	case meta.ToolSchemaHash != want.ToolSchemaHash:
		return fmt.Errorf("subagent reference %q uses different tool schemas", meta.Ref)
	case meta.Model != want.Model || meta.Effort != want.Effort:
		return fmt.Errorf("subagent reference %q uses model/effort %q/%q, current run would use %q/%q", meta.Ref, meta.Model, meta.Effort, want.Model, want.Effort)
	}
	return nil
}

func (s *SubagentStore) lock(ref string) (func(), error) {
	if !validSubagentRef(ref) {
		return nil, fmt.Errorf("invalid subagent reference %q", ref)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked[ref] {
		return nil, fmt.Errorf("subagent reference %q is already running; retry after it finishes", ref)
	}
	s.locked[ref] = true
	return func() {
		s.mu.Lock()
		delete(s.locked, ref)
		s.mu.Unlock()
	}, nil
}

func (s *SubagentStore) newRef() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sa_" + time.Now().UTC().Format("20060102_150405_000000000") + "_" + hex.EncodeToString(b[:]), nil
}

func (s *SubagentStore) sessionPath(ref string) string { return filepath.Join(s.dir, ref+".jsonl") }
func (s *SubagentStore) metaPath(ref string) string    { return filepath.Join(s.dir, ref+".meta.json") }

func (s *SubagentStore) saveMeta(meta SubagentMeta) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.dir, ".subagent-meta.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, s.metaPath(meta.Ref))
}

func validSubagentRef(ref string) bool {
	if !strings.HasPrefix(ref, "sa_") {
		return false
	}
	for _, r := range ref {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func toolIdentity(reg *tool.Registry) ([]string, string) {
	if reg == nil {
		return nil, bytesHash(nil)
	}
	names := reg.Names()
	sort.Strings(names)
	schemas := normalizeToolSchemas(reg.Schemas())
	data, _ := json.Marshal(schemas)
	return names, bytesHash(data)
}

func bytesHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
