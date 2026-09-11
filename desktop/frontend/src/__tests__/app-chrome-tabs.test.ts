// Run: tsx src/__tests__/app-chrome-tabs.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createBoundedRefreshCoordinator, sameTabMetaLists, shouldRefreshTabMetaForEvent, tabMetaFallbackDelay } from "../lib/tabMetaRefresh";
import type { TabMeta } from "../lib/types";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../AppRuntime.tsx"), "utf8"), workspaceFocusSource = readFileSync(resolve(testDir, "../lib/workspaceRefreshStore.ts"), "utf8");
const commandPaletteSource = readFileSync(resolve(testDir, "../components/CommandPalette.tsx"), "utf8");
const projectTreeSource = readFileSync(resolve(testDir, "../components/ProjectTree.tsx"), "utf8");
const topicShortcutsSource = readFileSync(resolve(testDir, "../lib/topicShortcuts.ts"), "utf8");
const topicShortcutOwnerSource = readFileSync(resolve(testDir, "../app-runtime/useTopicNavigationShortcuts.ts"), "utf8");
const runtimeHandlersSource = readFileSync(resolve(testDir, "../app-runtime/useRuntimeEventHandlers.ts"), "utf8");
const sessionNavigationSource = readFileSync(resolve(testDir, "../app-runtime/useSessionNavigationCommands.ts"), "utf8");
const chromeCommandsSource = readFileSync(resolve(testDir, "../app-runtime/useAppChromeCommands.ts"), "utf8");
const desktopNavigationSource = readFileSync(resolve(testDir, "../app-runtime/useDesktopNavigation.ts"), "utf8");
const dockToggleSource = readFileSync(resolve(testDir, "../app-shell/DockToggleButton.tsx"), "utf8");
const chatPaneSource = readFileSync(resolve(testDir, "../app-shell/ChatPaneRegion.tsx"), "utf8");
const transcriptSurfaceSource = readFileSync(resolve(testDir, "../app-runtime/useTranscriptSurfaceProjection.ts"), "utf8");
const desktopNavigationOwnerSource = readFileSync(resolve(testDir, "../app-runtime/desktopNavigationOwner.ts"), "utf8");
const appViewSource = readFileSync(resolve(testDir, "../app-shell/AppRuntimeView.tsx"), "utf8");
const transcriptSource = readFileSync(resolve(testDir, "../components/Transcript.tsx"), "utf8");
const composerSource = readFileSync(resolve(testDir, "../components/Composer.tsx"), "utf8");
const controllerSource = readFileSync(resolve(testDir, "../lib/useController.ts"), "utf8"), forkWorktreeSource = readFileSync(resolve(testDir, "../lib/forkWorktree.ts"), "utf8");
const bridgeSource = readFileSync(resolve(testDir, "../lib/bridge.ts"), "utf8");
const workspacePanelSource = readFileSync(resolve(testDir, "../components/WorkspacePanel.tsx"), "utf8");
const rewindCommitSource = readFileSync(resolve(testDir, "../lib/rewindCommit.ts"), "utf8");
const layoutStoreSource = readFileSync(resolve(testDir, "../store/layout.ts"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function matchingBlocks(selector: string): string[] {
  const blocks: string[] = [];
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let match: RegExpExecArray | null;
  while ((match = rule.exec(stylesSource)) !== null) {
    const selectors = match[1].split(",").map((part) => part.trim());
    if (selectors.includes(selector)) blocks.push(match[2]);
  }
  return blocks;
}

function finalDeclaration(selector: string, property: string): string | undefined {
  let value: string | undefined;
  for (const block of matchingBlocks(selector)) {
    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let match: RegExpExecArray | null;
    while ((match = declaration.exec(block)) !== null) {
      value = match[1].trim();
    }
  }
  return value;
}

console.log("\napp chrome tabs");

const tabMeta = (overrides: Partial<TabMeta> = {}): TabMeta => ({
  id: "tab-1",
  scope: "project",
  workspaceRoot: "/repo",
  workspaceName: "repo",
  topicId: "topic-1",
  topicTitle: "Topic",
  label: "model",
  ready: true,
  running: false,
  cancellable: false,
  mode: "normal",
  active: true,
  cwd: "/repo",
  ...overrides,
});

ok(sameTabMetaLists([tabMeta()], [tabMeta()]), "identical tab metadata suppresses redundant state writes");
ok(!sameTabMetaLists([tabMeta()], [tabMeta({ running: true })]), "runtime tab changes still invalidate metadata state");
ok(tabMetaFallbackDelay("visible") === 15_000, "visible tab metadata fallback runs at low frequency");
ok(tabMetaFallbackDelay("hidden") === 60_000, "hidden tab metadata fallback backs off further");
ok(shouldRefreshTabMetaForEvent("turn_started"), "turn start refreshes tab runtime metadata immediately");
ok(shouldRefreshTabMetaForEvent("approval_request"), "approval prompts refresh tab runtime metadata immediately");
ok(!shouldRefreshTabMetaForEvent("text_delta"), "stream deltas do not trigger tab-list requests");

{
  const coordinator = createBoundedRefreshCoordinator<TabMeta[]>(2);
  const first = deferred<TabMeta[]>();
  const second = deferred<TabMeta[]>();
  let loads = 0;
  const firstRefresh = coordinator.run(() => {
    loads += 1;
    return first.promise;
  });
  const secondRefresh = coordinator.run(() => {
    loads += 1;
    return second.promise;
  });
  const saturatedRefresh = coordinator.run(() => {
    loads += 1;
    return Promise.resolve([]);
  });
  await Promise.resolve();
  ok(loads === 2, "tab metadata refresh caps outstanding backend calls");

  const latestTabs = [tabMeta({ id: "tab-latest" })];
  second.resolve(latestTabs);
  const saturatedResult = await saturatedRefresh;
  ok(saturatedResult.coalesced, "saturated tab metadata refresh joins the newest request");
  ok(saturatedResult.value === latestTabs, "saturated tab metadata refresh returns authoritative tabs instead of an empty sentinel");
  ok(saturatedResult.latest, "coalesced newest tab metadata remains eligible to update state");

  first.resolve([tabMeta({ id: "tab-stale" })]);
  const firstResult = await firstRefresh;
  await secondRefresh;
  ok(!firstResult.latest, "an older tab metadata response cannot replace a newer snapshot");
}

{
  const coordinator = createBoundedRefreshCoordinator<TabMeta[]>(2);
  const preMutationA = deferred<TabMeta[]>();
  const preMutationB = deferred<TabMeta[]>();
  const postMutation = deferred<TabMeta[]>();
  let loads = 0;
  const loadValues: TabMeta[][] = [
    [tabMeta({ id: "pre-a" })],
    [tabMeta({ id: "pre-b" })],
    [tabMeta({ id: "post-mutation" })],
  ];
  const loadPromises = [preMutationA.promise, preMutationB.promise, postMutation.promise];

  const firstRefresh = coordinator.run(() => {
    const index = loads;
    loads += 1;
    return loadPromises[index] ?? Promise.resolve(loadValues[index] ?? []);
  });
  const secondRefresh = coordinator.run(() => {
    const index = loads;
    loads += 1;
    return loadPromises[index] ?? Promise.resolve(loadValues[index] ?? []);
  });
  await Promise.resolve();
  ok(loads === 2, "pre-mutation tab metadata fills both in-flight slots");

  const mutationRefresh = coordinator.run(
    () => {
      const index = loads;
      loads += 1;
      return loadPromises[index] ?? Promise.resolve(loadValues[index] ?? []);
    },
    { invalidate: true },
  );
  await Promise.resolve();
  ok(loads === 2, "post-mutation refresh does not start until an in-flight slot frees");

  preMutationA.resolve(loadValues[0]);
  const firstResult = await firstRefresh;
  await Promise.resolve();
  await Promise.resolve();
  ok(loads === 3, "post-mutation refresh starts as a trailing load after a slot frees");
  ok(!firstResult.latest, "pre-mutation snapshot is not authoritative after invalidate");

  const postTabs = loadValues[2];
  postMutation.resolve(postTabs);
  const mutationResult = await mutationRefresh;
  ok(!mutationResult.coalesced, "post-mutation refresh does not join a pre-mutation request");
  ok(mutationResult.value === postTabs, "post-mutation refresh returns the mutation-after snapshot");
  ok(mutationResult.latest, "post-mutation trailing refresh remains eligible to update state");

  preMutationB.resolve(loadValues[1]);
  const secondResult = await secondRefresh;
  ok(!secondResult.latest, "pre-mutation coalesced request cannot overwrite post-mutation state");
}

{
  const coordinator = createBoundedRefreshCoordinator<TabMeta[]>(1);
  const preMutation = deferred<TabMeta[]>();
  const latestPostMutation = deferred<TabMeta[]>();
  const started: string[] = [];

  const preMutationRefresh = coordinator.run(() => {
    started.push("pre");
    return preMutation.promise;
  });
  await Promise.resolve();

  const firstMutationRefresh = coordinator.run(
    () => {
      started.push("first-mutation");
      return Promise.resolve([tabMeta({ id: "first-mutation" })]);
    },
    { invalidate: true },
  );
  const latestMutationRefresh = coordinator.run(
    () => {
      started.push("latest-mutation");
      return latestPostMutation.promise;
    },
    { invalidate: true },
  );

  preMutation.resolve([tabMeta({ id: "pre" })]);
  const preMutationResult = await preMutationRefresh;
  await Promise.resolve();
  await Promise.resolve();
  ok(started.join(",") === "pre,latest-mutation", "queued invalidations retain only the latest trailing load");
  ok(!preMutationResult.latest, "queued invalidations fence the pre-mutation load");

  const latestTabs = [tabMeta({ id: "latest-mutation" })];
  latestPostMutation.resolve(latestTabs);
  const [firstMutationResult, latestMutationResult] = await Promise.all([firstMutationRefresh, latestMutationRefresh]);
  ok(firstMutationResult.value === latestTabs, "an older queued mutation waits for the latest post-mutation snapshot");
  ok(latestMutationResult.value === latestTabs, "the latest queued mutation receives its post-mutation snapshot");
  ok(firstMutationResult.latest && latestMutationResult.latest, "the shared trailing snapshot remains authoritative");
}

ok(
  !appSource.includes("setInterval(() => void refreshTabMetas(), 2000)") && runtimeHandlersSource.includes('import("../lib/workspaceRefreshStore")') &&
    workspaceFocusSource.includes('document.addEventListener("visibilitychange", onVisibilityChange)') &&
    runtimeHandlersSource.includes("createBoundedRefreshCoordinator<TabMeta[]>(TAB_META_MAX_IN_FLIGHT)") &&
    /void refreshTabMetas\(\);\s+schedule\(\);/.test(workspaceFocusSource),
  "tab metadata refresh is event-driven with a visibility-aware fallback",
);


ok(
  /const WORKSPACE_PANEL_DEFAULT_OPEN = true;/.test(layoutStoreSource) &&
    /workspacePanelOpen:\s*loadWorkspacePanelOpen\(""\)/.test(layoutStoreSource) &&
    /export function saveWorkspacePanelOpen\(open: boolean, workspaceRoot = ""\)/.test(layoutStoreSource) &&
    /reasonix\.workspacePanel\.open/.test(layoutStoreSource),
  "right dock open state is restored from per-project localStorage with expanded first-launch default",
);

ok(
  /workbenchChromeHidden\s*=\s*sidebarWorkbench/.test(appViewSource),
  "workbench chrome is hidden for every desktop platform",
);

ok(
  /topicbar__chrome-btn/.test(dockToggleSource),
  "workbench keeps chrome controls in the topic bar",
);

// The app tab strip that consumed the tab reveal signal is gone; the transcript
// keeps its own cell and the shared reveal still has to bump both independently.
ok(
  /const \[transcriptRevealSignal, setTranscriptRevealSignal\] = useState\(0\);/.test(appSource) &&
    /revealSignal=\{transcript\.revealSignal\}/.test(chatPaneSource) &&
    /input\.setTabRevealSignal\(value => value \+ 1\); input\.setTranscriptRevealSignal\(value => value \+ 1\);/.test(desktopNavigationSource),
  "transcript bottom reveal keeps its own signal and still settles with the shared reveal",
);


ok(
  /aria-label=\{t\("transcript\.jumpToBottom"\)\}/.test(transcriptSource) &&
    /title=\{t\("transcript\.jumpToBottom"\)\}/.test(transcriptSource),
  "jump-to-bottom affordance uses localized transcript text",
);

ok(
  /setActive\(items\.length > 0 \? 0 : -1\)/.test(commandPaletteSource),
  "command palette highlights the first item when opened with an empty query",
);

ok(
  /topicShortcutIndexFromEvent\(event, input\.platform\)/.test(topicShortcutOwnerSource) &&
    /useTopicShortcuts\(input\.enabled, input\.platform\)/.test(topicShortcutOwnerSource),
  "topic shortcuts use the resolved desktop platform",
);

ok(
  /topicShortcutLabel\(shortcutIndex, shortcutPlatform\)/.test(projectTreeSource),
  "topic shortcut badges render the platform-specific modifier",
);

ok(
  /if \(!enabled\) hideBadges\(\);/.test(topicShortcutsSource) &&
    /if \(heldRef\.current\) hideBadges\(\);/.test(topicShortcutsSource) &&
    /window\.removeEventListener\("blur", onBlur\);\s*hideBadges\(\);/.test(topicShortcutsSource),
  "topic shortcut badge state is cleared when disabled, interrupted, or cleaned up",
);

// session-submission-lifecycle.test.tsx verifies source-only undo invalidation
// before send, and zero invalidation for stale/read-only/disposed submissions.

// session-undo-lifecycle.test.tsx drives the production useSessionUndo owner:
// code-only rewind retains the committed transaction id, full rewinds fill the
// composer only after success, failures leave the banner untouched, and the
// edit prompt honors the undo banner gate.



// pending-plan-revision-lifecycle.test.tsx drives running/idle, tab changes,
// replacement sessions, identical queued text, old finally and disposal.



ok(
  /app\.NewSessionForTab\(tabId\)/.test(controllerSource) &&
    /app\.ClearSessionForTab\(tabId\)/.test(controllerSource) &&
    /app\.CompactForTab\(tabId\)/.test(controllerSource) &&
    /import\("\.\/rewindCommit"\)/.test(controllerSource) &&
    /app\.PreviewRewindForTab\(sourceTabId, turn, scope\)/.test(rewindCommitSource) &&
    /app\.CommitRewindForTab\(sourceTabId, remoteLegacy \? "" : \(plan\.planId \|\| ""\), turn, scope\)/.test(rewindCommitSource) &&
    /app\.UndoRewindForTab\(sourceTabId, transactionId\)/.test(rewindCommitSource) &&
    /bindings\.ForkForTab\(sourceTabId, turn\)[\s\S]*bindings\.ForkWorktreeForTab\(sourceTabId, turn\)/.test(forkWorktreeSource) &&
    /app\.SummarizeFromForTab\(sourceTabId, turn\)/.test(controllerSource) &&
    /NewSessionForTab\(tabID: string\)/.test(bridgeSource) &&
    /CompactForTab\(tabID: string\)/.test(bridgeSource) &&
    /PreviewRewindForTab\(tabID: string, turn: number, scope: string\)/.test(bridgeSource) &&
    /CommitRewindForTab\(tabID: string, planID: string, turn: number, scope: string\)/.test(bridgeSource) &&
    /UndoRewindForTab\(tabID: string, transactionID: string\)/.test(bridgeSource),
  "session-changing controller actions use explicit tab-scoped bridge bindings",
);

ok(
  /plan\.coverage === "partial"/.test(rewindCommitSource) &&
    /window\.confirm\(t\("rewind\.confirmPartialCoverage"/.test(rewindCommitSource) &&
    /plan\?\.conflicts\?\.length \? "overwrite_checkpoint" : ""/.test(workspacePanelSource),
  "rewind previews warn on incomplete coverage and only authorize file overwrite after a conflict confirmation",
);

ok(/const transcriptHydrating = input\.hydrating && !input\.hydrateHistoryLoaded;/.test(transcriptSurfaceSource) &&
    /hydrating=\{transcript\.transcriptHydrating \|\| \(transitioning && !transcript\.navigationDataReady\)\}/.test(chatPaneSource) &&
    /surfaceCommitToken=\{transcript\.surfaceCommitToken\}/.test(chatPaneSource) && /onSurfacePaintReady=\{commands\.onSurfacePaintReady\}/.test(chatPaneSource),
  "Welcome stays suppressed through target data commit and navigation settles only after paint readiness",
);


ok(
  /if \(heroMode\) \{[\s\S]*?const maxHeight = composerHeroInputMaxHeight\(\);[\s\S]*?setTextareaAutoHeight/.test(composerSource) &&
    !/if \(heroMode\) \{\s*setTextareaAutoHeight\(20\);/.test(composerSource),
  "Creation hero composer auto-grows multi-line drafts instead of clipping at 20px",
);



ok(
  /return navigation\.enqueueNavigation\(\{ kind: "topic", scope, workspaceRoot, topicId, sessionPath \}\);/.test(sessionNavigationSource) &&
    /const targetRoot = scope === "project" \? workspaceRoot : ""/.test(sessionNavigationSource) &&
    /enqueueNavigation\(\{ kind: "blank", scope, workspaceRoot: targetRoot \}\)/.test(sessionNavigationSource) &&
    /return navigation\.enqueueNavigation\(\{ kind: "sidebar-im", connection \}\);/.test(sessionNavigationSource) &&
    /navigation\.enqueueNavigation\(\{ kind: "resume-session", session \}\)/.test(sessionNavigationSource),
  "topic, blank, IM, and history navigation all use the shared coalescing path",
);


// The owner resumes history through topic activation alone; a second
// resumeSession call would re-pin a session the activation already pinned.
const historyResumeBlock = desktopNavigationOwnerSource.match(/const \{ session \} = request;[\s\S]*?ports\.closeHistory\(\);/)?.[0] ?? "";
ok(
  historyResumeBlock.includes("ports.closeHistory()") && !historyResumeBlock.includes("resumeSession"),
  "history navigation does not re-resume a session that topic activation already pinned",
);


ok(
  finalDeclaration(".sidebar", "--reasonix-draggable") === "drag" &&
    finalDeclaration(".app--windows .sidebar", "--reasonix-draggable") === "no-drag" &&
    finalDeclaration(".sidebar-resizer", "--reasonix-draggable") === "no-drag",
  "Windows sidebar avoids native window drag without changing other platforms",
);

ok(
  finalDeclaration(".app--windows.app--creation .topicbar", "position") === "relative" &&
    finalDeclaration(".app--windows.app--creation .topicbar", "z-index") === "var(--z-app-chrome)" &&
    finalDeclaration(".app--windows.app--creation .topicbar", "min-height") === "40px" &&
    finalDeclaration(":root[data-theme-style] .app--windows.app--creation .topicbar", "min-height") === "40px" &&
    finalDeclaration(".app--windows.app--creation .topicbar", "transform") === "none !important" &&
    finalDeclaration(".app--windows.app--creation .topicbar__title-row", "transform") === "none" &&
    finalDeclaration(".app--windows-frameless.app--creation", "--windows-window-controls-height") === "40px" &&
    finalDeclaration(".app--creation .topicbar", "min-height") === "56px" &&
    finalDeclaration(":root[data-theme-style] .app--creation .topicbar", "padding-top") === "14px" &&
    finalDeclaration(".app--creation .topicbar__title-row", "transform") === "translateY(-3px)",
  "Windows Creation stays 40px while macOS and Linux keep the shared Creation geometry",
);

// Every style now renders the bar as the layout's own first row, so there is no
// chrome row left to remove and no layout class describing its absence.
ok(
  /\.topicbar \{\s*position: relative;\s*z-index: var\(--z-inline-sticky\);\s*grid-row: 1;\s*grid-column: 1 \/ -1;/.test(stylesSource) &&
    /grid-template-rows: auto minmax\(0, 1fr\) var\(--statusbar-height\)/.test(stylesSource),
  "the shell bar spans every column as the layout's first row",
);

// The bar covers the sidebar's column too, so the macOS inset moves from a
// sidebar-collapsed special case onto the bar itself. The sidebar is the row
// below the bar now, so the traffic lights can no longer reach it.
ok(
  finalDeclaration(".app--darwin .topicbar", "padding-left") === "var(--chrome-left-safe-offset)" &&
    finalDeclaration(".app--darwin .sidebar--workbench", "padding") === "14px 12px 10px",
  "macOS leaves safe space for inset window controls on the shell bar, not the sidebar",
);

// The shell bar owns the window's drag region now. While the dock's control
// strip was also draggable, any control that did not opt out individually —
// the leading overview chevron did not — never received a click.
ok(
  finalDeclaration(".workbench-dock__tools", "--reasonix-draggable") === "no-drag" &&
    finalDeclaration(".workbench-dock__tabs", "--reasonix-draggable") === "no-drag" &&
    finalDeclaration(".workbench-dock__tab", "--reasonix-draggable") === "no-drag" &&
    finalDeclaration(".workbench-dock__tab-overview", "--reasonix-draggable") !== "drag",
  "the dock's control strip is not a window drag region, so every control stays clickable",
);

// The dock wraps TabContainer in .workbench-dock__panel. It must stretch that
// container: unstyled, the wrapper is a content-sized block, so the container
// collapses to its content and its own overflow clip then cuts off the tab
// overview popover, which is positioned inside it.
ok(
  finalDeclaration(".workbench-dock__panel", "flex") === "1 1 auto" &&
    finalDeclaration(".workbench-dock__panel", "display") === "flex" &&
    finalDeclaration(".tab-container", "overflow") === "hidden",
  "the dock panel stretches its tab container so in-dock popovers are not clipped",
);

ok(
  finalDeclaration(":root[data-theme-style] .workbench-dock__tab--active::after", "display") === "none",
  "active dock tab underline is removed in favor of the rounded-rect selected state",
);

ok(
  finalDeclaration(".app--workbench .workbench-dock__tab + .workbench-dock__tab::before", "width") === "1px" &&
    finalDeclaration(".app--workbench .workbench-dock__tab + .workbench-dock__tab::before", "height") === "16px" &&
    finalDeclaration(".app--workbench .workbench-dock__tab + .workbench-dock__tab::before", "background")?.includes("--border-soft"),
  ".app--workbench .workbench-dock__tab + .workbench-dock__tab::before renders a restrained divider between right-dock tabs",
);

ok(
  finalDeclaration(".app--creation .workbench-dock__tab + .workbench-dock__tab::before", "content") === undefined,
  "Creation right-dock tabs keep their equal-column treatment without dividers",
);

// The bar, not the dock's tools row, is the window's native title surface now:
// it carries the caption inset at every dock state, and the tools row is a plain
// tab strip that reserves nothing for the window controls.
ok(
  finalDeclaration(".app--windows-frameless .topicbar", "padding-right") === "var(--windows-window-controls-safe)" &&
    finalDeclaration(".app--windows-frameless.app--workbench .workbench-dock__tools", "height") === undefined,
  "the Windows caption inset sits on the shell bar, not on the dock's tools row",
);

ok(
  /@container \(max-width: 420px\) \{[\s\S]*?\.app--workbench \.workbench-dock__tab,[\s\S]*?:root\[data-theme-style\] \.app--workbench \.workbench-dock__tab \{[\s\S]*?padding-left:\s*10px;[\s\S]*?padding-right:\s*10px;[\s\S]*?gap:\s*4px;/.test(stylesSource),
  "workbench keeps compact four-tab spacing at narrow dock widths",
);

for (const selector of [
  ":root[data-theme-style] .app--workbench .topicbar",
  ":root[data-theme-style] .app--workbench .topicbar__chrome-btn",
  ":root[data-theme-style] .app--workbench .topicbar__icon-btn",
  ":root[data-theme-style] .app--workbench .topicbar__action-btn",
]) {
  ok(
    finalDeclaration(selector, "box-shadow") === "none",
    `${selector} stays flat after removing the workbench chrome row`,
  );
}

ok(
  finalDeclaration(":root[data-theme-style] .app--workbench .topicbar", "background") === "var(--bg-elev)",
  "workbench topic bar uses elevated background for light-mode white",
);

for (const selector of [
  ":root[data-theme-style] .app--workbench .topicbar__identity",
  ":root[data-theme-style] .app--workbench .topicbar__title-row",
  ":root[data-theme-style] .app--workbench .topicbar__title-row h1",
  ":root[data-theme-style] .app--workbench .tooltip-trigger:has(.topicbar__icon-btn)",
]) {
  ok(
    finalDeclaration(selector, "background") === "transparent" &&
      finalDeclaration(selector, "box-shadow") === "none" &&
      finalDeclaration(selector, "filter") === "none",
    `${selector} cannot paint residual title-row shadows in workbench mode`,
  );
}

for (const selector of [
  ":root[data-theme-style] .app--workbench .topicbar__icon-btn",
  ":root[data-theme-style] .app--workbench .topicbar__chrome-btn",
  ":root[data-theme-style] .app--workbench .topicbar__icon-btn:hover",
  ":root[data-theme-style] .app--workbench .topicbar__icon-btn:focus-visible",
  ":root[data-theme-style] .app--workbench .topicbar__chrome-btn:hover:not(.topicbar__chrome-btn--blocked)",
  ":root[data-theme-style] .app--workbench .topicbar__chrome-btn:focus-visible:not(.topicbar__chrome-btn--blocked)",
]) {
  ok(
    finalDeclaration(selector, "background") === "transparent",
    `${selector} does not paint a hover block in workbench mode`,
  );
}

ok(
  finalDeclaration(".skip-to-composer", "box-shadow") === "none" &&
    finalDeclaration(".skip-to-composer:focus-visible", "box-shadow")?.includes("0 12px 28px"),
  "offscreen skip link does not leak its focus shadow into the workbench title area",
);

// The OS drag runtime drops any mousedown with detail !== 1, so a double
// click on a drag region never reaches the OS: both title-bar-hiding platforms
// have to zoom from here or not at all.
ok(
  /chromeDoubleClickZooms\s*=\s*input\.windowsFrameless\s*\|\|\s*input\.platform === "darwin"/.test(chromeCommandsSource),
  "title-bar double click zooms on macOS as well as frameless Windows",
);
ok(
  /handleChromeTitlebarDoubleClick[\s\S]{0,700}?closest\("button, input, textarea, select, a, \[role='button'\], \[role='tab'\], \.windows-window-controls"\)/.test(chromeCommandsSource),
  "title-bar double click still ignores interactive controls",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
