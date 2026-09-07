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
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const sessionActionsSource = readFileSync(resolve(testDir, "../components/TopicbarSessionActions.tsx"), "utf8");
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
eq(/\.app--darwin\.app--workbench \.workbench-dock__tools,[\s\S]*?padding-right:\s*48px;/.test(stylesSource), true, "macOS workspace header reserves the fixed toggle hit area");
eq(/\.app--darwin\.app--workbench \.workbench-dock__tabs,[\s\S]*?flex:\s*1 1 auto;[\s\S]*?width:\s*100%;[\s\S]*?min-width:\s*0;/.test(stylesSource), true, "macOS workspace tabs fill the remaining title row");
eq(/\.app--darwin\.app--workbench \.workbench-dock__tab,[\s\S]*?flex:\s*1 1 0;[\s\S]*?min-width:\s*0;[\s\S]*?max-width:\s*none;/.test(stylesSource), true, "macOS workspace tabs divide the available row without overlap");

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
  /\.workspace-panel-resizer \{[\s\S]*?grid-column: 3;[\s\S]*?justify-self: start;[\s\S]*?width: 1px;/.test(stylesSource)
    && /\.workspace-panel-resizer::before \{[\s\S]*?left: 0;[\s\S]*?right: -7px;/.test(stylesSource),
  true,
  "workspace resize hit area starts at the dock boundary and never overlaps the chat scrollbar gutter",
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
  /onPointerEnter=\{terminalEnabled \? prefetchTerminal : undefined\}/.test(sessionActionsSource)
    && /onFocus=\{terminalEnabled \? prefetchTerminal : undefined\}/.test(sessionActionsSource)
    && /void import\("\.\.\/components\/TerminalPanel"\)/.test(terminalLifecycleSource),
  true,
  "pointer and keyboard intent prefetch the terminal chunk before opening from the topic bar",
);
eq(
  /registerTerminalSink\(session\.id, \(bytes\) => terminal\.write\(bytes\), openRef\.current\)/.test(terminalViewSource)
    && /terminalSinkRef\.current\?\.setActive\(open\)/.test(terminalViewSource),
  true,
  "the warm terminal pauses PTY output while collapsed and resumes from its output cursor",
);
eq(
  /useGlobalShortcut\(\s*"selection\.addToChat"/.test(terminalPanelSource)
    && /<kbd>\{addShortcut\}<\/kbd>/.test(terminalPanelSource),
  true,
  "terminal selection-to-chat exposes the shared configurable shortcut",
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

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
