import { ChevronRight, Monitor, Server } from "lucide-react";
import type { SessionDescriptor } from "../protocol/types";
import { statusLabel, t, type Locale } from "../i18n/messages";

export function SessionList({
  sessions,
  activeId,
  locale,
  onSelect,
}: {
  sessions: SessionDescriptor[];
  activeId: string | null;
  locale: Locale;
  onSelect: (id: string) => void;
}) {
  return (
    <section className="list-section" aria-label={t(locale, "sessions.title")}>
      <div className="list-group" role="list">
        {sessions.map((s) => {
          const RuntimeIcon = s.runtime === "remote" ? Server : Monitor;
          return (
            <button
              key={s.id}
              type="button"
              className="list-row"
              role="listitem"
              onClick={() => onSelect(s.id)}
              aria-current={s.id === activeId ? "true" : undefined}
            >
              <span className="list-row-leading" data-runtime={s.runtime} aria-hidden>
                <RuntimeIcon size={16} strokeWidth={2} />
              </span>
              <span className="list-row-body">
                <div className="list-row-title">{s.title || s.id}</div>
                <div className="list-row-meta">
                  <span>
                    {s.runtime === "local"
                      ? t(locale, "sessions.runtimeLocal")
                      : t(locale, "sessions.runtimeRemote")}
                  </span>
                  <span aria-hidden>·</span>
                  <span className="status-label">
                    <span
                      className="status-dot"
                      data-status={s.status || "idle"}
                      aria-hidden
                    />
                    {statusLabel(locale, s.status)}
                  </span>
                </div>
              </span>
              <span className="list-row-trailing" aria-hidden>
                <ChevronRight size={18} strokeWidth={1.75} />
              </span>
            </button>
          );
        })}
      </div>
    </section>
  );
}
