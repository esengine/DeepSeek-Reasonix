import { useCallback, useEffect, useMemo, useState } from "react";
import {
  MessageSquare,
  Plus,
  Server,
  Sparkles,
  Trash2,
} from "lucide-react";
import {
  LocalBackend,
  RemoteBackend,
  type SessionBackend,
} from "./backend/session-backend";
import type { SessionDescriptor, SessionRuntime } from "./protocol/types";
import { resolveLocale, t, type Locale } from "./i18n/messages";
import { applyPlatform, detectPlatform, type Platform } from "./lib/platform";
import {
  loadPairedNodes,
  savePairedNodes,
  type PairedNode,
} from "./lib/paired-nodes";
import {
  isDangerousWrite,
  riskFromTool,
  type ApprovalRequest,
  type ApprovalRisk,
} from "./lib/approval";
import { ApprovalSheet } from "./components/ApprovalSheet";
import { ChatView, type ChatLine } from "./components/ChatView";
import { EmptyState } from "./components/EmptyState";
import { IconButton } from "./components/IconButton";
import { NewSessionSheet } from "./components/NewSessionSheet";
import { PairNodeSheet } from "./components/PairNodeSheet";
import { SessionList } from "./components/SessionList";
import { SettingsPage, type ThemePref } from "./components/SettingsPage";
import { TabBar, type Tab } from "./components/TabBar";
import { TopBar } from "./components/TopBar";

function applyTheme(pref: ThemePref) {
  const root = document.documentElement;
  if (pref === "system") {
    const light = window.matchMedia("(prefers-color-scheme: light)").matches;
    root.setAttribute("data-theme", light ? "light" : "dark");
  } else {
    root.setAttribute("data-theme", pref);
  }
}

function useWide(): boolean {
  const [wide, setWide] = useState(
    () => typeof window !== "undefined" && window.matchMedia("(min-width: 900px)").matches,
  );
  useEffect(() => {
    const mq = window.matchMedia("(min-width: 900px)");
    const on = () => setWide(mq.matches);
    on();
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, []);
  return wide;
}

function parseApprovalEvent(
  sessionId: string,
  event: unknown,
): ApprovalRequest | null {
  const e = event as {
    kind?: string;
    approval?: {
      id?: string;
      tool?: string;
      subject?: string;
      reason?: string;
      risk?: ApprovalRisk;
      command?: string;
      diff?: string;
      dangerousWrite?: boolean;
    };
  };
  if (e.kind !== "approval_request" || !e.approval?.id) return null;
  const tool = e.approval.tool || "tool";
  const subject = e.approval.subject || "";
  const risk = e.approval.risk || riskFromTool(tool, subject);
  return {
    id: e.approval.id,
    sessionId,
    tool,
    subject,
    reason: e.approval.reason,
    risk,
    command: e.approval.command,
    diff: e.approval.diff,
    dangerousWrite:
      e.approval.dangerousWrite ?? isDangerousWrite(risk, tool),
  };
}

export function App() {
  const [locale, setLocale] = useState<Locale>(() => resolveLocale(navigator.language));
  const [theme, setTheme] = useState<ThemePref>("system");
  const [platform, setPlatform] = useState<Platform>(() => detectPlatform());
  const [tab, setTab] = useState<Tab>("sessions");
  const [sessions, setSessions] = useState<SessionDescriptor[]>([]);
  const [backends, setBackends] = useState<Record<string, SessionBackend>>({});
  const [activeId, setActiveId] = useState<string | null>(null);
  const [linesById, setLinesById] = useState<Record<string, ChatLine[]>>({});
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [pairOpen, setPairOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [pairedNodes, setPairedNodes] = useState<PairedNode[]>(() => loadPairedNodes());
  const [approval, setApproval] = useState<ApprovalRequest | null>(null);
  const [approvalOpen, setApprovalOpen] = useState(false);
  const [approvalBusy, setApprovalBusy] = useState(false);

  const localBackend = useMemo(() => new LocalBackend(), []);
  const wide = useWide();
  const active = sessions.find((s) => s.id === activeId) ?? null;
  const activeBackend = activeId ? backends[activeId] : undefined;
  const lines = activeId ? linesById[activeId] ?? [] : [];
  const chatOpen = Boolean(active) && (wide || tab === "sessions");
  const pendingApproval =
    Boolean(approval && activeId && approval.sessionId === activeId) ||
    active?.status === "pending_approval";

  useEffect(() => {
    applyPlatform(platform);
  }, [platform]);

  useEffect(() => {
    applyTheme(theme);
    if (theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const onChange = () => applyTheme("system");
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [theme]);

  useEffect(() => {
    savePairedNodes(pairedNodes);
  }, [pairedNodes]);

  const openNewSheet = useCallback(() => {
    setCreateError(null);
    setSheetOpen(true);
  }, []);

  const createSession = useCallback(
    async (input: { runtime: SessionRuntime; nodeUrl?: string }) => {
      setCreating(true);
      setCreateError(null);
      try {
        let backend: SessionBackend;
        let d: SessionDescriptor;
        if (input.runtime === "local") {
          backend = localBackend;
          d = await backend.createSession({
            runtime: "local",
            title: t(locale, "sessions.runtimeLocal"),
          });
        } else {
          const url = (input.nodeUrl || "http://127.0.0.1:8790").replace(/\/$/, "");
          backend = new RemoteBackend(url);
          d = await backend.createSession({
            runtime: "remote",
            title: t(locale, "sessions.runtimeRemote"),
          });
        }
        setBackends((prev) => ({ ...prev, [d.id]: backend }));
        setSessions((prev) => [d, ...prev]);
        setLinesById((prev) => ({ ...prev, [d.id]: [] }));
        setActiveId(d.id);
        setDraft("");
        setTab("sessions");
        setSheetOpen(false);
      } catch (err) {
        const msg = err instanceof Error ? err.message : t(locale, "sessions.createError");
        setCreateError(msg);
      } finally {
        setCreating(false);
      }
    },
    [localBackend, locale],
  );

  const send = useCallback(async () => {
    if (!active || !activeBackend || !draft.trim() || sending) return;
    const text = draft.trim();
    const sessionId = active.id;
    setDraft("");
    setSending(true);
    setSessions((prev) =>
      prev.map((s) =>
        s.id === sessionId ? { ...s, status: "running", updatedAt: new Date().toISOString() } : s,
      ),
    );
    setLinesById((prev) => ({
      ...prev,
      [sessionId]: [
        ...(prev[sessionId] ?? []),
        { id: `u-${Date.now()}`, kind: "user", text, role: "user" },
      ],
    }));
    const unsub = activeBackend.subscribe(sessionId, (event, seq) => {
      const e = event as { kind?: string; text?: string };
      const kind = e.kind || "event";
      const appr = parseApprovalEvent(sessionId, event);
      if (appr) {
        setApproval(appr);
        setApprovalOpen(true);
        setSessions((prev) =>
          prev.map((s) =>
            s.id === sessionId ? { ...s, status: "pending_approval" } : s,
          ),
        );
      }
      let role: ChatLine["role"] = "assistant";
      if (kind === "notice" || kind.startsWith("tool_") || kind === "approval_request") {
        role = "tool";
      }
      if (kind === "turn_started" || kind === "turn_done") role = "system";
      let textOut = e.text;
      if (!textOut) {
        if (kind === "turn_started") textOut = "…";
        else if (kind === "turn_done") textOut = "✓";
        else if (kind === "approval_request") textOut = t(locale, "approval.banner");
        else textOut = JSON.stringify(event);
      }
      setLinesById((prev) => ({
        ...prev,
        [sessionId]: [
          ...(prev[sessionId] ?? []),
          { id: `e-${seq}-${kind}`, kind, text: textOut!, role },
        ],
      }));
    });
    try {
      await activeBackend.submit(sessionId, { text }, `req_${Date.now()}`);
      const snap = await activeBackend.snapshot(sessionId);
      setSessions((prev) =>
        prev.map((s) => (s.id === sessionId ? snap.descriptor : s)),
      );
      if (snap.descriptor.status !== "pending_approval") {
        setApproval((a) => (a?.sessionId === sessionId ? null : a));
        setApprovalOpen(false);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : "send failed";
      setLinesById((prev) => ({
        ...prev,
        [sessionId]: [
          ...(prev[sessionId] ?? []),
          { id: `err-${Date.now()}`, kind: "notice", text: msg, role: "tool" },
        ],
      }));
    } finally {
      unsub();
      setSending(false);
    }
  }, [active, activeBackend, draft, sending, locale]);

  const resolveApproval = useCallback(
    async (allow: boolean) => {
      if (!approval || !activeBackend) return;
      setApprovalBusy(true);
      try {
        await activeBackend.approve(
          approval.sessionId,
          { id: approval.id, allow },
          `req_appr_${Date.now()}`,
        );
        setApprovalOpen(false);
        if (!allow) setApproval(null);
      } finally {
        setApprovalBusy(false);
      }
    },
    [approval, activeBackend],
  );

  const selectSession = (id: string) => {
    setActiveId(id);
    setDraft("");
    setTab("sessions");
  };

  const iosChrome = platform === "ios";

  const listPane = (
    <div className="session-list-pane anim-page">
      <TopBar
        title={t(locale, "sessions.title")}
        largeTitle={iosChrome}
        trailing={
          <IconButton label={t(locale, "sessions.new")} onClick={openNewSheet}>
            <Plus size={22} strokeWidth={2.25} />
          </IconButton>
        }
      />
      <div className="page-scroll">
        {iosChrome ? <h1 className="large-title">{t(locale, "sessions.title")}</h1> : null}
        {sessions.length === 0 ? (
          <EmptyState
            icon={<MessageSquare size={28} strokeWidth={1.75} />}
            title={t(locale, "sessions.emptyTitle")}
            description={t(locale, "sessions.empty")}
            actions={
              <button type="button" className="btn-primary" onClick={openNewSheet}>
                <Plus size={18} />
                {t(locale, "sessions.new")}
              </button>
            }
          />
        ) : (
          <SessionList
            sessions={sessions}
            activeId={activeId}
            locale={locale}
            onSelect={selectSession}
          />
        )}
      </div>
      <button
        type="button"
        className="fab"
        aria-label={t(locale, "sessions.new")}
        onClick={openNewSheet}
      >
        <Plus size={26} strokeWidth={2.25} />
      </button>
    </div>
  );

  const detailPane = active ? (
    <div className="session-detail-pane">
      <ChatView
        session={active}
        lines={lines}
        draft={draft}
        sending={sending}
        locale={locale}
        showBack={!wide}
        pendingApproval={pendingApproval}
        onBack={() => setActiveId(null)}
        onDraftChange={setDraft}
        onSend={() => void send()}
        onOpenApproval={() => setApprovalOpen(true)}
      />
    </div>
  ) : wide ? (
    <div className="session-detail-pane session-detail-placeholder">
      {t(locale, "sessions.selectHint")}
    </div>
  ) : null;

  return (
    <div
      className="app-shell"
      data-chat-open={chatOpen && !wide ? "true" : "false"}
      data-wide={wide ? "true" : "false"}
    >
      <div className="app-body">
        {tab === "sessions" && (
          <div
            className="sessions-root sessions-split"
            data-detail={active ? "true" : "false"}
          >
            {listPane}
            {detailPane}
          </div>
        )}

        {tab === "nodes" && (
          <div className="page anim-page">
            <TopBar
              title={t(locale, "nodes.title")}
              largeTitle={iosChrome}
              trailing={
                <IconButton label={t(locale, "nodes.pair")} onClick={() => setPairOpen(true)}>
                  <Plus size={22} strokeWidth={2.25} />
                </IconButton>
              }
            />
            <div className="page-scroll">
              {iosChrome ? <h1 className="large-title">{t(locale, "nodes.title")}</h1> : null}
              {pairedNodes.length === 0 ? (
                <EmptyState
                  icon={<Server size={28} strokeWidth={1.75} />}
                  title={t(locale, "nodes.emptyTitle")}
                  description={t(locale, "nodes.empty")}
                  actions={
                    <>
                      <button
                        type="button"
                        className="btn-primary"
                        onClick={() => setPairOpen(true)}
                      >
                        {t(locale, "nodes.pair")}
                      </button>
                      <p className="empty-desc" style={{ marginTop: 4 }}>
                        {t(locale, "nodes.pairHint")}
                      </p>
                    </>
                  }
                />
              ) : (
                <section className="list-section anim-enter">
                  <div className="list-group" role="list">
                    {pairedNodes.map((n) => (
                      <div key={n.id} className="list-row node-row" role="listitem">
                        <span
                          className="list-row-leading"
                          data-runtime="remote"
                          aria-hidden
                        >
                          <Server size={16} strokeWidth={2} />
                        </span>
                        <span className="list-row-body">
                          <div className="list-row-title">{n.name}</div>
                          <div className="list-row-meta">
                            <span className="mono">{n.baseUrl}</span>
                          </div>
                          <div className="list-row-meta">
                            <span className="status-label">
                              <span
                                className="status-dot"
                                data-status={n.online ? "idle" : "failed"}
                              />
                              {n.online
                                ? t(locale, "nodes.online")
                                : t(locale, "nodes.offline")}
                            </span>
                            {n.fingerprint ? (
                              <>
                                <span aria-hidden>·</span>
                                <span className="mono faint">
                                  {t(locale, "nodes.fingerprint")}: {n.fingerprint.slice(0, 12)}
                                </span>
                              </>
                            ) : null}
                          </div>
                          <div className="node-actions">
                            <button
                              type="button"
                              className="btn-secondary btn-compact"
                              onClick={() =>
                                void createSession({
                                  runtime: "remote",
                                  nodeUrl: n.baseUrl,
                                })
                              }
                            >
                              {t(locale, "nodes.useForSession")}
                            </button>
                            <button
                              type="button"
                              className="icon-btn neutral"
                              aria-label={t(locale, "nodes.remove")}
                              onClick={() =>
                                setPairedNodes((prev) => prev.filter((x) => x.id !== n.id))
                              }
                            >
                              <Trash2 size={18} />
                            </button>
                          </div>
                        </span>
                      </div>
                    ))}
                  </div>
                  <p className="footnote">{t(locale, "nodes.pairHint")}</p>
                </section>
              )}
            </div>
            <button
              type="button"
              className="fab"
              aria-label={t(locale, "nodes.pair")}
              onClick={() => setPairOpen(true)}
            >
              <Plus size={26} strokeWidth={2.25} />
            </button>
          </div>
        )}

        {tab === "providers" && (
          <div className="page anim-page">
            <TopBar title={t(locale, "providers.title")} largeTitle={iosChrome} />
            <div className="page-scroll">
              {iosChrome ? (
                <h1 className="large-title">{t(locale, "providers.title")}</h1>
              ) : null}
              <EmptyState
                icon={<Sparkles size={28} strokeWidth={1.75} />}
                title={t(locale, "providers.emptyTitle")}
                description={t(locale, "providers.empty")}
                actions={
                  <button type="button" className="btn-primary" disabled>
                    {t(locale, "providers.add")}
                  </button>
                }
              />
            </div>
          </div>
        )}

        {tab === "settings" && (
          <div className="page anim-page">
            <TopBar title={t(locale, "settings.title")} largeTitle={iosChrome} />
            <SettingsPage
              locale={locale}
              theme={theme}
              platform={platform}
              showLargeTitle={iosChrome}
              onLocale={setLocale}
              onTheme={setTheme}
              onPlatform={setPlatform}
            />
          </div>
        )}
      </div>

      <TabBar
        tab={tab}
        locale={locale}
        onChange={(next) => {
          setTab(next);
          if (next !== "sessions" && !wide) setActiveId(null);
        }}
      />

      <NewSessionSheet
        open={sheetOpen}
        locale={locale}
        busy={creating}
        error={createError}
        pairedNodes={pairedNodes}
        onClose={() => !creating && setSheetOpen(false)}
        onCreate={(input) => void createSession(input)}
      />

      <PairNodeSheet
        open={pairOpen}
        locale={locale}
        onClose={() => setPairOpen(false)}
        onPaired={(p) => {
          const node: PairedNode = {
            id: p.id,
            name: p.name,
            baseUrl: p.baseUrl,
            fingerprint: p.fingerprint,
            online: p.online,
            pairedAt: new Date().toISOString(),
          };
          setPairedNodes((prev) => {
            const rest = prev.filter((x) => x.id !== node.id && x.baseUrl !== node.baseUrl);
            return [node, ...rest];
          });
        }}
      />

      <ApprovalSheet
        open={approvalOpen && Boolean(approval)}
        locale={locale}
        request={approval}
        busy={approvalBusy}
        onClose={() => setApprovalOpen(false)}
        onAllow={() => void resolveApproval(true)}
        onDeny={() => void resolveApproval(false)}
      />
    </div>
  );
}
