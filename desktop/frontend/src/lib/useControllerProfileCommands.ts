import { useEffect, useMemo } from "react";
import { useCommittedSlot, type CommittedSlot } from "./useCommittedSlot";
import { useCommittedCommand } from "./useCommittedCommand";
import { CommandCancelled } from "./commandOutcome";
import { executeControllerModel, executeControllerProfile, type ControllerProfilePorts, type ControllerProfileResource } from "../app-runtime/controllerProfileOwner";
import type { SessionResource, useSessionOperations } from "../app-runtime/useSessionOperations";

function bindProfileRead(slot: CommittedSlot<readonly ControllerProfileResource[]>) {
  return (target: SessionResource): ControllerProfileResource => {
    if (slot.phase !== "ready") throw new CommandCancelled(slot.phase === "disposed" ? "disposed" : "not-ready");
    const resource = slot.value?.find(value => value.target.tabId === target.tabId && value.target.sessionKey === target.sessionKey);
    if (!resource) throw new CommandCancelled("superseded");
    return resource;
  };
}

export function useControllerProfileCommands(options: {
  target: SessionResource; profiles: readonly ControllerProfileResource[]; ready: boolean; remote: boolean; runtimeEpoch?: string;
  ports: ControllerProfilePorts; remoteModel(name: string): Promise<void>;
  operations: ReturnType<typeof useSessionOperations>; report(error: unknown): void;
}) {
  const { target, profiles, ready, remote, runtimeEpoch, operations, ports, remoteModel, report } = options;
  const slot = useCommittedSlot(profiles);
  const read = useMemo(() => bindProfileRead(slot), [slot]);
  const restore = useCommittedCommand(async (source: SessionResource): Promise<boolean> => {
    const result = await operations(source, "controller-profile", { target: source, read, ports }, executeControllerProfile);
    if (result.status === "failed") throw result.error;
    return result.status === "completed" && result.value;
  });
  const applyProfile = useCommittedCommand(async (tabId = target.tabId, propagateError = true): Promise<boolean> => {
    const source = profiles.find(value => value.target.tabId === tabId);
    if (!source) return false;
    try { return await restore(source.target); } catch (error) {
      if (propagateError) throw error;
      return false;
    }
  });
  const switchModel = useCommittedCommand(async (name: string, tabId = target.tabId): Promise<boolean> => {
    const source = profiles.find(value => value.target.tabId === tabId);
    if (!source || (source.remote && tabId !== target.tabId)) return false;
    const result = await operations(source.target, "model", { target: source.target, read, ports, restore,
      name, remote: source.remote ? remoteModel : undefined }, executeControllerModel);
    if (result.status === "failed") throw result.error;
    return result.status === "completed" && result.value;
  });
  const reportError = useCommittedCommand(report);
  const switchModelFromUi = useCommittedCommand(async (name: string): Promise<boolean> => {
    try { return await switchModel(name); } catch (error) { reportError(error); return false; }
  });
  const active = profiles.find(value => value.target.tabId === target.tabId)?.profile;
  useEffect(() => {
    if (ready && target.tabId && !remote) void applyProfile().catch(reportError);
  }, [ready, remote, runtimeEpoch, target.tabId, target.sessionKey, active?.collaboration, active?.approval, active?.goal, applyProfile, reportError]);
  return { applyProfile, switchModel, switchModelFromUi };
}
