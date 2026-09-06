import type { Dispatch, SetStateAction } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { sessionsForScope, type HistoryViewState } from "./historyViewProjection";
import { useOverlayStore } from "../store/overlays";
import type { SessionMeta } from "../lib/types";

export type HistoryCommandsInput = {
  running: boolean;
  setHistView: Dispatch<SetStateAction<HistoryViewState | null>>;
  ports: {
    listSessions(): Promise<SessionMeta[]>;
    deleteSession(path: string): Promise<void>;
    renameSession(path: string, title: string): Promise<void>;
    openPage(page: { kind: "trash" }): void;
  };
};

/**
 * Owns the trash/history commands: opening the trash page, closing and
 * refreshing the history view, deleting a history session (local filtering
 * after the backend succeeds) and renaming one (topic or session path by
 * availability). Deletes/renames are gated on a stopped runtime.
 */
export function useHistoryCommands(input: HistoryCommandsInput) {
  const { running, setHistView, ports } = input;
  const setTransientOverlayDismissSignal = useOverlayStore((state) => state.setTransientOverlayDismissSignal);

  const closeTransientOverlays = useCommittedCommand(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  });

  const openTrash = useCommittedCommand(async () => {
    closeTransientOverlays();
    setHistView(null);
    ports.openPage({ kind: "trash" });
  });
  const closeHistory = useCommittedCommand(() => {
    closeTransientOverlays();
    setHistView(null);
  });
  const refreshHistoryView = useCommittedCommand(async () => {
    const sessions = await ports.listSessions().catch(() => null);
    if (!sessions) return;
    setHistView((cur) =>
      cur === null || cur.kind !== "history"
        ? cur
        : cur.source === "scope"
          ? { ...cur, sessions: sessionsForScope(sessions, cur.filter) }
          : { ...cur, sessions },
    );
  });

  const onDeleteSession = useCommittedCommand(async (path: string) => {
    if (running) return;
    try {
      await ports.deleteSession(path);
    } catch {
      await refreshHistoryView();
      return;
    }
    // Local state removal: filter the deleted session out of the current
    // history view instead of re-fetching the full list from the backend.
    setHistView((cur) =>
      cur === null || cur.kind !== "history"
        ? cur
        : { ...cur, sessions: cur.sessions.filter((s) => s.path !== path) },
    );
  });
  const onRenameHistorySession = useCommittedCommand(async (session: SessionMeta, title: string) => {
    if (running) return;
    if (session.topicId) await app.RenameTopic(session.topicId, title);
    else await ports.renameSession(session.path, title);
    const sessions = await ports.listSessions();
    setHistView((cur) =>
      cur === null
        ? null
        : cur.kind === "history"
          ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
          : cur,
    );
  });

  return { openTrash, closeHistory, refreshHistoryView, onDeleteSession, onRenameHistorySession };
}
