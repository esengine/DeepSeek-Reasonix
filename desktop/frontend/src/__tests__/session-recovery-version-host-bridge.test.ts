import { bindSessionVersionInspector, requestSessionVersions } from "../lib/sessionRecoveryVersionHostBridge";
import type { RecoveryLineageView, SessionMeta } from "../lib/types";

const session = (path: string): SessionMeta => ({
  path, preview: path, turns: 1, turnsState: "valid", createdAt: 1,
  lastActivityAt: 1, modTime: 1, current: false, open: false,
  scope: "global", topicId: "topic",
});
const view: RecoveryLineageView = {
  groupId: "group", state: "diverged", branchCount: 2, unresolved: 2,
  cleanupEligible: 0, members: [],
};
const received: string[] = [];

requestSessionVersions(session("first"), view);
requestSessionVersions(session("latest"), view);
const unbind = bindSessionVersionInspector((next) => received.push(next.path));
if (received.join(",") !== "latest") throw new Error(`queued request mismatch: ${received.join(",")}`);

requestSessionVersions(session("live"), view);
if (received.join(",") !== "latest,live") throw new Error(`live request mismatch: ${received.join(",")}`);

unbind();
requestSessionVersions(session("remount"), view);
bindSessionVersionInspector((next) => received.push(next.path));
if (received.join(",") !== "latest,live,remount") throw new Error(`remount request mismatch: ${received.join(",")}`);

console.log("  PASS  session version host bridge preserves last-click-wins across lazy mounting");
