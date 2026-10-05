---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Local issue fixture

| Item | Requirement |
| --- | --- |
| Target | The user's temporary copy of this package's `fixture/` directory. |
| Input | `prefix.Take` should return the first `limit` values, clamping a negative limit to zero and an oversized limit to the input length. |
| Invariant | The input is not modified. The result may share the input's storage. |
| Trigger | The bundled `go test ./...` initially panics when `limit` exceeds the input length. |
| Scope | Correct `Take` and keep the existing tests; no new dependency or API is needed. |
| Acceptance | Existing tests pass for a normal limit, zero, negative, oversized, and nil input. The handoff uses `delivery.md`. |

1. Record the initial test failure before editing. A panic is a failed check,
   not evidence that a check was skipped.
2. Fix the behavior in the temporary fixture and rerun the same command.
   The PR draft must cite the observed before/after results and the final diff.
3. If Go is unavailable, report the missing prerequisite and the unrun check.
   Do not claim the fix is verified or silently install a toolchain.
4. Remove the temporary fixture after preserving any exercise output the user
   wants. Do not delete an unrelated repository or publish a real PR.
