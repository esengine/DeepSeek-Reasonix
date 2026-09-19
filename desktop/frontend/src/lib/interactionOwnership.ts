import { hasSessionGeneration, sessionIdentityStableKey } from "./sessionIdentity";
import { interactionInstanceKey, sameInteractionIdentity, type InteractionKind, type InteractionTarget } from "./interactionTarget";
import type { Meta, WireApproval, WireAsk, WireMCPInteraction } from "./types";

type Prompt = WireApproval | WireAsk | WireMCPInteraction;

export interface InteractionState {
  meta?: Meta;
  activeTurnId?: string;
  approval?: WireApproval;
  ask?: WireAsk;
  mcpInteraction?: WireMCPInteraction;
}

export function promptForInteraction(state: InteractionState, target: Pick<InteractionTarget, "kind">): Prompt | undefined {
  if (target.kind === "ask") return state.ask;
  if (target.kind === "mcp") return state.mcpInteraction;
  return state.approval;
}

export function stateOwnsInteraction(state: InteractionState, target: InteractionTarget): boolean {
  if (!target.sessionId || !hasSessionGeneration(target.sessionGeneration) || !state.meta?.session?.sessionId || !hasSessionGeneration(state.meta.sessionGeneration)) return false;
  if (state.meta.session.sessionId !== target.sessionId || state.meta.sessionGeneration !== target.sessionGeneration) return false;
  if (target.hostId && state.meta.session.hostId !== target.hostId) return false;
  const currentSessionKey = sessionIdentityStableKey(state.meta);
  if (currentSessionKey && target.sessionKey && currentSessionKey !== target.sessionKey) return false;
  const prompt = promptForInteraction(state, target);
  if (!sameInteractionIdentity(prompt, target)) return false;
  if (target.requestGeneration !== undefined && prompt && "generation" in prompt && prompt.generation !== target.requestGeneration) return false;
  if (target.permissionRevision !== undefined && prompt && "permissionRevision" in prompt && prompt.permissionRevision !== target.permissionRevision) return false;
  return true;
}

export function promptInstanceKeyForState(state: InteractionState, prompt: Prompt, kind: InteractionKind): string {
  return interactionInstanceKey({
    tabId: "",
    sessionKey: sessionIdentityStableKey(state.meta),
    hostId: state.meta?.session?.hostId,
    sessionId: state.meta?.session?.sessionId,
    sessionGeneration: state.meta?.sessionGeneration,
    promptId: prompt.id,
    turnId: prompt.turnId,
    runtimeEpoch: prompt.runtimeEpoch,
    kind,
    requestGeneration: "generation" in prompt ? prompt.generation : undefined,
    permissionRevision: "permissionRevision" in prompt ? prompt.permissionRevision : undefined,
  });
}

export function interactionTargetFromState(
  tabId: string,
  state: InteractionState | undefined,
  kind: InteractionKind,
  promptId: string,
): InteractionTarget {
  const prompt = state ? promptForInteraction(state, { kind }) : undefined;
  const base = {
    tabId,
    sessionKey: sessionIdentityStableKey(state?.meta) || tabId,
    hostId: state?.meta?.session?.hostId,
    sessionId: state?.meta?.session?.sessionId,
    sessionGeneration: state?.meta?.sessionGeneration,
    promptId,
    turnId: prompt?.turnId ?? state?.activeTurnId,
    runtimeEpoch: prompt?.runtimeEpoch ?? state?.meta?.runtime?.epoch,
    kind,
    requestGeneration: prompt && "generation" in prompt ? prompt.generation : undefined,
    permissionRevision: prompt && "permissionRevision" in prompt ? prompt.permissionRevision : undefined,
  };
  return { ...base, instanceKey: interactionInstanceKey(base) };
}
