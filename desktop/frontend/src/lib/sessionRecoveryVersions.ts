import { asArray } from "./array";
import type { ProjectTopicKey } from "./sessionCatalogTypes";
import type { RecoveryLineageMember, RecoveryLineageView, SessionRecoveryEvent } from "./types";

export type PendingSessionRecovery = {
  eventKey: string;
  topic: ProjectTopicKey;
};

export type RecoveryLineageResolution = "notify" | "clear" | "wait";

export type RecoveryEventRegistration = {
  pending: PendingSessionRecovery | null;
  isNew: boolean;
  occurrence: number;
};

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function count(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? Math.max(0, Math.floor(value)) : 0;
}

export function normalizeRecoveryLineageView(value: unknown): RecoveryLineageView {
  const raw = value && typeof value === "object" ? value as Partial<RecoveryLineageView> : {};
  return {
    groupId: text(raw.groupId),
    state: text(raw.state),
    branchCount: count(raw.branchCount),
    unresolved: count(raw.unresolved),
    cleanupEligible: count(raw.cleanupEligible),
    members: asArray<RecoveryLineageMember>(raw.members).filter((member) => Boolean(member && text(member.path))),
  };
}

export function userVisibleRecoveryVersions(view: Pick<RecoveryLineageView, "members">): RecoveryLineageMember[] {
  return asArray(view.members)
    .filter((member) => member.role !== "covered_copy")
    .sort((left, right) => Number(right.canonical) - Number(left.canonical)
      || count(right.lastActivityAt || right.createdAt) - count(left.lastActivityAt || left.createdAt));
}

export function recoveryLineageResolution(view: RecoveryLineageView): RecoveryLineageResolution {
  if (!view.groupId || view.state === "repairing") return "wait";
  const visibleVersions = userVisibleRecoveryVersions(view);
  if (view.state === "diverged" && view.unresolved > 0 && visibleVersions.length > 1) return "notify";
  if (visibleVersions.length <= 1 && asArray(view.members).length > 0) return "clear";
  if (view.state === "covered" || view.state === "adopted" || view.state === "preferred") return "clear";
  if (view.unresolved === 0 && asArray(view.members).length > 0) return "clear";
  return "wait";
}

export function pendingSessionRecovery(event: SessionRecoveryEvent): PendingSessionRecovery | null {
  const recoveryPath = text(event.recoveryPath);
  const topicId = text(event.topicId);
  if (!recoveryPath || !topicId) return null;
  const scope = text(event.scope) || (text(event.workspaceRoot) ? "project" : "global");
  const workspaceRoot = scope === "project" ? text(event.workspaceRoot) : "";
  const parent = text(event.recoveryParentId);
  return {
    eventKey: [scope, workspaceRoot, topicId, parent, recoveryPath].join("\u0000"),
    topic: { scope, workspaceRoot: workspaceRoot || undefined, topicId, path: recoveryPath },
  };
}

export function recoveryTopicOccurrenceKey(topic: ProjectTopicKey): string {
  return [text(topic.scope) || "global", text(topic.workspaceRoot), text(topic.topicId)].join("\u0000");
}

export function pendingRecoveryMatchesRoots(pending: PendingSessionRecovery, roots: readonly string[]): boolean {
  if (roots.length === 0) return true;
  const workspaceRoot = pending.topic.scope === "project" ? text(pending.topic.workspaceRoot) : "";
  return roots.some((root) => text(root) === workspaceRoot);
}

export function sanitizedRecoveryReason(value: unknown): string {
  const reason = text(value).toLowerCase();
  if (reason.includes("shutdown") || reason.includes("file_lock")) return "shutdown_lock";
  if (reason.includes("external") || reason.includes("removed")) return "external_change";
  if (reason.includes("snapshot") || reason.includes("conflict") || reason.includes("stale")) return "snapshot_conflict";
  return reason ? "other" : "unknown";
}

export class SessionRecoveryDivergenceTracker {
  private readonly pending = new Map<string, PendingSessionRecovery>();
  private readonly notified = new Set<string>();
  private readonly topicOccurrences = new Map<string, number>();

  register(event: SessionRecoveryEvent): RecoveryEventRegistration {
    const pending = pendingSessionRecovery(event);
    if (!pending) return { pending: null, isNew: false, occurrence: 0 };
    if (this.pending.has(pending.eventKey) || this.notified.has(pending.eventKey)) {
      return { pending, isNew: false, occurrence: 0 };
    }
    this.pending.set(pending.eventKey, pending);
    const topicKey = recoveryTopicOccurrenceKey(pending.topic);
    const occurrence = (this.topicOccurrences.get(topicKey) ?? 0) + 1;
    this.topicOccurrences.set(topicKey, occurrence);
    return { pending, isNew: true, occurrence };
  }

  entries(): PendingSessionRecovery[] {
    return [...this.pending.values()];
  }

  resolve(eventKey: string, view: RecoveryLineageView): RecoveryLineageResolution {
    const resolution = recoveryLineageResolution(view);
    if (resolution === "wait") return resolution;
    this.pending.delete(eventKey);
    if (resolution !== "notify" || this.notified.has(eventKey)) return "clear";
    this.notified.add(eventKey);
    return "notify";
  }
}
