// Run: tsx src/__tests__/app-chrome-tabs.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createBoundedRefreshCoordinator, sameTabMetaLists, shouldRefreshTabMetaForEvent, tabMetaFallbackDelay } from "../lib/tabMetaRefresh";
import type { TabMeta } from "../lib/types";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const appChromeSource = readFileSync(resolve(testDir, "../components/AppChrome.tsx"), "utf8");
const commandPaletteSource = readFileSync(resolve(testDir, "../components/CommandPalette.tsx"), "utf8");
const projectTreeSource = readFileSync(resolve(testDir, "../components/ProjectTree.tsx"), "utf8");
const topicShortcutsSource = readFileSync(resolve(testDir, "../lib/topicShortcuts.ts"), "utf8");
const transcriptSource = readFileSync(resolve(testDir, "../components/Transcript.tsx"), "utf8");
const composerSource = readFileSync(resolve(testDir, "../components/Composer.tsx"), "utf8");
const controllerSource = readFileSync(resolve(testDir, "../lib/useController.ts"), "utf8");
const bridgeSource = readFileSync(resolve(testDir, "../lib/bridge.ts"), "utf8");
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
  !appSource.includes("setInterval(() => void refreshTabMetas(), 2000)") &&
    appSource.includes('document.addEventListener("visibilitychange", onVisibilityChange)') &&
    appSource.includes("createBoundedRefreshCoordinator<TabMeta[]>(TAB_META_MAX_IN_FLIGHT)") &&
    appSource.includes("void refreshTabMetas();\n        schedule();"),
  "tab metadata refresh is event-driven with a visibility-aware fallback",
);

ok(
  appSource.includes("refreshTabMetas(undefined, { afterMutation: true })") &&
    appSource.includes("{ afterMutation: true }") &&
    appSource.includes("if (shouldRefreshTabMetaForEvent(e.kind)) {") &&
    appSource.includes("void refreshTabMetas(undefined, { afterMutation: true });") &&
    /await refreshTabMetas\(\s*\(\) => isNavigationIntentCurrent\(request\.navigationIntentSeq\),\s*\{\s*afterMutation:\s*true\s*\},?\s*\)/.test(appSource),
  "tab lifecycle events and explicit mutations force a post-mutation trailing metadata refresh",
);

ok(
  /import \{ TabBar \} from "\.\/TabBar";/.test(appChromeSource),
  "AppChrome keeps the classic top session tab strip implementation",
);

for (const propName of ["onTabChange", "onTabClose", "onTabsClose", "onTabsReorder", "onNewTab"]) {
  ok(
    new RegExp(`\\b${propName}\\b`).test(appChromeSource),
    `AppChrome exposes ${propName} for classic tabs`,
  );
}

ok(
  /app-chrome__tab-strip/.test(appChromeSource),
  "AppChrome markup includes classic tab strip containers",
);

ok(
  /const titlebarDragRail = darwinChrome \|\| platform === "windows";/.test(appChromeSource) &&
    /\{titlebarDragRail && <span className="app-chrome__drag-rail"/.test(appChromeSource),
  "AppChrome exposes the classic drag rail on macOS and Windows",
);

ok(
  finalDeclaration(".app--darwin .app-chrome--tabs .tabbar", "--wails-draggable") === "drag" &&
    finalDeclaration(".app--windows-frameless:not(.app--workbench):not(.app--creation) .app-chrome--native-tabs .tabbar", "--wails-draggable") === "drag",
  "classic tabbar whitespace drags the window on macOS and frameless Windows",
);

ok(
  finalDeclaration(".app--darwin .app-chrome--tabs .tabbar *", "--wails-draggable") === "no-drag" &&
    finalDeclaration(".app--windows .app-chrome--native-tabs .tabbar *", "--wails-draggable") === "no-drag",
  "classic tabbar controls and tab gaps remain interactive no-drag regions",
);

ok(
  /const WORKSPACE_PANEL_DEFAULT_OPEN = true;/.test(layoutStoreSource) &&
    /workspacePanelOpen:\s*loadWorkspacePanelOpen\(\)/.test(layoutStoreSource) &&
    /export function saveWorkspacePanelOpen\(open: boolean\)/.test(layoutStoreSource) &&
    /reasonix\.workspacePanel\.open/.test(layoutStoreSource),
  "right dock open state is restored from localStorage with expanded first-launch default",
);

ok(
  finalDeclaration(".app-chrome__tab-strip", "overflow") === "hidden",
  "AppChrome tab strip clips tabs to the available chrome width",
);

ok(
  finalDeclaration(".app-chrome__tab-strip", "min-width") === "0",
  "AppChrome tab strip can shrink beside the right dock",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tabs", "max-width")?.includes("--chrome-panel-control-size"),
  "themed AppChrome tab lists reserve a flowing new-tab button slot",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tabs", "flex") === "0 1 auto",
  "themed AppChrome tab lists size to tab content before shrinking",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tabs", "width") === "max-content",
  "themed AppChrome tab lists keep the new-tab button next to the last tab",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar > .tooltip-trigger:has(.tabbar__new)", "flex")?.includes("--chrome-panel-control-size"),
  "themed AppChrome new-tab button keeps a stable slot beside the tabs",
);

ok(
  finalDeclaration(":root[data-theme-style] .tabbar__tab--active", "box-shadow")?.includes(
    "inset 0 -2px 0 var(--project-accent, var(--accent))",
  ),
  "active themed tab carries the project-accent underline",
);

ok(
  finalDeclaration(":root[data-theme-style] .tabbar__tab--active:focus-visible", "box-shadow")?.includes(
    "inset 0 -2px 0 var(--project-accent, var(--accent))",
  ) &&
    finalDeclaration(":root[data-theme-style] .tabbar__tab--active:focus-visible", "box-shadow")?.includes(
      "0 0 0 3px var(--accent-soft)",
    ),
  "keyboard focus on the active tab keeps both the focus ring and the accent underline",
);

ok(
  matchingBlocks(".app--darwin .app-chrome--tabs .tabbar__tab--active").every(
    (block) => !block.includes("inset 0 2px"),
  ),
  "macOS active tab declares no dead top-edge accent (the themed bottom-edge layer owns it)",
);

ok(
  finalDeclaration(":root[data-theme-style] .tabbar__tabs", "gap") === "6px" &&
    finalDeclaration(":root[data-theme-style] .tabbar__tab", "border") === "1px solid var(--border)",
  "themed tabs keep distinct full outlines with visible spacing",
);

ok(
  finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tab + .tabbar__tab:not(.tabbar__tab--drop-before)::before", "width") === "1px" &&
    finalDeclaration(":root[data-theme-style] .app-chrome--tabs .tabbar__tab + .tabbar__tab:not(.tabbar__tab--drop-before)::before", "background") === "var(--border-2)",
  "adjacent AppChrome tabs render a stronger divider inside their gap",
);

ok(
  finalDeclaration(":root[data-theme-style] .tabbar__tab--active", "border-color") === "var(--border-2)" &&
    finalDeclaration(":root[data-theme-style] .tabbar__tab--active", "font-weight") === "600",
  "active themed tabs combine a stronger border outline and heavier label weight",
);

ok(
  /workbenchChrome \? \(\s*<span className="app-chrome__spacer" aria-hidden="true" \/>/s.test(appChromeSource),
  "AppChrome workbench branch skips the tab strip",
);

ok(
  /app-chrome__tools--fixed/.test(appChromeSource),
  "AppChrome renders the command search as a fixed chrome tool",
);

ok(
  /workbenchChromeHidden\s*=\s*sidebarWorkbench/.test(appSource),
  "workbench chrome is hidden for every desktop platform",
);

ok(
  /\{!appChromeHidden && \(/.test(appSource),
  "workbench skips rendering the top AppChrome row",
);

ok(
  /topicbar__chrome-btn/.test(appSource),
  "workbench keeps chrome controls in the topic bar",
);

ok(
  /const \[transcriptRevealSignal, setTranscriptRevealSignal\] = useState\(0\);/.test(appSource) &&
    /revealActiveSignal=\{tabRevealSignal\}/.test(appSource) &&
    /revealSignal=\{transcriptRevealSignal\}/.test(appSource),
  "transcript bottom reveal is decoupled from tab-strip reveal",
);

const tabsReorderBlock = appSource.match(/const handleTabsReorder = useCallback\([\s\S]*?\n  \}, \[refreshTabMetas, reorderTabs\]\);/)?.[0] ?? "";
ok(
  /setTabRevealSignal/.test(tabsReorderBlock) && !/setTranscriptRevealSignal/.test(tabsReorderBlock),
  "tab reordering refreshes the tab strip without snapping the transcript",
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
  /topicShortcutIndexFromEvent\(event, desktopPlatform\)/.test(appSource) &&
    /useTopicShortcuts\(!sidebarCollapsed, desktopPlatform\)/.test(appSource),
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

ok(
  /const \[rewindStatesByTab, setRewindStatesByTab\] = useState<Record<string, RewindState>>\(\{\}\);/.test(appSource) &&
    /setRewindStateForTab\(sourceTabId, null\);/.test(appSource) &&
    /setRewindCommittingForTab\(sourceTabId, true\);/.test(appSource),
  "committing optimistic rewind clears only the source tab before awaiting the backend",
);

ok(
  /const controllerReady =\s*state\.meta\?\.ready === true &&\s*\(!state\.meta\.runtime \|\| state\.meta\.runtime\.phase === "ready"\) &&\s*!state\.meta\.startupErr &&\s*!state\.backendActivationPending &&\s*!runtimeTransitioning;/.test(appSource) &&
    /if \(!activeTabId \|\| !controllerReady\) return;\s*void commitThenSend\(activeTabId, text\)\.catch/.test(appSource) &&
    /onPrompt=\{handleTranscriptPrompt\}/.test(appSource) &&
    /submitDisabled=\{!controllerReady\}/.test(appSource),
  "welcome prompts and composer submit share the controller readiness gate",
);

ok(
  /pendingPlanRevisionsByTab\[activeTabId\]/.test(appSource) &&
    /commitThenSendRef\.current\(activeTabId, text\)/.test(appSource) &&
    !/const \[pendingPlanRevision, setPendingPlanRevision\]/.test(appSource),
  "queued plan revisions stay scoped to their source tab",
);

ok(
  /commitThenSendRef\.current\(sourceTabId, trimmed, submitText\.trim\(\), structured\)/.test(appSource) &&
    /sendToTab\(sourceTabId, displayText, submitText, undefined, structured\)/.test(appSource) &&
    /onSteer=\{handleSteer\}/.test(appSource) &&
    /composerInsertRequestsByTab\[activeTabId\]/.test(appSource) &&
    /consumedInsertIdByDraftRef\.current\[draftKey\]/.test(composerSource),
  "composer sends and steers carry an explicit source tab through async preparation",
);

ok(
  appSource.includes('key={`${activeTabId ?? ""}:${state.approval.id}`}') &&
    appSource.includes('key={`${activeTabId ?? ""}:${state.ask.id}`}') &&
    /planRevisionInsertRequest\.tabId === activeTabId/.test(appSource) &&
    /planRevisionInsertRequest\.approvalId === state\.approval\?\.id/.test(appSource),
  "approval and ask local state is scoped by tab plus prompt identity",
);

ok(
  /app\.NewSessionForTab\(tabId\)/.test(controllerSource) &&
    /app\.ClearSessionForTab\(tabId\)/.test(controllerSource) &&
    /app\.CompactForTab\(tabId\)/.test(controllerSource) &&
    /app\.RewindForTab\(sourceTabId, turn, actionScope\)/.test(controllerSource) &&
    /app\.ForkForTab\(sourceTabId, turn\)/.test(controllerSource) &&
    /app\.SummarizeFromForTab\(sourceTabId, turn\)/.test(controllerSource) &&
    /NewSessionForTab\(tabID: string\)/.test(bridgeSource) &&
    /CompactForTab\(tabID: string\)/.test(bridgeSource) &&
    /RewindForTab\(tabID: string, turn: number, scope: string\)/.test(bridgeSource),
  "session-changing controller actions use explicit tab-scoped Wails bindings",
);

ok(
  /const transcriptHydrating = state\.hydrating && !state\.hydrateHistoryLoaded;/.test(appSource) &&
    /hydrating=\{transcriptHydrating\}/.test(appSource),
  "Welcome is suppressed only until transcript history has loaded",
);

ok(
  /const \[workspaceControllerEpoch, setWorkspaceControllerEpoch\] = useState\(0\);/.test(appSource) &&
    /const workspaceScopeKey = \[/.test(appSource) &&
    /activeTab\?\.sessionPath/.test(appSource) &&
    /state\.meta\?\.sessionPath/.test(appSource) &&
    /state\.meta\?\.cwd/.test(appSource) &&
    /state\.sessionGen/.test(appSource) &&
    /workspaceControllerEpoch/.test(appSource) &&
    Array.from(appSource.matchAll(/workspaceScopeKey=\{workspaceScopeKey\}/g)).length === 3,
  "workspace file consumers receive a session and controller scoped identity",
);

ok(
  /const unsubReady = onReady\(\(readyTabId\) => \{[\s\S]*?setWorkspaceControllerEpoch[\s\S]*?\n    \}\);/.test(appSource) &&
    /const unsubRebuilt = onRuntimeRebuilt\(\(rebuiltTabId\) => \{[\s\S]*?setWorkspaceControllerEpoch[\s\S]*?\n    \}\);/.test(appSource),
  "controller ready and rebuilt events invalidate active workspace file scopes",
);

const navigationBlock = appSource.match(/const runNavigationRequest = useCallback\([\s\S]*?\n  \}, \[[^\]]*singleSurfaceLayout[^\]]*\]\);/)?.[0] ?? "";
ok(
  /const navigationRunningRef = useRef\(false\);/.test(appSource) &&
    /const navigationPendingRef = useRef<PendingDesktopNavigationRequest \| null>\(null\);/.test(appSource) &&
    /const runNavigationRequest = useCallback\(async \(request: PendingDesktopNavigationRequest\)/.test(appSource) &&
    /const latest = \(\) => request\.seq === navigationSeqRef\.current && isNavigationIntentCurrent\(request\.navigationIntentSeq\);/.test(appSource) &&
    /return activateTopic\(scope, workspaceRoot, topicId, sessionPath \|\| "", request\.navigationIntentSeq\)/.test(appSource) &&
    /return openTopicSession\(scope, workspaceRoot, topicId, sessionPath, request\.navigationIntentSeq\)/.test(appSource) &&
    /return openGlobalTab\(topicId, request\.navigationIntentSeq\)/.test(appSource) &&
    /return openProjectTab\(workspaceRoot, topicId, request\.navigationIntentSeq\)/.test(appSource) &&
    /enqueueNavigationRequest\([\s\S]*runningRef: navigationRunningRef, pendingRef: navigationPendingRef/.test(appSource) &&
    !/openTopicQueueRef\.current\.catch\(\(\) => \{\}\)\.then/.test(appSource) &&
    /const refreshLatestTabMetas = async \(\): Promise<TabMeta\[]> => \{[\s\S]*if \(latest\(\)\) setTabMetas\(tabs\);/.test(navigationBlock) &&
    /if \(!latest\(\)\) return;[\s\S]*seedActiveTabMeta\(openedTab\);[\s\S]*void refreshLatestTabMetas\(\);/.test(navigationBlock),
  "desktop navigation coalesces pending requests, ignores stale results, and seeds active tab metadata before background refresh",
);

ok(
  /return enqueueNavigation\(\{ kind: "topic", scope, workspaceRoot, topicId, sessionPath \}\);/.test(appSource) &&
    /enqueueNavigation\(\{ kind: "blank", scope, workspaceRoot: scope === "project" \? workspaceRoot : "" \}\)/.test(appSource) &&
    /return enqueueNavigation\(\{ kind: "sidebar-im", connection \}\);/.test(appSource) &&
    /return enqueueNavigation\(\{ kind: "resume-session", session \}\);/.test(appSource),
  "topic, blank, IM, and history navigation all use the shared coalescing path",
);

ok(
  !/await resumeSession\(session\.path, targetTab\.id\);/.test(navigationBlock),
  "history navigation does not re-resume a session that OpenTopicSession already pinned",
);

ok(
  /<HeartbeatPanel[\s\S]*onOpenTopic=\{\(scope, workspaceRoot, topicId\) => \{[\s\S]*void handleOpenTopic\(scope, workspaceRoot, topicId\);[\s\S]*\}\}/.test(appSource),
  "heartbeat topic navigation uses the guarded open-topic path",
);

for (const selector of [
  ".app--darwin .app-chrome--tabs",
  ":root[data-theme-style] .app--darwin .app-chrome--tabs",
]) {
  const rightSpace = finalDeclaration(selector, "padding-right") ?? finalDeclaration(selector, "padding") ?? "";
  ok(
    rightSpace.includes("--chrome-toggle-size") && !rightSpace.includes("--chrome-right-toggle-offset"),
    `${selector} reserves fixed chrome tool width without shrinking for the right dock`,
  );
}

for (const selector of [
  ".app--windows .app-chrome--native-tabs",
  ".app--linux .app-chrome--native-tabs",
  ":root[data-theme-style] .app--windows .app-chrome--native-tabs",
  ":root[data-theme-style] .app--linux .app-chrome--native-tabs",
]) {
  const rightSpace = finalDeclaration(selector, "padding-right") ?? finalDeclaration(selector, "padding") ?? "";
  ok(
    rightSpace.includes("--chrome-right-toggle-offset"),
    `${selector} reserves right-dock width before rendering tabs`,
  );
}

for (const selector of [
  ".app--windows-frameless .app-chrome--native-tabs",
  ":root[data-theme-style] .app--windows-frameless .app-chrome--native-tabs",
]) {
  const paddingRight = finalDeclaration(selector, "padding-right") ?? "";
  ok(
    finalDeclaration(selector, "--windows-frameless-titlebar-tools-offset") === "var(--windows-window-controls-safe)" &&
      paddingRight.includes("--windows-frameless-titlebar-tools-offset") &&
      paddingRight.includes("--chrome-panel-control-size") &&
      !paddingRight.includes("--chrome-right-toggle-offset"),
    `${selector} keeps titlebar tools fixed beside the Windows controls`,
  );
}

for (const selector of [
  ".app--windows-frameless .app-chrome--native-tabs .app-chrome__panel-toggle--right",
  ":root[data-theme-style] .app--windows-frameless .app-chrome--native-tabs .app-chrome__panel-toggle--right",
]) {
  ok(
    finalDeclaration(selector, "right") === "calc(var(--windows-frameless-titlebar-tools-offset) + 8px)",
    `${selector} stays fixed outside the Windows window controls`,
  );
}

ok(
  finalDeclaration(".app--windows-frameless:not(.app--workbench):not(.app--creation) .app-chrome--native-tabs .app-chrome__drag-rail", "--wails-draggable") === "drag" &&
    finalDeclaration(".app--windows-frameless:not(.app--workbench):not(.app--creation) .app-chrome--native-tabs .app-chrome__drag-rail", "right")?.includes("--windows-window-controls-safe") &&
    finalDeclaration(".app--windows .app-chrome--native-tabs .tabbar", "--wails-draggable") === "no-drag",
  "Windows classic chrome keeps a draggable rail while tabs remain clickable",
);

ok(
  finalDeclaration(".sidebar", "--wails-draggable") === "drag" &&
    finalDeclaration(".app--windows .sidebar", "--wails-draggable") === "no-drag" &&
    finalDeclaration(".sidebar-resizer", "--wails-draggable") === "no-drag",
  "Windows sidebar avoids native window drag without changing other platforms",
);

ok(
  finalDeclaration(".app--windows.app--creation .topicbar", "position") === "relative" &&
    finalDeclaration(".app--windows.app--creation .topicbar", "z-index") === "var(--z-app-chrome)" &&
    finalDeclaration(".app--windows.app--creation .topicbar", "transform") === "translateY(-4px) !important",
  "Windows Creation topic bar lifts its menus above conversation content without changing titlebar alignment",
);

for (const selector of [
  ".layout--workbench-chrome-hidden",
  ":root[data-theme-style] .layout--workbench-chrome-hidden",
]) {
  ok(
    finalDeclaration(selector, "--app-chrome-height") === "0px" &&
      finalDeclaration(selector, "grid-template-rows") === "minmax(0, 1fr)" &&
      finalDeclaration(selector, "background") === "var(--bg)",
    `${selector} removes the workbench chrome row`,
  );
}

ok(
  finalDeclaration(":root[data-theme-style] .app--darwin .layout--workbench-chrome-hidden", "--app-chrome-height") === "0px" &&
    finalDeclaration(".app--darwin .layout--workbench-chrome-hidden .sidebar--workbench", "padding-top") === "46px" &&
    finalDeclaration(".app--darwin .layout--workbench-chrome-hidden.layout--sidebar-collapsed .topicbar", "padding-left") === "96px",
  "macOS workbench leaves safe space for inset window controls",
);

ok(
  finalDeclaration(".app--darwin .layout--workbench-chrome-hidden.layout--workspace-maximized .workbench-dock__tools", "padding-left") === "96px",
  "macOS maximized workbench dock leaves safe space for inset window controls",
);

ok(
  /@media \(max-width: 820px\) \{[\s\S]*\.app--darwin \.layout--workbench-chrome-hidden \.topicbar\s*\{[\s\S]*padding-left:\s*96px;/.test(stylesSource) &&
    /@media \(max-width: 820px\) \{[\s\S]*\.app--darwin \.layout--workbench-chrome-hidden\.layout--workspace-maximized \.workbench-dock__tools\s*\{[\s\S]*padding-left:\s*96px;/.test(stylesSource),
  "macOS workbench keeps safe space when responsive CSS hides the sidebar",
);

ok(
  finalDeclaration(".workbench-dock__tools", "--wails-draggable") === "drag" &&
    finalDeclaration(".workbench-dock__tabs", "--wails-draggable") === "no-drag" &&
    finalDeclaration(".workbench-dock__tab", "--wails-draggable") === "no-drag",
  "maximized workbench dock keeps a draggable title region while tabs remain clickable",
);

for (const selector of [
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__action-btn",
]) {
  ok(
    finalDeclaration(selector, "box-shadow") === "none",
    `${selector} stays flat after removing the workbench chrome row`,
  );
}

ok(
  finalDeclaration(":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar", "background") === "var(--bg-elev)",
  "workbench topic bar uses elevated background for light-mode white",
);

for (const selector of [
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__identity",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__title-row",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__title-row h1",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .tooltip-trigger:has(.topicbar__icon-btn)",
]) {
  ok(
    finalDeclaration(selector, "background") === "transparent" &&
      finalDeclaration(selector, "box-shadow") === "none" &&
      finalDeclaration(selector, "filter") === "none",
    `${selector} cannot paint residual title-row shadows in workbench mode`,
  );
}

for (const selector of [
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn:hover",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__icon-btn:focus-visible",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn:hover:not(.topicbar__chrome-btn--blocked)",
  ":root[data-theme-style] .layout--workbench-chrome-hidden .topicbar__chrome-btn:focus-visible:not(.topicbar__chrome-btn--blocked)",
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

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
