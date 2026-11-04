import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort, TreeSession, TreeWorkspace } from "../port/hub";
import type { RemoteHost } from "../port/remote";
import { hidesLegacySessions, onHidesLegacySessionsChange } from "../state/prefs";
import { SessionAge } from "./SessionAge";
import { StudioIcon } from "./StudioIcon";
import { TREE_PAGE } from "./treewindow";
import { useModalFocus } from "./useModalFocus";
import { listenAction } from "./listen";

export function RecentSessions({ hub, hosts, runs = {}, onPick, onClose }: {
  hub: HubPort;
  hosts: RemoteHost[];
  runs?: Record<string, { run: string }>;
  onPick: (host: string, workspace: TreeWorkspace, session: TreeSession) => Promise<void>;
  onClose: () => void;
}) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  const hideLegacy = useSyncExternalStore(onHidesLegacySessionsChange, hidesLegacySessions, hidesLegacySessions);
  const [view, setView] = useState({ host: "", q: "", offset: 0 });
  const [draft, setDraft] = useState("");
  const [tree, setTree] = useState<TreeWorkspace[] | null>(null);
  const [busy, setBusy] = useState(true);
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const timer = setTimeout(() => setView((current) => current.q === draft.trim() ? current : { ...current, q: draft.trim(), offset: 0 }), 250);
    return () => clearTimeout(timer);
  }, [draft]);
  useEffect(() => { sheet.current?.querySelector<HTMLInputElement>("input")?.focus(); }, []);
  useEffect(() => hub.onTreeChanged(() => setRetry((n) => n + 1)), [hub]);
  useEffect(() => {
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape" && !opening) onClose(); };
    return listenAction(window, "keydown", { action: "layer.dismiss", listener: escape as EventListener });
  }, [onClose, opening]);
  useEffect(() => {
    let stale = false;
    setBusy(true);
    setError("");
    const window = { order: "recent" as const, scope: "active" as const, limit: TREE_PAGE, offset: view.offset, q: view.q, hideLegacy };
    const read = view.host ? hub.remoteTree(view.host, window) : hub.tree(window);
    void read.then((next) => {
      if (stale) return;
      if (next?.some((ws) => ws.recentOffset !== view.offset)) throw new Error(t("session.this_environment_does_not_support"));
      setTree(next ?? []);
    }).catch((e: unknown) => { if (!stale) setError(reason(e)); })
      .finally(() => { if (!stale) setBusy(false); });
    return () => { stale = true; };
  }, [hub, view, hideLegacy, retry]);
  const rows = (tree ?? []).flatMap((ws) => ws.sessions.map((session) => ({ ws, session })))
    .sort((a, b) => (b.session.lastActiveAt ?? 0) - (a.session.lastActiveAt ?? 0) || (a.session.path < b.session.path ? -1 : a.session.path > b.session.path ? 1 : 0));
  const walkRows = (event: React.KeyboardEvent) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    const all = [...(sheet.current?.querySelectorAll<HTMLElement>(".recent-session:not(:disabled)") ?? [])];
    const at = all.indexOf(document.activeElement as HTMLElement);
    if (at < 0) return;
    event.preventDefault();
    if (event.key === "ArrowDown") all[Math.min(at + 1, all.length - 1)].focus();
    else if (at === 0) sheet.current?.querySelector<HTMLInputElement>("input")?.focus();
    else all[at - 1].focus();
  };
  const startOfToday = new Date().setHours(0, 0, 0, 0);
  const isToday = (at?: number) => !!at && at >= startOfToday;
  const remaining = (tree ?? []).reduce((n, ws) => n + (ws.remaining ?? 0), 0);
  const pick = async (ws: TreeWorkspace, session: TreeSession) => {
    setOpening(true);
    setError("");
    try {
      await onPick(view.host, ws, session);
      onClose();
    } catch (e) {
      setError(reason(e));
    } finally {
      setOpening(false);
    }
  };
  return createPortal(
    <div className="pickveil" role="dialog" aria-modal="true" aria-label={t("session.recent_sessions")} ref={sheet}>
      <div className="pickcard recent-sessions">
        <h2>{t("session.recent_sessions")}</h2>
        <p className="recent-description">{t("session.ordered_by_last_activity_including")}</p>
        <div className="recent-filters" onKeyDown={(event) => { if (event.key === "ArrowDown" && (event.target as HTMLElement).tagName === "INPUT") { const first = sheet.current?.querySelector<HTMLElement>(".recent-session:not(:disabled)"); if (first) { event.preventDefault(); first.focus(); } } }}>
          <label className="recent-search"><StudioIcon name="search" /><input data-action="workspace.search" type="search" aria-label={t("session.search_recent_sessions_or_projects")} placeholder={t("session.search_recent_sessions_or_projects")} value={draft} disabled={opening} onChange={(event) => setDraft(event.target.value)} /></label>
          {hosts.some((host) => !host.disabled) && (
            <select className="recent-machine" data-action="session.recent-environment" aria-label={t("session.where_it_runs")} value={view.host} disabled={opening} onChange={(event) => setView({ host: event.target.value, q: view.q, offset: 0 })}>
              <option value="">{t("session.this_machine")}</option>
              {hosts.filter((host) => !host.disabled).map((host) => <option key={host.name} value={host.name}>{host.name}</option>)}
            </select>
          )}
        </div>
        <div className="picklist" aria-busy={busy} data-busy={busy ? "" : undefined} onKeyDown={walkRows}>
          {!busy && !error && !rows.length && <p className="pickempty">{t("common.nothing_matches")}</p>}
          {busy && <p className="picknote" role="status">{t("session.reading_sessions")}</p>}
          {!busy && !error && rows.map(({ ws, session }, i) => [
            (i === 0 || isToday(rows[i - 1].session.lastActiveAt) !== isToday(session.lastActiveAt)) && <p key={`g${session.path}`} className="recent-group">{t(isToday(session.lastActiveAt) ? "common.today" : "session.earlier")}</p>,
            <button key={session.path} className="pickrow recent-session" disabled={opening}
            data-action="session.open" data-target={session.path} aria-label={`${session.title || session.name} · ${ws.name}`}
            title={`${session.workspaceRoot || ws.root}\n${session.path}`} onClick={() => void pick(ws, session)}>
            <StudioIcon name="chat" />
            <span className="recent-session-text"><b>{session.title || session.name}</b><small>{ws.name} · {session.workspaceRoot || ws.root}</small></span>
            {session.runtimeId && (runs[session.runtimeId]?.run === "running" || runs[session.runtimeId]?.run === "halt")
              ? <span className="sessword" data-run={runs[session.runtimeId].run}>{t(runs[session.runtimeId].run === "running" ? "common.running" : "session.needs_you")}</span>
              : <SessionAge at={session.lastActiveAt} />}
          </button>,
          ])}
          {error && <><p className="picknote" role="alert">{error}</p><button className="pickgo" data-action="session.recent-retry" onClick={() => setRetry((n) => n + 1)}>{t("common.retry")}</button></>}
        </div>
        <p className="recent-keys">{t("session.to_choose_enter_to_open")}</p>
        <div className="pickact">
          <button data-action="session.recent-page" data-value="previous" disabled={busy || opening || view.offset === 0} onClick={() => setView({ ...view, offset: Math.max(0, view.offset - TREE_PAGE) })}>{t("common.previous_page")}</button>
          <span className="recent-page">{t("session.page_n", { n: view.offset / TREE_PAGE + 1 })}</span>
          <button data-action="session.recent-page" data-value="next" disabled={busy || opening || !!error || remaining === 0} onClick={() => setView({ ...view, offset: view.offset + TREE_PAGE })}>{t("common.next_page")}</button>
          <button data-action="layer.dismiss" disabled={opening} onClick={onClose}>{t("common.close")}</button>
        </div>
      </div>
    </div>, document.body,
  );
}
