package boot

import "reasonix/internal/runtime/writeclaim"

func (b *builder) retainedDelegationScheduler() *writeclaim.SubagentScheduler {
	if b.session.mutations == nil {
		return nil
	}
	return b.tools.sub.scheduler
}
