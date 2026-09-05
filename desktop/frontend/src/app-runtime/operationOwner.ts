export type OperationTarget =
  | { kind: "session"; tabId: string; sessionKey: string }
  | { kind: "workspace"; workspaceKey: string }
  | { kind: "application" };

export type OperationIdentity = {
  ownerEpoch: number;
  requestId: number;
  target: OperationTarget;
  navigationIntent?: number;
};

export type OperationTerminalStatus = "completed" | "failed" | "cancelled";

export function operationTargetsEqual(left: OperationTarget, right: OperationTarget): boolean {
  if (left.kind !== right.kind) return false;
  if (left.kind === "application") return true;
  if (left.kind === "workspace" && right.kind === "workspace") {
    return left.workspaceKey === right.workspaceKey;
  }
  return left.kind === "session"
    && right.kind === "session"
    && left.tabId === right.tabId
    && left.sessionKey === right.sessionKey;
}

function freezeIdentity(identity: OperationIdentity): OperationIdentity {
  return Object.freeze({ ...identity, target: Object.freeze({ ...identity.target }) });
}

export type OperationOwner = ReturnType<typeof createOperationOwner>;

/**
 * Owns a last-request-wins interaction without retaining request payloads.
 * Resource data may complete independently; `owns` governs current UI rights.
 */
export function createOperationOwner(trackOperation: (delta: 1 | -1) => void = () => {}) {
  let ownerEpoch = 0;
  let requestId = 0;
  let mounted = false;
  let active: OperationIdentity | undefined;
  const terminalCounts: Record<OperationTerminalStatus, number> = {
    completed: 0,
    failed: 0,
    cancelled: 0,
  };

  return {
    mount(): number {
      if (mounted) return ownerEpoch;
      ownerEpoch += 1;
      mounted = true;
      active = undefined;
      return ownerEpoch;
    },

    unmount(epoch: number): void {
      if (!mounted || epoch !== ownerEpoch) return;
      if (active) {
        terminalCounts.cancelled += 1;
        trackOperation(-1);
      }
      active = undefined;
      mounted = false;
    },

    begin(target: OperationTarget, navigationIntent?: number): OperationIdentity {
      if (!mounted) throw new Error("operation owner is not mounted");
      if (active) terminalCounts.cancelled += 1;
      else trackOperation(1);
      active = freezeIdentity({
        ownerEpoch,
        requestId: ++requestId,
        target,
        ...(navigationIntent === undefined ? {} : { navigationIntent }),
      });
      return active;
    },

    owns(identity: OperationIdentity): boolean {
      return Boolean(
        mounted
        && active
        && identity.ownerEpoch === ownerEpoch
        && identity.requestId === active.requestId
        && operationTargetsEqual(identity.target, active.target)
        && identity.navigationIntent === active.navigationIntent,
      );
    },

    finish(identity: OperationIdentity, status: OperationTerminalStatus = "completed"): boolean {
      if (!this.owns(identity)) return false;
      active = undefined;
      trackOperation(-1);
      terminalCounts[status] += 1;
      return true;
    },

    cancel(identity: OperationIdentity): boolean {
      return this.finish(identity, "cancelled");
    },

    get activeCount(): number {
      return active ? 1 : 0;
    },

    get diagnostics(): Readonly<Record<OperationTerminalStatus, number>> {
      return { ...terminalCounts };
    },
  };
}
