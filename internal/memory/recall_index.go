// The session recall index: the recall pool read and tokenized once per
// memory snapshot, so each user turn's automatic recall costs zero disk IO.
// Markdown files stay the source of truth — every write path reloads the
// snapshot through memory.Load, which rebuilds this index with it.
package memory

import (
	"strings"

	"reasonix/internal/retrieval"
)

// RecallIndex is the prebuilt retrieval state for one immutable Set snapshot.
type RecallIndex struct {
	docs    []autoRecallDoc
	fielded []retrieval.FieldedDoc // V2 shadow pool, prebuilt field split
}

// BuildRecallIndex reads and tokenizes the recall pool once. A zero store
// yields nil, which recall reports as an empty memory store.
func BuildRecallIndex(store Store) *RecallIndex {
	memories := recallMemories(store.ListAll())
	if len(memories) == 0 {
		return nil
	}
	index := &RecallIndex{}
	for _, memory := range memories {
		index.fielded = append(index.fielded, retrieval.FieldedDoc{ID: memory.ID, Fields: map[string]string{
			"name": memory.Name, "title": memory.Title, "keywords": memory.Keywords,
			"subject": memory.SubjectKey, "description": memory.Description, "body": memory.Body,
		}})
		text := autoRecallSearchText(memory)
		terms := retrieval.Tokens(text)
		if len(terms) == 0 {
			continue
		}
		index.docs = append(index.docs, autoRecallDoc{
			memory: memory,
			text:   text,
			counts: retrieval.Counts(terms),
			length: len(terms),
		})
	}
	return index
}

// AutoRecall runs automatic recall against this snapshot's prebuilt index —
// the per-turn path. Semantics are identical to the package-level AutoRecall.
func (s *Set) AutoRecall(query string, opts RecallOptions) RecallResult {
	result := RecallResult{Query: strings.TrimSpace(query), CharBudget: recallCharBudget(opts.MaxChars)}
	if genericRecallQuery(result.Query) {
		result.Suppressed = "generic user turn"
		return result
	}
	if s == nil {
		result.Suppressed = "memory store is empty"
		return result
	}
	index := s.recall
	if index == nil {
		// Hand-built sets (tests, embedders) carry no prebuilt index; the
		// Load path always does, so per-turn recall stays disk-free there.
		index = BuildRecallIndex(s.Store)
	}
	return autoRecallIndexed(index, result, opts)
}
