// The session recall index: the recall pool read and tokenized once per
// memory snapshot, so each user turn's automatic recall costs zero disk IO.
// Markdown files stay the source of truth — every write path reloads the
// snapshot through memory.Load, which rebuilds this index with it.
package memory

import (
	"strings"

	"reasonix/internal/base/retrieval"
)

// RecallIndex is the prebuilt retrieval state for one immutable Set snapshot.
type RecallIndex struct {
	docs    []autoRecallDoc
	fielded []retrieval.FieldedDoc // V2 shadow pool, prebuilt field split
	// unreadable names the fact files that exist but could not be read while
	// this index was built. They are carried, not treated as absent: a turn
	// still recalls from the facts that did load and the audit names the rest.
	unreadable []*FactReadError
}

// BuildRecallIndex reads and tokenizes the recall pool once. A zero store
// yields nil, which recall reports as an empty memory store. A store whose only
// present facts are unreadable yields a non-nil index with no documents, so
// recall reports the read failure instead of claiming the store is empty.
func BuildRecallIndex(store Store) (*RecallIndex, error) {
	report, err := store.ListActive()
	if err != nil {
		return nil, err
	}
	memories := recallMemories(report.Facts)
	if len(memories) == 0 && len(report.Unreadable) == 0 {
		return nil, nil
	}
	index := &RecallIndex{unreadable: report.Unreadable}
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
	return index, nil
}

// AutoRecall runs automatic recall against this snapshot's prebuilt index —
// the per-turn path. Semantics are identical to the package-level AutoRecall.
func (s *Set) AutoRecall(query string, opts RecallOptions) RecallResult {
	opts = s.withConfiguredBudgets(opts)
	result := RecallResult{Query: strings.TrimSpace(query), CharBudget: recallCharBudget(opts.MaxChars)}
	if genericRecallQuery(result.Query) {
		result.Suppress(SuppressGenericTurn, "generic user turn")
		return result
	}
	if s == nil {
		result.Suppress(SuppressEmptyStore, "memory store is empty")
		return result
	}
	if s.StoreError != nil {
		return recallReadFailure(result, s.StoreError)
	}
	index := s.recall
	if index == nil {
		// Hand-built sets (tests, embedders) carry no prebuilt index; the
		// Load path always does, so per-turn recall stays disk-free there.
		var err error
		index, err = BuildRecallIndex(s.Store)
		if err != nil {
			return recallReadFailure(result, err)
		}
	}
	return autoRecallIndexed(index, result, opts)
}

// withConfiguredBudgets fills unset recall axes from what the user configured,
// so a caller passing RecallOptions{} gets their settings rather than the
// package defaults.
func (s *Set) withConfiguredBudgets(opts RecallOptions) RecallOptions {
	if s == nil {
		return opts
	}
	if opts.Limit <= 0 {
		opts.Limit = s.opts.RecallLimit
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = s.opts.RecallMaxChars
	}
	return opts
}
