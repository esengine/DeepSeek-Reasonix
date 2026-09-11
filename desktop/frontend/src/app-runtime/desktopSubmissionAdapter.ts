import { app } from "../lib/bridge";
import { displayedComposerProfileCollaborationMode, type ComposerProfile } from "../lib/composerProfile";
import type { TabMeta } from "../lib/types";
import type { StructuredInvocationSubmit } from "../lib/invocationDisplay";
import type { ControllerProfileResource } from "./controllerProfileOwner";
import type { InitialGoal, SubmissionPorts, SubmissionResource } from "./sessionSubmissionOwner";

export function createSubmissionPorts(input: {
  send(tab: string, display: string, submit?: string, original?: string, structured?: StructuredInvocationSubmit, initialGoal?: InitialGoal): Promise<void>;
  setGoal(tab: string, goal: string): Promise<void>; clearGoal(tab: string): Promise<void>;
  clearUndo: SubmissionPorts["clearUndo"]; patchGoal: SubmissionPorts["patchGoal"]; profile: SubmissionPorts["profile"];
}): SubmissionPorts {
  return { clearUndo: input.clearUndo, patchGoal: input.patchGoal, profile: input.profile,
    send: (tab, display, submit, structured, goal) => input.send(tab, display, submit, undefined, structured, goal),
    setGoal: (tab, goal, remote) => remote ? app.SetRemoteTabGoal(tab, goal) : goal ? input.setGoal(tab, goal) : input.clearGoal(tab),
  };
}

export function projectSubmissionResources(resources: readonly ControllerProfileResource[], tabs: readonly TabMeta[],
  profiles: Readonly<Record<string, ComposerProfile>>, active: { tabId: string; profile: ComposerProfile; ready: boolean },
  messages: { starting: string; readOnly: string }): SubmissionResource[] {
  return resources.map(resource => {
    const tab = tabs.find(value => value.id === resource.target.tabId);
    const profile = resource.target.tabId === active.tabId ? active.profile : profiles[resource.target.tabId];
    const ready = Boolean(tab?.ready && (!tab.runtime || tab.runtime.phase === "ready") && !tab.startupErr)
      && (resource.target.tabId !== active.tabId || active.ready);
    return { target: resource.target, remote: resource.remote,
      ready,
      unavailable: tab?.readOnly ? messages.readOnly : ready ? "" : tab?.runtime?.issue?.message || tab?.startupErr || messages.starting,
      goalDraft: Boolean(profile && displayedComposerProfileCollaborationMode(profile) === "goal" && !profile.goal.trim()),
      collaboration: resource.profile.collaboration, approval: resource.profile.approval,
    };
  });
}
