import { useMemo } from "react";
import { useCommittedSlot, type CommittedSlot } from "./useCommittedSlot";
import { useCommittedCommand } from "./useCommittedCommand";
import { CommandCancelled } from "./commandOutcome";
import { executeSubmission, type InitialGoal, type SubmissionInput, type SubmissionPorts, type SubmissionResource } from "../app-runtime/sessionSubmissionOwner";
import type { SessionOperationAuthority, SessionResource, useSessionOperations } from "../app-runtime/useSessionOperations";
import type { StructuredInvocationSubmit } from "./invocationDisplay";

function bindRead(slot: CommittedSlot<readonly SubmissionResource[]>) {
  return (target: SessionResource) => {
    if (slot.phase !== "ready") throw new CommandCancelled("disposed");
    const source = slot.value?.find(value => value.target.tabId === target.tabId && value.target.sessionKey === target.sessionKey);
    if (!source) throw new CommandCancelled("superseded");
    return source;
  };
}

export function useSessionSubmission(options: {
  target: SessionResource; resources: readonly SubmissionResource[];
  operations: ReturnType<typeof useSessionOperations>; ports: SubmissionPorts; missingSource: string;
}) {
  const { target, resources, operations, ports, missingSource } = options;
  const slot = useCommittedSlot(resources);
  const read = useMemo(() => bindRead(slot), [slot]);
  const run = useCommittedCommand(async (tab: string, request: SubmissionInput["request"]) => {
    const source = resources.find(value => value.target.tabId === tab);
    if (!source) throw Error(missingSource);
    const result = await operations(source.target, request.kind === "goal" ? "composer-profile" : "send", {
      target: source.target, request, read, ports,
    }, executeSubmission);
    if (result.status === "failed") throw result.error;
  });
  const commitThenSend = useCommittedCommand((tab: string, display: string, submit?: string,
    structured?: StructuredInvocationSubmit, initialGoal?: InitialGoal) => run(tab, { kind: "direct", content: { display, submit, structured, initialGoal } }));
  const submit = useCommittedCommand((tab: string, display: string, content = display, structured?: StructuredInvocationSubmit) =>
    run(tab, { kind: "composer", content: { display, submit: content, structured } }));
  const applyGoalForTab = useCommittedCommand((tab: string, goal: string) => run(tab, { kind: "goal", goal }));
  const applyGoal = useCommittedCommand((goal: string) => target.tabId ? applyGoalForTab(target.tabId, goal) : Promise.resolve());
  // A queue already owns its request and must retain resource failures before
  // its own UI outcome boundary. Reuse the executor, not a nested UI command.
  const sendRevision = useCommittedCommand((source: SessionResource, text: string, authority: SessionOperationAuthority) =>
    executeSubmission({ target: source, read, ports, request: { kind: "direct", content: { display: text } } }, authority));
  return { commitThenSend, submit, applyGoalForTab, applyGoal, sendRevision };
}
