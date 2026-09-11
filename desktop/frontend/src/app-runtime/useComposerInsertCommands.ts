import { useRef, useState } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { formatTerminalOutputForComposer } from "../lib/terminalOutput";
import { formatSelectionReference, type SelectedTextInsertRequest } from "../lib/selectedTextContext";
import type { ComposerInsertRequest } from "../lib/types";
import type { Translator } from "../lib/i18n";
import type { useSessionOperations } from "./useSessionOperations";

export type WorkspaceInsertTarget = "composer" | "planRevision";

export type ComposerInsertCommandsInput = {
  activeTabId: string | undefined;
  sessionKey: string;
  approval: { id: string; tool: string } | undefined | null;
  operations: ReturnType<typeof useSessionOperations>;
  t: Translator;
  showToast: (message: string, kind: "info" | "warn" | "error") => void;
  ports: {
    terminalOutput(tabId: string, sessionId: string): Promise<string>;
  };
};

/**
 * Owns every composer-bound insertion channel: per-tab composer insert
 * requests, selected-text/code requests, the plan-revision insert and the
 * workspace insert target that routes between them, plus terminal-output
 * insertion through the session operations authority. The plan-revision
 * input is plain text and only consumes request.text, so structured
 * references land there in their fenced rendering.
 */
export function useComposerInsertCommands(input: ComposerInsertCommandsInput) {
  const { activeTabId, approval, t, showToast, ports } = input;
  const [composerInsertRequestsByTab, setComposerInsertRequestsByTab] = useState<Record<string, ComposerInsertRequest>>({});
  const [selectedTextRequestsByTab, setSelectedTextRequestsByTab] = useState<Record<string, SelectedTextInsertRequest>>({});
  const selectedTextRequestIdRef = useRef(0);
  const [planRevisionInsertRequest, setPlanRevisionInsertRequest] = useState<{
    tabId: string;
    approvalId: string;
    request: ComposerInsertRequest;
  } | null>(null);
  const [workspaceInsertTarget, setWorkspaceInsertTarget] = useState<WorkspaceInsertTarget>("composer");

  const activePlanRevisionInsertRequest =
    planRevisionInsertRequest &&
    planRevisionInsertRequest.tabId === activeTabId &&
    planRevisionInsertRequest.approvalId === approval?.id
      ? planRevisionInsertRequest.request
      : null;
  const composerInsertRequest = activeTabId ? composerInsertRequestsByTab[activeTabId] ?? null : null;
  const selectedTextRequest = activeTabId ? selectedTextRequestsByTab[activeTabId] ?? null : null;

  const setInsertTarget = useCommittedCommand((target: WorkspaceInsertTarget) => setWorkspaceInsertTarget(target));
  const handleRevisionActiveChange = useCommittedCommand((active: boolean) => {
    setWorkspaceInsertTarget(active ? "planRevision" : "composer");
  });

  const replaceComposerInsert = useCommittedCommand((tabId: string, text: string) => {
    setComposerInsertRequestsByTab((current) => ({ ...current, [tabId]: { id: Date.now(), text, mode: "replace" } }));
  });
  const prefillSubagentCommand = useCommittedCommand((command: string) => {
    if (!activeTabId) return;
    setComposerInsertRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: Date.now(), text: command, mode: "prefix" },
    }));
  });

  const addWorkspaceTextToComposer = useCommittedCommand((text: string) => {
    if (activeTabId && workspaceInsertTarget === "planRevision" && approval?.tool === "exit_plan_mode") {
      setPlanRevisionInsertRequest({
        tabId: activeTabId,
        approvalId: approval.id,
        request: { id: Date.now(), text },
      });
      return;
    }
    if (activeTabId) {
      setComposerInsertRequestsByTab((current) => ({
        ...current,
        [activeTabId]: { id: Date.now(), text },
      }));
    }
  });

  const addTerminalOutputToComposer = useCommittedCommand(async (sessionId: string) => {
    if (!activeTabId) return;
    const target = { tabId: activeTabId, sessionKey: input.sessionKey };
    const outcome = await input.operations(target, `terminal-output:${sessionId}`, {}, async (_operationInput, authority) =>
      (await import("./sessionRuntimeOwner")).executeTerminalOutputInsertion(target, sessionId, {
        read: (tabId, terminalSessionId) => ports.terminalOutput(tabId, terminalSessionId),
        apply: addWorkspaceTextToComposer,
      }, formatTerminalOutputForComposer, authority),
    );
    if (outcome.status === "completed" && !outcome.value) showToast(t("terminal.noOutput"), "info");
    if (outcome.status === "failed") showToast(outcome.error instanceof Error ? outcome.error.message : String(outcome.error), "error");
  });

  const addSelectedTextToComposer = useCommittedCommand((text: string, source?: SelectedTextInsertRequest["source"]) => {
    const selected = text.trim();
    if (!activeTabId || !selected) return;
    selectedTextRequestIdRef.current += 1;
    setSelectedTextRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: selectedTextRequestIdRef.current, text: selected, ...(source ? { source } : {}) },
    }));
  });

  const addTerminalSelectionToComposer = useCommittedCommand((text: string) => addSelectedTextToComposer(text, "terminal"));
  const addWorkspaceCodeToComposer = useCommittedCommand((path: string, code: string) => {
    if (!activeTabId || !code.trim()) return;
    if (workspaceInsertTarget === "planRevision" && approval?.tool === "exit_plan_mode") {
      setPlanRevisionInsertRequest({
        tabId: activeTabId,
        approvalId: approval.id,
        request: { id: Date.now(), text: formatSelectionReference(path, code) },
      });
      return;
    }
    selectedTextRequestIdRef.current += 1;
    setSelectedTextRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: selectedTextRequestIdRef.current, text: code, path },
    }));
  });

  return {
    composerInsertRequest,
    selectedTextRequest,
    activePlanRevisionInsertRequest,
    setInsertTarget,
    handleRevisionActiveChange,
    replaceComposerInsert,
    prefillSubagentCommand,
    addWorkspaceTextToComposer,
    addTerminalOutputToComposer,
    addSelectedTextToComposer,
    addTerminalSelectionToComposer,
    addWorkspaceCodeToComposer,
  };
}
