package runtimeservice

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"reasonix/internal/agent"
	"reasonix/internal/autoresearch"
	"reasonix/internal/config"
	"reasonix/internal/memory"
	"reasonix/internal/memorycompiler"
	"reasonix/internal/provider"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/skill"
)

var (
	ErrCapabilityUnavailable  = errors.New("runtime service: capability unavailable")
	ErrInvalidMemoryInput     = errors.New("runtime service: invalid memory input")
	ErrUnknownMemoryID        = errors.New("runtime service: unknown memory id")
	ErrUnknownDocumentID      = errors.New("runtime service: unknown document id")
	ErrUnknownSuggestionID    = errors.New("runtime service: unknown suggestion id")
	ErrStaleRevision          = errors.New("runtime service: stale revision")
	ErrMemoryMutationFailed   = errors.New("runtime service: memory mutation failed")
	ErrInvalidResearchInput   = errors.New("runtime service: invalid research input")
	ErrResearchTaskNotFound   = errors.New("runtime service: research task not found")
	ErrResearchMutationFailed = errors.New("runtime service: research mutation failed")
)

const (
	suggestionSessionLimit     = 12
	memorySuggestionLimit      = 6
	compilerSuggestionLimit    = 3
	compilerSuggestionMinCount = 2
	memoryResearchSourceItems  = 10000
	memoryResearchJSONBytes    = 6 << 20
)

// MemoryController is the target-neutral Controller surface used by both Local
// and Remote RuntimeAPI implementations. It deliberately excludes transport,
// tab, and filesystem browsing concepts. Paths returned by mutations never
// leave this service; they are collapsed to safe display labels.
type MemoryController interface {
	Memory() *memory.Set
	QuickAdd(memory.Scope, string) (string, error)
	ForgetMemory(string) error
	SaveDoc(string, string) (string, error)
	SaveMemory(memory.Memory) (string, error)
	AllSkills() []skill.Skill
	CreateSkill(string, skill.Scope, string) (string, error)
	WorkspaceRoot() string
	SessionDir() string
	SessionPath() string
}

type memorySuggestionCandidate struct {
	Name        string
	Title       string
	Description string
	Type        memory.Type
	Body        string
	Reason      string
	Evidence    []string
}

type skillSuggestionCandidate struct {
	Name        string
	Description string
	Scope       skill.Scope
	Body        string
	Reason      string
	Evidence    []string
}

type suggestionCache struct {
	revision runtimeapi.CatalogRevision
	memories map[runtimeapi.SuggestionID]memorySuggestionCandidate
	skills   map[runtimeapi.SuggestionID]skillSuggestionCandidate
}

type memoryDocumentBinding struct {
	path  string
	scope runtimeapi.CatalogScope
}

type memoryFactBinding struct {
	name       string
	scope      runtimeapi.CatalogScope
	document   string
	beforeBody string
	afterBody  string
}

type researchCriterionBinding struct {
	taskID      string
	criterionID string
}

// MemoryResearchService owns opaque IDs, exact suggestion admission state, and
// revision-bound research cursors for one Runtime incarnation. Its mutex makes
// the same implementation safe for a Local adapter; the Remote Host additionally
// calls every method from its Session actor barrier.
type MemoryResearchService struct {
	mu               sync.Mutex
	binding          RuntimeBinding
	workspaceRoot    string
	key              [32]byte
	documents        map[runtimeapi.DocumentID]memoryDocumentBinding
	facts            map[runtimeapi.MemoryID]memoryFactBinding
	researchTasks    map[runtimeapi.ResearchTaskID]string
	researchCriteria map[runtimeapi.CriterionID]researchCriterionBinding
	suggestions      suggestionCache
}

func NewMemoryResearchService(binding RuntimeBinding, workspaceRoot string) (*MemoryResearchService, error) {
	if err := requireSession(binding.Session); err != nil || strings.TrimSpace(binding.Incarnation) == "" {
		return nil, ErrInvalidSession
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil, ErrInvalidPath
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrInvalidPath
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	s := &MemoryResearchService{
		binding: binding, workspaceRoot: filepath.Clean(abs),
		documents: make(map[runtimeapi.DocumentID]memoryDocumentBinding), facts: make(map[runtimeapi.MemoryID]memoryFactBinding),
		researchTasks: make(map[runtimeapi.ResearchTaskID]string), researchCriteria: make(map[runtimeapi.CriterionID]researchCriterionBinding),
	}
	if _, err := rand.Read(s.key[:]); err != nil {
		return nil, fmt.Errorf("runtime service: initialize memory/research key: %w", err)
	}
	return s, nil
}

func (s *MemoryResearchService) bindingMatches(session runtimeapi.SessionRef) bool {
	return s != nil && session.Valid() && session == s.binding.Session
}

func (s *MemoryResearchService) opaque(prefix string, values ...string) string {
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(s.binding.Incarnation))
	for _, value := range values {
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write([]byte(value))
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18])
}

func (s *MemoryResearchService) MemoryView(session runtimeapi.SessionRef, controller MemoryController) (runtimeapi.MemoryView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || controller == nil {
		return runtimeapi.MemoryView{}, ErrInvalidMemoryInput
	}
	set := controller.Memory()
	if set == nil {
		return runtimeapi.MemoryView{}, ErrCapabilityUnavailable
	}
	if len(set.Docs) > memoryResearchSourceItems {
		return runtimeapi.MemoryView{}, ErrQueryFailed
	}
	view := runtimeapi.MemoryView{
		Available: true, Documents: []runtimeapi.MemoryDocument{}, Facts: []runtimeapi.MemoryFact{},
		Archives: []runtimeapi.MemoryArchive{}, Scopes: []runtimeapi.MemoryScope{},
	}
	documents := make(map[runtimeapi.DocumentID]memoryDocumentBinding)
	for index, source := range set.Docs {
		path := filepath.Clean(source.Path)
		id := runtimeapi.DocumentID(s.opaque("document", string(source.Scope), path))
		if _, duplicate := documents[id]; duplicate {
			return runtimeapi.MemoryView{}, ErrQueryFailed
		}
		documents[id] = memoryDocumentBinding{path: path, scope: catalogScopeForMemoryScope(source.Scope)}
		body := source.Body
		view.Documents = append(view.Documents, runtimeapi.MemoryDocument{
			DocumentID: id, Scope: string(source.Scope), Body: &body,
			DisplayPath: collapsedMemoryPath(string(source.Scope), path, index),
		})
	}

	facts := make(map[runtimeapi.MemoryID]memoryFactBinding)
	// memory/remember returns an addressable ID for the appended document note.
	// Preserve those runtime-bound bindings across later memory/get refreshes;
	// active fact bindings below are rebuilt from the current Store snapshot.
	for id, binding := range s.facts {
		if binding.document != "" {
			facts[id] = binding
		}
	}
	active := append([]memory.Memory(nil), set.Store.List()...)
	if len(active)+len(set.Docs) > memoryResearchSourceItems {
		return runtimeapi.MemoryView{}, ErrQueryFailed
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].Name < active[j].Name })
	for _, fact := range active {
		id := runtimeapi.MemoryID(s.opaque("memory", "active", fact.Name))
		if _, duplicate := facts[id]; duplicate {
			return runtimeapi.MemoryView{}, ErrQueryFailed
		}
		facts[id] = memoryFactBinding{name: fact.Name, scope: catalogScopeForMemoryType(fact.Type)}
		body := fact.Body
		view.Facts = append(view.Facts, runtimeapi.MemoryFact{
			MemoryID: id, Name: fact.Name, Title: fact.Title, Description: fact.Description,
			Type: string(fact.Type), Body: &body,
		})
	}
	archives := append([]memory.ArchivedMemory(nil), set.Store.ListArchived()...)
	if len(archives)+len(active)+len(set.Docs) > memoryResearchSourceItems {
		return runtimeapi.MemoryView{}, ErrQueryFailed
	}
	sort.SliceStable(archives, func(i, j int) bool {
		if archives[i].ArchivedAt.Equal(archives[j].ArchivedAt) {
			return archives[i].Name < archives[j].Name
		}
		return archives[i].ArchivedAt.After(archives[j].ArchivedAt)
	})
	for _, archived := range archives {
		id := runtimeapi.MemoryID(s.opaque("memory", "archive", archived.Name, archived.ArchivedAt.UTC().Format(time.RFC3339Nano)))
		body := archived.Body
		archivedAt := ""
		if !archived.ArchivedAt.IsZero() {
			archivedAt = archived.ArchivedAt.UTC().Format(time.RFC3339)
		}
		view.Archives = append(view.Archives, runtimeapi.MemoryArchive{
			MemoryFact: runtimeapi.MemoryFact{
				MemoryID: id, Name: archived.Name, Title: archived.Title, Description: archived.Description,
				Type: string(archived.Type), Body: &body,
			},
			ArchivedAt: archivedAt,
		})
	}
	for _, scope := range []memory.Scope{memory.ScopeUser, memory.ScopeProject, memory.ScopeLocal} {
		path := set.DocPath(scope)
		if path == "" {
			continue
		}
		view.Scopes = append(view.Scopes, runtimeapi.MemoryScope{
			Scope: string(scope), DisplayPath: collapsedMemoryPath(string(scope), path, 0),
		})
		// A writable empty/new document is also a valid document/save target even
		// before discovery loads it into Docs.
		id := runtimeapi.DocumentID(s.opaque("document", string(scope), filepath.Clean(path)))
		if _, exists := documents[id]; !exists {
			documents[id] = memoryDocumentBinding{path: filepath.Clean(path), scope: catalogScopeForMemoryScope(scope)}
		}
	}
	view.Revision = memoryViewRevision(view, s.binding)
	if !withinMemoryResearchBudget(view) {
		return runtimeapi.MemoryView{}, ErrQueryFailed
	}
	s.documents = documents
	s.facts = facts
	return view, nil
}

func collapsedMemoryPath(scope, raw string, ordinal int) string {
	label := strings.ToLower(strings.TrimSpace(scope))
	switch label {
	case "user", "ancestor", "project", "local":
	default:
		label = "memory"
	}
	base := filepath.Base(filepath.Clean(raw))
	if base == "." || base == string(filepath.Separator) || strings.TrimSpace(base) == "" {
		base = "document"
	}
	if label == "ancestor" && ordinal > 0 {
		return fmt.Sprintf("ancestor-%d/%s", ordinal+1, base)
	}
	return label + "/" + base
}

func memoryViewRevision(view runtimeapi.MemoryView, binding RuntimeBinding) runtimeapi.CatalogRevision {
	view.Revision = ""
	raw, _ := json.Marshal(view)
	sum := sha256.Sum256(append([]byte(sessionBinding(binding.Session)+"\x00"+binding.Incarnation+"\x00"), raw...))
	return runtimeapi.CatalogRevision("memory_" + base64.RawURLEncoding.EncodeToString(sum[:]))
}

func (s *MemoryResearchService) Remember(session runtimeapi.SessionRef, controller MemoryController, scope, note string) (runtimeapi.RememberMemoryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || controller == nil {
		return runtimeapi.RememberMemoryResult{}, ErrInvalidMemoryInput
	}
	if controller.Memory() == nil {
		return runtimeapi.RememberMemoryResult{}, ErrCapabilityUnavailable
	}
	memoryScope, ok := parseWritableMemoryScope(scope)
	note = strings.TrimSpace(note)
	if !ok || note == "" || !utf8.ValidString(note) || len(note) > memoryResearchJSONBytes {
		return runtimeapi.RememberMemoryResult{}, ErrInvalidMemoryInput
	}
	set := controller.Memory()
	expectedPath := filepath.Clean(set.DocPath(memoryScope))
	beforeBody := memoryDocumentBody(set, expectedPath)
	path, err := controller.QuickAdd(memoryScope, note)
	if err != nil {
		return runtimeapi.RememberMemoryResult{}, fmt.Errorf("%w: %v", ErrMemoryMutationFailed, err)
	}
	path = filepath.Clean(path)
	if expectedPath == "." || path != expectedPath {
		return runtimeapi.RememberMemoryResult{}, ErrMemoryMutationFailed
	}
	afterBody := memoryDocumentBody(controller.Memory(), path)
	if afterBody == beforeBody {
		return runtimeapi.RememberMemoryResult{}, ErrMemoryMutationFailed
	}
	memoryID := runtimeapi.MemoryID(s.opaque("memory", "note", string(memoryScope), note, path, beforeBody, afterBody))
	s.facts[memoryID] = memoryFactBinding{
		scope: catalogScopeForMemoryScope(memoryScope), document: path,
		beforeBody: beforeBody, afterBody: afterBody,
	}
	return runtimeapi.RememberMemoryResult{
		MemoryID:          memoryID,
		DisplayPath:       collapsedMemoryPath(string(memoryScope), path, 0),
		InvalidationScope: catalogScopeForMemoryScope(memoryScope),
	}, nil
}

func parseWritableMemoryScope(value string) (memory.Scope, bool) {
	switch memory.Scope(strings.TrimSpace(value)) {
	case memory.ScopeUser:
		return memory.ScopeUser, true
	case memory.ScopeProject:
		return memory.ScopeProject, true
	case memory.ScopeLocal:
		return memory.ScopeLocal, true
	default:
		return "", false
	}
}

func (s *MemoryResearchService) Forget(session runtimeapi.SessionRef, controller MemoryController, id runtimeapi.MemoryID) (runtimeapi.ForgetMemoryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || controller == nil {
		return runtimeapi.ForgetMemoryResult{}, ErrInvalidMemoryInput
	}
	if controller.Memory() == nil {
		return runtimeapi.ForgetMemoryResult{}, ErrCapabilityUnavailable
	}
	binding, ok := s.facts[id]
	if !ok || strings.TrimSpace(string(id)) == "" {
		return runtimeapi.ForgetMemoryResult{}, ErrUnknownMemoryID
	}
	if binding.document != "" {
		currentBody := memoryDocumentBody(controller.Memory(), binding.document)
		if currentBody != binding.afterBody {
			return runtimeapi.ForgetMemoryResult{}, ErrMemoryMutationFailed
		}
		if _, err := controller.SaveDoc(binding.document, binding.beforeBody); err != nil {
			return runtimeapi.ForgetMemoryResult{}, fmt.Errorf("%w: %v", ErrMemoryMutationFailed, err)
		}
	} else if err := controller.ForgetMemory(binding.name); err != nil {
		return runtimeapi.ForgetMemoryResult{}, fmt.Errorf("%w: %v", ErrMemoryMutationFailed, err)
	}
	delete(s.facts, id)
	return runtimeapi.ForgetMemoryResult{Forgotten: true, InvalidationScope: binding.scope}, nil
}

func memoryDocumentBody(set *memory.Set, path string) string {
	if set == nil || strings.TrimSpace(path) == "" {
		return ""
	}
	path = filepath.Clean(path)
	for _, document := range set.Docs {
		if filepath.Clean(document.Path) == path {
			return strings.TrimSpace(document.Body)
		}
	}
	return ""
}

func (s *MemoryResearchService) SaveDocument(session runtimeapi.SessionRef, controller MemoryController, id runtimeapi.DocumentID, body string) (runtimeapi.SaveMemoryDocumentResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || controller == nil || !utf8.ValidString(body) || len(body) > memoryResearchJSONBytes {
		return runtimeapi.SaveMemoryDocumentResult{}, ErrInvalidMemoryInput
	}
	if controller.Memory() == nil {
		return runtimeapi.SaveMemoryDocumentResult{}, ErrCapabilityUnavailable
	}
	binding, ok := s.documents[id]
	if !ok || strings.TrimSpace(string(id)) == "" {
		return runtimeapi.SaveMemoryDocumentResult{}, ErrUnknownDocumentID
	}
	if _, err := controller.SaveDoc(binding.path, body); err != nil {
		return runtimeapi.SaveMemoryDocumentResult{}, fmt.Errorf("%w: %v", ErrMemoryMutationFailed, err)
	}
	return runtimeapi.SaveMemoryDocumentResult{DocumentID: id, Saved: true, InvalidationScope: binding.scope}, nil
}

func (s *MemoryResearchService) Suggestions(session runtimeapi.SessionRef, controller MemoryController) (runtimeapi.MemorySuggestionsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || controller == nil {
		return runtimeapi.MemorySuggestionsView{}, ErrInvalidMemoryInput
	}
	set := controller.Memory()
	if set == nil {
		return runtimeapi.MemorySuggestionsView{}, ErrCapabilityUnavailable
	}
	sessions, err := loadSuggestionSessions(controller.SessionDir(), controller.SessionPath(), suggestionSessionLimit)
	if err != nil {
		return runtimeapi.MemorySuggestionsView{}, ErrQueryFailed
	}
	memoryCandidates := suggestMemories(set, sessions)
	memoryCandidates = append(memoryCandidates, suggestCompilerMemories(controller.WorkspaceRoot(), set, memoryCandidates)...)
	skillCandidates := suggestSkills(controller.WorkspaceRoot(), controller.AllSkills(), sessions)

	revision := suggestionRevision(s.binding, memoryCandidates, skillCandidates)
	view := runtimeapi.MemorySuggestionsView{
		Revision: revision, Available: true,
		Memories: []runtimeapi.MemorySuggestion{}, Skills: []runtimeapi.SkillSuggestion{},
	}
	cache := suggestionCache{
		revision: revision, memories: make(map[runtimeapi.SuggestionID]memorySuggestionCandidate),
		skills: make(map[runtimeapi.SuggestionID]skillSuggestionCandidate),
	}
	for index, candidate := range memoryCandidates {
		raw, _ := json.Marshal(candidate)
		id := runtimeapi.SuggestionID(s.opaque("suggestion", "memory", string(revision), fmt.Sprint(index), string(raw)))
		cache.memories[id] = cloneMemorySuggestionCandidate(candidate)
		body := candidate.Body
		view.Memories = append(view.Memories, runtimeapi.MemorySuggestion{
			SuggestionID: id, Name: candidate.Name, Title: candidate.Title,
			Description: candidate.Description, Type: string(candidate.Type), Body: &body,
			Reason: candidate.Reason, Evidence: append([]string(nil), candidate.Evidence...),
		})
	}
	for index, candidate := range skillCandidates {
		raw, _ := json.Marshal(candidate)
		id := runtimeapi.SuggestionID(s.opaque("suggestion", "skill", string(revision), fmt.Sprint(index), string(raw)))
		cache.skills[id] = cloneSkillSuggestionCandidate(candidate)
		body := candidate.Body
		view.Skills = append(view.Skills, runtimeapi.SkillSuggestion{
			SuggestionID: id, Name: candidate.Name, Description: candidate.Description,
			Scope: string(candidate.Scope), Body: &body, Reason: candidate.Reason,
			Evidence: append([]string(nil), candidate.Evidence...),
		})
	}
	if !withinMemoryResearchBudget(view) {
		return runtimeapi.MemorySuggestionsView{}, ErrQueryFailed
	}
	s.suggestions = cache
	return view, nil
}

func suggestionRevision(binding RuntimeBinding, memories []memorySuggestionCandidate, skills []skillSuggestionCandidate) runtimeapi.CatalogRevision {
	raw, _ := json.Marshal(struct {
		Memories []memorySuggestionCandidate `json:"memories"`
		Skills   []skillSuggestionCandidate  `json:"skills"`
	}{memories, skills})
	sum := sha256.Sum256(append([]byte(sessionBinding(binding.Session)+"\x00"+binding.Incarnation+"\x00"), raw...))
	return runtimeapi.CatalogRevision("suggestions_" + base64.RawURLEncoding.EncodeToString(sum[:]))
}

func cloneMemorySuggestionCandidate(in memorySuggestionCandidate) memorySuggestionCandidate {
	in.Evidence = append([]string(nil), in.Evidence...)
	return in
}

func cloneSkillSuggestionCandidate(in skillSuggestionCandidate) skillSuggestionCandidate {
	in.Evidence = append([]string(nil), in.Evidence...)
	return in
}

func (s *MemoryResearchService) AcceptMemorySuggestion(session runtimeapi.SessionRef, controller MemoryController, id runtimeapi.SuggestionID, revision runtimeapi.CatalogRevision) (runtimeapi.AcceptMemorySuggestionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || controller == nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, ErrInvalidMemoryInput
	}
	if controller.Memory() == nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, ErrCapabilityUnavailable
	}
	if revision == "" || revision != s.suggestions.revision {
		return runtimeapi.AcceptMemorySuggestionResult{}, ErrStaleRevision
	}
	if current, err := s.currentSuggestionRevision(controller); err != nil || current == "" || current != revision {
		s.suggestions = suggestionCache{}
		return runtimeapi.AcceptMemorySuggestionResult{}, ErrStaleRevision
	}
	candidate, ok := s.suggestions.memories[id]
	if !ok {
		return runtimeapi.AcceptMemorySuggestionResult{}, ErrUnknownSuggestionID
	}
	name := acceptedSuggestionName(candidate.Name, candidate.Description)
	if _, err := controller.SaveMemory(memory.Memory{
		Name: name, Title: oneLine(candidate.Title), Description: oneLine(candidate.Description),
		Type: candidate.Type, Body: strings.TrimSpace(candidate.Body),
	}); err != nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, fmt.Errorf("%w: %v", ErrMemoryMutationFailed, err)
	}
	memoryID := runtimeapi.MemoryID(s.opaque("memory", "active", name))
	scope := catalogScopeForMemoryType(candidate.Type)
	s.facts[memoryID] = memoryFactBinding{name: name, scope: scope}
	s.suggestions = suggestionCache{}
	return runtimeapi.AcceptMemorySuggestionResult{MemoryID: memoryID, InvalidationScope: scope}, nil
}

func (s *MemoryResearchService) AcceptSkillSuggestion(session runtimeapi.SessionRef, controller MemoryController, id runtimeapi.SuggestionID, revision runtimeapi.CatalogRevision) (runtimeapi.AcceptSkillSuggestionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || controller == nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, ErrInvalidMemoryInput
	}
	if controller.Memory() == nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, ErrCapabilityUnavailable
	}
	if revision == "" || revision != s.suggestions.revision {
		return runtimeapi.AcceptSkillSuggestionResult{}, ErrStaleRevision
	}
	if current, err := s.currentSuggestionRevision(controller); err != nil || current == "" || current != revision {
		s.suggestions = suggestionCache{}
		return runtimeapi.AcceptSkillSuggestionResult{}, ErrStaleRevision
	}
	candidate, ok := s.suggestions.skills[id]
	if !ok {
		return runtimeapi.AcceptSkillSuggestionResult{}, ErrUnknownSuggestionID
	}
	content := skill.RenderSkillFile(skill.SkillFileOptions{
		Name: candidate.Name, Description: oneLine(candidate.Description), Body: strings.TrimSpace(candidate.Body),
	})
	if _, err := controller.CreateSkill(candidate.Name, candidate.Scope, content); err != nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, fmt.Errorf("%w: %v", ErrMemoryMutationFailed, err)
	}
	s.suggestions = suggestionCache{}
	return runtimeapi.AcceptSkillSuggestionResult{
		SkillID:           runtimeapi.SkillID(s.opaque("skill", candidate.Name, string(candidate.Scope))),
		InvalidationScope: catalogScopeForSkillScope(candidate.Scope),
	}, nil
}

func (s *MemoryResearchService) currentSuggestionRevision(controller MemoryController) (runtimeapi.CatalogRevision, error) {
	if controller == nil || controller.Memory() == nil {
		return "", ErrCapabilityUnavailable
	}
	sessions, err := loadSuggestionSessions(controller.SessionDir(), controller.SessionPath(), suggestionSessionLimit)
	if err != nil {
		return "", err
	}
	memories := suggestMemories(controller.Memory(), sessions)
	memories = append(memories, suggestCompilerMemories(controller.WorkspaceRoot(), controller.Memory(), memories)...)
	skills := suggestSkills(controller.WorkspaceRoot(), controller.AllSkills(), sessions)
	return suggestionRevision(s.binding, memories, skills), nil
}

func catalogScopeForMemoryScope(scope memory.Scope) runtimeapi.CatalogScope {
	if scope == memory.ScopeUser {
		return runtimeapi.CatalogHost
	}
	return runtimeapi.CatalogWorkspace
}

func catalogScopeForMemoryType(value memory.Type) runtimeapi.CatalogScope {
	if value == memory.TypeUser || value == memory.TypeFeedback {
		return runtimeapi.CatalogHost
	}
	return runtimeapi.CatalogWorkspace
}

func catalogScopeForSkillScope(scope skill.Scope) runtimeapi.CatalogScope {
	if scope == skill.ScopeGlobal {
		return runtimeapi.CatalogHost
	}
	return runtimeapi.CatalogWorkspace
}

// ResearchStatus projects one explicit current task without exposing TaskPath.
func (s *MemoryResearchService) ResearchStatus(session runtimeapi.SessionRef, summary *autoresearch.Summary) (runtimeapi.ResearchStatusView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) {
		return runtimeapi.ResearchStatusView{}, ErrInvalidResearchInput
	}
	if summary == nil {
		return runtimeapi.ResearchStatusView{Available: true}, nil
	}
	projected, err := s.projectResearchTask(*summary)
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	result := runtimeapi.ResearchStatusView{Available: true, Task: &projected}
	if !withinMemoryResearchBudget(result) {
		return runtimeapi.ResearchStatusView{}, ErrQueryFailed
	}
	return result, nil
}

func (s *MemoryResearchService) ResearchList(session runtimeapi.SessionRef, summaries []autoresearch.Summary, cursor runtimeapi.Cursor, limit int) (runtimeapi.ResearchPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) {
		return runtimeapi.ResearchPage{}, ErrInvalidResearchInput
	}
	if len(summaries) > memoryResearchSourceItems {
		return runtimeapi.ResearchPage{}, ErrQueryFailed
	}
	pageLimit, err := normalizedPageLimit(limit)
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	ordered := append([]autoresearch.Summary(nil), summaries...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].TaskID > ordered[j].TaskID })
	items := make([]runtimeapi.ResearchTask, 0, len(ordered))
	for _, summary := range ordered {
		item, err := s.projectResearchTask(summary)
		if err != nil {
			return runtimeapi.ResearchPage{}, err
		}
		items = append(items, item)
	}
	revision := snapshotRevision(items, sessionBinding(s.binding.Session), s.binding.Incarnation, "research/list")
	offset, err := s.researchCursorOffset(cursor, "research/list", "", revision, len(items))
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	end := offset + pageLimit
	if end > len(items) {
		end = len(items)
	}
	result := runtimeapi.ResearchPage{Items: append([]runtimeapi.ResearchTask(nil), items[offset:end]...)}
	if end < len(items) {
		result.HasMore = true
		result.Next = s.encodeResearchCursor(researchCursorPayload{
			Method: "research/list", Revision: revision, Offset: end,
		})
	}
	if !withinMemoryResearchBudget(result) {
		return runtimeapi.ResearchPage{}, ErrQueryFailed
	}
	return result, nil
}

func (s *MemoryResearchService) ResearchFindings(session runtimeapi.SessionRef, taskID runtimeapi.ResearchTaskID, findings []autoresearch.Finding, cursor runtimeapi.Cursor, limit int) (runtimeapi.ResearchFindingsPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || s.researchTasks[taskID] == "" {
		return runtimeapi.ResearchFindingsPage{}, ErrInvalidResearchInput
	}
	if len(findings) > memoryResearchSourceItems {
		return runtimeapi.ResearchFindingsPage{}, ErrQueryFailed
	}
	pageLimit, err := normalizedPageLimit(limit)
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	items := make([]runtimeapi.ResearchFinding, 0, len(findings))
	for _, finding := range findings {
		item, err := s.projectResearchFinding(finding)
		if err != nil {
			return runtimeapi.ResearchFindingsPage{}, err
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
	revision := snapshotRevision(items, sessionBinding(s.binding.Session), s.binding.Incarnation, "research/findings", string(taskID))
	offset, err := s.researchCursorOffset(cursor, "research/findings", string(taskID), revision, len(items))
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	end := offset + pageLimit
	if end > len(items) {
		end = len(items)
	}
	result := runtimeapi.ResearchFindingsPage{Items: append([]runtimeapi.ResearchFinding(nil), items[offset:end]...)}
	if end < len(items) {
		result.HasMore = true
		result.Next = s.encodeResearchCursor(researchCursorPayload{
			Method: "research/findings", TaskID: string(taskID), Revision: revision, Offset: end,
		})
	}
	if !withinMemoryResearchBudget(result) {
		return runtimeapi.ResearchFindingsPage{}, ErrQueryFailed
	}
	return result, nil
}

func (s *MemoryResearchService) projectResearchTask(summary autoresearch.Summary) (runtimeapi.ResearchTask, error) {
	if !safeResearchTaskID(summary.TaskID) || !validResearchStatus(summary.Status) || summary.Iteration < 0 ||
		summary.StaleCount < 0 || summary.PivotCount < 0 || summary.FindingCount < 0 {
		return runtimeapi.ResearchTask{}, ErrQueryFailed
	}
	issuedTaskID := runtimeapi.ResearchTaskID(s.opaque("research-task", summary.TaskID))
	item := runtimeapi.ResearchTask{
		TaskID: issuedTaskID, Status: summary.Status,
		Iteration: summary.Iteration, StaleCount: summary.StaleCount, PivotCount: summary.PivotCount,
		PivotRequired: summary.PivotRequired, FindingCount: summary.FindingCount,
		OpenCriteria: []runtimeapi.ResearchCriterion{},
	}
	s.researchTasks[issuedTaskID] = summary.TaskID
	item.Goal = optionalText(summary.Goal)
	item.CurrentDirection = optionalText(summary.CurrentDirection)
	item.Blocker = optionalText(summary.Blocker)
	item.NextRequiredAction = optionalText(summary.NextRequiredAction)
	if !summary.LastHeartbeatAt.IsZero() {
		item.LastHeartbeatAt = summary.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}
	for _, criterion := range summary.OpenCriteria {
		if !safeOpaqueText(criterion.ID, 256) || strings.ContainsAny(criterion.ID, `/\\`) ||
			strings.TrimSpace(criterion.Description) == "" || !utf8.ValidString(criterion.Description) ||
			criterion.EvidenceCount < 0 || strings.TrimSpace(criterion.Status) == "" {
			return runtimeapi.ResearchTask{}, ErrQueryFailed
		}
		issuedCriterionID := runtimeapi.CriterionID(s.opaque("research-criterion", summary.TaskID, criterion.ID))
		s.researchCriteria[issuedCriterionID] = researchCriterionBinding{taskID: summary.TaskID, criterionID: criterion.ID}
		item.OpenCriteria = append(item.OpenCriteria, runtimeapi.ResearchCriterion{
			CriterionID: issuedCriterionID, Description: criterion.Description,
			Required: criterion.Required, EvidenceCount: criterion.EvidenceCount, Status: criterion.Status,
		})
	}
	return item, nil
}

func (s *MemoryResearchService) projectResearchFinding(finding autoresearch.Finding) (runtimeapi.ResearchFinding, error) {
	if !safeOpaqueText(finding.ID, 256) || strings.ContainsAny(finding.ID, `/\\`) || !validFindingKind(finding.Kind) ||
		!validFindingSource(finding.Source) || finding.CreatedAt.IsZero() {
		return runtimeapi.ResearchFinding{}, ErrQueryFailed
	}
	paths := make([]string, 0, len(finding.Paths))
	for _, raw := range finding.Paths {
		if projected, ok := primaryRelativeDisplayPath(s.workspaceRoot, raw); ok {
			paths = append(paths, projected)
		}
		// Unsafe legacy paths are omitted, never returned verbatim.
	}
	command := safeDisplayCommand(finding.Command, s.workspaceRoot)
	return runtimeapi.ResearchFinding{
		ID: finding.ID, Kind: finding.Kind, Summary: optionalText(finding.Summary), Source: finding.Source,
		Command: command, Paths: paths, Accepted: finding.Accepted,
		CreatedAt: finding.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// PrepareResearchEvidence validates all client-controlled fields and collapses
// every path to a primary-relative representation before Controller persistence.
func (s *MemoryResearchService) PrepareResearchEvidence(session runtimeapi.SessionRef, taskID runtimeapi.ResearchTaskID, criterionID runtimeapi.CriterionID, input runtimeapi.ResearchEvidence, now time.Time) (autoresearch.Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) || s.researchTasks[taskID] == "" || s.researchCriteria[criterionID].taskID == "" ||
		!safeOpaqueText(input.ID, 256) || strings.ContainsAny(input.ID, `/\\`) || !validFindingKind(input.Kind) ||
		strings.TrimSpace(input.Summary) == "" || !utf8.ValidString(input.Summary) ||
		!validFindingSource(input.Source) || !utf8.ValidString(input.Command) || !withinMemoryResearchBudget(input) {
		return autoresearch.Finding{}, ErrInvalidResearchInput
	}
	if s.researchCriteria[criterionID].taskID != s.researchTasks[taskID] {
		return autoresearch.Finding{}, ErrInvalidResearchInput
	}
	paths := make([]string, 0, len(input.Paths))
	for _, raw := range input.Paths {
		projected, ok := primaryRelativeDisplayPath(s.workspaceRoot, raw)
		if !ok {
			return autoresearch.Finding{}, ErrInvalidResearchInput
		}
		paths = append(paths, projected)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return autoresearch.Finding{
		ID: input.ID, Kind: input.Kind, Summary: strings.TrimSpace(input.Summary), Source: strings.TrimSpace(input.Source),
		Command: strings.TrimSpace(input.Command), Paths: paths, Accepted: input.Accepted, CreatedAt: now.UTC(),
	}, nil
}

// ResolveResearchTask converts a previously issued, runtime-bound wire ID into
// the Host store identity. Unseen or cross-runtime IDs are never forwarded to
// the Controller.
func (s *MemoryResearchService) ResolveResearchTask(session runtimeapi.SessionRef, taskID runtimeapi.ResearchTaskID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) {
		return "", ErrInvalidResearchInput
	}
	resolved := s.researchTasks[taskID]
	if resolved == "" {
		return "", ErrInvalidResearchInput
	}
	return resolved, nil
}

// ResolveResearchEvidenceTarget resolves an issued task/criterion pair and
// enforces that the criterion was issued for that task.
func (s *MemoryResearchService) ResolveResearchEvidenceTarget(session runtimeapi.SessionRef, taskID runtimeapi.ResearchTaskID, criterionID runtimeapi.CriterionID) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatches(session) {
		return "", "", ErrInvalidResearchInput
	}
	resolvedTask := s.researchTasks[taskID]
	binding := s.researchCriteria[criterionID]
	if resolvedTask == "" || binding.taskID == "" || binding.taskID != resolvedTask {
		return "", "", ErrInvalidResearchInput
	}
	return resolvedTask, binding.criterionID, nil
}

func optionalText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	copyValue := value
	return &copyValue
}

func withinMemoryResearchBudget(value any) bool {
	raw, err := json.Marshal(value)
	return err == nil && len(raw) <= memoryResearchJSONBytes
}

var researchTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func safeResearchTaskID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 256 &&
		researchTaskIDPattern.MatchString(value) && !strings.Contains(value, "..") && !strings.ContainsAny(value, `/\\`)
}

// ValidResearchTaskID lets adapters reject traversal-like task identities
// before calling a Controller/store. The task remains an explicit domain ID;
// this function performs no filesystem access and reveals no Host path.
func ValidResearchTaskID(value string) bool { return safeResearchTaskID(value) }

func safeOpaqueText(value string, max int) bool {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > max {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validResearchStatus(value string) bool {
	switch value {
	case autoresearch.StatusRunning, autoresearch.StatusBlocked, autoresearch.StatusComplete, autoresearch.StatusStopped, autoresearch.StatusInvalid:
		return true
	default:
		return false
	}
}

func validFindingKind(value string) bool {
	switch value {
	case autoresearch.FindingKindCommand, autoresearch.FindingKindFile, autoresearch.FindingKindTest,
		autoresearch.FindingKindBenchmark, autoresearch.FindingKindManual, autoresearch.FindingKindReview:
		return true
	default:
		return false
	}
}

func validFindingSource(value string) bool {
	switch value {
	case autoresearch.FindingSourceCommand, autoresearch.FindingSourceFile, autoresearch.FindingSourceManual:
		return true
	default:
		return false
	}
}

func primaryRelativeDisplayPath(root, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "~") || containsControl(raw) {
		return "", false
	}
	var relative string
	if filepath.IsAbs(raw) {
		value, err := filepath.Rel(root, filepath.Clean(raw))
		if err != nil {
			return "", false
		}
		relative = value
	} else {
		// On a non-Windows Host, filepath.IsAbs does not recognize a Windows
		// drive path. Relative paths must also stay slash-normalized so a
		// backslash or colon cannot acquire platform-dependent semantics.
		if windowsAbsolutePathPattern.MatchString(raw) || strings.ContainsAny(raw, `\:`) {
			return "", false
		}
		relative = filepath.Clean(filepath.FromSlash(raw))
	}
	if relative == "." || !filepath.IsLocal(relative) || filepath.IsAbs(relative) ||
		relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	projected := filepath.ToSlash(relative)
	if strings.ContainsAny(projected, `\:`) || containsControl(projected) {
		return "", false
	}
	return projected, true
}

func safeDisplayCommand(command, root string) string {
	command = strings.TrimSpace(command)
	if command == "" || !utf8.ValidString(command) || len(command) > 16<<10 || containsControl(command) ||
		strings.ContainsAny(command, "<>|&;$`'\"") {
		return ""
	}
	root = filepath.Clean(root)
	fields := strings.Fields(command)
	for index, field := range fields {
		candidate := strings.Trim(field, `"'(),;[]{}:`)
		if !filepath.IsAbs(candidate) {
			continue
		}
		relative, err := filepath.Rel(root, filepath.Clean(candidate))
		if err == nil && relative == "." {
			fields[index] = strings.Replace(field, candidate, ".", 1)
		}
	}
	command = strings.Join(fields, " ")
	// The only accepted Windows separator is the exact workspace-root token
	// collapsed above. Every other backslash remains ambiguous or external.
	if strings.Contains(command, `\`) {
		return ""
	}
	fields = strings.Fields(command)
	for index, field := range fields {
		candidate := strings.Trim(field, `"'(),;[]{}:`)
		if candidate == "" || filepath.IsAbs(candidate) || strings.HasPrefix(candidate, "/") || windowsAbsolutePathPattern.MatchString(candidate) ||
			strings.ContainsAny(candidate, `\:`) || strings.HasPrefix(candidate, "~") || containsControl(candidate) {
			return ""
		}
		if index == 0 && strings.Contains(candidate, "=") && !strings.HasPrefix(candidate, "-") {
			return ""
		}
		for _, fragment := range strings.Split(candidate, "=") {
			if filepath.IsAbs(fragment) || strings.HasPrefix(fragment, "/") || windowsAbsolutePathPattern.MatchString(fragment) || strings.HasPrefix(fragment, "~") {
				return ""
			}
		}
	}
	return command
}

var windowsAbsolutePathPattern = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

type researchCursorPayload struct {
	Version     int    `json:"v"`
	Target      string `json:"t"`
	Incarnation string `json:"i"`
	Method      string `json:"m"`
	TaskID      string `json:"k,omitempty"`
	Revision    string `json:"r"`
	Offset      int    `json:"o"`
}

func (s *MemoryResearchService) encodeResearchCursor(payload researchCursorPayload) runtimeapi.Cursor {
	payload.Version = cursorVersion
	payload.Target = sessionBinding(s.binding.Session)
	payload.Incarnation = s.binding.Incarnation
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write(raw)
	return runtimeapi.Cursor(base64.RawURLEncoding.EncodeToString(append(raw, mac.Sum(nil)...)))
}

func (s *MemoryResearchService) researchCursorOffset(cursor runtimeapi.Cursor, method, taskID, revision string, length int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(string(cursor))
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != string(cursor) {
		return 0, ErrInvalidCursor
	}
	message, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write(message)
	var payload researchCursorPayload
	if err := json.Unmarshal(message, &payload); err != nil || payload.Version != cursorVersion || payload.Offset < 0 {
		return 0, ErrInvalidCursor
	}
	if !hmac.Equal(signature, mac.Sum(nil)) || payload.Target != sessionBinding(s.binding.Session) ||
		payload.Incarnation != s.binding.Incarnation || payload.Method != method || payload.TaskID != taskID {
		return 0, ErrStaleCursor
	}
	if payload.Revision != revision || payload.Offset > length {
		return 0, ErrStaleCursor
	}
	return payload.Offset, nil
}

type suggestionSession struct {
	ID       string
	Messages []provider.Message
}

type workflowCategory struct {
	Name        string
	Description string
	Reason      string
	Keywords    []string
	Steps       []string
}

func loadSuggestionSessions(sessionDir, sessionPath string, limit int) ([]suggestionSession, error) {
	dir := strings.TrimSpace(sessionDir)
	if dir == "" && strings.TrimSpace(sessionPath) != "" {
		dir = filepath.Dir(sessionPath)
	}
	if dir == "" || limit <= 0 {
		return nil, nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries, readErr := directory.ReadDir(memoryResearchSourceItems + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > memoryResearchSourceItems {
		return nil, ErrQueryFailed
	}
	infos, err := agent.ListSessions(dir)
	if err != nil {
		return nil, err
	}
	out := make([]suggestionSession, 0, min(limit, len(infos)))
	for _, info := range infos {
		if len(out) >= limit {
			break
		}
		stat, err := os.Stat(info.Path)
		if err != nil || stat.Size() < 0 || stat.Size() > memoryResearchJSONBytes {
			continue
		}
		loaded, err := agent.LoadSession(info.Path)
		if err != nil {
			continue
		}
		out = append(out, suggestionSession{
			ID: fmt.Sprintf("recent-session-%d", len(out)+1), Messages: loaded.Snapshot(),
		})
	}
	return out, nil
}

func suggestMemories(set *memory.Set, sessions []suggestionSession) []memorySuggestionCandidate {
	if set == nil || len(sessions) == 0 {
		return []memorySuggestionCandidate{}
	}
	existing := existingMemoryText(set)
	seen := map[string]bool{}
	var out []memorySuggestionCandidate
	for _, session := range sessions {
		for _, message := range session.Messages {
			if message.Role != provider.RoleUser {
				continue
			}
			statement, reason := extractMemoryStatement(message.Content)
			if statement == "" {
				continue
			}
			key := normalizeSuggestionKey(statement)
			if key == "" || seen[key] || existingCovers(existing, key) {
				continue
			}
			seen[key] = true
			name := stableSuggestionName(statement, "memory-candidate")
			out = append(out, memorySuggestionCandidate{
				Name: name, Title: suggestionTitle(statement, "Memory candidate"),
				Description: oneLine(statement), Type: inferMemoryType(statement),
				Body: memoryCandidateBody(statement, reason, session), Reason: reason,
				Evidence: []string{sessionEvidence(session, statement)},
			})
			if len(out) >= memorySuggestionLimit {
				return out
			}
		}
	}
	return out
}

func suggestCompilerMemories(workspaceRoot string, set *memory.Set, already []memorySuggestionCandidate) []memorySuggestionCandidate {
	runtime := memorycompiler.New(config.MemoryCompilerDir(workspaceRoot))
	if runtime == nil || set == nil {
		return nil
	}
	existing := existingMemoryText(set)
	seen := map[string]bool{}
	for _, suggestion := range already {
		seen[normalizeSuggestionKey(suggestion.Description)] = true
	}
	var out []memorySuggestionCandidate
	for _, pattern := range runtime.StableNoisePatterns(compilerSuggestionMinCount, compilerSuggestionLimit*2) {
		statement := compilerPatternStatement(pattern.Pattern)
		key := normalizeSuggestionKey(statement)
		if key == "" || seen[key] || existingCovers(existing, key) {
			continue
		}
		seen[key] = true
		name := compilerCandidateName(pattern.Pattern)
		out = append(out, memorySuggestionCandidate{
			Name: name, Title: suggestionTitle(statement, "Memory v5 pattern"), Description: oneLine(statement),
			Type: memory.TypeProject, Body: compilerCandidateBody(statement, pattern.Count),
			Reason:   fmt.Sprintf("Memory v5 observed this failure pattern in %d separate turns", pattern.Count),
			Evidence: []string{fmt.Sprintf("memory-v5 execution traces: %s (x%d)", truncateRunes(pattern.Pattern, 160), pattern.Count)},
		})
		if len(out) >= compilerSuggestionLimit {
			break
		}
	}
	return out
}

func suggestSkills(workspaceRoot string, existing []skill.Skill, sessions []suggestionSession) []skillSuggestionCandidate {
	if len(sessions) == 0 {
		return []skillSuggestionCandidate{}
	}
	existingNames := map[string]bool{}
	for _, existingSkill := range existing {
		existingNames[config.SkillNameKey(existingSkill.Name)] = true
	}
	scope := skill.ScopeProject
	if strings.TrimSpace(workspaceRoot) == "" {
		scope = skill.ScopeGlobal
	}
	var out []skillSuggestionCandidate
	for _, category := range workflowCategories() {
		if existingNames[config.SkillNameKey(category.Name)] {
			continue
		}
		evidence := workflowEvidence(category, sessions)
		if len(evidence) < 2 {
			continue
		}
		out = append(out, skillSuggestionCandidate{
			Name: category.Name, Description: category.Description, Scope: scope,
			Body: skillCandidateBody(category, evidence), Reason: category.Reason, Evidence: evidence,
		})
	}
	return out
}

func existingMemoryText(set *memory.Set) []string {
	var out []string
	for _, document := range set.Docs {
		out = append(out, normalizeSuggestionKey(document.Body))
	}
	for _, fact := range set.Store.List() {
		out = append(out, normalizeSuggestionKey(strings.Join([]string{fact.Name, fact.Title, fact.Description, fact.Body}, " ")))
	}
	return out
}

func existingCovers(existing []string, key string) bool {
	if key == "" {
		return true
	}
	for _, text := range existing {
		if text != "" && (strings.Contains(text, key) || strings.Contains(key, text)) {
			return true
		}
	}
	return false
}

func extractMemoryStatement(content string) (string, string) {
	text := oneLine(content)
	if len([]rune(text)) < 8 || len([]rune(text)) > 420 {
		return "", ""
	}
	lower := strings.ToLower(text)
	markers := []struct{ value, reason string }{
		{"记住", "explicit remember request"}, {"以后", "future-facing preference"},
		{"始终", "persistent working rule"}, {"总是", "persistent working rule"},
		{"每次", "repeated workflow preference"}, {"默认", "default behavior preference"},
		{"不要", "negative working preference"}, {"偏好", "user preference"},
		{"规则", "durable rule"}, {"约定", "project convention"},
		{"remember", "explicit remember request"}, {"always", "persistent working rule"},
		{"never", "negative working preference"}, {"prefer", "user preference"},
		{"preference", "user preference"}, {"by default", "default behavior preference"},
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker.value) {
			return trimMemoryLead(text, marker.value), marker.reason
		}
	}
	return "", ""
}

func trimMemoryLead(text, marker string) string {
	index := strings.Index(strings.ToLower(text), marker)
	if index < 0 {
		return text
	}
	trimmed := strings.TrimSpace(text[index:])
	for _, separator := range []string{"：", ":", "-", "—"} {
		trimmed = strings.TrimPrefix(trimmed, marker+separator)
	}
	return strings.TrimSpace(trimmed)
}

func inferMemoryType(statement string) memory.Type {
	lower := strings.ToLower(statement)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "github.com/") {
		return memory.TypeReference
	}
	if hasAny(lower, "反馈", "回复", "回答", "不要", "always", "never", "始终", "总是") {
		return memory.TypeFeedback
	}
	if hasAny(lower, "项目", "分支", "pr", "pull request", "仓库", "repo", "约定") {
		return memory.TypeProject
	}
	return memory.TypeUser
}

func memoryCandidateBody(statement, reason string, session suggestionSession) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(statement))
	builder.WriteString("\n\n**Why:** Suggested from recent local history")
	if reason != "" {
		builder.WriteString(" (" + reason + ")")
	}
	builder.WriteString(".\n**How to apply:** Treat this as durable guidance only after the user confirms it still applies.\n")
	if session.ID != "" {
		builder.WriteString("\nEvidence: [" + session.ID + "] " + truncateRunes(statement, 180))
	}
	return builder.String()
}

func workflowCategories() []workflowCategory {
	return []workflowCategory{
		{Name: "reasonix-pr-followup", Description: "Review or update a Reasonix GitHub PR, address feedback, verify, and publish safely.",
			Reason:   "recent history repeatedly touched PR review, bot feedback, commits, or GitHub publication",
			Keywords: []string{"pr", "pull request", "github", "review", "机器人", "评审", "提交到pr", "更新pr", "code rabbit", "coderabbit"},
			Steps:    []string{"Fetch the live PR state and confirm branch, base, head SHA, and review status.", "Inspect the real diff and related implementation before changing code.", "Fix only actionable feedback, run focused verification, and keep cache-sensitive surfaces stable.", "Stage intended files, commit with an English behavior-focused message, push to the verified PR head, and update the PR."}},
		{Name: "reasonix-memory-ui", Description: "Iterate on the Reasonix desktop Memory page with source-backed UI decisions and browser verification.",
			Reason:   "recent history repeatedly discussed Memory page layout, labels, filters, and interaction details",
			Keywords: []string{"memory", "记忆", "设置-记忆", "memory panel", "指令文件", "归档", "全局", "项目", "添加记忆"},
			Steps:    []string{"Identify the active Memory settings component and current browser-rendered state before editing.", "Keep active memories, archived memories, instruction files, and suggestions visually distinct.", "Use neutral secondary actions and confirmation for persistent writes or archive operations.", "Run frontend checks and verify the affected Memory page in the in-app browser."}},
		{Name: "desktop-ui-iteration", Description: "Apply focused desktop UI layout feedback, preserve existing design tokens, and verify in browser.",
			Reason:   "recent history repeatedly involved screenshot-driven desktop UI layout and interaction feedback",
			Keywords: []string{"ui", "布局", "设计", "交互", "红框", "页面", "按钮", "浏览器", "frontend", "desktop"},
			Steps:    []string{"Map the screenshot target to the exact component, selector, and state in source.", "Patch the smallest component and CSS surface using existing settings/page recipes.", "Check responsive behavior and text overflow for the changed controls.", "Verify with the running local UI instead of relying only on code inspection."}},
	}
}

func workflowEvidence(category workflowCategory, sessions []suggestionSession) []string {
	seen := map[string]bool{}
	var evidence []string
	for _, session := range sessions {
		for _, message := range session.Messages {
			if message.Role != provider.RoleUser {
				continue
			}
			text := oneLine(message.Content)
			if text == "" || !hasAny(strings.ToLower(text), category.Keywords...) || seen[session.ID] {
				continue
			}
			seen[session.ID] = true
			evidence = append(evidence, sessionEvidence(session, text))
			break
		}
	}
	if len(evidence) > 4 {
		return evidence[:4]
	}
	return evidence
}

func skillCandidateBody(category workflowCategory, evidence []string) string {
	var builder strings.Builder
	title := strings.TrimPrefix(strings.ReplaceAll(category.Name, "-", " "), "reasonix ")
	builder.WriteString("# " + cases.Title(language.Und).String(title) + "\n\n")
	builder.WriteString("Use this skill when the user asks for this repeated Reasonix workflow.\n\n## Evidence\n\n")
	for _, item := range evidence {
		builder.WriteString("- " + item + "\n")
	}
	builder.WriteString("\n## Workflow\n\n")
	for index, step := range category.Steps {
		fmt.Fprintf(&builder, "%d. %s\n", index+1, step)
	}
	builder.WriteString("\n## Stop Condition\n\nFinish only after the requested change is implemented, verified, and any requested PR or UI update is delivered.\n")
	return builder.String()
}

func acceptedSuggestionName(given, description string) string {
	if isWellFormedSlug(given) {
		return given
	}
	return suggestionName("", description, "memory-candidate")
}

func isWellFormedSlug(value string) bool {
	if value == "" || len(value) > 128 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	previousDash := false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			previousDash = false
		case character == '-':
			if previousDash {
				return false
			}
			previousDash = true
		default:
			return false
		}
	}
	return true
}

func stableSuggestionName(source, fallback string) string {
	slug := asciiSlug(source)
	if slug != "" && len(slug) < 56 {
		return slug
	}
	base := suggestionName("", source, fallback)
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(source))
	return fmt.Sprintf("%s-%08x", base, hash.Sum32())
}

func suggestionName(given, source, fallback string) string {
	for _, value := range []string{given, source, fallback} {
		if name := asciiSlug(value); name != "" {
			return name
		}
	}
	return "candidate"
}

func asciiSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
			lastDash = false
		case character == '-' || character == '_' || character == '.' || unicode.IsSpace(character):
			if builder.Len() > 0 && !lastDash {
				builder.WriteRune('-')
				lastDash = true
			}
		}
		if builder.Len() >= 56 {
			break
		}
	}
	return strings.Trim(builder.String(), "-")
}

func compilerCandidateName(pattern string) string {
	base := suggestionName("", "memory-v5 "+pattern, "memory-v5-pattern")
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(pattern))
	return fmt.Sprintf("%s-%08x", base, hash.Sum32())
}

func compilerPatternStatement(pattern string) string {
	pattern = oneLine(pattern)
	if pattern == "" {
		return ""
	}
	return "Known repeated failure in this workspace: " + pattern + "."
}

func compilerCandidateBody(statement string, count int) string {
	return statement + "\n\n" + fmt.Sprintf("**Why:** Memory v5 recorded this failure pattern in %d separate execution traces for this workspace.\n", count) +
		"**How to apply:** Address the known cause before retrying the same command; drop this memory once the failure stops reproducing.\n"
}

func suggestionTitle(value, fallback string) string {
	title := truncateRunes(oneLine(value), 64)
	if title == "" {
		return fallback
	}
	return title
}

func sessionEvidence(session suggestionSession, text string) string {
	return session.ID + ": " + truncateRunes(oneLine(text), 160)
}

func normalizeSuggestionKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
func oneLine(value string) string { return strings.Join(strings.Fields(value), " ") }

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit-1]) + "..."
}

func hasAny(haystack string, needles ...string) bool {
	haystack = strings.ToLower(haystack)
	for _, needle := range needles {
		if strings.Contains(haystack, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
