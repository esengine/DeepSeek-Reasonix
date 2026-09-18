package attachment

// RebindVerified renews an owner-transferred capability after the caller has
// validated its original. Possession of a digest is never sufficient. Previous
// credentials remain proofs for response-loss retries until explicit release.
func (d *DraftStore) RebindVerified(scope, id string) (DraftCredential, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	item, ok := d.items[id]
	if !ok || item.scope != scope {
		return DraftCredential{}, Error{Code: CodeMissing, Message: "draft credential is not valid", Retry: true}
	}
	if !item.needsRebind {
		return item.draft, nil
	}
	for _, bound := range d.items {
		if bound.scope == scope && bound.family == item.family && !bound.needsRebind {
			return bound.draft, nil
		}
	}
	item.draft.ID = newID()
	item.needsRebind = false
	d.items[item.draft.ID] = item
	return item.draft, nil
}
