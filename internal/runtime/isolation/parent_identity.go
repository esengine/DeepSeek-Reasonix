package isolation

import "path/filepath"

func matchesParentResult(link *ParentResult, parent *Entry, token string) bool {
	if link == nil || link.ID != parent.ID || link.Generation != parent.RunGeneration || link.Token != token {
		return false
	}
	if link.Record == entryRecordPath(parent) {
		return true
	}
	// Terminal promotion changes the marker name, while a settled child's
	// immutable backlink retains the original marker in that same directory.
	return terminalState(parent.State) && !parent.legacy &&
		link.Record == filepath.Join(filepath.Dir(entryPath(parent.Cand)), legacyEntryFile)
}
