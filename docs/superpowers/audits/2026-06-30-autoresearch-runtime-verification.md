# AutoResearch Runtime Verification Matrix

Date: 2026-06-30

Scope: host-managed AutoResearch runtime, controller integration, desktop API,
frontend visibility, and focused end-to-end verification.

## Summary

AutoResearch has moved from a prompt convention to a host-managed runtime for
the implemented scope. The host now creates and resumes task state under
`.reasonix/autoresearch/<task-id>/`, injects a runtime summary into active goal
turns, records heartbeats and stale progress, records accepted evidence from
structured assistant evidence blocks, gates completion through default required
criteria, and exposes desktop/frontend status APIs with compact tab metadata.

The implementation is verified against the design requirements listed below.
The only residual limitation is that an external browser smoke test cannot call
the Wails `window.go` bridge directly; bridge behavior is covered by Go and
TypeScript tests, and the running Wails app was smoke-tested through `./dev`.

## Requirement Matrix

| Requirement | Evidence | Status |
| --- | --- | --- |
| Host creates task id and directory layout | `internal/autoresearch.Store.CreateTask`; `TestCreateTaskCreatesHostOwnedLayoutAndInitialState` | Verified |
| State files are host-owned JSON/JSONL under `.reasonix/autoresearch/<task-id>/` | `task_spec.json`, `progress.json`, `directions_tried.json`, `findings.jsonl`, `iteration_log.jsonl`, `heartbeat.jsonl`; store tests and `./dev` smoke task directory | Verified |
| Schema validation rejects missing required fields | `ValidateTask`; `TestValidateTaskReportsSchemaErrors` | Verified |
| JSONL append rejects invalid entries | `AppendFinding`; `TestAppendFindingRejectsInvalidEntry` | Verified |
| Accepted findings satisfy readiness | `Readiness`; `TestAppendFindingRecordsAcceptedEvidenceForReadiness`, `TestRecordEvidenceLinksFindingToCriterionAndSatisfiesReadiness` | Verified |
| Direction repetition increments stale count and pivot threshold | `RecordDirection`; `TestRecordDirectionIncrementsStaleAndRequiresPivot` | Verified |
| Controller computes stale/pivot instead of relying on prompt | `recordAutoResearchTurnProgress`; `TestResearchGoalTurnUpdatesAutoResearchStaleProgress` | Verified |
| Per-task concurrent writes are serialized | `Store.lockTask`; `TestConcurrentDirectionWritesAreSerializedPerTask` | Verified |
| `/goal --research` creates task and persists `AutoResearchTaskID` | `SetGoalWithResearchMode`; `TestResearchGoalCreatesHostManagedAutoResearchTask` | Verified |
| Explicit `.reasonix/autoresearch/<task-id>/` resumes existing task | `ResumeFromGoalText`; `TestResumeFromGoalTextLoadsExplicitTaskPath` | Verified |
| Missing explicit task path blocks instead of silently creating a new task | `ensureAutoResearchTask`; `TestResearchGoalMissingExplicitTaskBlocksInsteadOfCreatingNewTask` | Verified |
| Cold resume restores running AutoResearch task id | `goalMachine.restoreRunningFromState`; `TestResumeRestoresRunningAutoResearchGoalFromSidecar` | Verified |
| Compose injects host-generated `<autoresearch-runtime>` | `Controller.Compose`; `TestResearchGoalCreatesHostManagedAutoResearchTask`, resume test | Verified |
| Heartbeats are written around turns | `appendAutoResearchHeartbeat`; `TestResearchGoalTurnAppendsAutoResearchHeartbeats`; `./dev` smoke heartbeat log | Verified |
| Summary includes last heartbeat | `Summary`; `TestAppendHeartbeatRecordsDurableTurnStatus` | Verified |
| Completion is intercepted when readiness fails | `autoResearchReadinessFailure`; `TestResearchGoalCompletionIsInterceptedWhenReadinessFails` | Verified |
| Completion requires host-readable evidence for default criteria | `defaultAutoResearchSuccessCriteria`; `TestResearchGoalCreatesHostManagedAutoResearchTask`, `TestResearchGoalCompletionMarksAutoResearchTaskComplete` | Verified |
| Completion/blocked goal updates AutoResearch status | `finalizeAutoResearchTask`; `TestResearchGoalCompletionMarksAutoResearchTaskComplete`, `TestResearchGoalBlockedMarksAutoResearchTaskBlocked` | Verified |
| Host controller records structured evidence | `RecordAutoResearchEvidence`; `TestControllerRecordsAutoResearchEvidence` | Verified |
| Assistant replies can record structured evidence without changing tool schemas | `<autoresearch-evidence>` parser; `TestResearchGoalCompletionMarksAutoResearchTaskComplete` | Verified |
| AutoResearch evidence recording does not alter the default provider-visible tool surface | `TestAutoResearchEvidenceDoesNotChangeDefaultToolSurface`; boot tool contract tests | Verified |
| Desktop API exposes current/status/list/findings/open/record evidence | `desktop/app.go`; `TestAutoResearchStatusSurfaceForActiveTab`, `TestAutoResearchFindingsAreLoadedOnDemand`, `TestAutoResearchListReturnsWorkspaceTasks`, `TestAutoResearchOpenTaskRevealsTaskDirectory`, `TestAutoResearchRecordEvidenceThroughDesktopAPI` | Verified |
| Tab metadata includes compact summary only | `compactAutoResearch`; `TestAutoResearchStatusSurfaceForActiveTab` | Verified |
| Findings load on demand and cap by limit | `AutoResearchFindings`; `TestAutoResearchFindingsAreLoadedOnDemand` | Verified |
| AutoResearch does not appear as a configurable default status bar item | `statusbar-workspace.test.tsx` | Verified |
| Frontend bridge tolerates transient Wails IPC timing errors | `bridge-drag-rejection.test.ts` | Verified |
| Transcript lifecycle cards exist | Controller emits `event.Notice`; frontend renders notices via `NoticeCard`; `./dev` smoke shows `autoresearch task created` notice | Verified |
| `./dev` starts and AutoResearch creates durable state | Browser smoke against `http://127.0.0.1:34193`; task `20260630-065721-e2e-verify-autoresearch-ui-smoke-test` created | Verified |

## Commands Run

```bash
GOCACHE=/private/tmp/reasonix-go-build-cache go test ./internal/autoresearch ./internal/control -count=1
GOCACHE=/private/tmp/reasonix-go-build-cache go test . -run 'TestAutoResearch|TestMetaReportsGoalStatus' -count=1
GOCACHE=/private/tmp/reasonix-go-build-cache go test . -count=1
./node_modules/.bin/tsc --noEmit -p tsconfig.test.json
./node_modules/.bin/tsx src/__tests__/statusbar-workspace.test.tsx
pnpm test
./dev
```

`tsx` tests required running outside the sandbox because `tsx` creates IPC pipes
under `/var/folders`.

## End-to-End Smoke Notes

- `./dev` started successfully.
- Wails devserver: `http://127.0.0.1:34193`.
- External browser loaded the page with title `Reasonix`.
- Browser console had no error/warn entries.
- Submitted `/goal --research e2e verify autoresearch ui smoke test`.
- The transcript rendered `autoresearch task created:
  20260630-065721-e2e-verify-autoresearch-ui-smoke-test`.
- Host state files were created under `.reasonix/autoresearch/...`.
- Cancelling the smoke goal wrote heartbeat warning `context canceled`.
- Known Wails dev terminal noise remains: `runtime:ready -> Unknown message from
  front end: runtime:ready`.

## Remaining Risk

- The external browser smoke test cannot directly call Wails `window.go`; bridge
  behavior is covered by Go/TS tests and the running Wails desktop app, but not
  by direct browser-side method calls.
- The implementation intentionally avoids background daemon behavior; AutoResearch
  advances only during normal Goal turns.
