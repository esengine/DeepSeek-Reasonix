import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");

function read(rel) {
  return fs.readFileSync(path.join(frontendRoot, rel), "utf8");
}

function fail(message) {
  console.error(message);
  process.exitCode = 1;
}

function mustNotContain(rel, source, needle, reason) {
  if (source.includes(needle)) {
    fail(`${rel}: found forbidden "${needle}" (${reason})`);
  }
}

function mustContain(rel, source, needle, reason) {
  if (!source.includes(needle)) {
    fail(`${rel}: missing "${needle}" (${reason})`);
  }
}

function mustNotMatch(rel, source, pattern, reason) {
  if (pattern.test(source)) {
    fail(`${rel}: matched forbidden pattern ${pattern} (${reason})`);
  }
}

const anchoredPopover = read("src/components/AnchoredPopover.tsx");
mustNotContain(
  "src/components/AnchoredPopover.tsx",
  anchoredPopover,
  "anchored-popover__backdrop",
  "non-modal popovers must not render a transparent page-covering click layer",
);
mustNotContain(
  "src/components/AnchoredPopover.tsx",
  anchoredPopover,
  "document.querySelector<HTMLElement>(\"[data-anchored-popover='active']\")",
  "popover positioning must measure its own ref, not the first matching portal in the document",
);
mustContain(
  "src/components/AnchoredPopover.tsx",
  anchoredPopover,
  "const popoverRef = useRef<HTMLDivElement>(null);",
  "popover positioning and outside-click handling must be scoped to the current instance",
);
mustContain(
  "src/components/AnchoredPopover.tsx",
  anchoredPopover,
  "popoverRef.current?.getBoundingClientRect()",
  "popover positioning must use the current popover instance",
);

const statusBar = read("src/components/StatusBar.tsx");
mustNotContain(
  "src/components/StatusBar.tsx",
  statusBar,
  "modelsw__backdrop",
  "status-bar popovers must not cover the whole viewport after opening",
);
mustContain(
  "src/components/StatusBar.tsx",
  statusBar,
  "wrapRef.current?.contains(target)",
  "status-bar popover outside-click handling must be scoped to the wrapper",
);

const composer = read("src/components/Composer.tsx");
mustContain(
  "src/components/Composer.tsx",
  composer,
  "composer-action-trigger",
  "more actions must live behind the compact plus affordance",
);
mustContain(
  "src/components/Composer.tsx",
  composer,
  "composer-mode-chip--plan",
  "active plan mode must surface as a dismissible composer chip",
);
mustContain(
  "src/components/Composer.tsx",
  composer,
  "composer-mode-chip--goal",
  "active goal mode must surface as a dismissible composer chip",
);
mustContain(
  "src/components/Composer.tsx",
  composer,
  "composer-intent-menu__item${planModeOn",
  "plan mode must be created from the plus menu",
);
mustContain(
  "src/components/Composer.tsx",
  composer,
  "composer-modebar--approval",
  "tool approval must use a direct segmented control",
);
for (const mode of ["ask", "auto", "yolo"]) {
  mustNotMatch(
    "src/components/Composer.tsx",
    composer,
    new RegExp(`composer-modebar__item--${mode}[\\s\\S]{0,320}disabled=\\{disabled \\|\\| running\\}`),
    "approval mode must remain switchable while a model turn is running",
  );
}
mustNotContain(
  "src/components/Composer.tsx",
  composer,
  "composer-modebar--collaboration",
  "collaboration modes must not occupy the always-visible segmented control",
);
mustNotContain(
  "src/components/Composer.tsx",
  composer,
  "composer-intent-chip",
  "plan/goal chips must not return to the always-visible composer row",
);
mustNotContain(
  "src/components/Composer.tsx",
  composer,
  "composer-plan-toggle",
  "plan mode must not be an always-visible standalone control",
);
mustNotContain(
  "src/components/Composer.tsx",
  composer,
  "composer-access-trigger",
  "tool approval must not regress to a single dropdown trigger",
);

const projectTree = read("src/components/ProjectTree.tsx");
mustContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "GLOBAL_PROJECT_ORDER_KEY",
  "Global must participate in project tree reorder through a stable virtual key",
);
mustContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "draggable={draggableProject}",
  "project rows should be directly draggable for reorder",
);
mustContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "target.closest(\".project-tree__action-slot\")",
  "project row drag must not start from the new-topic action area",
);
mustNotContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "project-tree__drag-handle",
  "project reorder should not expose a separate drag handle",
);
mustContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "manuallyCollapsedRef",
  "project-tree async refresh must read the latest manual collapse state",
);
mustContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "project-tree__collapse-all",
  "project tree needs a dedicated collapse-all affordance",
);
mustContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "collapseSnapshot",
  "collapse-all must be reversible to the previous tree view",
);
mustContain(
  "src/components/ProjectTree.tsx",
  projectTree,
  "collapsibleFolderKeys",
  "collapse-all must target every expandable project folder key",
);

const styles = read("src/styles.css");
mustNotContain(
  "src/styles.css",
  styles,
  ".anchored-popover__backdrop",
  "stale backdrop CSS can hide accidental page-covering layers",
);
mustNotContain(
  "src/styles.css",
  styles,
  ".modelsw__backdrop",
  "stale backdrop CSS can hide accidental page-covering layers",
);
mustNotContain(
  "src/styles.css",
  styles,
  ".composer-access-trigger",
  "stale single approval dropdown CSS should not survive the segmented control migration",
);
mustContain(
  "src/styles.css",
  styles,
  ".composer-modebar--approval",
  "approval segmented control needs dedicated state styling",
);
mustContain(
  "src/styles.css",
  styles,
  ".composer-mode-chip",
  "active plan/goal chips need shared dismiss styling",
);
mustContain(
  "src/styles.css",
  styles,
  ".composer-intent-switch",
  "plan/goal plus menu needs explicit switch feedback",
);
mustNotContain(
  "src/styles.css",
  styles,
  ".project-tree__drag-handle",
  "project reorder should not expose a separate drag handle",
);
mustContain(
  "src/styles.css",
  styles,
  ".project-tree__folder--draggable",
  "project reorder needs whole-row drag cursor feedback",
);
mustContain(
  "src/styles.css",
  styles,
  ".project-tree__collapse-all",
  "project collapse-all button must keep its compact icon affordance",
);
mustContain(
  "src/styles.css",
  styles,
  ".project-tree__collapse-all--restore",
  "restorable project tree state must have a distinct visual affordance",
);
mustContain(
  "src/styles.css",
  styles,
  "grid-template-rows 0.2s cubic-bezier",
  "project folder collapse needs an intentional height transition",
);

const app = read("src/App.tsx");
mustContain(
  "src/App.tsx",
  app,
  "const [rightDockMode, setRightDockMode] = useState<RightDockMode>(\"context\");",
  "the right dock should open on overview as the first information layer",
);
mustContain(
  "src/App.tsx",
  app,
  "openWorkspacePanel(\"context\");",
  "reopening the right dock should return to the overview entry point",
);
mustContain(
  "src/App.tsx",
  app,
  "{SHOW_CONTEXT_DOCK && (\n                  <button\n                    type=\"button\"\n                    role=\"tab\"\n                    aria-selected={rightDockMode === \"context\"}",
  "right dock tabs should be ordered overview, files, changes",
);
mustContain(
  "src/App.tsx",
  app,
  "const rightDockDetailActive = rightDockMode === \"context\" ? contextDetailActive : workspacePreviewActive;",
  "right dock width must derive from the current tab's own detail state",
);
mustNotContain(
  "src/App.tsx",
  app,
  "rightDockMode === \"context\"\n      ? RIGHT_DOCK_CONTEXT_WIDTH",
  "context view must not have a separate hard-coded width that fights the shared narrow/detail widths",
);
mustContain(
  "src/App.tsx",
  app,
  "onDetailModeChange={handleContextDetailModeChange}",
  "context detail pages must be able to request the detail-width dock",
);

const workspacePanel = read("src/components/WorkspacePanel.tsx");
mustContain(
  "src/components/WorkspacePanel.tsx",
  workspacePanel,
  "const previewModeActive = open && (filePreviewActive || changeDetailActive);",
  "changes view should not force a wide dock until a file preview or change detail is open",
);
mustContain(
  "src/components/WorkspacePanel.tsx",
  workspacePanel,
  "workspace-panel--detail-only",
  "changes view needs a narrow single-column layout before detail expansion",
);

const contextPanel = read("src/components/ContextPanel.tsx");
mustContain(
  "src/components/ContextPanel.tsx",
  contextPanel,
  "onDetailModeChange?.(true);",
  "context detail entry must notify the parent dock to use detail width",
);
mustContain(
  "src/styles.css",
  styles,
  ".workspace-panel--detail-only",
  "changes view needs CSS support for the narrow single-column state",
);
mustContain(
  "src/styles.css",
  styles,
  "grid-template-columns: minmax(0, 1fr);",
  "context overview must fit the narrow right dock without forcing a two-column chart layout",
);

const bridge = read("src/lib/bridge.ts");
mustContain(
  "src/lib/bridge.ts",
  bridge,
  "function mockScenario(): \"demo\" | \"fresh\" | \"running\"",
  "browser preview needs explicit scenarios instead of always presenting busy mock data",
);
mustContain(
  "src/lib/bridge.ts",
  bridge,
  "value === \"running\"",
  "running preview must be opt-in with ?mock=running",
);
mustContain(
  "src/lib/bridge.ts",
  bridge,
  "const runningMock = scenario === \"running\";",
  "busy topic/tab state must be gated behind the running scenario",
);
mustContain(
  "src/lib/bridge.ts",
  bridge,
  "const mockTopicRunsInScenario = (topicId: string) => runningMock && mockTopicIsRunning(topicId);",
  "all mock runtime state must stay behind the explicit running scenario",
);
mustContain(
  "src/lib/bridge.ts",
  bridge,
  "if (!runningMock) return;",
  "mock runtime event injection must be disabled for the default browser preview",
);
mustNotContain(
  "src/lib/bridge.ts",
  bridge,
  "running: mockTopicIsRunning(",
  "tab running state must not bypass the explicit running scenario",
);
for (const status of ["streaming", "thinking", "waiting_confirmation"]) {
  mustContain(
    "src/lib/bridge.ts",
    bridge,
    `status: runningMock ? "${status}"`,
    `mock ${status} state must not appear in the default browser preview`,
  );
}
mustContain(
  "src/lib/bridge.ts",
  bridge,
  "running: runningMock && mockTopicIsRunning(\"topic_p3b_pd\")",
  "mock tab running state must not be active in the default browser preview",
);

if (process.exitCode) {
  process.exit(process.exitCode);
}

console.log("Browser preview stability check passed");
