package proc

func publishRootWait(done chan struct{}, results chan<- error, err error) {
	close(done)
	results <- err
}

// RootWaitCompleted closes after the registered cmd.Wait returns, before its
// result is received. Nil means no registered wait, not completed execution.
// It does not prove that descendants or an external execution scope have ended.
func (p *TrackedCommand) RootWaitCompleted() <-chan struct{} {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rootWait
}
