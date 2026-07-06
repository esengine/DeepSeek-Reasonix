package history

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/retrieval"
	"reasonix/internal/store"
)

// Kind identifies the part of a saved message indexed for retrieval.
type Kind string

const (
	KindUserText      Kind = "user_text"
	KindAssistantText Kind = "assistant_text"
	KindToolInput     Kind = "tool_input"
	KindToolError     Kind = "tool_error"
	KindToolOutput    Kind = "tool_output"
)

const (
	scopeProject = "project"
	scopeGlobal  = "global"

	defaultLimit  = 8
	maxLimit      = 20
	defaultAround = 3
	maxAround     = 10
	maxSnippet    = 240
	scoreFloor    = 0.15
)

var defaultKinds = map[Kind]bool{
	KindUserText:      true,
	KindAssistantText: true,
	KindToolInput:     true,
	KindToolError:     true,
}

// Options binds a Searcher to the session/history roots it may read.
type Options struct {
	// SessionDir is the current controller's session directory. In desktop this
	// is usually project-scoped; in CLI it is often the user-global session dir.
	SessionDir string
	// GlobalSessionDir is the user-global session directory. It is searched only
	// when the caller asks for global scope, and may equal SessionDir.
	GlobalSessionDir string
	ArchiveDir       string
}

// Searcher performs lightweight BM25 retrieval over saved session JSONL files.
type Searcher struct {
	sessionDir       string
	globalSessionDir string
	archiveDir       string
}

// NewSearcher returns a searcher confined to the supplied directories.
func NewSearcher(opts Options) *Searcher {
	return &Searcher{
		sessionDir:       strings.TrimSpace(opts.SessionDir),
		globalSessionDir: strings.TrimSpace(opts.GlobalSessionDir),
		archiveDir:       strings.TrimSpace(opts.ArchiveDir),
	}
}

// SearchRequest describes a history search.
type SearchRequest struct {
	Query    string
	Scope    string
	Kinds    []Kind
	ToolName string
	Limit    int
}

// AroundRequest fetches messages adjacent to a search hit.
type AroundRequest struct {
	SessionPath  string
	MessageIndex int
	Before       int
	After        int
}

// Hit is a ranked search result.
type Hit struct {
	Score        float64
	SessionPath  string
	SessionID    string
	Source       string
	MessageIndex int
	Role         provider.Role
	Kind         Kind
	ToolName     string
	Snippet      string
}

// MessageContext is one message returned by Around.
type MessageContext struct {
	Index int
	Text  string
}

type sourceFile struct {
	path   string
	source string
	mod    int64
}

type document struct {
	source       sourceFile
	messageIndex int
	role         provider.Role
	kind         Kind
	toolName     string
	text         string
	counts       map[string]int
	length       int
}

// Search ranks saved history by BM25. It indexes only the selected documents for
// this call, which keeps the implementation dependency-free and cache-neutral.
func (s *Searcher) Search(ctx context.Context, req SearchRequest) ([]Hit, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	queryTerms, err := retrieval.QueryTerms(query)
	if err != nil {
		return nil, err
	}
	scope, err := normalizeScope(req.Scope)
	if err != nil {
		return nil, err
	}
	limit := clamp(req.Limit, defaultLimit, maxLimit)
	kindSet, err := normalizeKinds(req.Kinds)
	if err != nil {
		return nil, err
	}
	toolName := strings.TrimSpace(req.ToolName)

	sources, err := s.sources(scope)
	if err != nil {
		return nil, err
	}
	var docs []document
	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		msgs, err := loadMessages(src.path)
		if err != nil {
			continue
		}
		docs = append(docs, extractDocuments(src, msgs, kindSet, toolName)...)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	df := map[string]int{}
	totalLen := 0
	for i := range docs {
		totalLen += docs[i].length
		seen := map[string]bool{}
		for term := range docs[i].counts {
			if !seen[term] {
				df[term]++
				seen[term] = true
			}
		}
	}
	avgLen := float64(totalLen) / float64(len(docs))
	if avgLen <= 0 {
		avgLen = 1
	}

	var hits []Hit
	for _, doc := range docs {
		score := retrieval.BM25Score(doc.counts, doc.length, queryTerms, df, len(docs), avgLen)
		if score <= 0 {
			continue
		}
		hits = append(hits, Hit{
			Score:        score,
			SessionPath:  doc.source.path,
			SessionID:    sessionID(doc.source.path),
			Source:       doc.source.source,
			MessageIndex: doc.messageIndex,
			Role:         doc.role,
			Kind:         doc.kind,
			ToolName:     doc.toolName,
			Snippet:      retrieval.MakeSnippet(doc.text, query, queryTerms, maxSnippet),
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			if hits[i].SessionPath == hits[j].SessionPath {
				return hits[i].MessageIndex < hits[j].MessageIndex
			}
			return hits[i].SessionPath < hits[j].SessionPath
		}
		return hits[i].Score > hits[j].Score
	})
	hits = retrieval.KeepTopRelativeScore(hits, scoreFloor, func(hit Hit) float64 {
		return hit.Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// Around returns a compact transcript window around a saved message.
func (s *Searcher) Around(ctx context.Context, req AroundRequest) ([]MessageContext, error) {
	path := strings.TrimSpace(req.SessionPath)
	if path == "" {
		return nil, fmt.Errorf("session_path is required")
	}
	if req.MessageIndex < 0 {
		return nil, fmt.Errorf("message_index must be non-negative")
	}
	if !s.allowedPath(path) {
		return nil, fmt.Errorf("session_path is outside the configured history roots")
	}
	if !s.visiblePath(path) {
		return nil, fmt.Errorf("session_path is pending cleanup")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	msgs, err := loadMessages(path)
	if err != nil {
		return nil, err
	}
	if req.MessageIndex >= len(msgs) {
		return nil, fmt.Errorf("message_index %d is outside session length %d", req.MessageIndex, len(msgs))
	}
	before := clamp(req.Before, defaultAround, maxAround)
	after := clamp(req.After, defaultAround, maxAround)
	start := req.MessageIndex - before
	if start < 0 {
		start = 0
	}
	remainingAfter := len(msgs) - req.MessageIndex - 1
	end := len(msgs)
	if after < remainingAfter {
		end = len(msgs) - (remainingAfter - after)
	}
	out := make([]MessageContext, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, MessageContext{Index: i, Text: renderMessage(i, msgs[i])})
	}
	return out, nil
}

func normalizeScope(scope string) (string, error) {
	switch strings.TrimSpace(scope) {
	case "", scopeProject:
		return scopeProject, nil
	case scopeGlobal:
		return scopeGlobal, nil
	default:
		return "", fmt.Errorf("scope must be %q or %q", scopeProject, scopeGlobal)
	}
}

func normalizeKinds(kinds []Kind) (map[Kind]bool, error) {
	if len(kinds) == 0 {
		out := make(map[Kind]bool, len(defaultKinds))
		for k, v := range defaultKinds {
			out[k] = v
		}
		return out, nil
	}
	out := map[Kind]bool{}
	for _, k := range kinds {
		switch k {
		case KindUserText, KindAssistantText, KindToolInput, KindToolError, KindToolOutput:
			out[k] = true
		default:
			return nil, fmt.Errorf("unknown kind %q", k)
		}
	}
	return out, nil
}

func (s *Searcher) sources(scope string) ([]sourceFile, error) {
	var out []sourceFile
	seen := map[string]bool{}
	out = appendSessionSources(out, seen, s.sessionDir, scopeProject)
	if scope == scopeGlobal {
		out = appendSessionSources(out, seen, s.globalSessionDir, scopeGlobal)
		out = appendFiles(out, seen, listJSONL(s.archiveDir, "archive", nil)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].mod == out[j].mod {
			return out[i].path < out[j].path
		}
		return out[i].mod > out[j].mod
	})
	return out, nil
}

func appendSessionSources(out []sourceFile, seen map[string]bool, dir, source string) []sourceFile {
	out = appendFiles(out, seen, listJSONL(dir, source, agent.IsVisibleSession)...)
	if strings.TrimSpace(dir) != "" {
		out = appendFiles(out, seen, listJSONL(subagentsDir(dir), source, func(path string) bool {
			return visibleSubagentSession(dir, path)
		})...)
	}
	return out
}

func appendFiles(out []sourceFile, seen map[string]bool, files ...sourceFile) []sourceFile {
	for _, file := range files {
		key := file.path
		if abs, err := filepath.Abs(file.path); err == nil {
			key = abs
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, file)
	}
	return out
}

func listJSONL(dir, source string, visible func(string) bool) []sourceFile {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sourceFile
	for _, entry := range entries {
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if visible != nil && !visible(path) {
			continue
		}
		// Recency must track the event log too: the .jsonl checkpoint's mtime
		// only moves at checkpoints.
		mod := info.ModTime()
		if contentMod := agent.SessionContentModTime(path); !contentMod.IsZero() {
			mod = contentMod
		}
		out = append(out, sourceFile{
			path:   path,
			source: source,
			mod:    mod.UnixNano(),
		})
	}
	return out
}

func loadMessages(path string) ([]provider.Message, error) {
	sess, err := agent.LoadSession(path)
	if err != nil {
		return nil, err
	}
	return sess.Snapshot(), nil
}

func extractDocuments(src sourceFile, msgs []provider.Message, kinds map[Kind]bool, toolName string) []document {
	var docs []document
	for i, msg := range msgs {
		switch msg.Role {
		case provider.RoleUser:
			if kinds[KindUserText] && strings.TrimSpace(msg.Content) != "" {
				docs = appendDoc(docs, src, i, msg.Role, KindUserText, "", stripComposePrefixes(msg.Content))
			}
		case provider.RoleAssistant:
			if kinds[KindAssistantText] && strings.TrimSpace(msg.Content) != "" {
				docs = appendDoc(docs, src, i, msg.Role, KindAssistantText, "", msg.Content)
			}
			if kinds[KindToolInput] {
				for _, call := range msg.ToolCalls {
					if toolName != "" && call.Name != toolName {
						continue
					}
					text := strings.TrimSpace(call.Name + " " + call.Arguments)
					docs = appendDoc(docs, src, i, msg.Role, KindToolInput, call.Name, text)
				}
			}
		case provider.RoleTool:
			if toolName != "" && msg.Name != toolName {
				continue
			}
			if kinds[KindToolError] && isToolError(msg.Content) {
				docs = appendDoc(docs, src, i, msg.Role, KindToolError, msg.Name, msg.Name+" "+msg.Content)
			}
			if kinds[KindToolOutput] {
				docs = appendDoc(docs, src, i, msg.Role, KindToolOutput, msg.Name, msg.Name+" "+msg.Content)
			}
		}
	}
	return docs
}

func appendDoc(docs []document, src sourceFile, idx int, role provider.Role, kind Kind, toolName, text string) []document {
	text = strings.TrimSpace(text)
	if text == "" {
		return docs
	}
	terms := retrieval.Tokens(text)
	if len(terms) == 0 {
		return docs
	}
	counts := retrieval.Counts(terms)
	return append(docs, document{
		source:       src,
		messageIndex: idx,
		role:         role,
		kind:         kind,
		toolName:     toolName,
		text:         text,
		counts:       counts,
		length:       len(terms),
	})
}

func isToolError(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	return strings.HasPrefix(s, "error:") ||
		strings.HasPrefix(s, "blocked:") ||
		strings.Contains(s, "permission denied")
}

func sessionID(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func renderMessage(idx int, msg provider.Message) string {
	var b strings.Builder
	switch msg.Role {
	case provider.RoleUser:
		fmt.Fprintf(&b, "[%d user]\n%s", idx, truncate(stripComposePrefixes(msg.Content), 2000))
	case provider.RoleAssistant:
		if strings.TrimSpace(msg.Content) != "" {
			fmt.Fprintf(&b, "[%d assistant]\n%s", idx, truncate(msg.Content, 2000))
		} else {
			fmt.Fprintf(&b, "[%d assistant]", idx)
		}
		for _, call := range msg.ToolCalls {
			fmt.Fprintf(&b, "\n[tool call: %s]\n%s", call.Name, truncate(call.Arguments, 1200))
		}
	case provider.RoleTool:
		fmt.Fprintf(&b, "[%d tool %s result]\n%s", idx, msg.Name, truncate(msg.Content, 2000))
	case provider.RoleSystem:
		fmt.Fprintf(&b, "[%d system]\n%s", idx, truncate(msg.Content, 1200))
	default:
		fmt.Fprintf(&b, "[%d %s]\n%s", idx, msg.Role, truncate(msg.Content, 2000))
	}
	return strings.TrimSpace(b.String())
}

func truncate(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func clamp(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func (s *Searcher) visiblePath(path string) bool {
	switch {
	case underRoot(path, subagentsDir(s.sessionDir)):
		return visibleSubagentSession(s.sessionDir, path)
	case underRoot(path, s.sessionDir):
		return agent.IsVisibleSession(path)
	case underRoot(path, subagentsDir(s.globalSessionDir)):
		return visibleSubagentSession(s.globalSessionDir, path)
	case underRoot(path, s.globalSessionDir):
		return agent.IsVisibleSession(path)
	case underRoot(path, s.archiveDir):
		return true
	default:
		return false
	}
}

func visibleSubagentSession(sessionDir, path string) bool {
	if !agent.IsVisibleSession(path) {
		return false
	}
	parentSession, ok := subagentParentSession(path)
	if !ok || parentSession == "" {
		return true
	}
	return !agent.IsCleanupPending(filepath.Join(sessionDir, parentSession+".jsonl"))
}

func subagentParentSession(path string) (string, bool) {
	ref := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if ref == "" || ref == filepath.Base(path) {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(path), ref+".meta.json"))
	if err != nil {
		return "", false
	}
	var meta agent.SubagentMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return "", false
	}
	return strings.TrimSpace(meta.ParentSession), true
}

func subagentsDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, "subagents")
}

func (s *Searcher) allowedPath(path string) bool {
	roots := []string{s.sessionDir, s.globalSessionDir, s.archiveDir}
	if s.sessionDir != "" {
		roots = append(roots, subagentsDir(s.sessionDir))
	}
	if s.globalSessionDir != "" {
		roots = append(roots, subagentsDir(s.globalSessionDir))
	}
	for _, root := range roots {
		if underRoot(path, root) {
			return true
		}
	}
	return false
}

func underRoot(path, root string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(root) == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// MarshalJSON keeps Hit stable if frontends choose to expose the same data later.
func (h Hit) MarshalJSON() ([]byte, error) {
	type hit struct {
		Score        float64       `json:"score"`
		SessionPath  string        `json:"session_path"`
		SessionID    string        `json:"session_id"`
		Source       string        `json:"source"`
		MessageIndex int           `json:"message_index"`
		Role         provider.Role `json:"role"`
		Kind         Kind          `json:"kind"`
		ToolName     string        `json:"tool_name,omitempty"`
		Snippet      string        `json:"snippet"`
	}
	return json.Marshal(hit(h))
}
