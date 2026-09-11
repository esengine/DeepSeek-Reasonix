import { composerProfileFromTab, composerProfileMode, controllerComposerProfileCollaborationMode, displayedComposerProfileCollaborationMode, type ComposerProfile } from "../lib/composerProfile";
import type { CollaborationMode, TabMeta, ToolApprovalMode } from "../lib/types";
import { sessionIdentityKey } from "./sessionTarget";
import type { SessionOperationAuthority, SessionResource } from "./useSessionOperations";

export type ControllerProfile = { collaboration: CollaborationMode; approval: ToolApprovalMode; goal: string };
export type ControllerProfileResource = { target: SessionResource; profile: ControllerProfile; remote: boolean };
export type ControllerProfilePorts = {
  model(tabId: string, name: string): Promise<boolean>;
  profile(tabId: string, collaboration: CollaborationMode, approval: ToolApprovalMode, goal: string, options: { propagateError: boolean }): Promise<boolean>;
};
const runtimeProfile = (profile: ComposerProfile): ControllerProfile => ({
  collaboration: controllerComposerProfileCollaborationMode(profile), approval: profile.toolApprovalMode, goal: profile.goal,
});

/** A display-free read projection, not a second profile store. */
export function projectControllerProfiles(tabs: readonly TabMeta[], profiles: Readonly<Record<string, ComposerProfile>>,
  active: { target: SessionResource; profile: ComposerProfile; remote: boolean }): ControllerProfileResource[] {
  return [{ target: active.target, profile: runtimeProfile(active.profile), remote: active.remote },
    ...tabs.filter(tab => tab.id !== active.target.tabId).map(tab => ({
      target: { tabId: tab.id, sessionKey: sessionIdentityKey({ tabId: tab.id, sessionPath: tab.sessionPath,
        sessionGeneration: tab.sessionGeneration, scope: tab.scope, workspaceRoot: tab.workspaceRoot, topicId: tab.topicId }) },
      profile: runtimeProfile(profiles[tab.id] ?? composerProfileFromTab(tab)), remote: Boolean(tab.remote),
    }))];
}

export type ControllerProfileInput = {
  target: SessionResource;
  read(target: SessionResource): ControllerProfileResource;
  ports: ControllerProfilePorts;
};

/** Rebuild and ordinary readiness restoration use the same source-profile application. */
export async function executeControllerProfile(input: ControllerProfileInput, authority: SessionOperationAuthority): Promise<boolean> {
  authority.checkpoint();
  const resource = input.read(input.target);
  if (resource.remote) return false;
  // Rebuilding may outlive a profile commit. Read only this resource's committed
  // values, never the render captured before rebuilding or the active tab.
  const { collaboration, approval, goal } = input.read(input.target).profile;
  const applied = await input.ports.profile(input.target.tabId, collaboration, approval, goal, { propagateError: true });
  authority.checkpoint();
  return applied;
}

export async function executeControllerModel(input: ControllerProfileInput & {
  name: string; remote?: (name: string) => Promise<void>;
  restore(target: SessionResource): Promise<boolean>;
}, authority: SessionOperationAuthority): Promise<boolean> {
  authority.checkpoint();
  if (input.read(input.target).remote) {
    if (!input.remote) return false;
    await input.remote(input.name);
  } else {
    if (!await input.ports.model(input.target.tabId, input.name)) return false;
    authority.checkpoint();
    // Startup, explicit send readiness and post-model restoration share one
    // request channel. A coalesced Controller failure has only one UI owner.
    if (!await input.restore(input.target)) return false;
  }
  authority.checkpoint();
  return authority.ownsUI();
}

/** Visible tab strip projection: committed order plus profile display fields. */
export function projectVisibleTabs(input: {
  tabs: readonly TabMeta[];
  orderIds: readonly string[];
  profiles: Readonly<Record<string, ComposerProfile>>;
  visibleTabId: string | undefined;
  running: boolean;
}) {
  const byId = new Map(input.tabs.map((tab) => [tab.id, tab]));
  const ordered = input.orderIds.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
  const missing = input.tabs.filter((tab) => !input.orderIds.includes(tab.id));
  return [...ordered, ...missing].map((tab) => {
    const profile = input.profiles[tab.id] ?? composerProfileFromTab(tab);
    return {
      ...tab,
      running: tab.id === input.visibleTabId ? tab.running || input.running : tab.running,
      mode: composerProfileMode(profile),
      collaborationMode: displayedComposerProfileCollaborationMode(profile),
      toolApprovalMode: profile.toolApprovalMode,
      goal: profile.goal,
      active: tab.id === input.visibleTabId,
    };
  });
}
