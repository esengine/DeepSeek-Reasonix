package main

// removeTabOrderLocked drops the tab from the visible order and releases the
// browser grant that was scoped to it. Caller holds App.mu.
func (a *App) removeTabOrderLocked(tabID string) {
	a.forgetBrowserExecutorLocked(tabID)
	next := a.tabOrder[:0]
	for _, id := range a.tabOrder {
		if id != tabID {
			next = append(next, id)
		}
	}
	a.tabOrder = next
}
