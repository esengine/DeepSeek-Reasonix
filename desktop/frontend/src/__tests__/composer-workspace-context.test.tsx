import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { buildComposerSurface, type ComposerSurfaceInput } from "../app-shell/decisionFooterBuilders";
import { ComposerWorkspaceContextBar, type ComposerWorkspaceContext } from "../components/ComposerWorkspaceContextBar";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import { installBridgeApp, installDom } from "./composerInboxHarness";

const dom = installDom("en-US");
const calls = {
  snapshots: 0,
  switches: [] as string[],
  checkouts: [] as string[],
  creates: [] as string[],
  history: 0,
  globals: 0,
};
installBridgeApp({
  GetProjectTreeSnapshot: async () => {
    calls.snapshots += 1;
    return {
      revision: calls.snapshots,
      projects: [
        { key: "project:/repo", kind: "project", label: "Reasonix", root: "/repo" },
        { key: "project:/other", kind: "project", label: "Other", root: "/other" },
      ],
      catalog: { state: "ready", revision: 1, indexed: 2, total: 2, repairPending: 0 },
      indexed: 2,
      total: 2,
      indexingDone: true,
    };
  },
  GitBranchesForTab: async () => ["main", "feature/other"],
  GitCheckoutForTab: async (_tab: string, _root: string, name: string) => { calls.checkouts.push(name); },
  GitCreateBranchForTab: async (_tab: string, _root: string, name: string) => { calls.creates.push(name); },
  WorkspaceGitHistory: async () => {
    calls.history += 1;
    return [{ hash: "abcdef123456", author: "Ada", date: "2026-09-16T02:00:00Z", message: "Ship workspace context" }];
  },
});

const baseContext: ComposerWorkspaceContext = {
  scope: "project",
  workspaceRoot: "/repo",
  workspaceName: "Reasonix",
  gitBranch: "feature/current",
  tabId: "tab-a",
  scopeKey: "project:/repo",
  onSwitchWorkspace: async (path) => { if (path) calls.switches.push(path); },
  onWorkWithoutProject: async () => { calls.globals += 1; },
  onRefreshProjects: async () => {},
};

const noop = () => {};
const surfaceInput = {
  base: { running: false },
  view: {
    hidden: false,
    inert: false,
    hero: true,
    headline: "Welcome",
    remote: false,
    rewindCommitting: false,
    messageActionPending: false,
    decisionActive: false,
    runtimeTransitioning: false,
    controllerReady: true,
    showContextWindowRing: false,
  },
  profile: { collaborationMode: "normal", toolApprovalMode: "ask", goal: "" },
  router: { handleSend: noop, handleSteer: noop },
  modes: { applyMode: noop, applyToolApprovalMode: noop },
  goals: { setCollaborationModeFromUi: noop, clearGoalFromUi: noop, editGoalFromUi: noop },
  remoteGoal: { pauseGoal: noop, resumeGoal: noop, setEffort: noop },
  modelSwitch: { switchModelFromUi: noop },
  inserts: { composerInsertRequest: undefined, selectedTextRequest: undefined },
  control: { handleCancelActive: noop },
  remoteComposer: { send: noop, cancel: noop, ready: true, profileReady: true, liveStore: undefined },
  localLiveStore: undefined,
  onInvocationMetadataChange: noop,
  onCycleMode: noop,
  transientDismissSignal: 0,
  sessionKey: "session-a",
  workspaceScopeKey: "project:/repo",
  workspaceContext: baseContext,
  fileRefRefreshKey: "",
  guidance: null,
  guidanceQueuePreviewItems: undefined,
} as unknown as ComposerSurfaceInput;
assert.equal(buildComposerSurface(surfaceInput).props.workspaceContext, baseContext, "empty sessions expose workspace selection");
const missingTab = { authentication: { status: "missing_credential", providerName: "relay" } };
assert.equal(buildComposerSurface({ ...surfaceInput, tab: missingTab }).props.submitDisabled, true, "missing credentials block unchanged settings");
assert.equal(buildComposerSurface({ ...surfaceInput, tab: { ...missingTab, modelSettingsPending: true } }).props.submitDisabled, false, "saved settings can reach backend apply-before-admission");
assert.equal(buildComposerSurface({ ...surfaceInput, tab: { ...missingTab, modelSettingsPending: true }, view: { ...surfaceInput.view, controllerReady: false } }).props.submitDisabled, true, "pending settings never bypass controller readiness");
assert.equal(buildComposerSurface({ ...surfaceInput, view: { ...surfaceInput.view, hero: false } }).props.workspaceContext, undefined, "established sessions use the compact follow-up composer");
const draftSurfaceInput = {
  ...surfaceInput,
  view: { ...surfaceInput.view, hero: false },
  draft: {
    surface: {
      kind: "draft",
      draft: { id: "draft-a", workspaceId: "workspace-a", scope: "project", workspaceRoot: "/repo", revision: 1, contentJson: "{}", settings: {}, status: "active", updatedAt: 1 },
      content: { text: "", invocations: [], attachments: [], workspaceRefs: [], pastedBlocks: [], openPastedLabels: [], sessionRefs: [], selectedTextRefs: [] },
      settings: { model: "fixture/model", mode: "normal", toolApprovalMode: "ask", disabledMcp: {}, mcpOrder: [] },
      commands: [], servers: [], generation: 1, editVersion: 0, pendingTasks: 0, preparingSubmission: false, saveState: "saved",
    },
    captureSubmission: noop, releasePreparation: noop, flushPreparation: noop, submitFrom: noop,
    updateSettingsFor: noop, cancelSubmission: noop, updateContentFor: noop, patchContentFor: noop,
    isCurrentHandle: () => true, canEditHandle: () => true, trackTask: noop, reportTaskError: noop,
  },
} as unknown as ComposerSurfaceInput;
const draftContext = buildComposerSurface(draftSurfaceInput).props.workspaceContext;
assert.equal(draftContext?.workspaceRoot, "/repo", "drafts keep workspace selection when the backing tab is not in its hero state");
assert.equal(draftContext?.scopeKey, "draft:workspace-a", "draft workspace actions use the draft owner identity");

const rootElement = document.getElementById("root");
assert(rootElement);
const root: Root = createRoot(rootElement);
let context = baseContext;
const flush = async () => {
  await new Promise<void>((resolve) => setTimeout(resolve, 0));
  await Promise.resolve();
};
const render = async (next?: Partial<ComposerWorkspaceContext>) => {
  context = { ...context, ...next };
  await act(async () => {
    root.render(<LocaleProvider><ToastProvider><ComposerWorkspaceContextBar context={context} /></ToastProvider></LocaleProvider>);
    await flush();
  });
};
const click = async (element: Element | null) => {
  assert(element);
  await act(async () => {
    element.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
    await flush();
  });
};
await render();
assert(document.querySelector('[role="toolbar"][aria-label="Current work context"]'));
assert(document.querySelector('button[aria-label="Project: Reasonix"]'));
assert(document.querySelector('button[aria-label="Current Git branch: feature/current"]'));

await click(document.querySelector('button[aria-label="Project: Reasonix"]'));
assert.equal(calls.snapshots, 1);
assert(document.querySelector('input[aria-label="Search workspaces"]'));
const projectItems = [...document.querySelectorAll('.composer-workspace-menu--projects [role="menuitem"]')];
assert(projectItems.some((item) => item.textContent?.includes("Reasonix")));
assert(projectItems.some((item) => item.textContent?.includes("Other")));
await click(projectItems.find((item) => item.textContent?.includes("Reasonix")) ?? null);
assert.deepEqual(calls.switches, [], "selecting the current workspace only closes the menu");

await click(document.querySelector('button[aria-label="Project: Reasonix"]'));
await click([...document.querySelectorAll('.composer-workspace-menu--projects [role="menuitem"]')].find((item) => item.textContent?.includes("Other")) ?? null);
assert.deepEqual(calls.switches, ["/other"]);

await click(document.querySelector('button[aria-label="Current Git branch: feature/current"]'));
const currentBranch = [...document.querySelectorAll('.composer-workspace-menu--branches [role="menuitem"]')].find((item) => item.textContent?.includes("feature/current"));
assert(currentBranch, "the active branch stays visible even when the branch RPC omits it");
assert(currentBranch.classList.contains("composer-workspace-menu__item--active"));
await click(currentBranch);
assert.deepEqual(calls.checkouts, [], "selecting the current branch does not issue a redundant checkout");

await click(document.querySelector('button[aria-label="Current Git branch: feature/current"]'));
const otherBranch = [...document.querySelectorAll('.composer-workspace-menu--branches [role="menuitem"]')].find((item) => item.textContent?.includes("feature/other"));
await click(otherBranch ?? null);
assert.deepEqual(calls.checkouts, ["feature/other"]);
assert(document.querySelector('button[aria-label="Current Git branch: feature/other"]'));

await click(document.querySelector('button[aria-label="Current Git branch: feature/other"]'));
const createAction = [...document.querySelectorAll('.composer-workspace-menu--branches [role="menuitem"]')].find((item) => item.textContent?.includes("Create and check out new branch"));
await click(createAction ?? null);
const branchInput = document.querySelector('input[aria-label="New branch name"]') as HTMLInputElement | null;
assert(branchInput);
assert.equal(branchInput.placeholder, "New branch name");
assert.equal([...document.querySelectorAll<HTMLButtonElement>('.composer-workspace-menu--branches [role="menuitem"]')].find((item) => item.textContent?.includes("Create and check out new branch"))?.disabled, true);
const graphAction = [...document.querySelectorAll('.composer-workspace-menu--branches [role="menuitem"]')].find((item) => item.textContent?.includes("Git graph"));
await click(graphAction ?? null);
assert.equal(calls.history, 1);
assert(document.querySelector('[role="dialog"][aria-labelledby="composer-git-graph-title"]'));
assert(document.body.textContent?.includes("Ship workspace context"));
assert(document.body.textContent?.includes("abcdef1"));
await click(document.querySelector('.composer-git-graph__actions button[aria-label="Close"]'));
assert.equal(document.querySelector('.composer-git-graph'), null);
assert.equal(document.activeElement, document.querySelector('button[aria-label="Current Git branch: feature/other"]'), "closing the Git graph restores focus to the branch trigger");

await click(document.querySelector('button[aria-label="Work without a project"]'));
assert.equal(calls.globals, 1);

await render({ scope: "global", workspaceRoot: "/stale-cwd", workspaceName: "Stale workspace", remote: false });
assert(document.querySelector('button[aria-label="Project: No project"]'), "global sessions never inherit a stale cwd label");
assert.equal(document.querySelector('button[aria-label="Work without a project"]'), null, "global sessions do not render a redundant clear action");

await act(async () => { root.unmount(); await flush(); });
dom.window.close();
console.log("PASS composer workspace context project, branch, create and git graph flows");
