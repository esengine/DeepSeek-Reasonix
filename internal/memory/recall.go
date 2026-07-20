package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"reasonix/internal/retrieval"
	"reasonix/internal/tool"
)

const (
	defaultRecallLimit        = 8
	maxRecallLimit            = 20
	maxRecallSnippet          = 260
	recallScoreFloor          = 0.15
	defaultDiversityWeight    = 0.35
	defaultDuplicateThreshold = 0.82
	defaultStalenessHalfLife  = 365 * 24 * time.Hour
)

// RecallOptions controls the post-BM25 ranking layer. Nil booleans use the
// production defaults (enabled); pointers let callers explicitly disable each
// behavior without changing legacy constructor semantics.
type RecallOptions struct {
	Diversity          *bool
	DiversityWeight    float64
	DuplicateThreshold float64
	Staleness          *bool
	StalenessHalfLife  time.Duration
	Now                func() time.Time
}

type recallTool struct {
	store   Store
	options RecallOptions
}

// NewRecallTool returns the read-only `memory` tool for searching saved facts.
func NewRecallTool(store Store) tool.Tool { return recallTool{store: store} }

// NewRecallToolWithOptions returns a recall tool with explicit ranking policy.
func NewRecallToolWithOptions(store Store, options RecallOptions) tool.Tool {
	return recallTool{store: store, options: options}
}

func (recallTool) Name() string { return "memory" }

func (recallTool) Description() string {
	return "Search, list, and read saved project memories. " +
		"Use this before saving a new memory to avoid duplicates, and when a saved memory from the index looks relevant but needs its full body. " +
		"This tool is read-only; use remember to save or update a memory, and forget to delete one."
}

func (recallTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {"type": "string", "enum": ["search", "read", "list"], "description": "search ranks saved memories; read returns one full memory by name; list returns the saved-memory index."},
			"query": {"type": "string", "description": "Search query for operation=search."},
			"name": {"type": "string", "description": "Memory slug for operation=read, e.g. the name in [Label](name.md)."},
			"type": {"type": "string", "enum": ["user", "feedback", "project", "reference"], "description": "Optional memory type filter for search or list."},
			"limit": {"type": "integer", "description": "Maximum search/list results to return, default 8, max 20."}
		},
		"required": ["operation"]
	}`)
}

func (t recallTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Operation string `json:"operation"`
		Query     string `json:"query"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if t.store.Dir == "" {
		return "Memory store is unavailable.", nil
	}
	memType, err := recallTypeFilter(in.Type)
	if err != nil {
		return "", err
	}
	limit := clampRecallLimit(in.Limit)
	switch strings.TrimSpace(in.Operation) {
	case "search":
		hits, err := searchMemoriesWithOptions(ctx, t.store, in.Query, memType, limit, t.options)
		if err != nil {
			return "", err
		}
		return formatMemoryHits(in.Query, hits), nil
	case "read":
		m, ok := readMemoryByName(t.store, in.Name)
		if !ok {
			return "", fmt.Errorf("memory %q not found", slug(in.Name))
		}
		return formatMemory(t.store, m), nil
	case "list":
		return formatMemoryList(t.store, filterMemories(t.store.List(), memType), limit), nil
	case "":
		return "", fmt.Errorf("operation is required")
	default:
		return "", fmt.Errorf("unknown operation %q", in.Operation)
	}
}

func (recallTool) ReadOnly() bool { return true }

type memoryHit struct {
	Memory           Memory
	Path             string
	Score            float64
	RankScore        float64
	DiversityPenalty float64
	StalenessFactor  float64
	Snippet          string
	terms            []string
}

type memoryDoc struct {
	memory Memory
	path   string
	text   string
	counts map[string]int
	length int
}

func searchMemories(ctx context.Context, store Store, query string, typ Type, limit int) ([]memoryHit, error) {
	return searchMemoriesWithOptions(ctx, store, query, typ, limit, RecallOptions{})
}

func searchMemoriesWithOptions(ctx context.Context, store Store, query string, typ Type, limit int, options RecallOptions) ([]memoryHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	queryTerms, err := retrieval.QueryTerms(query)
	if err != nil {
		return nil, err
	}
	memories := filterMemories(store.List(), typ)
	docs := make([]memoryDoc, 0, len(memories))
	for _, m := range memories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text := memorySearchText(m)
		terms := retrieval.Tokens(text)
		if len(terms) == 0 {
			continue
		}
		docs = append(docs, memoryDoc{
			memory: m,
			path:   store.Path(m.Name),
			text:   text,
			counts: retrieval.Counts(terms),
			length: len(terms),
		})
	}
	if len(docs) == 0 {
		return nil, nil
	}
	counts := make([]map[string]int, 0, len(docs))
	totalLen := 0
	for _, doc := range docs {
		counts = append(counts, doc.counts)
		totalLen += doc.length
	}
	df := retrieval.DocumentFrequency(counts)
	avgLen := float64(totalLen) / float64(len(docs))

	var hits []memoryHit
	for _, doc := range docs {
		score := retrieval.BM25Score(doc.counts, doc.length, queryTerms, df, len(docs), avgLen)
		if score <= 0 {
			continue
		}
		hits = append(hits, memoryHit{
			Memory:  doc.memory,
			Path:    doc.path,
			Score:   score,
			Snippet: retrieval.MakeSnippet(doc.text, query, queryTerms, maxRecallSnippet),
			terms:   retrieval.Unique(retrieval.Tokens(memorySimilarityText(doc.memory))),
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Memory.Name < hits[j].Memory.Name
		}
		return hits[i].Score > hits[j].Score
	})
	hits = retrieval.KeepTopRelativeScore(hits, recallScoreFloor, func(hit memoryHit) float64 {
		return hit.Score
	})
	return rerankMemoryHits(hits, limit, normalizeRecallOptions(options)), nil
}

type normalizedRecallOptions struct {
	diversity          bool
	diversityWeight    float64
	duplicateThreshold float64
	staleness          bool
	stalenessHalfLife  time.Duration
	now                time.Time
}

func normalizeRecallOptions(in RecallOptions) normalizedRecallOptions {
	out := normalizedRecallOptions{
		diversity:          in.Diversity == nil || *in.Diversity,
		diversityWeight:    in.DiversityWeight,
		duplicateThreshold: in.DuplicateThreshold,
		staleness:          in.Staleness == nil || *in.Staleness,
		stalenessHalfLife:  in.StalenessHalfLife,
		now:                time.Now().UTC(),
	}
	if out.diversityWeight <= 0 || out.diversityWeight > 1 {
		out.diversityWeight = defaultDiversityWeight
	}
	if out.duplicateThreshold <= 0 || out.duplicateThreshold > 1 {
		out.duplicateThreshold = defaultDuplicateThreshold
	}
	if out.stalenessHalfLife <= 0 {
		out.stalenessHalfLife = defaultStalenessHalfLife
	}
	if in.Now != nil {
		out.now = in.Now().UTC()
	}
	return out
}

func rerankMemoryHits(hits []memoryHit, limit int, options normalizedRecallOptions) []memoryHit {
	if len(hits) == 0 || limit <= 0 {
		return nil
	}
	for i := range hits {
		hits[i].StalenessFactor = memoryStalenessFactor(hits[i].Memory, options)
		hits[i].RankScore = hits[i].Score * hits[i].StalenessFactor
	}
	if !options.diversity {
		if options.staleness && len(hits) > 2 {
			// Preserve the strongest lexical hit, but let confirmation age
			// influence the remaining order even when diversity is disabled.
			sort.SliceStable(hits[1:], func(i, j int) bool {
				left := &hits[i+1]
				right := &hits[j+1]
				if left.RankScore == right.RankScore {
					return left.Memory.Name < right.Memory.Name
				}
				return left.RankScore > right.RankScore
			})
		}
		if len(hits) > limit {
			hits = hits[:limit]
		}
		return hits
	}

	// Keep the strongest lexical hit stable, then greedily choose complementary
	// evidence. Near-identical notes are omitted rather than consuming context.
	selected := []memoryHit{hits[0]}
	remaining := append([]memoryHit(nil), hits[1:]...)
	topScore := hits[0].Score
	for len(selected) < limit && len(remaining) > 0 {
		best := -1
		for i := range remaining {
			maxSimilarity := 0.0
			for _, prior := range selected {
				maxSimilarity = math.Max(maxSimilarity, tokenJaccard(remaining[i].terms, prior.terms))
			}
			if maxSimilarity >= options.duplicateThreshold {
				continue
			}
			remaining[i].DiversityPenalty = maxSimilarity
			remaining[i].RankScore = remaining[i].Score*remaining[i].StalenessFactor - options.diversityWeight*maxSimilarity*topScore
			if best < 0 || remaining[i].RankScore > remaining[best].RankScore ||
				(remaining[i].RankScore == remaining[best].RankScore && remaining[i].Memory.Name < remaining[best].Memory.Name) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		selected = append(selected, remaining[best])
		remaining = append(remaining[:best], remaining[best+1:]...)
	}
	return selected
}

func memoryStalenessFactor(m Memory, options normalizedRecallOptions) float64 {
	if !options.staleness || m.LastConfirmedAt.IsZero() || !options.now.After(m.LastConfirmedAt) {
		return 1
	}
	age := options.now.Sub(m.LastConfirmedAt)
	factor := math.Exp2(-float64(age) / float64(options.stalenessHalfLife))
	return math.Max(0.5, factor)
}

func tokenJaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(a))
	for _, term := range a {
		set[term] = struct{}{}
	}
	intersection := 0
	union := len(set)
	for _, term := range b {
		if _, ok := set[term]; ok {
			intersection++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func recallTypeFilter(s string) (Type, error) {
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	t := Type(strings.ToLower(strings.TrimSpace(s)))
	if !validTypes[t] {
		return "", fmt.Errorf("type must be one of user, feedback, project, reference")
	}
	return t, nil
}

func filterMemories(memories []Memory, typ Type) []Memory {
	if typ == "" {
		return memories
	}
	out := memories[:0]
	for _, m := range memories {
		if NormalizeType(string(m.Type)) == typ {
			out = append(out, m)
		}
	}
	return out
}

func readMemoryByName(store Store, name string) (Memory, bool) {
	name = slug(name)
	if name == "" {
		return Memory{}, false
	}
	m, ok := loadMemory(store.Path(name))
	if !ok || slug(m.Name) != name {
		return Memory{}, false
	}
	m.Name = name
	return m, true
}

func memorySearchText(m Memory) string {
	return strings.Join([]string{
		m.Name,
		m.Title,
		string(NormalizeType(string(m.Type))),
		m.Description,
		m.Body,
	}, "\n")
}

// memorySimilarityText intentionally excludes the memory name and filesystem
// path. Those identifiers help exact BM25 lookup, but treating them as semantic
// content makes otherwise identical notes appear artificially different.
func memorySimilarityText(m Memory) string {
	return strings.Join([]string{
		m.Title,
		m.Description,
		string(NormalizeType(string(m.Type))),
		m.Body,
	}, "\n")
}

func formatMemoryHits(query string, hits []memoryHit) string {
	if len(hits) == 0 {
		return strings.Join([]string{
			"No saved memories matched " + strconvQuote(query) + ".",
			"",
			"0 results does not prove the fact was never recorded. Try:",
			"1. Retry with 1-3 distinctive terms (function name, task id, rare phrase) instead of a long generic sentence.",
			"2. For exact literals that punctuation splits (URLs, ports, file paths, command flags), search one distinctive token or inspect the memory directory directly.",
			"3. For verbatim original wording or exact command output, use the history tool; saved memories may paraphrase.",
		}, "\n")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Memory search results for %s:\n", strconvQuote(query))
	for i, hit := range hits {
		m := hit.Memory
		fmt.Fprintf(&b, "\n%d. score=%.3f rank=%.3f diversity=%.3f staleness=%.3f name=%s type=%s source=%s/%s title=%s\n   description: %s\n   path: %s\n   snippet: %s\n",
			i+1, hit.Score, hit.RankScore, hit.DiversityPenalty, hit.StalenessFactor, m.Name, NormalizeType(string(m.Type)), emptyLabel(m.SourceScope), emptyLabel(m.SourceKind), displayTitle(m.Title, m.Name), oneLine(m.Description), hit.Path, hit.Snippet)
	}
	b.WriteString("\nUse operation=\"read\" with a memory name to inspect the full saved fact.")
	return strings.TrimSpace(b.String())
}

func emptyLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "legacy"
	}
	return strings.TrimSpace(value)
}

func formatMemory(store Store, m Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Memory %s\n", m.Name)
	fmt.Fprintf(&b, "title: %s\n", displayTitle(m.Title, m.Name))
	fmt.Fprintf(&b, "type: %s\n", NormalizeType(string(m.Type)))
	if desc := oneLine(m.Description); desc != "" {
		fmt.Fprintf(&b, "description: %s\n", desc)
	}
	fmt.Fprintf(&b, "path: %s\n\n%s", store.Path(m.Name), strings.TrimSpace(m.Body))
	return strings.TrimSpace(b.String())
}

func formatMemoryList(store Store, memories []Memory, limit int) string {
	if len(memories) == 0 {
		return "No saved memories found."
	}
	if len(memories) > limit {
		memories = memories[:limit]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Saved memories in %s:\n", store.Dir)
	for _, m := range memories {
		fmt.Fprintf(&b, "- [%s](%s.md) type=%s - %s\n",
			displayTitle(m.Title, m.Name), m.Name, NormalizeType(string(m.Type)), oneLine(m.Description))
	}
	return strings.TrimSpace(b.String())
}

func clampRecallLimit(n int) int {
	if n <= 0 {
		return defaultRecallLimit
	}
	if n > maxRecallLimit {
		return maxRecallLimit
	}
	return n
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
