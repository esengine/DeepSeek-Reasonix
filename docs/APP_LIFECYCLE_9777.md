# PR #9777 App lifecycle implementation checkpoint

[简体中文](APP_LIFECYCLE_9777.zh-CN.md)

## Status and scope

This is an implementation checkpoint, not a merge signoff. The current work
preserves the existing PR history and merges main-v2 `86051290b` through
`a2a4895ba`. Full App layering and native qualification remain required.

## Shared contracts implemented

- Synchronous and asynchronous commands share one layout-committed slot.
  Render cannot publish authority; cleanup immediately releases the input and
  revokes invocation. Ordinary updates preserve the epoch. StrictMode and
  hidden/revealed Suspense lifecycles revoke old work without changing the
  stable entry identity.
- Async capture is synchronous and separated from a standalone executor. The
  executor receives minimal captured input and a checkpoint authority. It must
  check before each post-await side effect. An outcome wrapper alone does not
  make existing App workflows source-safe: those workflows still need migration.
- Native ref attachment is not a business command. Transcript composes its
  kernel and entrance refs independently, preserving mutation-phase attachment
  while business command publication waits for layout commit.
- Paint receipts are unique even when the same navigation intent hydrates a
  replacement session. Successful consumption returns the original immutable
  target; App no longer substitutes the current active tab. Consumption is
  one-shot and unmount releases retained receipt/source references.
- Subscription scopes revoke all queued deliveries before unsubscribing and
  release every registration even if one cleanup throws. Terminal bridge users
  share reference-counted leases; an old cleanup cannot stop a new bridge.
- Operation-owner diagnostics are injected. Common command primitives and
  domain owners no longer import the App's browser diagnostic module.

The AST layer gate follows imports, re-exports, aliases and dynamic imports,
distinguishes type-only edges, and traverses domain/common runtime dependencies.
It checks migrated modules; it does **not** certify that the remaining App has
become a pure shell. New layer modules enable exhaustive hook dependencies.

## Evidence

| Contract | Evidence |
| --- | --- |
| Immediate unmount revocation | Old implementation executed twice instead of once; new deterministic test passes |
| Layout-started operation | Old passive setup incorrectly returned disposed; new deterministic test completes |
| Same-intent replacement paint | Old receipt reused `navigation-1`; mounted-hook test now rejects the stale receipt |
| Probe detects retained cohorts | Deliberately retained 4,096 tokens previously reported 2,048; probe now reports all live identities |
| Committed input and continuation effects | 512 updates, transition suspension, hidden/revealed Suspense, source capture, supersession and disposal pass |
| Footer masking and decisions | Production DecisionFooterRegion keeps Composer identity/draft, inert masking, Todo and rewind hosts |
| Subscriptions | Queued notification, throwing cleanup, synchronous registration disposal and overlapping terminal leases pass |

Four navigation source-string assertions were replaced by the mounted footer
test. Two relocated lazy-import assertions now inspect the owning region's AST.
This does not complete the audit of previously removed App source assertions.

Passed locally: frontend build, App lifecycle tests, existing three-layout
browser replay, Transcript unit tests, Chromium selection/scroll/composer replay,
Chromium and Playwright WebKit reader replay, frontend/test typechecks, hooks,
AST gate with negative fixtures, single writer and diff whitespace checks.

Current production bundle measurements (KiB): initial JS 418.4 / 426.8; shell
CSS 115.8 / 116.0; Chinese 60.5 / 60.6; Traditional Chinese 61.3 / 61.5; raw
initial assets 2313.5 / 2349.4. No capacity limit was increased. Settings-only
image-control CSS moved to the existing lazy settings stylesheet. The unused
labels for removed density/reasoning/fold controls were deleted in all three
locales; legacy configuration fields, setters, events and mirrors are unchanged.

## Remaining blocking evidence

- App is still 4,582 lines. Six domain owners, remaining effects, the pure shell
  and removal of its size exception are not complete.
- `test:all` currently stops at two old JSX-string assertions in
  `goal-action-errors.test.tsx`. Replace them with the real migrated Composer
  owner/interaction tests; do not just rewrite the expected punctuation.
- Repolint reports existing integrated size overruns in SettingsPanel, bridge,
  useController, desktop settings and config. The baseline has not been widened.
- Existing App browser replay checks the Sidebar project tree, not the actual
  WorkspacePanel/editor identity. Its six tracked subscriptions and zero
  instrumented operations do not represent every remaining App workflow.
- The probe retains unique weak identities and reports overflow/negative
  accounting, but the memory runner still needs build provenance, complete
  prewarming, consistent round-trip counts, 32-round sampling and retention
  analysis. Its old fixed-count/percentage checks are not qualification evidence.
- A fresh 24-cycle Transcript safety replay passed geometry gates but showed
  increasing total DOM/listener counters. These are not detached-DOM counts;
  attribution remains open. Do not infer an application leak or normal warmup
  solely from these counters.
- Formal 128/512-round, three-process App memory runs, native App soak, final
  Go/race/lint/CodeQL evidence, CI path filters and final-head checks are pending.

Continue with the common resource/UI ownership boundary and full vertical
domain migration. Do not fix subsequent cases with local timing guards,
forced remounts, cache clearing or weaker acceptance thresholds.
