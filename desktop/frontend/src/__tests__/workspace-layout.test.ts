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
  /rightDockMode === "terminal"[\s\S]*?workspacePanelRenderWidth >= rightDockMinRenderWidth/.test(appSource),
  true,
  "terminal remains renderable when a narrow viewport cannot fit the side dock",
);
eq(
  /const addTerminalOutputToComposer = useCallback\(async \(sessionId: string\) => \{[\s\S]*?app\.TerminalOutputForTab\(activeTabId, sessionId\)[\s\S]*?addWorkspaceTextToComposer\(/.test(appSource),
  true,
  "terminal output reaches chat only through the explicit add-output action",
);
eq(
  /@media \(max-width: 820px\) \{[\s\S]*?\.layout--terminal-open \.workbench-dock[\s\S]*?display: flex !important/.test(stylesSource),
  true,
  "terminal drawer stays visible on narrow viewports",
);
eq(
  /\.layout--terminal-open \.workbench-dock \{[\s\S]*?padding-bottom: var\(--statusbar-height\)/.test(stylesSource),
  true,
  "terminal drawer reserves the fixed status bar safe area",
);
eq(
  /\.layout--terminal-open \.workbench-dock__tools \{\s*display: none;\s*\}/.test(stylesSource),
  true,
  "terminal drawer hides the redundant workspace tab strip",
);
eq(
  /\.app--creation \.layout--creation-chrome-hidden\.layout--terminal-open \{[\s\S]*?grid-template-rows: minmax\(0, 1fr\) minmax\(220px, min\(42vh, 440px\)\)/.test(stylesSource)
    && /\.app--creation \.layout--creation-chrome-hidden\.layout--terminal-open \.workbench-dock \{[\s\S]*?grid-row: 2/.test(stylesSource),
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
