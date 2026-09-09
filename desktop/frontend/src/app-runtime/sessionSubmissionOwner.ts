import type { CollaborationMode, ToolApprovalMode } from "../lib/types";
import type { StructuredInvocationSubmit } from "../lib/invocationDisplay";
import type { SessionOperationAuthority, SessionResource } from "./useSessionOperations";

export type InitialGoal = { goal: string; collaborationMode: CollaborationMode; toolApprovalMode: ToolApprovalMode };
export type SubmissionResource = {
  target: SessionResource; remote: boolean; ready: boolean; unavailable: string; goalDraft: boolean;
  collaboration: CollaborationMode; approval: ToolApprovalMode;
};
export type Submission = { display: string; submit?: string; structured?: StructuredInvocationSubmit; initialGoal?: InitialGoal };
export type SubmissionPorts = {
  send(tab: string, display: string, submit?: string, structured?: StructuredInvocationSubmit, goal?: InitialGoal): Promise<void>;
  clearUndo(tab: string): void;
  setGoal(tab: string, goal: string, remote: boolean): Promise<void>;
  patchGoal(tab: string, goal: string): void;
  profile(tab: string, propagateError: boolean): Promise<boolean>;
};
export type SubmissionInput = {
  target: SessionResource; read(target: SessionResource): SubmissionResource; ports: SubmissionPorts;
  request: { kind: "direct" | "composer"; content: Submission } | { kind: "goal"; goal: string };
};
const legacyFlags = new Set(["--research", "--auto-research", "--deep", "--simple", "--no-research"]);
export function goalCommand(input: string) {
  const match = /^\/goal(?:\s+(.*))?$/.exec(input);
  if (!match) return undefined;
  const parts = (match[1] ?? "").trim().split(/\s+/).filter(Boolean);
  const legacy = legacyFlags.has(parts[0]?.toLowerCase());
  while (legacyFlags.has(parts[0]?.toLowerCase())) parts.shift();
  const value = parts.join(" ");
  const action = value.toLowerCase();
  return { value, legacy, activate: Boolean(value) && !["status", "clear", "off", "stop", "done", "pause", "resume"].includes(action),
    clear: ["clear", "off", "stop", "done"].includes(action) };
}

async function applyGoal(input: SubmissionInput, goal: string, authority: SessionOperationAuthority) {
  authority.checkpoint();
  await input.ports.setGoal(input.target.tabId, goal, input.read(input.target).remote);
  authority.checkpoint();
  input.ports.patchGoal(input.target.tabId, goal);
}

async function send(input: SubmissionInput, content: Submission, authority: SessionOperationAuthority) {
  authority.checkpoint();
  const source = input.read(input.target);
  if (!source.ready || source.unavailable) throw Error(source.unavailable);
  input.ports.clearUndo(input.target.tabId);
  await input.ports.send(input.target.tabId, content.display, content.submit, content.structured, content.initialGoal);
  authority.checkpoint();
}

/** Only minimal source data survives awaits. No active-tab reads or render refs. */
export async function executeSubmission(input: SubmissionInput, authority: SessionOperationAuthority): Promise<void> {
  authority.checkpoint();
  if (input.request.kind === "goal") return applyGoal(input, input.request.goal.trim(), authority);
  const { content } = input.request;
  if (input.request.kind === "direct") return send(input, content, authority);
  const source = input.read(input.target);
  const display = content.display.trim();
  const submit = content.submit ?? content.display;
  const command = goalCommand(display);
  if (command) {
    if (command.activate) {
      if (command.legacy) input.ports.patchGoal(input.target.tabId, command.value);
      else await applyGoal(input, command.value, authority);
    } else if (command.clear) await applyGoal(input, "", authority);
    authority.checkpoint();
    if (input.read(input.target).ready) await send(input, { display, submit: submit.trim() }, authority);
    return;
  }
  if (!source.ready) return;
  if (source.goalDraft) {
    await send(input, { display, submit: content.structured ? submit.trim() : `/goal ${submit.trim()}`,
      structured: content.structured, initialGoal: { goal: display, collaborationMode: source.collaboration, toolApprovalMode: source.approval } }, authority);
    authority.checkpoint();
    input.ports.patchGoal(input.target.tabId, display);
    return;
  }
  if (!await input.ports.profile(input.target.tabId, false)) return;
  authority.checkpoint();
  await send(input, { display, submit: submit.trim(), structured: content.structured }, authority);
}
