// Run: tsx src/__tests__/remote-project-tree.test.tsx
// Source-contract test: the remote project group's tree behavior — session
// rows, the + action, the remote context menu, the connection badge, and
// the local-action guards — is wired exactly once and in the remote shape.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { activeRemoteProjectAncestorKeys } from "../components/ProjectTreeRemoteGroups";
import type { ProjectNode } from "../lib/types";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nRemote project tree wiring");
const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(resolve(here, "../components/ProjectTree.tsx"), "utf8");
const remoteSource = readFileSync(resolve(here, "../components/ProjectTreeRemoteGroups.tsx"), "utf8");
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
const modeActionsSource = readFileSync(resolve(here, "../lib/useComposerModeActions.ts"), "utf8");
const composerSource = readFileSync(resolve(here, "../components/Composer.tsx"), "utf8");
const contentMenuSource = readFileSync(resolve(here, "../components/ComposerContentMenuActions.tsx"), "utf8");
const remoteIntegrationSource = readFileSync(resolve(here, "../lib/useRemoteComposerIntegration.ts"), "utf8");
const topicbarMenuSource = readFileSync(resolve(here, "../components/TopicbarMoreMenuContent.tsx"), "utf8");
const bridgeSource = readFileSync(resolve(here, "../lib/remoteProjectBridge.ts"), "utf8");
const remoteOpenSource = readFileSync(resolve(here, "../../../remote_projects.go"), "utf8");
const remotePendingSelectionSource = readFileSync(resolve(here, "../../../remote_tab_pending_selection.go"), "utf8");
const explicitEnsureSource = remoteSource.match(
  /const ensureRemoteGroupSessions[\s\S]*?\n  const openRemoteWindow/,
)?.[0] ?? "";

const sameWorkspaceRemoteTree: ProjectNode[] = [
  { key: "remote-host-a", kind: "project", label: "A", remote: { hostId: "host-a", workspace: "/repo" }, children: [] },
  { key: "remote-host-b", kind: "project", label: "B", remote: { hostId: "host-b", workspace: "/repo" }, children: [] },
];
ok(
  JSON.stringify(activeRemoteProjectAncestorKeys(sameWorkspaceRemoteTree, { hostId: "host-b", workspace: "/repo" }, (node) => node.key ?? "")) === JSON.stringify(["remote-host-b"]),
  "active remote group expansion uses host-qualified identity instead of local workspace fallback",
);

ok(
  /remoteSession: \{ hostId: node\.remote!\.hostId, workspace: node\.remote!\.workspace, name: row\.name, path: row\.path, title: row\.title \}/.test(remoteSource) &&
    /openRemoteSessionNode\(remote, openRemoteProject\)/.test(source) &&
    /sessionPath: remote\.path, sessionTitle: remote\.title/.test(remoteSource),
  "session rows open the matching in-app remote session",
);
ok(
  /rows\.map\(\(row\): ProjectNode =>/.test(remoteSource) && /mergeRemoteSessionsIntoTree\(tree, remoteSessions, t\)/.test(source) &&
    /root: node\.remote!\.workspace/.test(remoteSource) && /sessionPath: row\.path/.test(remoteSource),
  "remote group children render with the active workspace and session identity",
);
ok(
  /app\.RemoteProjectSessions\(hostId, workspace\)/.test(remoteSource),
  "sessions are fetched through the bridge",
);
ok(
  /state !== "connected" && state !== "degraded"/.test(remoteSource),
  "session fetch waits for a connected host",
);
ok(
  /key: "remote-new-session"[\s\S]*?key: "remote-open-window"[\s\S]*?key: "remote-stop-server"[\s\S]*?key: "remote-unpin"/.test(remoteSource),
  "the remote menu exposes new-session, browser, stop, and unpin actions",
);
ok(
  /items=\{node\.remote \? remoteProjectMenuItems :/.test(source),
  "remote groups swap out the local project menu",
);
ok(
  /app\.OpenRemoteProjectTab\(ref\.hostId, ref\.workspace,[\s\S]*?newSession: true/.test(remoteSource) &&
    /app\.ConnectRemoteHost\(ref\.hostId\)[\s\S]*?waitForRemoteConnection\(ref\.hostId\)[\s\S]*?app\.OpenRemoteWorkspace\(ref\.hostId, ref\.workspace\)/.test(remoteSource),
  "in-app tabs use the remote-session bridge while the browser surface reconnects first",
);
ok(
  /app\.RemoveRemoteProject\(ref\.hostId, ref\.workspace\)/.test(remoteSource) && /void refresh\(\);/.test(remoteSource),
  "unpin removes the registration and refreshes the tree",
);
ok(
  /remoteServeBadgeState\(remoteServers\[node\.remote\.hostId\]\?\.\[node\.remote\.workspace\], remoteGroupBusy/.test(source),
  "the group row badge reflects the workspace-specific serve state",
);
ok(
  /sessionLoads\.current\.has\(key\)/.test(remoteSource) &&
    /sessionLoadGenerations\.current\.set\(key, load\)/.test(remoteSource) &&
    /eligibleSessionKeys\.current\.has\(key\)/.test(remoteSource) &&
    /filter\(\(\[key\]\) => retained\.has\(key\)\)/.test(remoteSource) &&
    /acceptRemoteSessionRows\(key, rows\)/.test(remoteSource) &&
    /catch\(\(error\) => \{[\s\S]*?sessionLoadGenerations\.current\.get\(key\) === load && eligibleSessionKeys\.current\.has\(key\)[\s\S]*?recordRemoteSessionLoadError\(key, error\)/.test(remoteSource) &&
    /recordRemoteSessionLoadError[\s\S]*?setGroupError/.test(remoteSource) &&
    /setGroupError\(\(current\) => current\[key\] \? \{ \.\.\.current, \[key\]: "" \} : current\)/.test(remoteSource),
  "session fetches fence stale results, retain rows on passive failures, and clear recovered group errors",
);
ok(
  /catch \(error\) \{[\s\S]*?recordRemoteSessionLoadError\(key, error\)/.test(explicitEnsureSource) &&
    !/removeRemoteSessionCache\(key\)/.test(explicitEnsureSource) &&
    !/setSessions\(/.test(explicitEnsureSource),
  "an explicit session refresh preserves the last successful rows and cache when Serve fails",
);
ok(
  /useComposerModeActions\(\{[\s\S]*?remote: remoteSurfaceActive/.test(appSource) &&
    /if \(remote && activeTabId\)[\s\S]*?SetRemoteTabComposerProfile\(/.test(modeActionsSource),
  "remote composer mode changes publish all axes through one remote transaction",
);
ok(
  /tab\.id === tabId && tab\.remote[\s\S]*?SetRemoteTabGoal\(tabId, trimmed\)/.test(appSource) &&
    /onSend=\{remoteSurfaceActive \? remoteComposerSend : handleSend\}/.test(appSource),
  "remote goal activation and goal-draft submission stay on the remote controller",
);
ok(
  /remoteRuntimeCommand\(trimmed\)[\s\S]*?command\?\.method === "setModel"[\s\S]*?session\[command\.method\]\(command\.value\)[\s\S]*?await send/.test(remoteIntegrationSource) &&
    /\^\\\/\(model\|effort\)/.test(remoteIntegrationSource),
  "remote model and effort slash commands bypass optimistic conversational submit",
);
ok(
  /trimmed === "\/new"[\s\S]*?method: "newSession"/.test(remoteIntegrationSource) &&
    /trimmed === "\/clear"[\s\S]*?method: "clearSession"/.test(remoteIntegrationSource) &&
    /command\?\.method === "clearSession"[\s\S]*?requestClear\(\)/.test(remoteIntegrationSource) &&
    /command\?\.method === "newSession"[\s\S]*?openRemoteNewSession\(activeRemote, session\.retryHydration\)/.test(remoteIntegrationSource),
  "remote clear and new commands bypass optimistic submit and use session rotation",
);
ok(
  /verb === "compact"[\s\S]*?method: "compact"/.test(remoteIntegrationSource) &&
    /const management = new Set\(\[[\s\S]*?"context"[\s\S]*?"goal"[\s\S]*?"mcp"/.test(remoteIntegrationSource) &&
    /command\?\.method === "runManagementCommand"[\s\S]*?session\.runManagementCommand\(trimmed, command\.rehydrate\)/.test(remoteIntegrationSource) &&
    /command\?\.method === "compact"[\s\S]*?session\.compact\(command\.value\)/.test(remoteIntegrationSource) &&
    /verb === "goal" && remoteGoalCommandStartsTurn\(trimmed\)/.test(remoteIntegrationSource) &&
    /rehydrate: verb === "branch" \|\| verb === "switch" \|\| verb === "rewind"/.test(remoteIntegrationSource),
  "remote non-turn management commands bypass optimistic conversational submit",
);
ok(
  /if \(activeTab\?\.remote\) return openRemoteNewSession\(activeTab\.remote, remoteSession\.retryHydration\)/.test(appSource),
  "global New Session routes the active remote tab through its Serve controller",
);
ok(
  /item\.id !== "cmd-terminal" && item\.id !== "cmd-reload-runtime"/.test(appSource),
  "remote command palettes hide local-only terminal and runtime reload actions",
);
ok(
  /const remote = index\.get\(topicId\);[\s\S]*?if \(!remote\) return false;[\s\S]*?await action\(remote\);/.test(remoteSource) &&
    !/if \(remote\.name\) await action\(remote\)/.test(remoteSource),
  "synthetic blank remote sessions still invoke rename, pin, and delete mutations",
);
ok(
  /onRemoteTabUpdated\(\(meta\)/.test(remoteSource) &&
    /RemoteProjectSessions\(meta\.remote\.hostId, meta\.remote\.workspace\)/.test(remoteSource),
  "remote tab metadata updates refresh the affected session group",
);
ok(
  /attachmentInputEnabled=\{!remoteSurfaceActive\}/.test(appSource) &&
    /if \(!attachmentInputEnabled\) return;/.test(composerSource) &&
    /disabled=\{!attachmentInputEnabled\}/.test(composerSource) &&
    /attachmentInputEnabled \?/.test(contentMenuSource),
  "remote composer disables local attachment input and native file paths",
);
ok(
  /localDurableGuidance=\{!remoteSurfaceActive\}/.test(appSource) &&
    /if \(!localDurableGuidance && onSteer\)[\s\S]*?await onSteer\(guidanceSubmitText, submitTabId\)/.test(composerSource) &&
    /app\.SteerRemoteTab\(sourceTabId, text\.trim\(\)\)/.test(appSource),
  "running remote guidance uses the Serve inbox instead of the local durable inbox",
);
ok(
  /remoteSurfaceActive \? remoteSession\.transcript\.items : state\.items/.test(appSource) &&
    /sessionItemsToMarkdown\(sessionTitle, exportItems, exportLive\)/.test(appSource),
  "remote exports use the visible remote transcript",
);
ok(
  /const visibleRuntimeState = remoteSurfaceActive \? remoteSession\.transcript : state/.test(appSource) &&
    /tabId=\{remoteSurfaceActive \? undefined : activeTabId\}/.test(appSource) &&
    /onCancelJob=\{remoteSurfaceActive \? remoteSession\.cancelJob : cancelJob\}/.test(appSource) &&
    /backgroundRuntimes=\{remoteSurfaceActive \? \[\] : backgroundRuntimes\}/.test(appSource),
  "remote status and context chrome never fall back to local session telemetry",
);
ok(
  /turnPhase=\{visibleRuntimeState\.turnPhase\}/.test(appSource) &&
    /turnStartAt=\{visibleRuntimeState\.turnStartAt\}/.test(appSource) &&
    /liveStore=\{remoteSurfaceActive \? remoteSession\.liveStore : liveStore\}/.test(appSource) &&
    /goalRuntime=\{remoteSurfaceActive \? remoteSession\.goalRuntime : state\.meta\?\.goalRuntime\}/.test(appSource) &&
    /context=\{visibleRuntimeState\.context\}/.test(appSource),
  "remote composer timing, tokens, live stream, and cost use the visible remote runtime",
);
ok(
  /localWorkspaceDockBlocked = remoteSurfaceActive && \(rightDockMode === "files" \|\| rightDockMode === "changed"\)/.test(appSource) &&
    /surfaceWorkspacePanelRenderable = effectiveWorkspacePanelRenderable && !localWorkspaceDockBlocked/.test(appSource) &&
    /\{surfaceWorkspacePanelRenderable && \([\s\S]*?<WorkspacePanel/.test(appSource),
  "remote surfaces do not mount the local Files or Changes workspace panel",
);
ok(
  /session\.pauseGoal\(\)/.test(remoteIntegrationSource) && /session\.resumeGoal\(\)/.test(remoteIntegrationSource),
  "remote Goal pause and resume actions route to the remote session",
);
ok(
  /for \(let i = visibleRuntimeState\.items\.length - 1/.test(appSource) &&
    /!remoteSurfaceActive && activeTabId && todoBatch/.test(appSource),
  "remote todo shelf projects the visible transcript without calling the local dismissal backend",
);
ok(
  /if \(remoteSurfaceActive\) return;[\s\S]*?setTerminalPanelOpen/.test(appSource) &&
    /!remoteSurfaceActive && terminalContentVisible/.test(appSource) &&
    /disabled=\{!terminalEnabled\}/.test(topicbarMenuSource),
  "remote surfaces disable terminal shortcuts, mounting, and menu actions",
);
ok(
  /SetRemoteTabComposerProfile\(activeTabId, controllerMode, toolApprovalMode, ""\)/.test(modeActionsSource) &&
    !/Promise\.allSettled\(\[[\s\S]*?SetRemoteTabGoal\(activeTabId, goal\)/.test(modeActionsSource),
  "remote collaboration changes rely on the atomic backend transaction instead of tunnel rollback",
);
ok(
  /EnsureRemoteProjectSessions\(hostId: string, workspace: string\): Promise<RemoteSessionView\[\]>;/.test(bridgeSource),
  "the bridge exposes the explicit-intent ensure listing",
);
ok(
  /app\.EnsureRemoteProjectSessions\(hostId, workspace\)/.test(remoteSource) &&
    /if \(groupBusyRef\.current\.has\(key\)\) return;/.test(remoteSource) &&
    /sessionLoadGenerations\.current\.set\(key, load\);[\s\S]*?EnsureRemoteProjectSessions\(hostId, workspace\)[\s\S]*?sessionLoadGenerations\.current\.get\(key\) !== load/.test(remoteSource),
  "a group click cold-starts the serve through the deduped, generation-fenced ensure listing",
);
ok(
  /if \(node\.remote && willExpand\) \{\s*void ensureRemoteGroupSessions\(node\.remote\.hostId, node\.remote\.workspace\);/.test(source),
  "an explicit expand refreshes remote rows even when an optimistic cache exists",
);
ok(
  /activeRemoteProjectAncestorKeys\(treeWithRemoteSessions, activeRemote, projectNodeKey\)/.test(source) &&
    /defaultExpandedProjectTreeKeys\(treeWithRemoteSessions, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath\)/.test(source),
  "active remote ancestors come from the merged tree and host-qualified group identity",
);
ok(
  /markActive\(treeWithRemoteSessions\)/.test(source) &&
    /markNodeRead, treeWithRemoteSessions\]\);/.test(source) &&
    !/markActive\(tree\);/.test(source),
  "active synthesized remote rows are marked read when the merged session tree arrives",
);
ok(
  /project-tree__remote-status/.test(remoteSource) && /projectTree\.remoteConnectFailed/.test(remoteSource),
  "remote groups render retryable connect/error rows instead of going silent",
);
ok(
  /existing\.selectionRevision\+\+/.test(remoteOpenSource) &&
    /a\.goRemoteTabSafe\("remoteTabResume"[\s\S]*?restoreRejectedRemoteTabOpenSelection/.test(remotePendingSelectionSource),
  "session switches resume in the background behind a generation guard",
);
ok(
  /if \(\(project\.kind !== "project" && project\.kind !== "global_folder"\) \|\| project\.remote\) return;/.test(source),
  "remote groups never reach ListProjectTopics with their virtual root",
);

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
