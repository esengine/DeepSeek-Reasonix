// Run: tsx src/__tests__/workspace-layout.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  availableWorkspacePanelWidth,
  resolveLiveWorkspacePanelWidth,
  resolveWorkspacePanelWidth,
  workspacePanelAriaMinWidth,
} from "../lib/workspaceLayout";
import {
  clampTerminalHeight,
  terminalMaxHeight,
} from "../store/layout";

let passed = 0;
let failed = 0;
const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const moreMenuSource = [
  readFileSync(resolve(testDir, "../components/TopicbarMoreMenu.tsx"), "utf8"),
  readFileSync(resolve(testDir, "../components/TopicbarMoreMenuContent.tsx"), "utf8"),
].join("\n");
const terminalPanelSource = readFileSync(resolve(testDir, "../components/TerminalPanel.tsx"), "utf8");
const terminalViewSource = readFileSync(resolve(testDir, "../components/TerminalView.tsx"), "utf8");
const terminalRailSource = readFileSync(resolve(testDir, "../components/TerminalSessionRail.tsx"), "utf8");
const terminalLifecycleSource = readFileSync(resolve(testDir, "../lib/useWarmTerminalPanel.ts"), "utf8");

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

const CHAT_MIN_WIDTH = 400;
const SIDEBAR_WIDTH = 264;
const RESIZER_WIDTH = 8;
const PREVIEW_MIN_WIDTH = 420;
const PREVIEW_DEFAULT_WIDTH = 660;
const CHAT_COMFORT_MIN_WIDTH = 560;

console.log("\nworkspace dock layout");

const expandedAvailable = availableWorkspacePanelWidth({
  viewportWidth: 1280,
  sidebarCollapsed: false,
  sidebarWidth: SIDEBAR_WIDTH,
  chatMinWidth: CHAT_MIN_WIDTH,
  resizerWidth: RESIZER_WIDTH,
});
eq(expandedAvailable, 608, "1280px viewport leaves room for an expanded-sidebar dock");
eq(
  resolveWorkspacePanelWidth({
    open: true,
    maximized: false,
    preferredWidth: PREVIEW_DEFAULT_WIDTH,
    minWidth: PREVIEW_MIN_WIDTH,
    availableWidth: expandedAvailable,
  }),
  608,
  "expanded-sidebar preview clamps to available width instead of overflowing",
);

const collapsedAvailable = availableWorkspacePanelWidth({
  viewportWidth: 1280,
  sidebarCollapsed: true,
  sidebarWidth: SIDEBAR_WIDTH,
  chatMinWidth: CHAT_MIN_WIDTH,
  resizerWidth: RESIZER_WIDTH,
});
eq(collapsedAvailable, 872, "collapsed sidebar restores workspace room");
eq(
  resolveWorkspacePanelWidth({
    open: true,
    maximized: false,
    preferredWidth: PREVIEW_DEFAULT_WIDTH,
    minWidth: PREVIEW_MIN_WIDTH,
    availableWidth: collapsedAvailable,
  }),
  PREVIEW_DEFAULT_WIDTH,
  "wide-enough collapsed layout keeps the preferred preview width",
);

const narrowAvailable = availableWorkspacePanelWidth({
  viewportWidth: 900,
  sidebarCollapsed: false,
  sidebarWidth: SIDEBAR_WIDTH,
  chatMinWidth: CHAT_MIN_WIDTH,
  resizerWidth: RESIZER_WIDTH,
});
const narrowRendered = resolveWorkspacePanelWidth({
  open: true,
  maximized: false,
  preferredWidth: PREVIEW_DEFAULT_WIDTH,
  minWidth: PREVIEW_MIN_WIDTH,
  availableWidth: narrowAvailable,
});
eq(narrowAvailable, 228, "very narrow viewports may leave less than the nominal dock minimum");
eq(narrowRendered, 228, "very narrow dock still stays inside the viewport");
eq(workspacePanelAriaMinWidth(PREVIEW_MIN_WIDTH, narrowRendered), 228, "ARIA minimum follows constrained rendered width");

eq(
  resolveWorkspacePanelWidth({
    open: false,
    maximized: false,
    preferredWidth: PREVIEW_DEFAULT_WIDTH,
    minWidth: PREVIEW_MIN_WIDTH,
    availableWidth: 0,
  }),
  PREVIEW_DEFAULT_WIDTH,
  "closed panel preserves the saved preferred width",
);
eq(
  resolveWorkspacePanelWidth({
    open: true,
    maximized: true,
    preferredWidth: PREVIEW_DEFAULT_WIDTH,
    minWidth: PREVIEW_MIN_WIDTH,
    availableWidth: 228,
  }),
  PREVIEW_DEFAULT_WIDTH,
  "maximized panel preserves the saved preferred width",
);

eq(
  resolveLiveWorkspacePanelWidth({
    viewportWidth: 1268,
    sidebarCollapsed: false,
    sidebarWidth: 400,
    chatMinWidth: CHAT_COMFORT_MIN_WIDTH,
    resizerWidth: RESIZER_WIDTH,
    open: true,
    maximized: false,
    preferredWidth: PREVIEW_MIN_WIDTH,
    minWidth: PREVIEW_MIN_WIDTH,
  }),
  300,
  "live dock drag clamps the hard minimum to the available dock width",
);

eq(
  resolveLiveWorkspacePanelWidth({
    viewportWidth: 1280,
    sidebarCollapsed: false,
    sidebarWidth: 500,
    chatMinWidth: CHAT_COMFORT_MIN_WIDTH,
    resizerWidth: RESIZER_WIDTH,
    open: true,
    maximized: false,
    preferredWidth: PREVIEW_DEFAULT_WIDTH,
    minWidth: PREVIEW_MIN_WIDTH,
  }),
  212,
  "live sidebar drag recomputes dock width from the dragged sidebar width",
);
eq(terminalMaxHeight(480), 240, "terminal maximum follows half of the current viewport height");
eq(terminalMaxHeight(180), 120, "terminal maximum never falls below the accessible minimum");
eq(clampTerminalHeight(680, 480), 240, "restored terminal height clamps after the window shrinks");
eq(clampTerminalHeight(80, 720), 120, "terminal height clamps to its minimum");
eq(
  /const closeWorkspacePanel = useCallback\(\(\) => \{[\s\S]*?setLiveWorkspacePanelRenderWidth\(null\);[\s\S]*?setWorkspacePanelOpen\(false\);[\s\S]*?saveWorkspacePanelOpen\(false, activeWorkspaceRoot\);/.test(appSource),
  true,
  "closing the dock clears the transient render width, hides the panel, and persists the collapsed preference",
);
eq(
  /\.workspace-panel-resizer \{[\s\S]*?grid-column: 3;[\s\S]*?justify-self: start;[\s\S]*?width: 1px;/.test(stylesSource)
    && /\.workspace-panel-resizer::before \{[\s\S]*?left: 0;[\s\S]*?right: -7px;/.test(stylesSource),
  true,
  "workspace resize hit area starts at the dock boundary and never overlaps the chat scrollbar gutter",
);
eq(
  /createPointerResizeLifecycle\(\{[\s\S]*?separator,[\s\S]*?pointerId,[\s\S]*?onMove,[\s\S]*?onFinish: \(\) => \{[\s\S]*?liveResize\.flush\(\);/.test(appSource)
    && /workspacePanelResizeFinishRef\.current = lifecycle\.finish/.test(appSource),
  true,
  "workspace resize has one guarded finish path for capture loss, blur, cancellation, and unmount",
);
eq(
  /setWorkspacePanelOpen\(true\);[\s\S]*?saveWorkspacePanelOpen\(true, activeWorkspaceRoot\);/.test(appSource),
  true,
  "opening the dock persists the expanded preference for the next launch",
);
eq(
  /terminalPanelOpen[\s\S]*?terminal-drawer/.test(appSource),
  true,
  "terminal drawer is an independent panel, not a workspace dock mode",
);
eq(
  /const addTerminalOutputToComposer = useCallback\(async \(sessionId: string\) => \{[\s\S]*?app\.TerminalOutputForTab\(activeTabId, sessionId\)[\s\S]*?addWorkspaceTextToComposer\(/.test(appSource),
  true,
  "terminal output reaches chat only through the explicit add-output action",
);
eq(
  /const addSelectedTextToComposer = useCallback\(\(text: string, source\?: SelectedTextInsertRequest\["source"\]\)/.test(appSource)
    && /addSelectedTextToComposer\(text, "terminal"\)/.test(appSource)
    && /onAddToChat=\{addTerminalSelectionToComposer\}/.test(appSource),
  true,
  "terminal selections enter the composer as typed quoted context",
);
eq(
  /@media \(max-width: 820px\) \{[\s\S]*?\.layout--terminal-drawer-open \.terminal-drawer[\s\S]*?display: flex !important/.test(stylesSource),
  true,
  "terminal drawer stays visible on narrow viewports",
);
eq(
  /\.layout--terminal-drawer-open \{[\s\S]*?grid-template-rows: var\(--app-chrome-height\) minmax\(0, 1fr\) var\(--terminal-height, 280px\) var\(--statusbar-height\)/.test(stylesSource),
  true,
  "terminal-drawer-open layout reserves a grid row for the status bar below the terminal drawer",
);
eq(
  /@media \(max-width: 820px\) \{[\s\S]*?\.layout--terminal-drawer-open \.terminal-drawer-resizer[\s\S]*?grid-column: 1 !important[\s\S]*?\.layout--workbench-chrome-hidden\.layout--terminal-drawer-open \.terminal-drawer[\s\S]*?grid-row: 2;[\s\S]*?\.layout--workbench-chrome-hidden\.layout--terminal-drawer-open[\s\S]*?minmax\(0, 1fr\) var\(--terminal-height, 280px\) var\(--statusbar-height\)/.test(stylesSource),
  true,
  "narrow viewport keeps the resizer and drawer in the content column above the status bar",
);
eq(
  /const terminalRenderHeight = clampTerminalHeight\(terminalHeight, viewportHeight\)/.test(appSource)
    && /"--terminal-height": `\$\{terminalSurfaceOpen \? liveTerminalHeight \?\? terminalRenderHeight : 0\}px`/.test(appSource),
  true,
  "terminal render height re-clamps whenever the viewport changes",
);
eq(
  /aria-hidden=\{!terminalSurfaceOpen\}/.test(appSource)
    && /tabIndex=\{terminalSurfaceOpen \? 0 : -1\}/.test(appSource)
    && /onKeyDown=\{resizeTerminalWithKeyboard\}/.test(appSource),
  true,
  "closed terminal resizer leaves the tab order and open resizer supports keyboard adjustment",
);
eq(
  /terminalSurfaceOpen && !sidebarCreation \? "footer--compact" : ""/.test(appSource)
    && !/\.layout\.layout--terminal-drawer-open \.footer/.test(stylesSource),
  true,
  "footer compaction applies only while the terminal is expanded outside Creation mode",
);
eq(
  /sidebarImDetailConnection \? "layout--statusbar-hidden" : ""/.test(appSource)
    && /\.layout\.layout--statusbar-hidden,[\s\S]*?--statusbar-height: 0px;/.test(stylesSource),
  true,
  "IM detail collapses the status bar row when the bar is not rendered",
);
eq(
  /\.layout--terminal-drawer-expanded \.terminal-drawer \{[\s\S]*?border-top: 1px solid var\(--border-soft\)/.test(stylesSource),
  true,
  "terminal drawer has a top border only when expanded, avoiding a collapsed artifact line",
);
eq(
  /\.sidebar--workbench \{[\s\S]*?padding: 16px 16px 10px;/.test(stylesSource)
    && /\.app--darwin \.sidebar--workbench,\s*\.sidebar--workbench \{[\s\S]*?padding: 14px 12px 10px;/.test(stylesSource),
  true,
  "workbench sidebar does not reserve the docked status bar twice",
);
const workspaceDockTabsSource = appSource.match(/<div className="workbench-dock__tabs"[\s\S]*?<div className="workbench-dock__body">/)?.[0] ?? "";
eq(
  workspaceDockTabsSource.length > 0
    && !/rightDock\.terminal|terminalPanelOpen|toggleTerminalPanel/.test(workspaceDockTabsSource)
    && /<TopicbarMoreMenu[\s\S]*?toggleTerminal=\{toggleTerminalPanel\}/.test(appSource)
    && /className="topicbar__menu-item"[\s\S]*?closeAndRun\(toggleTerminal\)[\s\S]*?t\("rightDock\.terminal"\)/.test(moreMenuSource),
  true,
  "workspace dock omits the terminal view while the topic bar keeps the terminal drawer action",
);
eq(
  /\.topicbar \{\s*position: relative;\s*z-index: var\(--z-inline-sticky\);/.test(stylesSource)
    && /\.external-opener__menu \{[\s\S]*?z-index: var\(--z-topicbar-menu\);/.test(stylesSource),
  true,
  "topic bar establishes a raised stacking context for external-opener menus",
);
eq(
  /\.composer-meta__control--approval \{[\s\S]*?margin-inline-start: 2px;/.test(stylesSource)
    && /\.composer-modebar__item:hover:not\(:disabled\) \{[\s\S]*?transform: none;/.test(stylesSource)
    && /\.composer-task-mode-trigger:hover:not\(:disabled\),[\s\S]*?\.composer-task-mode-trigger--open \{[\s\S]*?transform: none;/.test(stylesSource),
  true,
  "composer mode controls keep spacing and icon baselines stable on hover",
);
eq(
  /\.app--creation \.layout\.layout--creation-chrome-hidden\.layout--terminal-drawer-open \{[\s\S]*?grid-template-rows: minmax\(0, 1fr\) var\(--terminal-height, 280px\)/.test(stylesSource),
  true,
  "creation style keeps the terminal drawer below the chat pane",
);
eq(
  /sessions\.length > 0 && \([\s\S]*?<TerminalSessionRail/.test(terminalPanelSource),
  true,
  "the single terminal session keeps a visible close control",
);
eq(
  /const syncWorkspace = useTerminalStore[\s\S]*?const capabilityChanged = previous\.tabId === tabId && previous\.readOnly !== readOnly[\s\S]*?void syncWorkspace\(tabId, capabilityChanged\)/.test(terminalPanelSource),
  true,
  "terminal panel refreshes changed capability while reusing an in-flight first-open request",
);
eq(
  /state\.tabId === tabId \? state\.workspace : null/.test(terminalPanelSource)
    && /state\.tabId === tabId \? state\.activeSessionId : null/.test(terminalPanelSource)
    && /setSelectionAction\(null\);\s*\}, \[active\?\.id, tabId\]\)/.test(terminalPanelSource),
  true,
  "rapid tab switches cannot paint the previous tab's terminal or selection action",
);
eq(
  /readOnly=\{Boolean\(activeTab\?\.readOnly\)\}/.test(appSource)
    && /const terminalReadOnly = readOnly \|\| Boolean\(workspace\?\.readOnly\)/.test(terminalPanelSource),
  true,
  "terminal controls follow the active tab read-only boundary",
);
eq(
  /terminal-session-rail__new|onNew/.test(terminalRailSource),
  false,
  "terminal tab strip does not duplicate the header's new-session action",
);
eq(
  /className="terminal-shell-select"[\s\S]*?shellOptions\.map/.test(terminalPanelSource),
  true,
  "terminal header renders the backend-approved shell options",
);
eq(
  /createSession\(tabId, "\.", selectedShellId\)/.test(terminalPanelSource),
  true,
  "new terminal sessions use the selected shell",
);
eq(
  /createSession\(tabId, "\.", "default"\)/.test(terminalPanelSource),
  false,
  "new terminal sessions are not hard-coded to the default shell",
);
eq(
  /const TerminalPanel = lazy\(\(\) => import\("\.\/components\/TerminalPanel"\)/.test(appSource),
  true,
  "terminal and xterm remain in a lazy chunk",
);
eq(
  /onPointerEnter=\{terminalEnabled \? prefetchTerminal : undefined\}/.test(moreMenuSource)
    && /onFocus=\{terminalEnabled \? prefetchTerminal : undefined\}/.test(moreMenuSource)
    && /void import\("\.\.\/components\/TerminalPanel"\)/.test(terminalLifecycleSource),
  true,
  "pointer and keyboard intent prefetch the terminal chunk before opening (More menu)",
);
eq(
  /useWarmTerminalPanel\(terminalPanelOpen, terminalResizing\)/.test(appSource)
    && /if \(open\) setMounted\(true\)/.test(terminalLifecycleSource)
    && !/setMounted\(false\)/.test(terminalLifecycleSource),
  true,
  "the terminal stays mounted after first open to preserve the live xterm",
);
eq(
  /registerTerminalSink\(session\.id, \(bytes\) => terminal\.write\(bytes\), openRef\.current\)/.test(terminalViewSource)
    && /terminalSinkRef\.current\?\.setActive\(open\)/.test(terminalViewSource),
  true,
  "the warm terminal pauses PTY output while collapsed and resumes from its output cursor",
);
eq(
  /fitEnabled=\{terminalFitEnabled\}/.test(appSource)
    && /setFitEnabled\(false\)/.test(terminalLifecycleSource)
    && /TERMINAL_TRANSITION_MS/.test(terminalLifecycleSource)
    && /fitEnabled=\{fitEnabled\}/.test(terminalPanelSource),
  true,
  "drawer transitions pause xterm fit and perform one fit after opening",
);
eq(
  /useGlobalShortcut\(\s*"selection\.addToChat"/.test(terminalPanelSource)
    && /<kbd>\{addShortcut\}<\/kbd>/.test(terminalPanelSource),
  true,
  "terminal selection-to-chat exposes the shared configurable shortcut",
);
eq(
  /className="terminal-drawer"[\s\S]*?aria-hidden=\{!terminalSurfaceOpen\}[\s\S]*?inert=\{!terminalSurfaceOpen \? true : undefined\}/.test(appSource),
  true,
  "the warm collapsed terminal is hidden from accessibility and focus navigation",
);
eq(
  /open=\{terminalSurfaceOpen\}/.test(appSource)
    && /open && selectionAction &&/.test(terminalPanelSource)
    && /if \(!open\) setSelectionAction\(null\)/.test(terminalPanelSource),
  true,
  "closing a warm terminal removes portaled selection controls",
);

// C1: the chat pane keeps its 400px floor no matter how wide the dock is
// dragged — the dock's available width is viewport minus sidebar minus the
// 400px chat minimum minus the resizer, so chat can never be squeezed below it.
const chatFloorDock = availableWorkspacePanelWidth({
  viewportWidth: 1000,
  sidebarCollapsed: false,
  sidebarWidth: SIDEBAR_WIDTH,
  chatMinWidth: CHAT_MIN_WIDTH,
  resizerWidth: RESIZER_WIDTH,
});
eq(
  chatFloorDock + SIDEBAR_WIDTH + CHAT_MIN_WIDTH + RESIZER_WIDTH <= 1000,
  true,
  "C1: dock width never consumes the chat 400px floor (chat stays readable)",
);
// Sanity: with a wide viewport the dock gets more room, but the chat floor is
// still reserved — chat is never the thing that shrinks.
const wideDock = availableWorkspacePanelWidth({
  viewportWidth: 1600,
  sidebarCollapsed: false,
  sidebarWidth: SIDEBAR_WIDTH,
  chatMinWidth: CHAT_MIN_WIDTH,
  resizerWidth: RESIZER_WIDTH,
});
eq(wideDock > chatFloorDock, true, "C1: wider viewport gives the dock more room, chat floor untouched");

// C3: switching dock tabs (context/files/changed) must never resize the dock —
// the preferred width is a single source (rightDockTreeWidth), not a
// detail-dependent ternary that would jump the sidebar per tab.
eq(
  /const preferredWorkspacePanelWidth = rightDockTreeWidth;/.test(appSource),
  true,
  "C3: preferredWorkspacePanelWidth is the single tree width (no detail ternary)",
);
eq(
  /const preferredWorkspacePanelWidth = rightDockDetailActive \? rightDockPreviewWidth : rightDockTreeWidth;/.test(appSource),
  false,
  "C3: no preview-width dual system that would resize the sidebar on tab switch",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
