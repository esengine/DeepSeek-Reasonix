# Reasonix project memory

This file is loaded into every session's system prompt (the cache-stable prefix),
so keep it concise and durable — it is the project's standing instructions to the
agent. It is the Reasonix analog of Claude Code's CLAUDE.md.

## Conventions

- Go kernel under `internal/`; each package owns one concern. A package's long
  explanation belongs in its `doc.go`, not spread across implementation files.
- One transport-agnostic `control.Controller` sits behind every frontend (chat
  TUI, HTTP/SSE serve, Wails desktop). Add behavior to the controller, not a
  frontend, so all three inherit it.
- Layering (enforced): utility packages import nothing under `reasonix/` except
  one another — that layer is closed, so a leaf reaching a leaf drags no graph
  along, while refusing it leaves one judgement copied into several of them; only
  the frontends `cli`, `serve`, `acp`, `bot`, `botruntime`, `boot` and the hosts
  `cmd/`, `desktop/` may import `control`; nothing below a frontend may import
  one. The declared sets live in `tools/repolint/layers.go`.
- Subagent delegation keeps five concepts apart: a profile says how a worker
  thinks, `TaskSpec` what this call wants, `CapabilityGrant` what it may touch,
  `ContextRequest` what it starts from, `SchedulerPolicy` when it runs. Put a
  field in whichever member decides its value — profiles carry ceilings, never
  per-call values. `internal/agent/profile_boundary_test.go` enforces it.
- Cache-first: the system-prompt prefix (base prompt, tools, declared prefix
  configuration) must stay byte-stable across turns so DeepSeek's automatic
  prefix cache stays warm. Never mutate it mid-session — ride the turn tail
  instead (see `control.Compose`), under the rules in *Context projection*.
- Performance features land with an effect test at their final boundary
  (`internal/boot/effect_test.go` pattern): assert what actually reaches the
  provider request, frontend sink, or trajectory through the real `boot.Build`
  assembly. Component correctness is not system effectiveness.
- A mutex- or atomic-guarded struct is ratcheted on its **scalar** field count
  (`struct-state`), not its total: independent flags multiply into states no
  type records as legal. Fixing a boundary case by adding one more `bool` is
  the move this blocks — group by lifetime into a named sub-state instead
  (`agent.perTurnState` is the pattern), which costs one field and removes the
  whole product.
- Judgements read structure, never wording: shell ASTs (`shellparse`), types,
  contracts, tool schemas. Phrase tables and message sniffing have been retired
  in batches (task-policy prose, the planner's approval phrases, the executor
  handoff tables) because each one misfires on real input, and a misfiring
  judgement inside a gate is worse than no gate. Security allow-lists are the
  exception and stay. Something that can only be built by matching words does
  not get built — say what is missing instead of guessing at it.
- The other half of that rule: an error a caller must tell apart **carries an
  identity** — a sentinel, a typed code — never a sentence. Only the producer
  knows a deadline expired rather than a socket closed, and a message is where
  that knowledge goes to die: the reader is left matching words, or with
  nothing. Across HTTP the identity is a dotted code through `refuse`;
  `refusal-path` fails a new `http.Error` in `internal/serve`, and the parity
  test fails a code the frontend cannot say. Inside Go it is `errors.Is` on a
  sentinel the wrapping preserves; `error-text` fails a new match against an
  error message, following it one hop into a local because storing the text
  first is what the direct form turns into.
- Failure attribution is host-owned state. When the host can determine why an
  operation failed or stopped — truncation, timeout, unavailable context,
  dependency skipping, policy refusal, output spill, argument parsing — that
  cause goes to the model. Surfacing only the downstream symptom leaves it to
  invent one, and the invented rule outlives the incident: a response cut at the
  output limit reaches the model as "invalid arguments", and the session learns
  to stop batching calls that were never the problem.
- A host-owned identity has to cross the model-visible boundary. Stable step
  ids, typed error codes, boundary causes — anything the host expects the model
  to cite must appear wherever that state is rendered for it. Telling the model
  in a schema to prefer X while every concrete rendering shows only Y trains it
  to use Y; `complete_step` said `step_id` PREFERRED while all four todo
  renderers printed bare ordinals. Canonical dynamic state still stays out of
  the cache-stable prefix — project the identities needed to act on it at the
  turn tail.
- A metric is its definition, not its name. Before a number becomes a finding,
  state what it counts, from which anchor, over which population, and what it
  excludes. Three readings in one study died in that gap: a command of unknown
  extent read as a proven write, the latest report's adjudication read as every
  report's, and a syntactically accepted call read as a closed one. Each time a
  convenient proxy had been left standing in for the state it was named after,
  and each time the mechanism story built on it was wrong. A number without
  those four lines is an observation, not a result.
- A constructed state is not a reachable one. Before a destructive test or a
  counterfactual is reported as a defect, say who in production produces that
  state, through which transitions, across which persistence boundary, under
  whose authority, and whether a real run has been seen reaching it. A test that
  assigns a private field proves the mechanism *given* the state and must be
  reported as exactly that: `a.projectChecks` is written once at boot and never
  again, so reassigning it stood in for a resume rather than demonstrating one,
  and the bypass built on it was never shown to be production-reachable.
  Severity is reachability times frequency times authority, not "can be
  bypassed".
- A wire type the desktop re-declares by hand is held to the kernel's by
  `wire-parity`, both directions: a field one side sends and the other cannot
  read fails, and so does one it reads that nothing sends. The two are compiled
  by different toolchains and meet only over JSON, so nothing else in the tree
  notices when one of them grows a field. The pairs are declared in
  `tools/repolint/wireparity.go`, never inferred — no spelling tells a mirrored
  contract from a struct that merely carries json tags.
- "Add behavior to the controller so every frontend inherits it" is held by
  `frontend-parity`: a capability one frontend already drives, a frontend whose
  declared port carries it, and no call, is a missing edge, and the ratchet
  counts edges. Which port each frontend drives is declared in
  `tools/repolint/frontendparity.go`; a deliberate exception goes in the scope
  table there with an issue and a reason, and costs what the edge cost, so the
  table cannot become where debt disappears. The matrix is read from
  `go/types` at one pinned build target, never from selector names — `Close`
  and `Status` are capabilities and also stdlib methods, and a name match
  records `os.File.Close` as a frontend driving `Lifecycle.Close`, a false
  "wired" that hides real debt. How a verb is spelled — `/branch`,
  `POST /branches` — is presentation and stays out of it.

## Comments

Default is none — the code is the truth. Write one only when the **why** is
non-obvious: a hidden constraint, a workaround anchored to something verifiable,
an invariant the type system cannot express, or an external-protocol quirk.

- Declaration doc: ≤5 lines. Package comment: ≤8 lines, or ≤40 in a `doc.go`.
- Every other comment: ≤3 lines. Struct-field and trailing `//`: 1 line.
- Never: restatements of the code, phase/stage narrative, incident or
  conversation history, section banners, commented-out code, `@param` lists.
- `TODO(#nnn):` and `HACK(#nnn):` need the issue anchor. `FIXME` is banned.
- One responsibility per file; 800 lines is the ceiling. Test files are exempt:
  their length tracks how many cases they cover, not how many concerns they
  carry, so splitting one only scatters a subject's table across files.

`go run ./tools/repolint` enforces all of it against a ratchet baseline: recorded
debt is tolerated, anything new fails CI. Never widen the baseline to land a
change — fix the code. `-update` lowers budgets freely and refuses to raise one
without `-allow-widen`, so carrying debt through a rename or an extraction is
asked for in the command and justified in the PR. A clean run also reports
budget the tree stopped using: it is only reclaimed by an `-update`, and until
then it is room a file can grow back into.

## Context projection

Canonical dynamic state — the skill registry, the memory store — lives in its own
owner, and what the model sees is a projection of it onto each request. The
prefix was never where these belonged wrongly; freshness ownership was missing.

- **Prefix ownership.** Only declared prefix configuration may move the
  cache-stable prefix; ordinary memory writes, skill writes and runtime facts
  ride the turn. Pinned memory bodies are the one declared exception, because
  pinning is a request to pay one cold start.
- **Projection freshness.** When canonical state changes, the next eligible turn
  carries the new projection or can say why it is not owed. "Right after a fold,
  or in the next session" is not an answer.
- **Projection ownership.** Whoever owns a dynamic fact owns the invalidation
  that makes its projection owed again. A projection kept fresh because every
  writer remembered to announce itself is one forgotten call from stale —
  `skillSet.owedCatalog` asks the registry rather than waiting to be told.
- **Determinism.** The same canonical state and the same projection state
  produce byte-identical model-visible bytes.
- **Detection is semantic.** Freshness may not rest on writer notification, on
  mtime, or on which call path made the change. The registry answers from
  canonical state, so a switch flipped and a file edited past every API are the
  same question, and a change no projection renders is not a change.

`internal/boot/context_projection_test.go` holds all five at the provider
boundary. One row is open: standing instructions reach a live session only
through the writers that publish them, so an edit to `REASONIX.md` made outside
them waits for the next build. The skills listing was that shape until it
started asking.

## Memory

- Standing instructions are hierarchical: committed/shared `REASONIX.md`,
  `AGENTS.md`, and `CLAUDE.md`; personal `*.local.md` variants; matching files in
  ancestor directories; and user-global files under the memory state root
  (`REASONIX_STATE_HOME`, otherwise `REASONIX_HOME`, otherwise `~/.reasonix` on
  macOS/Linux or `%APPDATA%\reasonix` on Windows). All distinct supported files
  in a directory load; `AGENTS.md` is not merely a fallback.
- `@path` on its own line imports another file's contents.
- `#<note>` in chat quick-adds an always-on instruction. The `remember` tool
  instead saves a fallible background fact (frontmatter file + `MEMORY.md`
  index). Fact `type` classifies content; independent `scope` controls whether it
  is project-only (the default) or explicitly global. The index loads into the
  stable prefix on the next session; global user/feedback bodies also load as
  lower-priority compatibility guidance. The current turn receives a tail note.

## Notes

One tool call costs one model round trip, so calls that do not depend on each
other belong in the same round: several reads, edits to different files, a check
after the edit that enables it. Several edits to the *same* file are one
`multi_edit` — it applies them against each other in memory and rewrites the
file only if all of them land, so a failure midway leaves nothing half-edited.

## Pre-push CI simulation

Run these **before every commit** to catch the fastest CI failures locally:

```bash
gofmt -w .                          # catches gofmt (saves ~13s CI)
go vet ./...                        # catches vet warnings (saves ~52s CI/lint)
make lint                           # golangci-lint at CI's pin + repolint
go test ./internal/tool/builtin/ ./internal/boot/  # catches tool/boot test breaks
```

`make lint` runs both gates CI runs, at the version in `.golangci-version`;
`make lint-install` installs it. Do not skip it: a `modernize` finding never
shows up in `go vet`, and the CI round trip that catches it instead costs ten
minutes.

A full repolint run reports every file over budget, including debt your change
never touched. To see only what your own edits owe:

```bash
make check
```

Repo-wide ceilings still report under `-only`, because a file can push the tree
past one without exceeding its own budget.

Run it through `make`, not as `go run ./tools/repolint -only ...`. The host
recognizes a `make` target as a check whose result it can read, while an
arbitrary `go run ./tools/...` is a program it has to assume writes — so only
the `make` form can be cited as evidence that this gate passed.

## Import cycle rule

Before importing a new internal package from a non-test file, verify the target package's **test files** aren't already importing back to you:

```
# BAD: agent(_test.go) → tool/builtin(sessions.go) → agent  → setup failed
```

Use `go test ./path/to/target/` to detect cycles **before** pushing. A `[setup failed]` message means a cycle exists.

## PR hygiene

- **One force-push per round of review feedback.** Multiple force-pushes destroy review history and confuse reviewers.
- **Keep the PR diff minimal.** Only the files relevant to the PR's purpose — no stray changes from other branches.
- **Amend, don't add commits, for review feedback** — keeps the commit history clean.

## Reasonix host checks

The paths below decide what the agent is allowed to do — a wrong change here
is not a bug in a feature, it is a hole in a boundary. Declaring them makes
every change under them demand `review` plus `security_review` before a turn
can finish. Sensitivity is declared, never inferred: the host does not read it
out of how a path is spelled, which cannot tell `internal/auth` from
`session_write_authority.go`, or a trace file from a data race.

- sensitive: internal/permission/**
- sensitive: internal/sandbox/**
- sensitive: internal/shellsafe/**
- sensitive: internal/shellparse/**
- sensitive: internal/control/approval.go
- sensitive: internal/control/approval_orchestration.go
- sensitive: internal/providerbroker/**
- sensitive: internal/installsource/**
- sensitive: internal/plugin/**
- sensitive: internal/pluginpkg/**
- sensitive: internal/netclient/**
- sensitive: internal/redirectguard/**

One declaration, two effects: the same list is also the coverage gate's subject.
`make coverage-gate` holds each path to the coverage it already has, because
when this was written these paths sat *below* the tree's median rather than
above it — the code deciding what the agent may execute was its least-tested
part. Floors ratchet like repolint's budgets: raising one is free, lowering one
needs `-allow-drop` and a reason in the PR. Adding a `sensitive:` path gets both
effects; there is no second list to keep in sync.

## Trust domain

Three sets, kept apart because conflating them turns a performance number into a
security claim: **VerificationRelevant** (state that can change whether a
delivery claim holds), **Observable** (state the host can reconcile after an
action, so it can say what that action did), **HostProtected** (state a
capability/sandbox boundary makes unwritable). The invariant is
`Writable ∩ VerificationRelevant ⊆ Observable ∪ HostProtected`:

> Any state that can both be modified by an action and influence the delivery
> claim must either be observable by the host or protected from modification by
> host-enforced boundaries. An incomplete observation never establishes scope.

Effects and state answer different questions and never stand in for each other —
observation proves what an action did, verification proves what the resulting
state satisfies — which is why `unproven_mutation` is settled only by
observation, never by a check that ran afterwards.

What follows bounds Observable today. It is not a restatement of the invariant:
moving a number here must not move the promise.

- `scanWorkspace` walks the workspace recursively, skips VCS store dirs, and is
  `complete` only below 50k files. Over that, or past a walk error, it
  establishes nothing and the mutation stays unproven.
- Symlinks are recorded by `Lstat`, so a write through one lands outside what
  the walk compares. Confining the resolved target is the sandbox's job.
- `internal/sandbox` confines writes to the workspace, configured extras, temp
  and toolchain caches — on macOS and Linux. Windows has no OS-level bash
  sandbox, so there nothing the host enforces bounds `Writable`.
- The governed host authorities are a *named* set — ssh-agent, docker, podman.
  DBus, Wayland, X11, gpg-agent, keyrings and arbitrary sockets stay reachable,
  so the claim is "these endpoints are governed", never "host IPC is isolated".

`stateEpoch` tracks host-observed mutations, not an exact filesystem snapshot;
unobserved external writers are a gap shared by the whole verification model. Go
baseline test criteria use the toolchain's build overlay and therefore share
ordinary verification's epoch semantics; the overlay is not a general filesystem
view.

A captured criterion may be superseded only by authority outside
execution-mutable state; the current workspace, its tests, and the agent itself
cannot authorize supersession.

Two gaps follow and are open, not decided: a build that reads `.git` makes VCS
state VerificationRelevant while the scan skips it, and on Windows a symlink out
of the workspace is neither observed nor protected. Closing either means
widening observation or narrowing writability — never widening what a check is
taken to prove.

## Sandbox dimensions

`HostProtected` above is the delivery-level abstraction. Sandbox policy refines
it into three independent protection dimensions. Their separation is empirical:
measured bypasses showed that protection on one dimension cannot be assumed to
imply protection on another.

- **Integrity** — what host state may be mutated. Expressed by write roots.
- **Confidentiality** — what host information may be observed. Expressed by
  `ForbidReadRoots`.
- **Authority** — what host capabilities may be exercised *without* possessing
  their secrets. Expressed by `HostAuthorities`.

`ReadOnly` is therefore an Integrity property only; it makes no Confidentiality
or Authority claim.

Network transport and host authority are independent even when a backend
primitive conflates them: on macOS, `(deny network*)` also cut ssh-agent, so one
flag could not express "no internet, still sign".

Filesystem visibility is not authority: denying `file-read*` on a socket did not
stop a connect. Possession is not authority: the key can be unreadable while an
agent still signs with it.

A protection claim exists only where a backend has a demonstrated enforcement
primitive for that dimension. Unsupported dimensions stay explicitly unsupported
— Windows currently enforces none of the three — because a claim that outruns
its enforcement is worse than an absent one: it is believed.

**Known unmeasured surface — macOS host-mediated delegation:** Apple Events and
Mach-service lookup are not denied by the current allow-default profile. Prior
`osascript` and `launchctl submit` probes were inconclusive; no delegation
effect has been demonstrated. This surface is outside the currently governed
host-authority endpoint set.
