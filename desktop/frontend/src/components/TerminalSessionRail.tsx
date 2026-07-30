import { X } from "lucide-react";

import { useT } from "../lib/i18n";
import type { TerminalSessionView } from "../lib/types";

export function TerminalSessionRail({
  sessions,
  activeSessionId,
  onSelect,
  onClose,
}: {
  sessions: TerminalSessionView[];
  activeSessionId: string | null;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
}) {
  const t = useT();
  return (
    <nav className="terminal-session-rail" aria-label={t("terminal.sessions")}>
      <div className="terminal-session-rail__list">
        {sessions.map((session) => (
          <div key={session.id} className={`terminal-session${session.id === activeSessionId ? " terminal-session--active" : ""}`}>
            <button type="button" className="terminal-session__select" onClick={() => onSelect(session.id)} aria-current={session.id === activeSessionId ? "page" : undefined}>
              <span className={`terminal-session__status${session.running ? " terminal-session__status--running" : ""}`} aria-hidden="true" />
              <span className="terminal-session__title">{session.title}</span>
            </button>
            <button type="button" className="terminal-session__close" onClick={() => onClose(session.id)} aria-label={t("terminal.closeSession")} title={t("terminal.closeSession")}>
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
    </nav>
  );
}
