package agent

import "context"

// ContinuationPolicy is the internal host policy for synthetic same-Run
// continuation. It is not a user configuration key.
type ContinuationPolicy uint8

const (
	// ContinuationDisabled is the default: a clean terminal ends the Run after
	// hard safety/readiness gates. Ordinary agents leave this unset.
	ContinuationDisabled ContinuationPolicy = iota
	// ContinuationExplicitFlow opts a dedicated Goal/review/guardian/typed-report
	// run into host-owned continuation helpers.
	ContinuationExplicitFlow
)

type continuationPolicyKey struct{}

// WithContinuationPolicy opts one Run into an explicit continuation flow.
func WithContinuationPolicy(ctx context.Context, policy ContinuationPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, continuationPolicyKey{}, policy)
}

func continuationPolicyFromContext(ctx context.Context) (ContinuationPolicy, bool) {
	if ctx == nil {
		return ContinuationDisabled, false
	}
	policy, ok := ctx.Value(continuationPolicyKey{}).(ContinuationPolicy)
	return policy, ok
}

func (a *Agent) hostContinuationEnabled(ctx context.Context) bool {
	if a == nil {
		return false
	}
	if policy, ok := continuationPolicyFromContext(ctx); ok {
		return policy == ContinuationExplicitFlow
	}
	return a.continuationPolicy == ContinuationExplicitFlow
}
