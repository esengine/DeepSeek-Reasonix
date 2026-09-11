package agent

import (
	"context"
	"testing"

	"reasonix/internal/jobs"
	"reasonix/internal/runtimepolicy"
	"reasonix/internal/sessiontemp"
)

// CodeQL's generic context model propagates the raw input through any Value
// lookup. These distinct private key types must never alias privileged values.
func TestRawInputCannotReplacePathOwningContextValues(t *testing.T) {
	manager := &jobs.Manager{}
	temp := sessiontemp.NewWithRoot(t.TempDir())
	t.Cleanup(temp.Release)
	trusted := func(ctx context.Context) context.Context {
		ctx = WithParentSession(ctx, "trusted-session")
		ctx = jobs.WithManager(ctx, manager)
		ctx = jobs.WithSession(ctx, "trusted-session")
		return sessiontemp.WithManager(ctx, temp)
	}
	for _, raw := range []string{"../../outside", `/absolute/path`, `C:\outside`, "trusted-session", ""} {
		for _, ctx := range []context.Context{
			WithRawUserInput(trusted(context.Background()), raw),
			trusted(WithRawUserInput(context.Background(), raw)),
		} {
			ctx = WithResponseFormat(ctx, raw)
			ctx = runtimepolicy.WithContext(ctx, runtimepolicy.Constraints{Notes: []string{raw}, AllowedChecks: []string{raw}})
			if RawUserInput(ctx, "fallback") != raw || ParentSession(ctx) != "trusted-session" || jobs.SessionFromContext(ctx) != "trusted-session" {
				t.Fatal("raw input crossed the parent-session key boundary")
			}
			if got, ok := jobs.FromContext(ctx); !ok || got != manager {
				t.Fatal("raw input replaced the jobs manager")
			}
			if sessiontemp.FromContext(ctx) != temp {
				t.Fatal("raw input replaced the temporary-directory manager")
			}
		}
		ctx := WithRawUserInput(context.Background(), raw)
		if ParentSession(ctx) != "" || jobs.SessionFromContext(ctx) != "" || sessiontemp.FromContext(ctx) != nil {
			t.Fatal("raw input fabricated a privileged context value")
		}
		if _, ok := jobs.FromContext(ctx); ok {
			t.Fatal("raw input fabricated a jobs manager")
		}
	}
}
