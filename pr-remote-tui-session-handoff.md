## Summary

Complete remote TUI session mirroring, ownership handoff, reclaim, and
session-identity-safe transcript hydration.

This PR depends on #10205 and must be merged after the previous PR.

## Issues

Ordinary TUI resume can acquire a session lease without registering a Serve
mirror. Desktop then treats the session as occupied and read-only, cannot
reclaim it reliably, and may load stale history or receive HTTP 409 responses.
Remote bootstrap can also select an incompatible CLI for the target platform
or break compatibility with an existing Serve installation.

Depends on #10205 — merge the previous PR first.

## Verification

- `git diff --check ef9cebf75..HEAD`
- `go test ./internal/serve ./internal/remote/bootstrap`
- `go test ./internal/cli -run 'TestCLIOrdinaryResumeRegistersMirrorAndReturnsLease|TestCLIOrdinaryMirrorDiscoveryRetriesAndRejectsWrongGrant|TestCLIAdoptionSerializesWithOrdinaryResumeAcquisition'`
- `cd desktop && go test ./... -run 'TestRemote|TestReclaim|TestRuntimeStateReconcile'`
- `cd desktop/frontend && pnpm exec tsx src/__tests__/session-takeover-ui.test.tsx`
- `cd desktop/frontend && pnpm exec tsx src/__tests__/remote-connect-wizard.test.tsx`

## Documentation impact

Documentation-impact: updated - updated `docs/REMOTE_SESSIONS.md` and
`docs/REMOTE_SESSIONS.zh-CN.md` for TUI sharing, ownership, reclaim
capability, and transcript synchronization.

## Cache impact

Cache-impact: none - no cache-sensitive prompt, memory, skill, tool, or
provider-visible surface changed.
Cache-guard: `bash scripts/check-cache-impact.sh` - the changed-file scan
contains no cache-sensitive files.
System-prompt-review: N/A - no provider-visible system prompt or standing
instruction changed.
