package usecap

// FreezeComposition keeps settings edits from widening this session's catalog.
func (r *MCPCapabilityRuntime) FreezeComposition() {
	if r == nil {
		return
	}
	r.dispatchMu.Lock()
	defer r.dispatchMu.Unlock()
	r.compositionFrozen = true
}
