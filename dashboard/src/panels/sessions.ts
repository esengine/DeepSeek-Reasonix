import { useCallback, useState } from "preact/hooks";
import { ChatMessage } from "../components/chat-internals.js";
import { api } from "../lib/api.js";
import { fmtBytes, fmtNum, fmtRelativeTime } from "../lib/format.js";
import { html } from "../lib/html.js";
import { usePoll } from "../lib/use-poll.js";
import { t, useLang } from "../i18n/index.js";

interface SessionEntry {
  name: string;
  messageCount: number;
  size: number;
  mtime: string | number;
}

interface SessionsData {
  sessions?: SessionEntry[];
  currentSession?: string | null;
  canSwitch?: boolean;
}

interface OpenSession {
  name: string;
  messages: unknown[] | null;
  error?: string;
}

export function SessionsPanel() {
  useLang();
  const { data, error, loading, refresh } = usePoll<SessionsData>("/sessions", 5000);
  const [open, setOpen] = useState<OpenSession | null>(null);
  const [openLoading, setOpenLoading] = useState(false);
  const [filter, setFilter] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const view = useCallback(async (name: string) => {
    setOpen({ name, messages: null });
    setOpenLoading(true);
    try {
      const detail = await api<{ messages: unknown[] }>(`/sessions/${encodeURIComponent(name)}`);
      setOpen({ name, messages: detail.messages });
    } catch (err) {
      setOpen({ name, messages: null, error: (err as Error).message });
    } finally {
      setOpenLoading(false);
    }
  }, []);

  const newSession = useCallback(async () => {
    setBusy("new");
    setActionError(null);
    try {
      await api("/sessions/new", { method: "POST" });
      setOpen(null);
      await refresh();
    } catch (err) {
      setActionError((err as Error).message);
    } finally {
      setBusy(null);
    }
  }, [refresh]);

  const switchTo = useCallback(
    async (name: string) => {
      setBusy(`switch:${name}`);
      setActionError(null);
      try {
        await api(`/sessions/${encodeURIComponent(name)}/switch`, { method: "POST" });
        setOpen(null);
        await refresh();
      } catch (err) {
        setActionError((err as Error).message);
      } finally {
        setBusy(null);
      }
    },
    [refresh],
  );

  const remove = useCallback(
    async (name: string) => {
      if (!confirm(t("sessions.deleteConfirm", { name }))) return;
      setBusy(`delete:${name}`);
      setActionError(null);
      try {
        await api(`/sessions/${encodeURIComponent(name)}`, { method: "DELETE" });
        if (open?.name === name) setOpen(null);
        await refresh();
      } catch (err) {
        setActionError((err as Error).message);
      } finally {
        setBusy(null);
      }
    },
    [open, refresh],
  );

  if (loading && !data)
    return html`<div class="card" style="color:var(--fg-3)">${t("sessions.loading")}</div>`;
  if (error)
    return html`<div class="card accent-err">${t("common.loadingFailed", { name: "sessions", error: error.message })}</div>`;
  const sessions = data?.sessions ?? [];
  const currentSession = data?.currentSession ?? null;
  const canSwitch = data?.canSwitch ?? false;

  const filtered = filter.trim()
    ? sessions.filter((s) => s.name.toLowerCase().includes(filter.toLowerCase()))
    : sessions;

  return html`
    <div class="sessions-grid">
      <div class="sessions-list">
        <div class="ssl-h" style="display:flex;gap:6px">
          <input
            type="text"
            placeholder=${t("sessions.filterPlaceholder")}
            value=${filter}
            onInput=${(e: Event) => setFilter((e.target as HTMLInputElement).value)}
            style="flex:1"
          />
          <button
            class="btn primary"
            disabled=${!canSwitch || busy === "new"}
            title=${canSwitch ? t("sessions.newHint") : t("sessions.attachRequired")}
            onClick=${newSession}
          >
            ${busy === "new" ? t("common.loading") : `+ ${t("sessions.newBtn")}`}
          </button>
        </div>
        ${
          !canSwitch
            ? html`<div style="padding:0 12px 6px;font-size:11.5px;color:var(--fg-3)">${t("sessions.attachRequired")}</div>`
            : null
        }
        ${
          actionError
            ? html`<div class="card accent-err" style="margin:0 12px 8px;padding:6px 10px;font-size:12px">${actionError}</div>`
            : null
        }
        <div class="chips" style="padding:0 12px 8px">
          <span class="chip-f static active">${t("common.all")} <span class="ct">${sessions.length}</span></span>
          ${currentSession ? html`<span class="chip-f static">${t("sessions.activeChip")} <span class="ct">${currentSession}</span></span>` : null}
        </div>
        ${
          sessions.length === 0
            ? html`<div class="ctx-empty" style="padding:24px 12px;color:var(--fg-3)">${t("sessions.noSessions")}</div>`
            : html`<div class="ssl-rows">
                ${filtered.map((s) => {
                  const isCurrent = currentSession === s.name;
                  return html`
                    <div
                      class=${`ssl-row ${open?.name === s.name ? "sel" : ""}`}
                      onClick=${() => view(s.name)}
                    >
                      <span class="name">
                        ${isCurrent ? html`<span class="pill ok" style="margin-right:6px">${t("sessions.activePill")}</span>` : null}
                        ${s.name}
                      </span>
                      <span class="meta">
                        <span><span class="v">${fmtNum(s.messageCount)}</span> ${t("sessions.msgs")}</span>
                        <span><span class="v">${fmtBytes(s.size)}</span></span>
                        <span>${fmtRelativeTime(s.mtime)}</span>
                      </span>
                    </div>
                  `;
                })}
              </div>`
        }
      </div>

      <div class="sessions-detail">
        ${
          open == null
            ? html`<div style="color:var(--fg-3);font-size:13px;text-align:center;padding:60px 20px">
                ${t("sessions.pickHint")}
              </div>`
            : (() => {
                const isCurrent = currentSession === open.name;
                return html`
                <div class="sessions-detail-h">
                  <span class="name">
                    ${isCurrent ? html`<span class="pill ok" style="margin-right:6px">${t("sessions.activePill")}</span>` : null}
                    ${open.name}
                  </span>
                  <span class="ws">
                    ${
                      open.messages
                        ? t("sessions.messages", { count: open.messages.length, s: open.messages.length === 1 ? "" : "s" })
                        : t("common.loading")
                    }
                  </span>
                  <span class="actions">
                    ${
                      canSwitch && !isCurrent
                        ? html`<button class="btn primary" disabled=${busy === `switch:${open.name}`} onClick=${() => switchTo(open.name)}>${busy === `switch:${open.name}` ? t("common.loading") : t("sessions.switchBtn")}</button>`
                        : null
                    }
                    <button
                      class="btn"
                      disabled=${isCurrent || busy === `delete:${open.name}`}
                      title=${isCurrent ? t("sessions.cantDeleteActive") : t("sessions.deleteBtn")}
                      style="border-color:var(--c-err);color:var(--c-err)"
                      onClick=${() => remove(open.name)}
                    >${busy === `delete:${open.name}` ? t("common.loading") : t("sessions.deleteBtn")}</button>
                    <button class="btn ghost" onClick=${() => setOpen(null)}>${t("common.back")}</button>
                  </span>
                </div>
                ${
                  !canSwitch
                    ? html`<div class="card accent-brand" style="margin-bottom:10px">
                        <div class="card-h"><span class="title">${t("sessions.resumeTitle")}</span></div>
                        <div class="card-b" style="font-size:12.5px;color:var(--fg-2)">
                          ${t("sessions.resumeDesc")}
                          <code class="mono" style="display:block;margin-top:8px;padding:8px 10px;background:var(--bg-input);border-radius:var(--r);color:var(--fg-0);font-size:12px;user-select:all">reasonix chat --session ${open.name}</code>
                        </div>
                      </div>`
                    : null
                }
                ${
                  openLoading
                    ? html`<div style="color:var(--fg-3)">${t("sessions.loadingTranscript")}</div>`
                    : open.error
                      ? html`<div class="card accent-err">${open.error}</div>`
                      : open.messages && open.messages.length > 0
                        ? html`<div class="chat-feed" style="max-height:calc(100vh - 220px);overflow-y:auto">
                            ${open.messages.map(
                              (m: any, i: number) => html`
                                <${ChatMessage}
                                  key=${i}
                                  msg=${{
                                    id: `r-${i}`,
                                    role:
                                      m.role === "tool"
                                        ? "tool"
                                        : m.role === "assistant"
                                          ? "assistant"
                                          : m.role === "user"
                                            ? "user"
                                            : "info",
                                    text: m.content ?? "",
                                    toolName: m.toolName,
                                  }}
                                  streaming=${false}
                                />
                              `,
                            )}
                          </div>`
                        : html`<div style="color:var(--fg-3)">${t("sessions.emptyTranscript")}</div>`
                }
              `;
              })()
        }
      </div>
    </div>
  `;
}
