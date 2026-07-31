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
const terminalPanelSource = readFileSync(resolve(testDir, "../components/TerminalPanel.tsx"), "utf8");
const terminalRailSource = readFileSync(resolve(testDir, "../components/TerminalSessionRail.tsx"), "utf8");

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
  /const closeWorkspacePanel = useCallback\(\(\) => \{[\s\S]*?setLiveWorkspacePanelRenderWidth\(null\);[\s\S]*?setWorkspacePanelOpen\(false\);[\s\S]*?saveWorkspacePanelOpen\(false\);/.test(appSource),
  true,
  "closing the dock clears the transient render width, hides the panel, and persists the collapsed preference",
);
eq(
  /setWorkspacePanelOpen\(true\);[\s\S]*?saveWorkspacePanelOpen\(true\);/.test(appSource),
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
    && /"--terminal-height": `\$\{liveTerminalHeight \?\? \(terminalPanelOpen \? terminalRenderHeight : 0\)\}px`/.test(appSource),
  true,
  "terminal render height re-clamps whenever the viewport changes",
);
eq(
  /aria-hidden=\{!terminalPanelOpen\}/.test(appSource)
    && /tabIndex=\{terminalPanelOpen \? 0 : -1\}/.test(appSource)
    && /onKeyDown=\{resizeTerminalWithKeyboard\}/.test(appSource),
  true,
  "closed terminal resizer leaves the tab order and open resizer supports keyboard adjustment",
);
eq(
  /terminalPanelOpen && !sidebarCreation \? "footer--compact" : ""/.test(appSource)
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
eq(
  /workbench-dock__tab[\s\S]*?terminalPanelOpen/.test(appSource),
  true,
  "terminal tab in workspace dock toggles the independent terminal panel, not rightDockMode",
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
    && /\.composer-task-mode-trigger:hover:not\(:disabled\),[\s\S]*?\.composer-task-mode-trigger--open \{[\s\S]*?transform: none;/.test(stylesSource)
    && /\.composer-profile-trigger:hover:not\(:disabled\),[\s\S]*?\.composer-profile-trigger--open \{[\s\S]*?transform: none;/.test(stylesSource),
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
  "terminal and xterm load only when the terminal drawer opens",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
