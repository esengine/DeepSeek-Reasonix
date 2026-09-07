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
const compositionSource = readFileSync(resolve(here, "../app-runtime/useAppSessionComposition.ts"), "utf8");
const todoSource = readFileSync(resolve(here, "../app-runtime/useTodoPanelCommands.ts"), "utf8");
const paletteSource = readFileSync(resolve(here, "../app-runtime/usePaletteCommands.tsx"), "utf8");
const exportSource = readFileSync(resolve(here, "../app-runtime/useSessionExportCommands.ts"), "utf8");
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
  /app\.ConnectRemoteHost\(ref\.hostId\)[\s\S]*?waitForRemoteConnection\(ref\.hostId\)[\s\S]*?publishNavigationIntent\("remote-workspace"\)[\s\S]*?app\.OpenRemoteWorkspace\(ref\.hostId, ref\.workspace\)/.test(remoteSource),
  "separate remote window registers its intent before switching the external surface",
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
  /item\.id !== "cmd-terminal" && item\.id !== "cmd-reload-runtime"/.test(paletteSource),
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
  /remoteSurfaceActive \? remoteSession\.transcript\.items : state\.items/.test(compositionSource) &&
    /sessionItemsToMarkdown\(sessionTitle, Array\.from\(items\), live\)/.test(exportSource),
  "remote exports use the visible remote transcript",
);
ok(
  /items: visibleRuntimeState\.items/.test(compositionSource) &&
    /for \(let i = items\.length - 1/.test(todoSource) &&
    /!remote && activeTabId && todoBatch/.test(todoSource),
  "remote todo shelf projects the visible transcript without calling the local dismissal backend",
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
