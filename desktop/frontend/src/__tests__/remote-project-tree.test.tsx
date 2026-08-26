// Run: tsx src/__tests__/remote-project-tree.test.tsx
// Source-contract test: the remote project group's tree behavior — session
// rows, the + action, the remote context menu, the connection badge, and
// the local-action guards — is wired exactly once and in the remote shape.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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

ok(
  /remoteSession: \{ hostId: node\.remote!\.hostId, workspace: node\.remote!\.workspace, name: row\.name \}/.test(remoteSource) &&
    /openRemoteSessionNode\(remote, openRemoteProject\)/.test(source) &&
    /void open\(remote, remote\.name \? \{ sessionName: remote\.name \} : \{ focus: true \}\)/.test(remoteSource),
  "session rows open the matching in-app remote session",
);
ok(
  /rows\.map\(\(row\): ProjectNode =>/.test(remoteSource) && /mergeRemoteSessionsIntoTree\(tree, remoteSessions, t\)/.test(source),
  "remote group children render from the fetched session list",
);
ok(
  /app\.RemoteProjectSessions\(hostId, workspace\)/.test(remoteSource),
  "sessions are fetched through the bridge",
);
ok(
  /state === "connected" \|\| state === "degraded"/.test(remoteSource),
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
  /project-tree__remote-badge--\$\{remoteServeBadgeState\(remoteServers\[node\.remote\.hostId\]\?\.\[node\.remote\.workspace\]\)\}/.test(source),
  "the group row badge reflects the workspace-specific serve state",
);
ok(
  /sessionLoads\.current\.has\(key\)/.test(remoteSource) &&
    /eligibleSessionKeys\.current\.has\(key\)/.test(remoteSource) &&
    /filter\(\(\[key\]\) => connected\.has\(key\)\)/.test(remoteSource),
  "session fetches deduplicate in flight and discard disconnected or stale group results",
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

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
