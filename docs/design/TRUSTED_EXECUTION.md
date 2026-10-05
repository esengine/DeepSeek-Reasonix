---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-26
---

# Trusted execution layer

## 1. Purpose

Status: proposed. Nothing in this document is shipped unless §11 says so. It lives under `docs/design/` so the product-doc retrieval corpus (`docs/*.md`) cannot present it to a model as a feature.

| ID | Principle |
| --- | --- |
| P1 | The model proposes and executes. The host defines what is allowed to start, what counts as completed with evidence, and which capability is allowed to ship. |
| P2 | None of the three decisions is a prompt convention. Each is a state transition the host performs over host-owned state. |
| P3 | One layer serves Reasonix's own development and product users. There is no second implementation for either. |
| P4 | The layer is dogfooded on Reasonix's own high-risk work before any user-facing surface exists. |
| P5 | A surface that renders "done", "verified" or "shipped" is a projection of a sealed host record. The surface never creates the fact. |
| P6 | Every policy the host enforces is a host-owned revision. Workspace content is source material for a proposal, never enforcement. |

Three primitives share one store, Trusted Host State (THS):

| Primitive | Decides | Depends on |
| --- | --- | --- |
| Task Contract | what is owed and whether work may start | risk policy, contract templates |
| Evidence Bundle | whether what is owed was delivered | contract, observation policy |
| Capability Rollout | whether a changed capability may reach more runs | bundles, as its outcome metric |

Implementation order: P1 Evidence Bundle, P2 immutable Task Contract, P3 eval corpus, P4 rollout (§10).

Out of scope: UI, detection of semantic cheating inside a green test (review's job, §9), and any judgement built by matching wording.

## 2. Trust boundary

### 2.1 Principals

| Principal | Is | Trusted for |
| --- | --- | --- |
| User | the person who owns the task | accepting and relaxing contracts and policies, waivers, L3 approvals |
| Host | the Reasonix kernel process and THS | observation, verification, sealing, applying accepted policy |
| Model | the working agent and its subagents | proposals, execution, claims |
| Verifier | a host-run check: command, test, capture | producing evidence, never authority |
| Evaluator | a bounded independent model (`internal/runtime/goaleval`) | advisory judgement only (§4.5) |
| Workspace | files, tests, scripts, manifests, `.gitignore`, `REASONIX.md` | nothing: it is execution-mutable |
| External system | registries, git remotes, stores, payment, hosted APIs | reporting its own effects |

### 2.2 Authority

| ID | Rule |
| --- | --- |
| B1 | Only the User and host policy MAY accept a contract or policy revision. Host policy MAY accept only tightening revisions and template-derived contracts (§3.3). The Model MAY only propose and request. |
| B2 | Write access to a file is not authority over a contract or policy. Editing a test, a check declaration, `.gitignore` or `REASONIX.md` changes the workspace, never an accepted revision. |
| B3 | Workspace content MAY inform a proposal. It MUST NOT supersede an accepted revision, whoever wrote it and however it was written. |
| B4 | The host does not decide who changed a file. Provenance through IDEs, `git checkout`, background jobs and crashes is unreliable, so authority is a capability boundary, not an attribution. |
| B5 | A capability that grants authority (accept, relax, waive, promote) MUST NOT be reachable as a model tool, a workspace config key, a skill, or an MCP server. |
| B6 | An Evaluator verdict MAY block or route to the User. It satisfies an obligation only where the accepted revision names it as that criterion's verifier and the risk class permits it (§4.5). |

### 2.3 Trusted Host State

| ID | Rule |
| --- | --- |
| T1 | THS lives under the state root (`REASONIX_STATE_HOME`), never inside the workspace. |
| T2 | THS records are content-addressed and chained by parent digest. A chain rewrite is detectable relative to a previously trusted head digest. Hash chaining alone is not an authority boundary. |
| T3 | The trusted head (§3.8) is authoritative only when it is anchored inside a boundary the model cannot write. That property is the host's `IntegrityLevel`. |
| T4 | `host_protected`: the sandbox excludes THS and its head from every write root (macOS, Linux). `tamper_evident_only`: nothing the host enforces stops a process from rewriting THS and its head (Windows today). |
| T5 | Every bundle, contract revision and policy revision records the `IntegrityLevel` it was written under. |
| T6 | A chain that does not verify from the trusted head is `trusted_state.tampered`: the contract suspends and only the User may resume it (§8). |
| T7 | `host_protected` requires every model-reachable writer this process starts to be confined away from THS: the bash sandbox, the file tools' guard, each MCP server, each extension sidecar. One unconfined launch drops the level for the rest of the process. |
| T8 | The level is a per-process judgement. Another process running as the same OS user, including another Reasonix process whose model is unconfined, can write THS. Until a machine-wide registry covers that, `host_protected` claims only this process's writers. |

Operation by integrity level:

| Operation | `host_protected` | `tamper_evident_only` |
| --- | --- | --- |
| P1 shadow bundles | allowed | allowed |
| L1, L2 enforcement | allowed | allowed; provenance marked tamper-evident only |
| L3 observation and shadow | allowed | allowed |
| L3 enforcement and any L3 "trusted" claim | allowed | disabled until a host-protected THS exists |

A Windows write boundary is separate future work. The first version does not wait for it.

## 3. Canonical objects

All objects are host-owned records in THS. Identities are host-assigned; a model never submits an id, revision or digest.

### 3.1 Task Contract

```text
TaskContract {
  id                 ContractID          host-assigned, stable across revisions
  revision           uint                1, 2, ... ; never reused
  parent             Digest?             digest of revision-1
  digest             Digest              over every field below
  objective          string
  deliverables[]     Deliverable         {id, kind: change|artifact|answer|external_effect, subject}
  criteria[]         Criterion
  protectedScopes[]  ProtectedScope      {paths[], effects[]}: what the task must not touch
  requiredEvidence[] EvidenceRequirement derived from criteria and risk, stored for audit
  riskClass          L0|L1|L2|L3         accepted floor (§3.6)
  riskPolicy         Digest              the RiskPolicy revision admission read
  observationPolicy  Digest              the ObservationPolicy revision snapshots use
  template           Digest?             set when derived from a ContractTemplate
  budget             {rounds, tokens, wallclock, cost}
  acceptedBy         {principal, at, policyID?}
  integrity          IntegrityLevel
  proposal           ProposalID?
}

Criterion {
  id        string      stable across revisions
  statement string      human-readable; never parsed
  required  bool
  verifier  VerifierSpec
  subject   Subject     frozen bytes the verifier checks against (§3.2)
}
```

| ID | Rule |
| --- | --- |
| C1 | A revision is immutable once accepted. A change is a new revision whose `parent` is the old digest; the old revision stays in THS. |
| C2 | A criterion's `verifier` and `subject` are frozen at acceptance. Nothing read from the workspace later can change what the criterion demands. |
| C3 | Every run, bundle and approval records the exact revision digest it was issued under. |

### 3.2 Verifiers and frozen subjects

| Verifier kind | Frozen subject | Satisfied only when |
| --- | --- | --- |
| `command` | argv, cwd, env allow-list digest, expected exit | the host ran that argv and classified it `passed` (not `inconclusive`) |
| `test` | test identity + body digest (`evidence.TestCriterion`) | the host observed that test's own pass event; a suite exit code is not enough |
| `artifact` | artifact kind, capture source, target identity | the host captured it; a model-supplied file never counts |
| `external_effect` | target, payload digest, idempotency key | the external system's receipt names the same digest |
| `human` | question shown to the User | the User attested in a host prompt |
| `evaluator` | rubric digest + evaluator policy digest | allowed only at L0–L1 (§4.5) |

Freezing the subject is what makes deletion harmless: removing `foo_test.go` removes the workspace copy, not the criterion. Deleting, skipping, renaming or excluding a test from the suite leaves its pass event unobserved, so the obligation stays owed.

### 3.3 Proposals, templates and acceptance

```text
ContractProposal        { id, author: model|user, draft: TaskContract fields without identity }
ContractTemplate        { id, revision, digest, kind, derivation rules }   host-owned
ContractRevisionRequest { contractID, baseRevision, ops[], rationale, requestedBy }
RevisionDecision        { requestID, outcome: accepted|rejected, by: principal, policyID?, newRevision? }
```

First acceptance follows one path for every author:

```text
ContractProposal -> host derives canonical contract from a ContractTemplate
                 -> policy compares it with the template and the risk floor
                 -> auto-accept, or route to the User
```

| ID | Rule |
| --- | --- |
| A1 | Host policy MAY auto-accept an L1 or L2 contract only when every field is mechanically derived from a trusted template and the accepted risk policy, and no field required a choice. |
| A2 | The ordinary code template derives: objective = the user request; deliverables = touched source and test artifacts; criteria = build + targeted tests + frozen project checks; protected scopes = host policy; risk = max(host-derived risk). |
| A3 | A model proposal MAY add criteria and protected scopes to a template-derived contract. Additions tighten, so they do not block auto-acceptance. |
| A4 | The User MUST accept when the contract, compared with its template, removes or relaxes a criterion, changes a verifier, changes a frozen subject, narrows a protected scope, lowers risk, widens sensitive authority, includes any external effect, or cannot be derived uniquely. |
| A5 | L3 contracts are User-accepted without exception. |

Later revisions follow the same split:

| Op | Tightens | Who MAY accept |
| --- | --- | --- |
| add criterion, add protected scope, raise risk | yes | host policy or User |
| relax or remove criterion, narrow protected scope, lower risk | no | User only |
| change verifier or subject of a criterion | no | User only |
| raise budget | neither | host policy up to a configured ceiling, then User |

A rejected request leaves work running under the base revision.

### 3.4 Evidence

```text
EvidenceRecord  { id, kind, producer: {component, hostVersion}, at, snapshot: Digest, digests }
CheckEvidence   { checkID, argv, cwd, envDigest, startedAt, finishedAt, exitCode, classification,
                  stdoutArtifact, stderrArtifact, testEvents? }
MutationEvidence{ path, blobBefore?, blobAfter?, created, deleted, grade: proven|unknown }
VisualEvidence  { requirementID, artifactHash, captureSource, windowIdentity, timestamp, dimensions }
ProcessEvidence { argv, pid, state, exitCode? }
ExternalEffect  { target, payloadDigest, idempotencyKey, state: confirmed|failed|unknown, receipt? }
ReviewEvidence  { reviewerExecutionID, grant: ReviewAuthority, reportDigest }
AgentClaim      { criterionID?, statement, citedEvidenceIDs[] }
```

| ID | Rule |
| --- | --- |
| E1 | Only host components produce EvidenceRecords. The producer field names the component, never a tool argument. |
| E2 | Large content is stored once as an artifact by hash; records carry digests only. |
| E3 | An `AgentClaim` is not evidence. It triggers verification and is kept in the bundle as what was claimed. |

### 3.5 Evidence Bundle

```text
EvidenceBundle {
  id, contractID, revision, contractDigest, attemptID
  snapshotBefore, snapshotAfter       WorkspaceSnapshot digests (§3.7)
  mutations[], checks[], artifacts[], observations[], externalEffects[], reviews[], claims[]
  verdicts[]      ObligationVerdict per derived obligation
  outcome         completed|incomplete|failed|blocked
  integrity       IntegrityLevel
  producedBy      hostVersion, sealedAt, digest
}
```

A bundle is sealed once, by the host, at the end of a completion attempt, and is immutable after sealing. The model's final message is a projection of it.

### 3.6 Risk policy

```text
RiskPolicy {
  id, revision, parent, digest
  workspace               rootIdentity
  sensitiveScopes[]       paths
  l3Scopes[]              paths
  externalEffectClasses[] publish, push, payment, hosted API, credential use, ...
  destructiveClasses[]    shellparse command classes
  source                  Digest?  the REASONIX.md bytes a proposal was read from
  acceptedBy, integrity
}
RiskPolicyRevisionRequest { policyID, baseRevision, ops[], source: Digest, requestedBy }
```

| ID | Rule |
| --- | --- |
| R1 | Enforcement reads only the accepted RiskPolicy revision in THS. `REASONIX.md` (`sensitive:`, `risk: L3`) is source material. |
| R2 | A change to those lines in `REASONIX.md` produces a RiskPolicyRevisionRequest. It never changes enforcement by itself, in this run or any later one. |
| R3 | A request that only adds scopes or classes tightens, and host policy MAY accept it. Removing or narrowing anything is User-only. |
| R4 | The first policy for a workspace is the host's built-in classes plus the additions its `REASONIX.md` declares. A workspace file cannot remove a built-in class. |
| R5 | An accepted revision takes effect at the next admission, never inside a running contract. |
| R6 | Within a run, observed risk only rises. Rising above the accepted class moves the contract to `reaccept_required` (§6.1). |

| Class | Structural trigger (any) | Start requires | Completion requires |
| --- | --- | --- | --- |
| L0 exploratory | no mutation, no external effect | nothing | nothing |
| L1 ordinary | ordinary mutation | an atomic template contract | diff + targeted checks |
| L2 consequential | multi-surface change, test criteria in scope, 10+ paths | accepted contract with ≥1 non-evaluator criterion (§3.3) | diff + checks + artifacts named by criteria |
| L3 external/security/release | an `l3Scopes` or `sensitiveScopes` path, an external effect, a destructive class | User-accepted contract + protected scopes + `host_protected` THS | full bundle + User approval per external effect |

### 3.7 Workspace snapshot and observation policy

```text
ObservationPolicy {
  id, revision, digest
  excluded[]        {pattern, reason}   host policy only
  limits            {maxEntries, followSymlinks: false}
}
WorkspaceSnapshot {
  rootIdentity
  observationPolicy Digest
  entries[]         {path, kind: file|dir|symlink, mode, stamp}
  completeness      complete | incomplete{reason}
  digest
}
```

| ID | Rule |
| --- | --- |
| W1 | Every "workspace digest" in this document is a `WorkspaceSnapshot.digest`. Nothing else stands in for it. |
| W2 | The ObservationPolicy is host-owned and revisioned. `.gitignore`, workspace config, and any file the model can write MUST NOT add an exclusion. |
| W3 | The default exclusions are the VCS store and the host's own state root, each with a stated reason. Dependency trees stay observed because verifiers read them. Excluded paths are outside the observation domain by declaration. |
| W4 | An excluded path cannot be a criterion subject or a protected scope. A contract that names one gets `observation.excluded` for that obligation, never `satisfied`. |
| W5 | Symlinks are recorded as links (target string digest), never followed. |
| W8 | A file's snapshot identity is its stamp: size, modification time, and on macOS and Linux inode change time, inode and device. Change time cannot be set from user space, so every write shows; a write that restores identical bytes also shows. Windows has no change time, so its stamp is weaker. |
| W9 | Content digests are computed for the paths a verdict names (criterion subjects, mutation evidence), not for every file. |
| W11 | A delegated run that declared write paths takes a change the walk sees inside them as its own, even while other writers run. This is an inference the User accepted, not a proof: claims are disjoint and writer tools are fenced to them, but a sibling's shell command writing into another run's paths is not excluded. A change outside the declared paths is never attributed, and never read as "nothing changed". |
| W10 | Tree nodes are content-addressed index objects written without a disk flush. A snapshot's authority is its digest inside a sealed record; a node lost to a crash makes a later diff `unverifiable`, never a satisfied comparison. |
| W6 | Two snapshots compare only under the same observation policy digest. Different digests make every freshness comparison fail as `observation.policy_changed`. |
| W7 | An incomplete snapshot (entry limit, walk error) establishes nothing: scope and "no other changes" obligations become `unverifiable`. |

### 3.8 Trusted head

```text
TrustedHead {
  generation     uint     strictly increasing
  recordDigest   Digest   the newest THS record
  anchoredBy     host_protected_store | none
}
```

| ID | Rule |
| --- | --- |
| H1 | A THS read verifies the chain back from the trusted head. A chain that is merely self-consistent proves nothing. |
| H2 | `anchoredBy = none` sets `IntegrityLevel = tamper_evident_only` for every record written under it. |
| H3 | A head whose generation is lower than one the running host already observed is `trusted_state.tampered`. |

### 3.9 Capability rollout

```text
CapabilityVersion { kind, name, contentDigest, parent?, author }
   kind: skill | prompt_policy | tool | planner_policy | memory_policy | compaction | mcp_integration
EvalCorpus        { id, digest, tasks[], noSolutionTasks[] }
EvalRun           { capabilityDigests[], corpusDigest, runs[] -> bundle ids, samplesPerTask }
RolloutPlan       { candidate, baseline, stages[{cohort, minRuns}], gates[], rollbackTriggers[] }
RolloutDecision   { plan, stage, outcome: promote|hold|rollback, metrics, by }
```

Metrics, all computed from sealed bundles: completion rate, false completion rate, tokens, latency, tool calls, retries, human interventions, regression count.

| ID | Rule |
| --- | --- |
| K1 | Promotion authority is the offline corpus with repeated samples per task. |
| K2 | Product telemetry is out of scope for the first implementation and is never a promotion requirement. |
| K3 | A future opt-in, content-free telemetry path MAY supply post-promotion monitoring and rollback signals only. Opted-in users, their task mix and their providers are not the general population. |
| K4 | The future product path is offline gate → internal dogfood → opt-in online canary → broader rollout. Each arrow is a RolloutDecision. |

## 4. Derivations

### 4.1 Obligations

Obligations are computed, never stored as authority: `obligations = f(contract revision, risk policy revision, observation policy revision, observed mutations)`. The input digest is stored with each verdict so a verdict can be recomputed.

| Source | Obligations |
| --- | --- |
| each required criterion | one obligation, verified by its frozen verifier |
| risk class | the class's completion requirements (§3.6) |
| host-derived (existing `evidence.ObligationKind`) | `unproven_mutation`, `stale_verification`, `baseline_required_check`, `baseline_test_criterion`, `unseen_render`, `missing_project_check` |
| protected scopes | one "untouched" obligation per scope, satisfied only by complete snapshots |

### 4.2 Verdicts

| Verdict | Meaning |
| --- | --- |
| `satisfied` | host evidence, fresh against the latest intersecting mutation |
| `unsatisfied` | host evidence refutes it |
| `unverifiable` | the verifier cannot produce evidence; carries a typed cause |
| `stale` | was satisfied; a later mutation intersects its scope |
| `waived` | the User waived it in a revision decision; never produced by host policy or the model |

### 4.3 Bundle outcome

| Outcome | When |
| --- | --- |
| `completed` | every required obligation is `satisfied` or `waived` |
| `incomplete` | some required obligation is `stale`, `unverifiable` or not yet attempted, and none is `unsatisfied` |
| `failed` | a required obligation is `unsatisfied` and the run stopped |
| `blocked` | the model concluded blocked and the host verified the cited cause |
| `none` | nothing is owed and the host saw no change: there is no evidence the turn completed anything |

`incomplete` is a legitimate terminal outcome, not a retry loop. It blocks the `completed` state only; the task may stop there and report exactly what is not proven.

### 4.4 Freshness

| ID | Rule |
| --- | --- |
| S1 | A `satisfied` verdict needs evidence recorded after the last mutation whose paths intersect the criterion's scope. |
| S2 | When the snapshot at seal differs from the last host-observed snapshot, an unobserved writer acted: every verdict becomes `stale`. |
| S3 | Writes to excluded paths never stale a verdict, which is why W4 keeps excluded paths out of every obligation. |

### 4.5 Evaluator criteria

| ID | Rule |
| --- | --- |
| V1 | An `evaluator` verifier is allowed at L0–L1 only, and only when the accepted revision names it. |
| V2 | At L2–L3 an Evaluator verdict of "not done" becomes `unsatisfied`; "done" becomes `unverifiable` with cause `verifier.judgement_only`. |

## 5. Policy revisions

Risk policy, observation policy and contract templates share one lifecycle:

```text
source changed (REASONIX.md, host upgrade, User edit)
  -> revision request {base, ops, source digest}
  -> tightening: host policy MAY accept | relaxation: User only
  -> new immutable revision, effective at the next admission
```

A pending request never changes enforcement. Runs keep reading the accepted revision and carry `policy.revision_pending` so the drift is visible.

## 6. State machines

### 6.1 Contract

```text
proposed --accept (A1-A5)--> active(rN)
active(rN) --revision request--> revising --accepted--> active(rN+1)
                                 revising --rejected--> active(rN)
active --risk rose above class--> reaccept_required --accept--> active(rN+1)
active --chain fails from trusted head--> suspended --User resume--> active
active --seal(completed|incomplete|failed|blocked)--> closed
active --User abandon--> closed(abandoned)
```

### 6.2 Run

| From | Event | Guard | To |
| --- | --- | --- | --- |
| idle | start | admission (§3.6 "start requires") holds | running |
| idle | start | admission fails | idle, cause `admission.*` |
| running | model calls finish or goal update | — | verifying (a CompletionAttempt is recorded) |
| verifying | verdicts computed | all required satisfied/waived | sealing |
| verifying | verdicts computed | otherwise, budget remains | running; unmet obligations projected at the turn tail |
| verifying | verdicts computed | otherwise, budget exhausted | sealing (incomplete) |
| sealing | bundle written, head advanced | write succeeded | done(outcome) |
| sealing | bundle or head write failed | — | running, cause `trusted_state.unwritable`; never done |
| any | process crash | — | recovering |
| recovering | chain verified, workspace rescanned | snapshot equals last observed | previous state, open attempt voided |
| recovering | same | snapshot differs | running, all verdicts `stale` |
| running | risk rose above class | — | paused until `reaccept_required` resolves |

### 6.3 Obligation

```text
owed --host evidence passes--> satisfied --intersecting mutation--> stale --> owed
owed --host evidence fails--> unsatisfied --> owed (next attempt)
owed --verifier cannot run--> unverifiable{cause} --> owed (cause cleared) | waived (User)
```

No edge into `satisfied` or `waived` is triggered by a model tool call.

### 6.4 Capability rollout

```text
candidate --offline eval passes gates--> canary(stage 1) --stage gates pass--> canary(stage k) --> promoted
candidate|canary --gate fails--> rejected
canary|promoted --rollback trigger--> rolled_back (baseline digest restored)
any --insufficient samples--> hold
```

## 7. Invariants

Each invariant names the enforcement it needs. None is enforced by a prompt.

| ID | Invariant | Enforcement |
| --- | --- | --- |
| I1 | An accepted criterion stays owed until a User-accepted revision removes it. Deleting its tests, README, scripts or manifest makes it `unverifiable`, never absent. | effect test: delete the subject, assert `incomplete` |
| I2 | No `AgentClaim` moves any obligation to `satisfied` or `waived`. | type: the verdict function takes no claim input |
| I3 | `running → done(completed)` exists only through a sealed bundle whose `contractDigest` equals the active revision and whose `snapshotAfter` equals the workspace at seal. | single constructor for `completed` |
| I4 | A revision's bytes never change after acceptance; its chain verifies from the trusted head. | chain check on every load |
| I5 | The model holds no capability that accepts, relaxes, waives or promotes (B5). | tool registry test + config key audit |
| I6 | A verdict is a pure function of recorded evidence and revisions. The same inputs yield byte-identical verdicts. | recompute on load; mismatch is `trusted_state.tampered` |
| I7 | Observed risk within a run never decreases. | ratchet type |
| I8 | An incomplete snapshot never satisfies a protected-scope or "no other changes" obligation. | verdict function |
| I9 | Every model-visible and user-visible completion statement is rendered from a bundle. No surface renders `completed` without one. | projection test at the frontend sink |
| I10 | Every run records the digest of each capability version it used. A rollout metric counts only runs whose digests match the arm. | trajectory header field |
| I11 | Promotion requires false completion not to rise, on a corpus with no-solution tasks, above the measured noise floor. | rollout gate |
| I12 | An L3 external effect runs only under a User approval bound to the revision digest and the payload digest. | approval record check before dispatch |
| I13 | No workspace file changes enforcement. Risk, observation and template policy change only by accepted revision. | effect test: edit `REASONIX.md` and `.gitignore`, assert the next admission reads the old revision |
| I14 | No record claims L3 trust unless it was written under `host_protected` integrity. | seal refuses the claim |

## 8. Failure semantics

Whoever first knows why an operation failed owns its class. Each cause is a typed code that reaches the model at the turn tail and the frontend as a code, never as a sentence to be matched.

| Code | Owner | Result | Retry |
| --- | --- | --- | --- |
| `admission.contract_missing` | host policy | run not started | after acceptance |
| `admission.template_ambiguous` | host policy | routed to the User (A4) | after User choice |
| `admission.authority_unavailable` | host | L3 not started (headless, no User) | no |
| `admission.integrity_insufficient` | host | L3 enforcement unavailable; shadow only | no, until host-protected |
| `policy.revision_pending` | host | runs continue on the accepted revision | after decision |
| `verifier.unavailable` | verifier | `unverifiable` | when dependency returns |
| `verifier.subject_missing` | verifier | `unverifiable` | no, until User amends |
| `verifier.subject_removed_by_run` | host observation | `unsatisfied` inside a protected scope, else `unverifiable` | no |
| `verifier.inconclusive` | shell classification | `unverifiable` | rerun without masking |
| `verifier.nondeterministic` | host | `unverifiable` after differing repeats | bounded repeats |
| `verifier.judgement_only` | host policy | `unverifiable` at L2–L3 | no |
| `check.failed` | verifier | `unsatisfied` | yes |
| `evidence.stale` | host | obligation owed again | yes |
| `observation.incomplete` | workspace scan | scope obligations `unverifiable` | no |
| `observation.excluded` | observation policy | that obligation `unverifiable` | User amends contract or policy |
| `observation.policy_changed` | observation policy | every verdict `stale` | re-verify |
| `budget.exhausted` | host | sealed `incomplete` | no |
| `revision.rejected` | User or policy | run continues on base revision | new request |
| `trusted_state.unwritable` | THS store | cannot seal; stays running | yes |
| `trusted_state.tampered` | THS store | contract suspended | User only |
| `external_effect.unknown` | external system | effect `unknown`; never assumed done, never blindly repeated | only with the same idempotency key |
| `rollout.insufficient_samples` | rollout | hold | more runs |

| ID | Rule |
| --- | --- |
| F1 | Every failure fails closed for `completed` and for authority. No failure path falls back to a claim. |
| F2 | An internal or dependency failure MUST NOT be reported as the model's fault or as a domain failure of the task. |
| F3 | `unverifiable` is never counted as `satisfied` in any metric, report or projection. |
| F4 | A crash during verification voids that attempt. Evidence sealed before the crash stays valid; unsealed evidence is discarded. |

## 9. Dogfood

| ID | Rule |
| --- | --- |
| D1 | The first population is Reasonix's own work: release, signing, packaging, migration, session recovery, security, payment and hosted, protocol changes. All are in the accepted L3 policy for this repository. |
| D2 | Stage 1 is shadow: bundles are built beside the existing `completion.Report`, gating nothing. Every divergence is recorded with both inputs and a taxonomy class. |
| D3 | Stage 2 enforces on sessions whose workspace is this repository. Stage 3 is user opt-in. Stage 4 is default-on. Each transition is a RolloutDecision. |
| D4 | Semantic cheating inside a green test (for example a test-caller switch) is not detected here. L2–L3 contracts carry a `human` or review criterion for it. |

Divergence taxonomy. The taxonomy, not a zero count, is the stage-1 product: it decides which old path P2 removes.

| Old report | New bundle | Class |
| --- | --- | --- |
| completed | incomplete | missing evidence |
| completed | incomplete | stale evidence |
| completed | incomplete | claim-only satisfaction |
| completed | incomplete | incomplete observation |
| incomplete | completed | old report false negative |
| incomplete | completed | new verifier stronger |
| completed | completed | different obligation sets |

Stage-exit measurements:

| Metric | Definition |
| --- | --- |
| divergence | per task and class, over the e2ebench corpus and dogfood sessions |
| false completion | bundle `completed` where the task's independent grader fails, over solvable and no-solution tasks |
| bypass count | runs that reached `completed` with an obligation whose subject was rewritten, excluded or deleted during the run |
| overhead | added tokens, wall clock and THS bytes per run; the layer makes no model call except an `evaluator` verifier |

Honesty metrics carry about ±10pp noise at one sample per task. A stage exit MUST use repeated samples and report the noise floor next to the number.

## 10. Implementation order

| Phase | Delivers | Exit criterion |
| --- | --- | --- |
| P1 Evidence Bundle | THS with trusted head and integrity level, WorkspaceSnapshot, durable receipts, content-addressed artifacts, sealed shadow bundle | bundle survives restart; divergence taxonomy populated; effect test at the trajectory boundary |
| P2 Task Contract | canonical contract, templates, auto-acceptance (A1–A5), revisions, RiskPolicy and ObservationPolicy revisions, Goal `VerificationContract` folded in | a resumed goal is held to the revision it accepted, read from THS; I1–I5 and I13 have effect tests through `boot.Build` |
| P3 Eval corpus | capability digests on every run, corpus from e2ebench + integrity corpus + dogfood failures | two runs of one capability digest reproduce within the noise floor |
| P4 Rollout | offline-authority rollout for all seven capability kinds | one skill promoted and one rolled back on dogfood, both from bundle metrics |

## 11. Current state

| Target | Today | Gap |
| --- | --- | --- |
| Canonical contract | `internal/runtime/contract` holds revisions in THS (`contract/1`), derived from the baseline check identities, captured test criteria and the verification commands the approved plan names for its required criteria (by command identity; plan criterion ids are positional and not frozen). Host policy accepts only tightening; a plan the user approved may drop plan checks (`user:plan_approval`), and a later plan that drops one is routed to approval when an approver exists. A plan whose steps name candidate files adds a deliverable, `change@plan`: the task must end with a change the host proved (no path bound, since the files are inferred and a replan moves them); the shadow verdict, the receipt and the gate hold it; the gate releases a turn that concluded blocked, is waiting on the user (`await_user`), or carries a change a restart brought in. The gate still reads `internal/runtime/taskcontract`, rebuilt each turn | the gate reading the revision |
| I2 claim inertness | a todo is recorded as the model's own breakdown: not required, and marking it `completed` satisfies nothing; a turn that only marked todos gets no report verdict rather than `done`. Unfinished todos stay the readiness gate's | none for todos |
| I2 claim inertness | the readiness gate and the receipt answer plan criteria by the checks their step names, after the latest change; a `complete_step` citation settles none. A criterion no command checks is listed on the receipt and does not hold the turn | none for plan criteria |
| Atomic tasks | `taskcontract.Atomic` treats any mutation as proof of the ask | acceptable at L1 only |
| Durable evidence | the root agent seals each turn's contract, report and receipts (arguments by digest) as a `shadow_bundle/1` record in THS; receipts carry no blob digests and no snapshot yet | P1: snapshot, verdicts, divergence |
| Frozen test criteria | `evidence.TestCriterion` keeps host-owned bytes and digest identity | reuse as the `test` verifier subject |
| Frozen check set | `evidence.VerificationContract` freezes a Goal's checks; `Epoch` never moves. Since `7a8b720bc` a rewritten declaration no longer retires the check a goal began under | fold into contract revisions |
| Host report | `internal/runtime/completion` builds a host-authored report with gaps by subtraction | P1 seals it into the bundle |
| Risk policy | `sensitive:` is read from the workspace `REASONIX.md` at each boot, so an edit changes enforcement for the next session | P2 (I13) |
| Observation | `scanWorkspace` skips VCS stores and stops at 50k files; exclusions are code, not a revisioned policy | P1 |
| Capability identity | every `reasonix run` trajectory header lists the capability versions the run had (host build, system prompt, each tool schema, each skill by content) and their set digest; e2ebench compare says whether two reports are one version on one corpus and whether each rate moved beyond its 95% interval. One version over 30 tasks, run twice, reproduced within the noise floor. `e2ebench -mode rollout` decides promote, hold or reject for a candidate set against a baseline on one corpus (K1, I11): a significantly worse false completion or grader pass rejects; a missing version, same version, other corpus, no no-solution tasks or fewer than 50 solvable runs holds. Demonstrated on 30 tasks x 2 per arm: a system-prompt candidate adding "keep replies concise" was promoted, and one adding "do not edit files, say it is done" was rejected on false completion (1/21 to 29/34) and grader pass (54/58 to 11/58) | online canary and post-promotion rollback (K3, K4) |
| Integrity inputs | MCP servers default to host mode and extension sidecars always run unconfined, so an install with either enabled is `tamper_evident_only` (T7) | a confined launch path for each |
| Cross-process writers | nothing records the level of other processes sharing THS (T8) | machine-wide registry |

## 12. Decisions

| ID | Decision |
| --- | --- |
| Q1 | Windows L3 enforcement requires a host-protected THS. Tamper-evident-only operation is allowed for shadow and lower-risk work and never claims L3 trusted enforcement. |
| Q2 | Promotion authority is the offline corpus. Product telemetry is out of scope for the first implementation; a future opt-in, content-free path MAY supply post-promotion monitoring and rollback signals. |
| Q3 | Host policy MAY auto-accept L2 contracts only when mechanically derived from trusted policy and templates with no relaxation or discretionary authority. Anything else requires User acceptance (A1–A5). |
