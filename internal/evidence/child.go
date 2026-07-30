package evidence

import "sort"

// ChildEvidenceSummary is the ordered, host-observable evidence a sub-agent
// produced. Parents merge these receipts so delegated writes, reads, commands,
// verifications, and structured reviews count toward delivery gates without
// treating the meta tool call itself as a mutation.
type ChildEvidenceSummary struct {
	Receipts []Receipt
}

// HasMutation reports whether any successful receipt is a real state change.
func (s ChildEvidenceSummary) HasMutation() bool {
	for _, r := range s.Receipts {
		if r.Success && r.Mutation {
			return true
		}
	}
	return false
}

// MutationPaths returns distinct production paths written by the child.
func (s ChildEvidenceSummary) MutationPaths() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range s.Receipts {
		if !r.Success || !r.Mutation {
			continue
		}
		for _, p := range r.Paths {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Summary returns a snapshot of every receipt recorded this turn in order.
func (l *Ledger) Summary() ChildEvidenceSummary {
	if l == nil {
		return ChildEvidenceSummary{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Receipt, len(l.receipts))
	copy(out, l.receipts)
	return ChildEvidenceSummary{Receipts: out}
}

// MergeChild appends successful child receipts into the parent ledger. Failed
// child receipts are retained for auditability with Success=false so they never
// satisfy host matchers.
func (l *Ledger) MergeChild(summary ChildEvidenceSummary) {
	if l == nil || len(summary.Receipts) == 0 {
		return
	}
	for _, r := range summary.Receipts {
		// Drop nested bookkeeping that the parent already owns.
		switch r.ToolName {
		case "todo_write", "complete_step", "ask":
			continue
		}
		l.Record(r)
	}
}

// MergeChildren merges multiple child summaries in the given order.
func (l *Ledger) MergeChildren(summaries ...ChildEvidenceSummary) {
	for _, s := range summaries {
		l.MergeChild(s)
	}
}
