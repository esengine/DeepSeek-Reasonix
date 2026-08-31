package sessioncatalog

// pathMutationAllowed applies the remove-versus-recreate generation rule at
// every writer boundary. A candidate newer than the observed removal may clear
// exactly that tombstone; if another removal replaces it concurrently, the
// compare fails and the candidate is re-evaluated against the newer generation.
func (c *Catalog) pathMutationAllowed(pathKey string, sequence uint64) bool {
	for {
		removedAt, removed := c.removedPaths.Load(pathKey)
		if !removed {
			return true
		}
		if c.testPathMutationLoadedHook != nil {
			c.testPathMutationLoadedHook(pathKey)
		}
		removedSequence, ok := removedAt.(uint64)
		if !ok || sequence <= removedSequence {
			return false
		}
		if c.removedPaths.CompareAndDelete(pathKey, removedAt) {
			return true
		}
	}
}

func (c *Catalog) filterPathMutations(records []SessionRecord, sequence uint64) []SessionRecord {
	filtered := records[:0]
	for _, record := range records {
		if c.pathMutationAllowed(c.pathKey(record.Path), sequence) {
			record.enqueueSequence = sequence
			filtered = append(filtered, record)
		}
	}
	return filtered
}
