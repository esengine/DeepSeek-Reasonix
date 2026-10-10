---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-06
---

# Worked issue delivery: bounded prefix selection

This is reference output from an observed temporary-fixture exercise on
2026-10-06 with Go 1.26.8 on macOS arm64. The package was installed by the
Reasonix CLI into isolated local state; Codex then read the installed skill,
scenario, and delivery reference and performed the exercise with file and
shell tools.

This was not a native Reasonix live-provider session or an independent human
acceptance review.

No fixture commit or PR was published.

The original [`prefix.go`](prefix.go) remains deliberately unfixed.

| Input | SHA256 for this run |
| --- | --- |
| `prefix.go` | `2ec164000ded53c1a2dd4ce16b9fdfb85ff968a8741064b459592132ee420b80` |
| [`prefix_test.go`](prefix_test.go), unchanged | `06d76dc9506870497fc540ccf1623b0eb6ddf5b371ba866383f85dbd7a7a8ecc` |

Treat the results below as this run's evidence; rerun checks for your copy.

## Problem

The installed `skills/issue-fix/references/scenario.md` supplies the requirement:
`Take` returns the first `limit` values, clamping negative limits to zero and
oversized limits to the input length. It must preserve the input; the result
may share its storage. The input contains no external issue number.

In the temporary copy, the original `go test ./...` exited 1. It stopped at
`TestTake/oversized` with `slice bounds out of range [:5] with capacity 3`.
Because this panic interrupted the suite, the remaining cases were also run
individually before editing rather than counted as executed by the full run.

## Cause and change

The original `Take` directly returns `values[:limit]`. A negative limit or
a limit beyond capacity panics; a limit beyond length but within capacity
extends the slice into elements outside the logical input. All three violate
the scenario. The owning layer is `Take`, where the limit becomes a slice bound.

The temporary fix clamps to `[0, len(values)]` before slicing. It adds no
dependency or API, and preserves the allowed storage sharing. The module in
[`go.mod`](go.mod) declares Go 1.22, which supports the `min` and `max` builtins.

The production change in the temporary copy was:

```diff
--- a/prefix.go
+++ b/prefix.go
@@ -3,0 +4 @@
+	limit = max(0, min(limit, len(values)))
```

The five original cases stayed byte-identical. An additional temporary
`adjacent_test.go` checked length rather than capacity and preserved the backing
values:

```go
package prefix

import (
	"slices"
	"testing"
)

func TestTakeClampsToLengthNotCapacity(t *testing.T) {
	backing := []int{1, 2, 3}
	before := slices.Clone(backing)
	values := backing[:1]
	if got := Take(values, 3); !slices.Equal(got, values) {
		t.Fatalf("Take(%v, 3) = %v, want %v", values, got, values)
	}
	if !slices.Equal(backing, before) {
		t.Fatalf("backing changed: got %v, want %v", backing, before)
	}
}
```

Before the fix, `go test -count=1 -run
'^TestTakeClampsToLengthNotCapacity$' ./...` exited 1 with
`Take([1], 3) = [1 2 3], want [1]`. This is a failed assertion rather than a
panic, and prevents a capacity-based clamp from appearing correct.

## Validation

All commands ran inside the temporary fixture with `GOWORK=off`,
`GOTOOLCHAIN=local`, and `GOPROXY=off`. The fixture has no external dependencies.
To rerun an individual original case, use `go test -count=1 -json -run
'^TestTake$/^CASE$' ./...`, substituting each case name below.

| Check | Before the fix | After the fix |
| --- | --- | --- |
| `go test ./...` | Exit 1; oversized panic interrupts the suite | Exit 0 |
| `TestTake/normal` | Exit 0 individually | Passed in the full uncached run |
| `TestTake/zero` | Exit 0 individually | Passed in the full uncached run |
| `TestTake/oversized` | Exit 1; bound `[:5]`, capacity 3 | Passed in the full uncached run |
| `TestTake/negative` | Exit 1; bound `[:-1]` | Passed in the full uncached run |
| `TestTake/nil` | Exit 1; bound `[:2]`, capacity 0 | Passed in the full uncached run |
| `TestTakeClampsToLengthNotCapacity` | Exit 1 individually; returned three values from a one-value input | Passed in the full uncached run |
| `go test -count=1 -v ./...` | Not run before the fix | Exit 0; all five original cases and the additional test passed |
| `gofmt -w .` | Not run before the fix | Exit 0 |
| `go vet ./...` | Not run before the fix | Exit 0 |

The original cases check that their input values are unchanged. The additional
case checks the complete backing values. Final review confirmed that only
`prefix.go` and the new adjacent test changed in the temporary Go fixture;
the tracked exercise input and original tests were retained unchanged.

## Limits

The exercise covers a small local Go module. It does not demonstrate a live
model completing a real repository issue, native Windows/Linux execution,
remote CI, maintainer approval, or market acceptance. CLI installation and
doctor validate package delivery, not task quality.

The comparison does not grant permission to publish the fixture draft.
No unresolved failure remained
in the temporary fixture's executed checks.

## Unpublished PR draft

**Title:** Clamp prefix selection to the input length

**Summary:** Clamp `Take` to `[0, len(values)]` before slicing, and add a
length-versus-capacity regression. Keep the original tests unchanged.

**Cause:** Directly using `limit` as a slice bound panics for negative or
beyond-capacity values. It also returns elements outside the input length when
spare capacity is available. The local scenario establishes the expected clamp.

**Blast radius:** One fixture function and one additional test. No API,
dependency, allocation guarantee, or storage-sharing contract changes.

**Neighbouring behaviours tested:** Normal, zero, negative, oversized, and nil
inputs; unchanged input values; a one-value slice with capacity three; unchanged
backing values. The additional regression fails on the original implementation.

**Why this layer:** `Take` owns the slice bound, so every caller receives the
required behavior without caller-specific guards.

**Issues:** Local scenario only; no external issue number or closure claim.

**Verification:** Original `go test ./...` exits 1 at the oversized panic;
after the fix, the same command and `go test -count=1 -v ./...` exit 0.
All five original cases and the added test pass.

`gofmt -w .` and
`go vet ./...` exit 0 on macOS arm64 with Go 1.26.8. Remote CI and other
platforms were not run. This is a draft, not a published PR or approval record.
