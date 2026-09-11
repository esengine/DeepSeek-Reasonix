// Run: tsx src/__tests__/isolated-worktree.test.ts
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { messageActionLabelKey } from "../lib/messageActions";
import type { TabMeta } from "../lib/types";

const dir = dirname(fileURLToPath(import.meta.url));
const source = (path: string) => readFileSync(resolve(dir, path), "utf8");
const bridge = source("../lib/bridge.ts");
const tree = source("../components/ProjectTree.tsx");
const badge = source("../components/WorktreeBadge.tsx");
const forkAction = source("../lib/forkWorktree.ts");
const message = source("../components/Message.tsx");
const mergeModal = source("../components/WorktreeMergeModal.tsx");
const mergeStyles = source("../components/WorktreeMergeModal.css");
const controller = source("../lib/useController.ts");
const navigationFence = source("../lib/useNavigationIntentFence.ts");

let failed = 0;
function ok(value: unknown, label: string) {
  if (value) process.stdout.write(`  PASS  ${label}\n`);
  else {
    failed += 1;
    process.stdout.write(`  FAIL  ${label}\n`);
  }
}

console.log("\nisolated worktree");
ok(/IsolatedWorktreeAvailability\(workspaceRoot: string\)/.test(bridge), "bridge exposes non-mutating availability probe");
ok(/CreateIsolatedWorktree\(workspaceRoot: string\)/.test(bridge), "bridge exposes isolated workspace creation");
ok(/app\.IsolatedWorktreeAvailability\(projectRoot\)/.test(tree), "project menu probes Git before enabling isolation");
ok(/disabled: isolatingProject !== null \|\| isolationAvailability\?\.available === false/.test(tree), "menu disables unavailable or duplicate creation");
ok(/onCreateIsolatedWorktree\?\.\(workspaceRoot\)/.test(tree), "project menu delegates isolated workspace creation");
// Project commands drive the production coalescing queue under deferred work
// in project-topic-lifecycle.test.tsx; callback location is not a contract.
// desktop-navigation-lifecycle.test.tsx verifies the actual dirty-worktree notice.
// The top session tab strip is gone; topicbar-region.test.tsx mounts the real
// badge and verifies conditional identity, accessible labeling and source-bound
// merge actions for isolated/ordinary topics.
ok(/node\.isolatedWorktree && <WorktreeBadge/.test(tree), "project tree identifies isolated worktrees");
ok(/GitBranch/.test(badge) && /#6119/.test(badge), "shared badge preserves the credited #6119 design contribution");
ok(/bindings\.ForkWorktreeForTab\(sourceTabId, turn\)/.test(forkAction) && /makeMockForkBindings/.test(bridge) && !/async ForkWorktreeForTab\(tabID, turn\)/.test(bridge), "isolated conversation fork and browser mock use the extracted two-argument binding");
ok(!/ForkForTab\(sourceTabId, turn, isolate/.test(forkAction), "shared fork never sends an extra bridge argument");
ok(/result\.sourceDirty[\s\S]*forkWorktreeDirtySource/.test(forkAction), "dirty sources are refused with actionable guidance");
ok(/result\.fallbackToShared[\s\S]*forkWorktreeFallbackNotice/.test(forkAction), "backend fallback state reaches the user");
ok(/scope === "fork" \|\| scope === "fork-worktree"/.test(message), "both fork modes require a conversation boundary");
ok(messageActionLabelKey("fork-worktree", false) === "rewind.forkWorktree", "isolated fork keeps its menu label after extraction");
ok(messageActionLabelKey("fork-worktree", true) === "rewind.confirmForkWorktree", "isolated fork keeps its confirmation label after extraction");
ok(/useState\(false\)/.test(mergeModal) && /autoCommitDirty/.test(mergeModal), "dirty auto-commit is opt-in by default");
ok(/InspectWorktreeMerge\(tabId\)[\s\S]*inspectionIdentity\(refreshed\)[\s\S]*MergeWorktreeBack\(\{/.test(mergeModal), "confirm re-inspects before sending one identity-bound merge request");
ok(/stateChanged/.test(mergeModal) && /setInspection\(refreshed\)/.test(mergeModal), "state drift refreshes the panel instead of continuing");
ok(/aria-modal="true"/.test(mergeModal) && /event\.key === "Escape"/.test(mergeModal) && /event\.key !== "Tab"/.test(mergeModal), "merge dialog exposes modal, escape, and focus-loop semantics");
ok(/WorktreeMergeModal\.css/.test(mergeModal) && mergeStyles.includes(".worktree-merge__body") && !/style=\{\{/.test(mergeModal), "lazy merge UI keeps layout rules out of inline styles");
ok(/worktreeStateToken/.test(mergeModal) && /expectedWorktreeStateToken/.test(mergeModal), "merge confirmation binds the exact dirty worktree content token");
ok(!/ModalCloseButton autoFocus/.test(mergeModal), "merge modal captures its trigger before moving focus so close restores the trigger");
ok(/CloseMergedWorktreeTab\(request: CloseMergedWorktreeTabRequest\)/.test(bridge), "worktree close is a request-object bridge call");
ok(/FinalizeWorktreeMerge\(request: WorktreeCleanupRequest\)/.test(bridge), "cleanup is a separate request-object bridge call");
const fencedNavigationCalls = [
  ["const resumeSession", "app.ResumeSessionPage"],
  ["const openChannelSession", "app.OpenChannelSessionPageForTab"],
  ["const pickWorkspace", "app.PickWorkspace"],
  ["const switchWorkspace", "app.SwitchWorkspace"],
  ["const switchTab", "app.SetActiveTab"],
  ["const openProjectTab", "app.OpenProjectTab"],
  ["const openGlobalTab", "app.OpenGlobalTab"],
  ["const openTopicSession", "app.OpenTopicSession"],
  ["const activateTopic", "app.StartTopicActivation"],
  ["const ensureBlankTab", "app.EnsureBlankTab"],
  ["const ensureBlankSurface", "app.EnsureBlankSurface"],
  ["const createIsolatedWorktree", "app.CreateIsolatedWorktree"],
  ["const closeTab", "app.CloseTabWithPolicy"],
];
ok(fencedNavigationCalls.every(([startMarker, callMarker]) => {
  const start = controller.indexOf(startMarker);
  const fence = controller.indexOf("await requireRegisteredNavigationIntent", start);
  const call = controller.indexOf(callMarker, start);
  return start >= 0 && fence > start && call > fence;
}), "navigation entry points await backend intent registration before switching");
ok(/navigationIntentRegistrationTail\.then/.test(navigationFence) && /navigationIntentRegistrationTail = registered/.test(navigationFence), "navigation registrations preserve user-intent order across deferred bridge calls and remounts");

const { makeMockForkBindings } = await import("../lib/mockForkWorktree");
const { settleForkConversationForTab } = await import("../lib/controllerSwitchNotices");
const original = { id: "source", active: true, workspaceRoot: "/project", topicTitle: "Source" } as TabMeta;
let mockTabs = [original];
const mockFork = makeMockForkBindings(() => mockTabs, tabs => { mockTabs = tabs; }, "Untitled");
const isolated = await mockFork.ForkWorktreeForTab(original.id, 3);
ok(isolated.isolated && isolated.tab.workspaceRoot === "/project-worktree" && mockTabs[0].active === false,
  "separate mock bindings retain isolated-worktree and activation behavior");
const forkCalls: string[] = [];
const bindings = {
  ForkForTab: async (id: string, turn: number) => { forkCalls.push(`shared:${id}:${turn}`); return isolated.tab; },
  ForkWorktreeForTab: async (id: string, turn: number) => { forkCalls.push(`isolated:${id}:${turn}`); return { ...isolated, sourceDirty: true }; },
};
const adopt = async () => { forkCalls.push("adopt"); };
const sync = async () => { forkCalls.push("sync"); };
const notice = () => { forkCalls.push("notice"); };
const sharedResult = await settleForkConversationForTab(bindings, original.id, 4, false, notice, adopt, sync);
ok(sharedResult.ok && forkCalls.join(",") === "shared:source:4,adopt", "lazy action entry retains exact shared-fork arguments and adoption");
forkCalls.length = 0;
const dirtyResult = await settleForkConversationForTab(bindings, original.id, 5, true, notice, adopt, sync);
ok(!dirtyResult.ok && forkCalls.join(",") === "isolated:source:5,notice,sync", "lazy action entry preserves dirty-worktree refusal without adoption");

if (failed) process.exit(1);
console.log("isolated worktree tests passed");
