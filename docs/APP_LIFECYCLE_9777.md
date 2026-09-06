# PR #9777 App lifecycle implementation checkpoint

[简体中文](APP_LIFECYCLE_9777.zh-CN.md)

## Status and scope

This is an implementation checkpoint, not a merge signoff. The current work
preserves the existing PR history, including the `5cb605a4d` mainline merge and
the `2b9aba0fb` resource/navigation implementation. The layering migration is
complete through the repolint ≤800-line per-module hard gate, and the merge
with current origin/main-v2 (exact prompt protocol, session-runtime ordering
fence, AskCard session-draft wiring) is resolved and green. Native
qualification remains required.

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

Latest checked production bundle measurements (KiB) after the main-v2 merge:
initial JS 441.0 / 441.1; shell CSS 116.4 / 116.5; Chinese 60.9 / 61.0;
Traditional Chinese 61.7 / 61.9; raw initial assets 2380.9 / 2381.0. The budget
script was ratcheted narrowly per policy: the pre-merge graph was already
0.5-1.4 over its old gate from toolchain drift, the layering split's bag
interfaces add ~13 KiB of unminifiable property names, main-v2's feature chain
adds only 0.7 KiB on the kernel-reduced startup graph, and the deferred-CSS and
locale ceilings cover pre-existing base drift. Per-item before/after
attribution lives in desktop/frontend/scripts/check-bundle-budget.mjs. No
complexity, mounted-block or drift budget was raised. Legacy configuration
fields, setters, events and mirrors are unchanged.

## Systemic repairs and diagnostic replay (September 5)

The monotonically growing DOM/listener counts had an attached-DOM retaining
path: ExternalOpener and TopicbarSessionActions were heterogeneous siblings
with the same tab key. React reconciliation left orphan ExternalOpener controls
attached. Role-scoped Fragments preserve resource remount semantics without new
DOM wrappers. Restoring the former sibling structure fails the real 128-update
component test at the first replacement (two openers instead of one).

One diagnostic process completed 32 A→B→A round trips per full/windowed/safety/
mixed phase. Before repair, each phase added 832 DOM nodes and 256 listeners.
After repair, all five samples (baseline plus four phases) remained at 5,980
nodes / 501 listeners. Detached objects did not explain that growth. The repaired
dirty build fingerprint was `f3720e364bcafaf85eed6a7bf1a797ea17b13e8a6d51fb44152240374f76aed5`;
it is not a final-head qualification. Live render-token counts were 12 at
baseline and 13 after each phase, not a new acceptance limit. Heap edges still
lead from project/navigation and workspace callbacks to retired App contexts.

Resource operations now use independent source/category channels in the shared
owner. Composer mode/profile and prompt decisions checkpoint every continuation;
prompt receipts additionally verify the original prompt ID. Remote topic rename
uses the captured session path rather than a later listing's `current` marker.
The latter defect reproduced by renaming B after a request captured A. Ordinary
data completion is distinct from UI authority, which ABA navigation cannot revive.

Preferences, onboarding, project/topic commands, terminal commands and remote
Composer actions have narrow committed inputs. A shared display-only projection
feeds Composer, Context and StatusBar; remote absence never falls back to local
telemetry, attachments, pinned files or inbox writes. This is partial vertical
migration, not certification of the remaining App.

Navigation now uses the existing coalescing queue with one standalone executor
for local topics, blank sessions, history, IM, worktrees and remote workspaces.
Queue result/error ownership belongs to each request, including old finally
delivery; remote opened events register resources but never claim navigation.
Remote and local renderers share the same surface ticket and Kernel paint
acknowledgement. A real App replay exposed remote Composer remaining disabled
after hydration because only local Transcript acknowledged navigation. The
shared target projection now derives readiness from the rendered runtime and
includes Serve generation in its paint identity. Deterministic tests reject old
Serve receipts and terminate failed remote hydration without a timeout unlock.

The production App browser replay now exercises remote project selection,
global New Session on that remote workspace, and return to local history, with
one continuously mounted Composer. Its fixture was repaired before acceptance:
remote open and ListTabs now share a catalog, and snapshot/status supply the
same complete profile. The former catalog fails the direct bridge test at its
first ListTabs refresh; no production status validation was relaxed.

Workspace commands own project preference restoration, remote explorer requests
and mode/maximize actions. Runtime polling uses a single injected-clock owner:
refreshes coalesce, dispose revokes pending delivery, and old source finally
cannot release the current request. These are shared lifecycle changes, not
additional polling delays or per-platform recovery paths.

The September 6 mainline integration preserves full-window Settings, Trash and
Automation pages in the existing region host, with the actual workspace kept
mounted and inert. Automation links use the common navigation queue and a
commit-owned page receipt; accepted targets preserve the return link, whereas
page replacement, old finally and unmount cannot revive it. Node component
discovery now shares one CSS-asset loader instead of a filename allowlist that
missed transitive imports introduced by the management shell. This does not
replace real CSS or browser verification.

Topicbar identity, rename input and source controls now live in a presentation
region with display data separate from commands. Its mounted test verifies the
original DOM structure, synchronous focus, keyboard cancellation and action
subtree identity; the production App replay still preserves Composer and actual
workspace file-preview nodes.

Controller model switching, startup/runtime-epoch restoration and send readiness
now share source-bound profile application. The owner reads the source resource's
latest layout-committed profile after a model rebuild, rather than restoring an
old render or the newly visible tab. A replacement session or disposed lifecycle
revokes continuations; A-B-A does not revive UI rights. Model and readiness paths
share the profile request channel, so both orders of a coalesced Controller failure
have one presentation owner. Deterministic mounted-hook tests also cover direct
rejection, boolean failure, remote routing and same-profile runtime replacement.
The production App replay also selects a different model through the real picker
and verifies stable hydrated block keys/content revisions, Composer draft and
writable readiness. Transient raw-Markdown fallback text is not a history oracle.
Local ordinary/direct submission and Goal activation now use one source-bound
submission owner. The render-published `commitThenSendRef` and rewind-state ref
are removed. After profile/Goal awaits, resource ownership is checked before
undo invalidation, submission or source-profile patching. Structured first-Goal
submissions retain the atomic Controller contract and exact existing trimming,
prefix and invocation semantics. The production App replay submits a real mock
turn, clicks Stop and verifies retained Composer identity and writable readiness.

Pending Plan revisions belong to an independent source/session queue owner.
Only the identical request releases its slot; replacement resources can proceed
while old transports finish. Failed revisions remain available for the existing
source-reactivation/idle-transition retry policy, but unrelated renders cannot
create a retry loop. Admission requires a committed, ready source without a pending
prompt. The queue reuses the submission executor under its own authority rather
than a nested UI command: failed resource data survives suppressed old error UI.
Disposal releases queued input synchronously. Native slash
commands, clear/steer/stop coordination and other App domains still need migration.

### Retired assertion → behavioral evidence

| Former source assertion | Production behavior exercised |
| --- | --- |
| Worktree badge JSX inside App | `topicbar-region.test.tsx` mounts isolated/ordinary topics and verifies accessible badge visibility and source-bound merge actions |
| Background profile restoration and awaited model callback text | `controller-profile-lifecycle.test.tsx` executes the real effect and model commands, including single failure ownership and direct reject semantics |
| App Goal activation, source submit and undo callback locations | `session-submission-lifecycle.test.tsx` exercises the real owner/adapter; existing `goal-activation-tab-routing.test.tsx` retains Controller/atomic bridge coverage |
| App pending-revision map and render-ref text | `pending-plan-revision-lifecycle.test.tsx` covers source queues, replacement, identical text, old finally, failure retention and disposal |
| Goal clear/mode JSX wiring | `goal-action-errors.test.tsx` mounts the real goal-command hook; failures are presented once |
| Startup snapshot, warnings, failed startup, IM reload | `desktop-preferences-lifecycle.test.tsx` checks bridge calls, revision fencing, backend authority and disposal |
| Onboarding model-access callback text | `onboarding-commands.test.tsx` checks actual overlay/dismissal state |
| General standard/deep JSX | Existing `settings-refresh-snapshot.test.tsx` mounts SettingsPanel; `session-experience-settings.test.tsx` adds busy/keyboard/same-value failure coverage |
| Theme appearance callback location | Existing direct appearance test in `theme-pack.test.ts`, plus the mounted preferences hook |
| Worktree merge callback spelling | Existing `worktree-merge-lifecycle.test.ts` tests source/intent ownership and terminal handling |
| Remote mode transaction and rollback strings | `composer-source-operations.test.tsx` checks full atomic remote arguments and prohibits local mode/goal calls |
| Remote send, slash commands and Goal runtime strings | `remote-composer-commands.test.tsx` exercises the production hooks, source continuations and unchanged submit bytes |
| Remote attachment and guidance JSX | `remote-composer-presentation.test.tsx` mounts actual Composer, rejects local file/inbox mutations and submits remote guidance |
| Remote telemetry/layout JSX | `conversation-projection.test.ts` checks the production projection and actual hidden dock presentation |
| Remote terminal JSX/shortcut strings | `terminal-panel-commands.test.tsx` drives native key events, the real action button and warm terminal host exclusion |
| Worktree creation queue and dirty-notice strings | `project-topic-lifecycle.test.tsx` and `desktop-navigation-lifecycle.test.tsx` exercise the actual queue, source operations and notice ports |
| Automation/dock visibility callback strings | `conversation-projection.test.ts` and `terminal-panel-commands.test.tsx` verify the shared projection, stored preferences and native shortcut effects |
| Remote wizard canonical path string | `remote-connect-wizard.test.tsx` finishes a merged canonical project through the actual navigation owner |
| Global remote New Session helper string | `test:app-browser` clicks the real global action after remote project selection and verifies hydrated remote content, Composer identity and return navigation |
| Mainline inline automation navigation string | `automation-navigation-lifecycle.test.tsx` checks page ABA, acceptance and exact cleanup; `desktop-navigation-lifecycle.test.tsx` proves only the winning target publishes acceptance |

These retirements do not certify every earlier removed assertion. Remaining
source-only tests must be audited as their owning features migrate.

## Remaining blocking evidence

- App layering is complete through the hard gate. `App.tsx` is a 10-line
  bridge-free composition entry (the entry contract check enforces it), and
  `AppRuntime.tsx` is a 161-line composition root: adapter destructuring,
  session identity/fence/operations, the navigation surface, store-backed
  state, three composition calls and one view render. Command domains live in
  `app-runtime/useAppSessionComposition.ts` (755 lines) and
  `app-runtime/useAppNavigationComposition.ts` (221); the returned tree lives
  in `app-shell/AppRuntimeView.tsx` (516) with ChatPaneRegion,
  TopicbarActionsStack, DockToggleButton and the footer/chrome/dock/overlay
  prop builders. The repolint global ≤800-line per-module ceiling passes for
  every App module with no App-specific exception.
  Already moved out: module-level code (`lib/sessionTitles.ts`,
  `lib/mockScenarios.ts`, `lib/todoDismissalStorage.ts`,
  `app-shell/NoticePreviewPanel.tsx`, `app-shell/HotkeyRegistrations.tsx`);
  the window-chrome lifecycle (`lib/desktopPlatform.ts`,
  `store/windowChrome.ts`, `app-runtime/WindowChromeLifecycle.tsx`,
  NativeWindowChrome deleted); shell geometry - the sidebar/right-dock/
  terminal pointer and keyboard resize lifecycles, toggleSidebar/pulse/anchor
  and every width projection now live in `app-runtime/useShellGeometry.ts`,
  with the transient drag state (resizing flags, live widths, toggle-pressed)
  on `store/layout.ts`; and the session status banner stack
  (reclaim/lease/startup-error banners, takeover dialog, config warnings,
  provider prompt, UpdateBanner) through `app-shell/SessionStatusBanners.tsx`
  with commands in `app-runtime/useSessionBannerCommands.ts`, the takeover/
  reclaim/provider gate state on `store/overlays.ts` and the startup
  onboarding probe in `app-runtime/StartupGateLifecycle.tsx`, and the
  session export vertical in `app-runtime/useSessionExportCommands.ts`
  (export commands, export popover outside-click close, theme scene), and the
  undo/rewind core in `app-runtime/useSessionUndo.ts` (per-tab rewind state
  trio, handleMessageAction/handleEditPrompt/handleSessionRevertCommitted;
  the undo banner reads the hook's returned state), the extension form
  surface in `app-runtime/useExtensionSurface.ts` (busy/submit/cancel plus
  the notification toast drain), and the tab-bar commands in
  `app-runtime/useTabBarCommands.ts` (single-flight switch queue, gated
  single/bulk close, background-runtime reveals and delivery-worktree
  continuation), and the runtime event surface in
  `app-runtime/useRuntimeEventHandlers.ts` (tab-meta registry with the
  single-flight coordinator, runtime event/ready/rebuilt listeners, remote
  status/forwards/server listeners, workspace focus reconciliation effect
  #23, now gone from App), and the command palette in
  `app-runtime/usePaletteCommands.tsx` (openPalette, the six global command
  shortcuts and the palette item builder over the session/extension/remote
  stores), and the composer command router in
  `app-runtime/useComposerRouter.ts` (shell/model/memory/clear/new routes,
  decision-mock seeds, goal-draft submits, theme commands and remote steer),
  and the trash/history commands in `app-runtime/useHistoryCommands.ts`
  (open/close/refresh plus running-gated delete and rename with local view
  filtering after backend success), and the remote workspace connection
  commands in `app-runtime/useRemoteWorkspaceCommands.ts` (launch gate,
  open-from-status, stop-and-retry connection timeouts, host-scoped
  failures). The final slices moved the clear-context chain to
  `app-runtime/useSessionClearCommands.ts`, the turn-verification reveal to
  `app-runtime/useTurnVerificationCommands.ts`, delivery continuation to
  `app-runtime/useDeliveryContinueCommands.ts`, the layout-committed active
  tab mirror to `app-runtime/activeTabMirror.ts`, the maximised sync to
  `store/windowChrome.ts` plus the `useWindowsMaximisedSync` lifecycle, the
  topic summary target/bridge/state to `app-runtime/useTopicSummary.ts`, the
  worktree merge overlay state to `app-runtime/useWorktreeMergeCommands.ts`,
  the footer ResizeObserver to `app-runtime/useFooterHeightLifecycle.ts`, the
  native settings event to `app-runtime/useNativeSettingsEvent.ts`, the todo
  panel chain to `app-runtime/useTodoPanelCommands.ts`, every composer insert
  channel to `app-runtime/useComposerInsertCommands.ts`, session control and
  navigation commands to `app-runtime/useSessionControlCommands.ts` and
  `app-runtime/useSessionNavigationCommands.ts`, window chrome commands to
  `app-runtime/useAppChromeCommands.ts`, the composer profile projection and
  patch commands to `app-runtime/useComposerProfileProjection.ts`, the
  transcript surface projection to
  `app-runtime/useTranscriptSurfaceProjection.ts`, the store subscription
  surface to `app-runtime/useAppShellStores.ts`, invocation metadata to
  `app-runtime/useInvocationMetadata.ts`, and the decision-surface/tab-strip/
  workspace-scope projections to `app-runtime/decisionSurfaceProjection.ts`,
  `projectVisibleTabs` in `app-runtime/controllerProfileOwner.ts` and
  `app-runtime/conversationProjection.ts`.
  Typechecks, layer/hooks gates and the App lifecycle and browser replays all
  pass.
- The old goal assertions and remote JSX assertions now have behavioral
  replacements. Cumulative `test:all`, App lifecycle/browser, Transcript unit,
  both Transcript browser suites, single-writer and repolint passed on this
  checkpoint. These are not final-head or native-platform qualifications.
- Repolint is clean with the App file-size exception removed: the global
  ≤800-line per-module ceiling now applies to every App module (1260
  baselined findings after the main-v2 merge; none new). No baseline was
  widened.
- App browser replay now checks actual WorkspacePanel, file tree, selected file
  and preview DOM identity across layouts, Settings visits and same-project
  session switching. This is file-preview evidence, not every editing path.
  Its six tracked subscriptions and zero instrumented operations still do not
  represent every remaining App workflow.
- The memory runner rebuilds production assets, records source/build
  fingerprints, counts completed round trips, samples every 32 and saves heap
  categories/weak identity cohorts. The captured final-structure run completed
  128 full/windowed/safety rounds plus 512 mixed rounds in three independent
  processes; all evidence, operation-release and page-error checks passed, and
  the retention classifier found no persistent post-baseline cohort. The
  classifier is a screening gate only: it blocks on persistent cohorts, native
  counter or subscription drift, and unreleased operations, while heap-retainer
  and control-build attribution remains a recorded offline duty that automated
  counters can never discharge.
- Native App soak, final Go/race/lint/CodeQL evidence and final-head checks are
  still pending. CI now invokes App lifecycle/browser/memory and the complete
  frontend suite; memory qualification is PASS for the captured Darwin/Chromium
  screening evidence but is not a substitute for native WebKit/WebView2
  evidence or offline heap-retainer attribution.

## Layering migration runbook (completed)

All slices landed with the established owner/adapter/region pattern; each
migrated one full chain (input capture, execution, result application,
failure, cleanup) with deterministic tests in the app-lifecycle pattern. The
per-slice inventory (30 effects, ~90 state/refs, 44 direct bridge call sites,
16 orchestrating handlers) is fully retired:

1. **Layout/Shell lifecycle (done)**: effects #6/#8/#11/#26/#27 and the
   platform/viewport state live in `lib/desktopPlatform.ts`,
   `store/windowChrome.ts` and `app-runtime/WindowChromeLifecycle.tsx`; the
   footer ResizeObserver (#25) is `app-runtime/useFooterHeightLifecycle.ts`,
   activeTabIdRef (#22) is the `app-runtime/activeTabMirror.ts` module
   singleton (layout-committed semantics unchanged), the maximised sync is
   `store/windowChrome.ts` state plus the `useWindowsMaximisedSync` lifecycle,
   and the three pointer-resize lifecycles with toggleSidebar live in
   `app-runtime/useShellGeometry.ts` with transient drag state on
   `store/layout.ts`.
2. **Banner/overlay stack (done)**: reclaim/lease/startup-error banners,
   takeover dialog, config warnings, provider prompt and UpdateBanner live in
   `app-shell/SessionStatusBanners.tsx` with
   `app-runtime/useSessionBannerCommands.ts`, `store/overlays.ts` and
   `app-runtime/StartupGateLifecycle.tsx`. The extension drain (#15) is
   `app-runtime/useExtensionSurface.ts`; the `app:open-settings` event (#10)
   is `app-runtime/useNativeSettingsEvent.ts`.
3. **Session actions (done)**: rewind/undo in `app-runtime/useSessionUndo.ts`
   (including handleUndoRewind), export in
   `app-runtime/useSessionExportCommands.ts`, confirmClearContext in
   `app-runtime/useSessionClearCommands.ts`, handleDeliveryContinue in
   `app-runtime/useDeliveryContinueCommands.ts`, openTurnVerification in
   `app-runtime/useTurnVerificationCommands.ts`, cancel/accept/disconnect/
   workspace-conflict/runtime-job control in
   `app-runtime/useSessionControlCommands.ts`. `goalSubmit.ts` is deleted:
   the first-Goal atomic activation is the controller `sendToTab`
   `initialGoal` branch (`SubmitInitialGoalToTabWithID`), and the send-failed
   scenarios point at `session-submission-lifecycle.test.tsx` and
   `goal-activation-tab-routing.test.tsx` on the real owner chain.
4. **Composer remainder (done)**: handleSend/handleSteer/theme routing in
   `app-runtime/useComposerRouter.ts`, every insert channel
   (composerInsertRequestsByTab, selected text, planRevisionInsert,
   workspaceInsertTarget, terminal output) in
   `app-runtime/useComposerInsertCommands.ts`, the profile projection and
   patch commands in `app-runtime/useComposerProfileProjection.ts`, and the
   mode/approval axes in `lib/useComposerModeActions.ts` (plan/approval memory
   and the YOLO toggle are internal to it now).
5. **Project/Topic (done)**: topic summary (effect #12, single-flight by
   topic identity) is `app-runtime/useTopicSummary.ts`; workspace focus
   reconciliation and the runtime event/ready/rebuilt profile restore are
   `app-runtime/useRuntimeEventHandlers.ts`; worktree merge coordination,
   including the overlay state, is `app-runtime/useWorktreeMergeCommands.ts`.
6. **Navigation remainder (done)**: tab close/reorder/change, reveals,
   delivery worktree and task-monitor sessions live in
   `app-runtime/useTabBarCommands.ts` and
   `app-runtime/useSessionNavigationCommands.ts` over
   `app-runtime/useDesktopNavigation.ts`.
7. **Preferences/Overlays (done)**: the store subscription surface is
   `app-runtime/useAppShellStores.ts`; palette in
   `app-runtime/usePaletteCommands.tsx`; the AppOverlayHost prop builders in
   `app-shell/overlayBuilders.ts`.
8. **Assembly (done)**: the chat-pane block is `app-shell/ChatPaneRegion.tsx`;
   the topicbar actions stack is `app-shell/TopicbarActionsStack.tsx` with
   `app-shell/DockToggleButton.tsx`; footer/chrome/dock prop assembly lives in
   `app-shell/decisionFooterBuilders.ts`, `app-shell/chromeRegionBuilders.ts`
   and `app-shell/dockRegionBuilders.ts`; `AppRuntime.tsx` is a 161-line
   composition root and the returned tree is `app-shell/AppRuntimeView.tsx`.
   Module-level residue (setRemoteComposerProfileForSessionAction, lazy
   loaders) moved with their owning modules. The repolint ≤800-line
   per-module hard gate passes with no App exception.

Every retired App-source string assertion got a behavioral replacement or a
retarget to the owning module's source in its own slice. goalSubmit.ts is
deleted. Bundle budgets were ratcheted narrowly per policy (see the Evidence
section); no test, gate or threshold was weakened.

### Assembly slice (8): final state and the architectural boundary

Final structure: `App.tsx` is the 10-line bridge-free composition entry;
`AppRuntime.tsx` (161 lines) owns the controller adapter, session
identity/fence/operations, the navigation surface and store-backed state,
then calls the session and navigation compositions and renders the view.
`app-runtime/useAppSessionComposition.ts` (755 lines) runs the session/
composer domain hooks in the App body's original order;
`app-runtime/useAppNavigationComposition.ts` (221) runs the
history/navigation/chrome domains; `app-shell/AppRuntimeView.tsx` (516) is
the pure prop-driven tree.

Architectural boundary (verified, unchanged): useController is a
single-instance hook whose state lives in no store, so no region component
can self-derive controller-backed views. The "App ≤ 200 line pure
composition" target is still not reachable under the current architecture:
it requires first store-ifying controller state — an independent long-running
epic beyond this PR's scope. Note the distinction: that aspirational target
remains open, while the repolint ≤800-line per-module hard gate this PR was
gated on is met.

The merge with current origin/main-v2 adopted the exact prompt protocol
(`resolvePromptForTab` + `handlePromptFailure` + the
`runtimeStatusSnapshotIsStale` freshness owner) inside this branch's
tab-scoped `*ForTab` prompt handlers; conflict resolution kept the layering
structure while taking main-v2's shared helpers at every equivalent point.
After the merge the full discovery runner passes 298/298 suites, including
the six suites that carried pre-existing failures on this branch.

Continue with the common resource/UI ownership boundary and full vertical
domain migration. Do not fix subsequent cases with local timing guards,
forced remounts, cache clearing or weaker acceptance thresholds.
