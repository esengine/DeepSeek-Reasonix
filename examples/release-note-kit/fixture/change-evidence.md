---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-06
---

# Prefix example: selected change evidence

1. This is a fixed local authoring exercise. Its audience is contributors to
   the prefix example. RN-01 is the only selected change. No release version,
   public commit URL, publication approval or deployment record is supplied.
2. The source and observed checks come from the repository's issue-fix example
   exercised in a temporary copy. Its original fixture remains intentionally
   unfixed. This record describes the copy's patch, not a change to Reasonix's
   runtime or to the original issue-fix fixture.
3. Read the complete installed `skills/release-note/references/format.md`
   before drafting. Cite the evidence by section; RN-01 is a local record ID,
   not a Git commit hash or remote pull request number.

## RN-01: selected change

The original `prefix.go` is:

```go
package prefix

func Take(values []int, limit int) []int {
	return values[:limit]
}
```

The temporary copy's complete production patch is:

```diff
--- a/prefix.go
+++ b/prefix.go
@@ -3,0 +4 @@
+	limit = max(0, min(limit, len(values)))
```

The resulting function is:

```go
package prefix

func Take(values []int, limit int) []int {
	limit = max(0, min(limit, len(values)))
	return values[:limit]
}
```

| Input | Requested limit | Result after the patch |
| --- | --- | --- |
| `[1, 2, 3]` | `2` | `[1, 2]` |
| `[1, 2, 3]` | `0` | Empty slice |
| `[1, 2, 3]` | `5` | `[1, 2, 3]` |
| `[1, 2, 3]` | `-1` | Empty slice |
| `nil` | `2` | Empty slice |
| Length `1`, capacity `3`, visible value `[7]` | `3` | `[7]`, without exposing capacity beyond the length |

1. The patch clamps the requested limit to the inclusive range from zero to
   the current slice length. Normal and zero limits keep their existing
   behavior. The returned slice still refers to the input's storage.
2. It introduces no new error, allocation, configuration or dependency. It
   does not prove any other slicing call was audited or repaired.

## Observed checks

1. The local exercise ran on macOS. Before the patch, the existing oversized,
   negative and nil cases panicked. The existing normal and zero cases passed.
   The added length-versus-capacity case failed because the old helper exposed
   three elements from a slice whose current length was one.
2. After the patch, `go test -count=1 -v ./...` in the temporary fixture passed
   the five existing cases and the one added boundary case. Existing cases
   also checked that the input values were not changed. The observed output
   was:

```text
=== RUN   TestTakeClampsToLengthNotCapacity
--- PASS: TestTakeClampsToLengthNotCapacity (0.00s)
=== RUN   TestTake
=== RUN   TestTake/normal
=== RUN   TestTake/zero
=== RUN   TestTake/oversized
=== RUN   TestTake/negative
=== RUN   TestTake/nil
--- PASS: TestTake (0.00s)
    --- PASS: TestTake/normal (0.00s)
    --- PASS: TestTake/zero (0.00s)
    --- PASS: TestTake/oversized (0.00s)
    --- PASS: TestTake/negative (0.00s)
    --- PASS: TestTake/nil (0.00s)
PASS
```

3. This is a fixture-level observation. It supplies no native Windows/Linux,
   integration, production deployment, live-model or independent-review
   result. A release draft must keep those limits with the check claim.

## Unselected idea

A possible future change would return a new independent copy of the prefix.
That idea has no implementation or test evidence in the selected patch. It
does not belong in this release draft. The current returned slice's storage
relationship must not be described as changed.
