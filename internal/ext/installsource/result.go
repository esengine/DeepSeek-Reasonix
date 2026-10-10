package installsource

import (
	"context"
	"encoding/json"
)

// AppliedItem is what an apply actually wrote, taken from the unprojected
// action: callers that record or act on it must not read the bounded JSON.
type AppliedItem struct {
	Kind, Name, Target, ConfigPath string
}

// Result is what a caller that gates on a plan reads instead of the bounded
// JSON: the actions that completed, and whether the unprojected plan is themes only.
type Result struct {
	Applied    []AppliedItem
	ThemesOnly bool
}

// ExecuteApplied is Execute that also returns the unprojected facts of the plan.
func (t *Tool) ExecuteApplied(ctx context.Context, raw json.RawMessage) (string, Result, error) {
	var res Result
	out, err := t.execute(ctx, raw, &res)
	return out, res, err
}

// fingerprint returns the plan's ticket and records the facts a caller gates on,
// both from the same unprojected actions.
func (r *Result) fingerprint(req request, actions []action) string {
	if r != nil {
		r.ThemesOnly = themesOnly(actions)
	}
	return computePlanID(req, actions)
}
