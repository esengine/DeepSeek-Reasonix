import { useState } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { Item } from "../lib/useController";
import type { RewindUndoState } from "../lib/rewindTypes";
import type { RewindResultView } from "../lib/types";

export type SessionUndoInput = {
  activeTabId: string | undefined;
  activeTabReadOnly: boolean;
  items: readonly Item[];
  hydratePlaceholderActive: boolean;
  controllerReady: boolean;
  running: boolean;
  messageActionOpen: boolean;
  approvalOpen: boolean;
  askOpen: boolean;
  clearContextPending: boolean;
  ports: {
    rewindForTab(tabId: string, turn: number, scope: string): Promise<boolean>;
    rewindForTabDetailed(tabId: string, turn: number, scope: string): Promise<RewindResultView>;
    refreshTabMetas(): void;
    undoRewindForTab(tabId: string, transactionId: string): Promise<boolean>;
    sendToTab(tabId: string, display: string, submit: string, original: string): Promise<void>;
    composeInsert(tabId: string, text: string): void;
    refreshDock(): void;
    refreshProject(): void;
  };
};

/**
 * Owns the undo/rewind lifecycle: per-tab rewind state and committing flags,
 * message-action rewinds (fork/code/summarize/full), edit-prompt rewinds and
 * the committed-session revert handler. The undo banner still reads
 * `rewindState`/`setRewindStateForTab` through this hook's return; only the
 * banner identity and its DOM live in the footer region.
 */
export function useSessionUndo(input: SessionUndoInput) {
  const { activeTabId, items, ports } = input;
  const [rewindStatesByTab, setRewindStatesByTab] = useState<Record<string, RewindUndoState>>({});
  const [rewindCommittingByTab, setRewindCommittingByTab] = useState<Record<string, boolean>>({});
  const [rewindSignal, setRewindSignal] = useState(0);

  const setRewindStateForTab = useCommittedCommand((tabId: string, nextState: RewindUndoState | null) => {
    if (!tabId) return;
    setRewindStatesByTab(current => {
      if (!nextState && !current[tabId]) return current;
      const next = { ...current };
      if (nextState) next[tabId] = nextState;
      else delete next[tabId];
      return next;
    });
  });

  const setRewindCommittingForTab = useCommittedCommand((tabId: string, committing: boolean) => {
    setRewindCommittingByTab((current) => {
      const next = { ...current };
      if (committing) next[tabId] = true;
      else delete next[tabId];
      return next;
    });
  });

  const bumpRewindSignal = useCommittedCommand(() => setRewindSignal((value) => value + 1));

  const handleSessionRevertCommitted = useCommittedCommand((sourceTabId: string, outcome: RewindResultView) => {
    if (!sourceTabId || !outcome.ok) return;
    setRewindStateForTab(sourceTabId, {
      turnDiff: 0,
      transactionId: outcome.transactionId,
      undoAvailable: outcome.undoAvailable,
      filesRestored: outcome.written ?? [],
      filesRemoved: outcome.deleted ?? [],
    });
    ports.refreshDock();
    ports.refreshProject();
  });

  const rewindState = activeTabId ? rewindStatesByTab[activeTabId] ?? null : null;
  const rewindCommitting = Boolean(activeTabId && rewindCommittingByTab[activeTabId]);

  const handleMessageAction = useCommittedCommand((turn: number, scope: string) => {
    const sourceTabId = activeTabId;
    if (!sourceTabId || input.activeTabReadOnly) return;
    if (input.hydratePlaceholderActive) return;
    if (scope === "fork") {
      // Fork still goes through the controller (not optimistic).
      ports.rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        ports.refreshTabMetas();
        ports.refreshProject();
      });
      return;
    }

    // Code-only rewind only affects files — no message truncation,
    // no optimistic UI needed.  Execute immediately.
    if (scope === "code") {
      setRewindCommittingForTab(sourceTabId, true);
      void ports.rewindForTabDetailed(sourceTabId, turn, scope).then((outcome) => {
        setRewindCommittingForTab(sourceTabId, false);
        if (!outcome.ok) return;
        setRewindStateForTab(sourceTabId, {
          turnDiff: 0,
          transactionId: outcome.transactionId,
          undoAvailable: outcome.undoAvailable,
          filesRestored: outcome.written ?? [],
          filesRemoved: outcome.deleted ?? [],
        });
        ports.refreshDock();
        ports.refreshProject();
      });
      return;
    }

    // Summarize only compresses the conversation log — no files touched,
    // no optimistic UI needed. Execute immediately like code-only rewind.
    if (scope === "summ-from" || scope === "summ-upto") {
      ports.rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        ports.refreshDock();
        ports.refreshProject();
      });
      return;
    }

    const hasCheckpointTurns = items.some((it) => it.kind === "user" && it.checkpointTurn != null);
    let boundaryIdx = -1;
    let userCount = 0;
    let targetUserCount = -1;
    for (let i = 0; i < items.length; i++) {
      if (items[i].kind === "user") {
        const item = items[i] as Extract<Item, { kind: "user" }>;
        const matches = hasCheckpointTurns ? item.checkpointTurn === turn : userCount === turn;
        if (matches) {
          boundaryIdx = i;
          targetUserCount = userCount;
          break;
        }
        userCount++;
      }
    }
    if (boundaryIdx < 0) {
      ports.rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        if (scope === "both") {
          ports.refreshDock();
          ports.refreshProject();
        }
      });
      return;
    }

    const prevUserCount = items.filter((it) => it.kind === "user").length;
    const turnDiff = prevUserCount - targetUserCount;
    const userItem = items[boundaryIdx]?.kind === "user" ? items[boundaryIdx] as Extract<Item, { kind: "user" }> : undefined;
    const prompt = userItem?.text ?? "";

    // Immediate backend commit — only update UI after success.
    setRewindCommittingForTab(sourceTabId, true);
    void ports.rewindForTabDetailed(sourceTabId, turn, scope).then((outcome) => {
      setRewindCommittingForTab(sourceTabId, false);
      if (!outcome.ok) {
        // Keep conversation/files as-is; notices already carry the reason.
        return;
      }
      const targetTabId = outcome.tabId || sourceTabId;
      setRewindStateForTab(targetTabId, {
        turnDiff: outcome.tabId ? 0 : turnDiff,
        transactionId: outcome.transactionId,
        undoAvailable: outcome.undoAvailable,
        undoTabId: sourceTabId,
        filesRestored: outcome.written ?? [],
        filesRemoved: outcome.deleted ?? [],
      });
      ports.composeInsert(targetTabId, prompt);
      bumpRewindSignal();
      if (scope === "both" || scope === "code") {
        ports.refreshDock();
        ports.refreshProject();
      }
    });
  });

  const handleEditPrompt = useCommittedCommand(async (turn: number, displayText: string, submitText?: string): Promise<boolean> => {
    const sourceTabId = activeTabId;
    if (!sourceTabId || input.activeTabReadOnly || !input.controllerReady || input.hydratePlaceholderActive
      || rewindStatesByTab[sourceTabId] || input.running || input.messageActionOpen
      || input.approvalOpen || input.askOpen || input.clearContextPending) return false;
    const next = displayText.trim();
    if (!next) return false;
    const submit = (submitText ?? displayText).trim();
    const hasCheckpointTurns = items.some((it) => it.kind === "user" && it.checkpointTurn != null);
    let original = "";
    let userCount = 0;
    for (const item of items) {
      if (item.kind !== "user") continue;
      const matches = hasCheckpointTurns ? item.checkpointTurn === turn : userCount === turn;
      if (matches) {
        original = (item.submitText ?? item.text).trim();
        break;
      }
      userCount++;
    }
    const outcome = await ports.rewindForTabDetailed(sourceTabId, turn, "conversation");
    if (!outcome.ok) return false;
    bumpRewindSignal();
    const targetTabId = outcome.tabId || sourceTabId;
    try {
      await ports.sendToTab(targetTabId, next, submit, original);
      return true;
    } catch {
      return false;
    }
  });

  return {
    rewindState,
    rewindCommitting,
    rewindSignal,
    setRewindStateForTab,
    setRewindCommittingForTab,
    bumpRewindSignal,
    handleSessionRevertCommitted,
    handleMessageAction,
    handleEditPrompt,
  };
}
