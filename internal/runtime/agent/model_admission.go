package agent

import (
	"context"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/mutation"
)

type modelAdmission struct {
	coordinator *mutation.Coordinator
	intent      mutation.Intent
}

func (m modelAdmission) AcquireRequest(ctx context.Context) (func(), error) {
	intent := m.intent
	if call, _, _, ok := CallContext(ctx); ok {
		intent.CallID = call
	}
	lease, err := m.coordinator.Acquire(ctx, intent)
	if err != nil {
		return nil, err
	}
	return func() { lease.End(mutation.NotExecuted) }, nil
}

func (a *Agent) bindModelAdmission(ctx context.Context) context.Context {
	var admission provider.RequestAdmission
	if a.svc.mutations != nil && len(a.svc.capacity.Model) > 0 {
		admission = modelAdmission{coordinator: a.svc.mutations, intent: mutation.Intent{
			Effect: mutation.CapacityOnly, Capacities: a.svc.capacity.Model,
			SessionID: ParentSession(ctx), ExecutionID: executionID(ctx), CallID: "model.request",
		}}
	}
	return provider.WithRequestAdmission(ctx, admission)
}
