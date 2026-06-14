import { useCallback, useEffect, useMemo, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { DaemonSessionView, DaemonStatusView } from "../lib/types";
import { PromptBadge, PromptDetailToggle, PromptShelf } from "./PromptShelf";

const DAEMON_ADDR = "";
const REFRESH_MS = 10000;
type SessionFilter = "all" | "project" | "global" | "running" | "waiting" | "blocked";

const filters: SessionFilter[] = ["all", "project", "global", "running", "waiting", "blocked"];

function shortID(id: string): string {
  if (id.length <= 12) return id;
  return `${id.slice(0, 6)}…${id.slice(-4)}`;
}

function sessionMatches(session: DaemonSessionView, filter: SessionFilter): boolean {
  switch (filter) {
    case "project":
      return session.scope === "project";
    case "global":
      return (session.scope || "global") === "global";
    case "running":
      return Boolean(session.active) || session.runStatus === "running" || session.runStatus === "queued";
    case "waiting":
      return Boolean(session.waitKind) || Boolean(session.runStatus?.startsWith("waiting_"));
    case "blocked":
      return session.goalStatus === "blocked" || session.runStatus === "blocked" || Boolean(session.budgetBlockedReason);
    default:
      return true;
  }
}

function budgetSummary(session: DaemonSessionView, t: ReturnType<typeof useT>): string {
  const parts: string[] = [];
  if ((session.dailyWakeupLimit ?? 0) > 0 || (session.dailyWakeups ?? 0) > 0) {
    parts.push(t("daemonSessions.budgetWakeups", { used: session.dailyWakeups ?? 0, limit: session.dailyWakeupLimit ?? 0 }));
  }
  if ((session.dailyModelCallLimit ?? 0) > 0 || (session.dailyModelCalls ?? 0) > 0) {
    parts.push(t("daemonSessions.budgetModels", { used: session.dailyModelCalls ?? 0, limit: session.dailyModelCallLimit ?? 0 }));
  }
  if ((session.dailyModelCostLimit ?? 0) > 0 || (session.dailyModelCost ?? 0) > 0) {
    const currency = session.modelCostCurrency || "$";
    parts.push(t("daemonSessions.budgetCost", {
      used: `${currency}${(session.dailyModelCost ?? 0).toFixed(2)}`,
      limit: `${currency}${(session.dailyModelCostLimit ?? 0).toFixed(2)}`,
    }));
  }
  if (session.budgetBlockedReason) parts.push(t("daemonSessions.budgetBlocked"));
  return parts.join(" · ");
}

function waitSummary(session: DaemonSessionView): string {
  const wait = [session.waitKind, session.waitId || session.waitTool || session.waitSubject].filter(Boolean).join(":");
  return wait || session.waitReason || "";
}

function nextWakeupLabel(value: string | undefined, t: ReturnType<typeof useT>): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return t("daemonSessions.nextWakeup", { time: date.toLocaleString() });
}

function sessionTitle(session: DaemonSessionView): string {
  return session.goalText || session.topicTitle || session.id;
}

function statusText(status: DaemonStatusView | null, sessions: DaemonSessionView[], t: ReturnType<typeof useT>): string {
  if (!status?.connected && sessions.length === 0) return "";
  const parts = [
    status?.status || (status?.connected ? "running" : ""),
    t("daemonSessions.sessionCount", { count: sessions.length }),
    status?.uptime ? t("daemonSessions.uptime", { uptime: status.uptime }) : "",
  ].filter(Boolean);
  return parts.join(" · ");
}

export function DaemonSessionsPanel() {
  const t = useT();
  const [status, setStatus] = useState<DaemonStatusView | null>(null);
  const [sessions, setSessions] = useState<DaemonSessionView[]>([]);
  const [filter, setFilter] = useState<SessionFilter>("all");
  const [open, setOpen] = useState(true);
  const [working, setWorking] = useState("");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const [nextStatus, nextSessions] = await Promise.all([
        app.DaemonStatus(DAEMON_ADDR),
        app.ListDaemonSessions(DAEMON_ADDR),
      ]);
      setStatus(nextStatus);
      setSessions(nextSessions ?? []);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setStatus(null);
      setSessions([]);
    }
  }, []);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const visibleSessions = useMemo(() => sessions.filter((session) => sessionMatches(session, filter)), [filter, sessions]);
  const connected = Boolean(status?.connected);
  if (!connected && sessions.length === 0 && !error) return null;

  const runAction = async (key: string, action: () => Promise<unknown>) => {
    if (working) return;
    setWorking(key);
    try {
      await action();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setWorking("");
    }
  };

  const badges = (
    <>
      <PromptBadge>{connected ? t("daemonSessions.connected") : t("daemonSessions.offline")}</PromptBadge>
      <PromptBadge>{t("daemonSessions.visibleCount", { visible: visibleSessions.length, total: sessions.length })}</PromptBadge>
    </>
  );

  return (
    <PromptShelf
      titleId="daemon-sessions-title"
      title={t("daemonSessions.title")}
      badges={badges}
      meta={error || statusText(status, sessions, t)}
      actionsWrap
      actions={
        <>
          {filters.map((value) => (
            <button
              key={value}
              className={`prompt-action${filter === value ? " prompt-action--selected" : ""}`}
              onClick={() => setFilter(value)}
            >
              <span className="prompt-action__label">{t(`daemonSessions.filter.${value}`)}</span>
            </button>
          ))}
          <PromptDetailToggle open={open} label={t("daemonSessions.show")} openLabel={t("daemonSessions.hide")} onClick={() => setOpen((current) => !current)} />
        </>
      }
    >
      {open && (
        <div className="daemon-sessions__panel">
          {visibleSessions.length === 0 ? (
            <div className="daemon-sessions__empty">{t("daemonSessions.empty")}</div>
          ) : visibleSessions.map((session) => {
            const rowKey = session.id;
            const wait = waitSummary(session);
            const budget = budgetSummary(session, t);
            const next = nextWakeupLabel(session.nextWakeupAt, t);
            return (
              <div className="daemon-sessions__row" key={session.id}>
                <div className="daemon-sessions__main">
                  <div className="daemon-sessions__title">
                    <span>{sessionTitle(session)}</span>
                    {session.open && <b>{t("daemonSessions.openBadge")}</b>}
                  </div>
                  <div className="daemon-sessions__meta">
                    <span>{shortID(session.id)}</span>
                    {session.scope && <span>{session.scope}</span>}
                    {session.runStatus && <span>{session.runStatus}</span>}
                    {session.goalStatus && <span>{session.goalStatus}</span>}
                    {wait && <span>{wait}</span>}
                    {session.scheduled && <span>{t("daemonSessions.scheduled")}</span>}
                    {session.watched && <span>{t("daemonSessions.watched")}</span>}
                  </div>
                  {(next || budget) && <div className="daemon-sessions__secondary">{[next, budget].filter(Boolean).join(" · ")}</div>}
                </div>
                <div className="daemon-sessions__actions">
                  <button className="btn btn--small" disabled={Boolean(working)} onClick={() => void runAction(`${rowKey}:open`, () => app.OpenDaemonSession(session.id, DAEMON_ADDR))}>
                    {t("daemonSessions.open")}
                  </button>
                  <button className="btn btn--small" disabled={Boolean(working)} onClick={() => void runAction(`${rowKey}:continue`, () => app.ContinueDaemonGoal(session.id, DAEMON_ADDR))}>
                    {t("daemonSessions.continue")}
                  </button>
                  <button className="btn btn--small" disabled={Boolean(working) || !session.scheduled} onClick={() => void runAction(`${rowKey}:schedule`, () => app.DisableDaemonSchedule(session.id, DAEMON_ADDR))}>
                    {t("daemonSessions.disableSchedule")}
                  </button>
                  <button className="btn btn--small" disabled={Boolean(working) || !session.watched} onClick={() => void runAction(`${rowKey}:watch`, () => app.DisableDaemonWatch(session.id, DAEMON_ADDR))}>
                    {t("daemonSessions.disableWatch")}
                  </button>
                  <button className="btn btn--small btn--danger" disabled={Boolean(working)} onClick={() => void runAction(`${rowKey}:stop`, () => app.StopDaemonSession(session.id, DAEMON_ADDR))}>
                    {t("daemonSessions.stop")}
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </PromptShelf>
  );
}
