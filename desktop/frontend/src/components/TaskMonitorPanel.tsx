import { useCallback, useEffect, useRef, useState } from "react";
import {
  AlertCircle,
  ChevronDown,
  ChevronRight,
  Clock,
  List,
  Loader2,
  RotateCw,
  X,
  XCircle,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { TaskEvent, TaskSnapshot } from "../lib/types";

// --- helpers ---

type TaskTimerSnapshot = TaskSnapshot & { runtime_lease_until?: string };

const STATE_CONFIG: Record<
  string,
  { key: "queued" | "running" | "waiting" | "succeeded" | "failed" | "cancelled" | "stale"; color: string; dot: string }
> = {
  queued: { key: "queued", color: "#6b7280", dot: "⚪" },
  running: { key: "running", color: "#3b82f6", dot: "🔵" },
  waiting: { key: "waiting", color: "#f59e0b", dot: "🟡" },
  succeeded: { key: "succeeded", color: "#22c55e", dot: "🟢" },
  failed: { key: "failed", color: "#ef4444", dot: "🔴" },
  cancelled: { key: "cancelled", color: "#9ca3af", dot: "⏹️" },
  stale: { key: "stale", color: "#d4d4d8", dot: "⬜" },
};

function stateConfig(state: string, t: ReturnType<typeof useT>) {
  const config = STATE_CONFIG[state];
  return config
    ? { ...config, label: t(`task.state.${config.key}` as never) }
    : { label: state, color: "#6b7280", dot: "❓" };
}

function runtimeConfig(state: string | undefined, t: ReturnType<typeof useT>) {
  switch (state) {
    case "alive":
      return { label: t("task.runtime.live"), color: "#22c55e" };
    case "exited":
      return { label: t("task.runtime.exited"), color: "#9ca3af" };
    default:
      return { label: t("task.runtime.unknown"), color: "#6b7280" };
  }
}

function safeStateClass(state: string): string {
  // Sanitize state for use in CSS class names — only allow word chars.
  return state.replace(/[^a-zA-Z0-9_-]/g, "_");
}

function isTerminalState(state: string): boolean {
  return state === "succeeded" || state === "failed" || state === "cancelled" || state === "stale";
}

function elapsed(task: TaskTimerSnapshot, nowMs: number): string {
  if (!task.created_at) return "—";
  const startMs = new Date(task.created_at).getTime();
  if (task.state === "queued") return "—";
  const live = task.runtime_state === "alive" && !isTerminalState(task.state);
  let endMs = live ? nowMs : new Date(task.updated_at).getTime();
  if (task.state === "stale" && task.runtime_lease_until) {
    const leaseEndMs = new Date(task.runtime_lease_until).getTime();
    // Stale is inferred when an alive runtime lease expires. The observer does
    // not rewrite updated_at, so the expired lease is the best bounded end time.
    if (!isNaN(leaseEndMs) && leaseEndMs >= startMs && leaseEndMs <= nowMs) {
      endMs = leaseEndMs;
    }
  }
  const ms = endMs - startMs;
  if (isNaN(ms) || ms < 0) return "—";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return `${h}h`;
}

function shortID(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}

function eventSummary(ev: TaskEvent, t: ReturnType<typeof useT>): string {
  if (ev.error_code) return t("task.event.error", { code: ev.error_code });
  switch (ev.event_type) {
    case "state_change":
      return t("task.event.stateChange", { state: ev.state, runtime: runtimeConfig(ev.runtime_state, t).label });
    case "error":
      return ev.error_summary || t("task.error");
    default:
      return ev.event_type;
  }
}

// --- component ---

const POLL_INTERVAL_MS = 5000;

export function TaskMonitorPanel({
	tabID,
  onClose,
  onOpenSession,
  initialOpen = false,
  popover = false,
  summaryMode = false,
}: {
  tabID: string;
  onClose?: () => void;
  onOpenSession?: (tabID: string, taskID: string) => Promise<boolean> | boolean;
  initialOpen?: boolean;
  popover?: boolean;
  summaryMode?: boolean;
}) {
  const t = useT();
  const [tasks, setTasks] = useState<TaskSnapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState(initialOpen);
  const [actionTask, setActionTask] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [nowMs, setNowMs] = useState(() => Date.now());
  const [pendingAction, setPendingAction] = useState<{ task: TaskSnapshot; action: "stop" | "cancel" } | null>(null);

  // Per-task event state
  const [taskEvents, setTaskEvents] = useState<Map<string, TaskEvent[]>>(
    () => new Map(),
  );
  const [eventsLoading, setEventsLoading] = useState<Set<string>>(new Set());
  const [eventsError, setEventsError] = useState<Map<string, string>>(
    () => new Map(),
  );
  const eventCursors = useRef<Map<string, number>>(new Map());

  const fetchTasks = useCallback(async () => {
    try {
      setError(null);
      const list = await app.ListTasksForTab(tabID);
      setTasks(list ?? []);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, [tabID]);

  // Fetch events for a single task, using afterSequence for incremental load.
  const fetchEvents = useCallback(async (taskID: string) => {
    setEventsLoading((prev) => new Set(prev).add(taskID));
    setEventsError((prev) => {
      const next = new Map(prev);
      next.delete(taskID);
      return next;
    });
    try {
      const cursor = eventCursors.current.get(taskID) ?? 0;
      const events = await app.ListTaskEventsForTab(tabID, taskID, cursor);
      if (events.length > 0) {
        setTaskEvents((prev) => {
          const next = new Map(prev);
          const existing = next.get(taskID) ?? [];
          // Merge, deduplicate by sequence
          const seen = new Set(existing.map((e) => e.sequence));
          const merged = [...existing, ...events.filter((e) => !seen.has(e.sequence))];
          merged.sort((a, b) => a.sequence - b.sequence);
          next.set(taskID, merged);
          return next;
        });
        // Update cursor to the max sequence
        const maxSeq = events.reduce(
          (max, e) => Math.max(max, e.sequence),
          cursor,
        );
        eventCursors.current.set(taskID, maxSeq);
      }
    } catch (e) {
      setEventsError((prev) => {
        const next = new Map(prev);
        next.set(taskID, String(e));
        return next;
      });
    } finally {
      setEventsLoading((prev) => {
        const next = new Set(prev);
        next.delete(taskID);
        return next;
      });
    }
  }, [tabID]);

  // Initial fetch + periodic polling
  useEffect(() => {
    fetchTasks();
    const interval = setInterval(() => {
      fetchTasks();
      // Also refresh events for expanded tasks
      expanded.forEach((id) => {
        fetchEvents(id);
      });
    }, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [fetchTasks, fetchEvents, expanded]);

  // Live tasks need a ticking clock; terminal and queued tasks stay frozen at
  // their persisted end/update time.
  useEffect(() => {
    if (!tasks.some((task) => task.runtime_state === "alive" && !isTerminalState(task.state))) return;
    const interval = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(interval);
  }, [tasks]);

  const toggleTask = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
        // Load events on first expand
        if (!taskEvents.has(id)) {
          fetchEvents(id);
        }
      }
      return next;
    });
  };

  const controlTask = async (task: TaskSnapshot, action: "stop" | "cancel" | "requeue" | "open") => {
    if ((action === "stop" || action === "cancel") && (!pendingAction || pendingAction.task.task_id !== task.task_id || pendingAction.action !== action)) {
      setPendingAction({ task, action });
      return;
    }
    setPendingAction(null);
    setActionTask(task.task_id);
    setActionError(null);
    setActionMessage(null);
    try {
      if (action === "open" && onOpenSession) {
        const opened = await onOpenSession(tabID, task.task_id);
        if (opened) onClose?.();
        return;
      }
      const result = action === "stop"
        ? await app.StopTaskForTab(tabID, task.task_id, task.version, "desktop request", `desktop-${action}-${task.task_id}-${task.version}`)
        : action === "cancel"
          ? await app.CancelTaskForTab(tabID, task.task_id, task.version, "desktop request", `desktop-${action}-${task.task_id}-${task.version}`)
          : action === "requeue"
            ? await app.RequeueTaskForTab(tabID, task.task_id, task.version, `desktop-${action}-${task.task_id}-${task.version}`)
            : await app.OpenTaskSessionForTab(tabID, task.task_id);
      if (result.error) {
        setActionError(`${result.error.code}: ${result.error.message}`);
      } else if (action === "open") {
        const sessionID = result.session_id?.trim();
        if (!sessionID) throw new Error("Task session is unavailable");
        setActionMessage(`Session: ${sessionID}`);
      } else {
        setActionMessage(result.idempotent ? "Already applied" : "Task updated");
        await fetchTasks();
      }
    } catch (e) {
      setActionError(String(e));
    } finally {
      setActionTask(null);
    }
  };

  const sorted = [...tasks].sort(
    (a, b) =>
      new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
  );

  return (
    <div className={`taskmonitor${popover ? " taskmonitor--popover" : ""}`}>
      <div className="taskmonitor__head">
        <button
          className="taskmonitor__toggle"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          aria-label={open ? "Collapse tasks" : "Expand tasks"}
        >
          {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </button>
        <span className="taskmonitor__title">{summaryMode ? t("summary.session") : t("summary.tasks")}</span>
        <span className="taskmonitor__count">{tasks.length}</span>
        <button
          className="taskmonitor__refresh"
          onClick={() => {
            setLoading(true);
            fetchTasks();
          }}
          title={t("summary.refresh")}
          aria-label={t("summary.refresh")}
        >
          <RotateCw size={12} />
        </button>
        {onClose && (
          <button
            className="taskmonitor__close"
            onClick={onClose}
            title={t("common.close")}
            aria-label={t("summary.close")}
          >
            <X size={14} />
          </button>
        )}
      </div>

      {open && (
        <div className="taskmonitor__body">
          {summaryMode && <div className="taskmonitor__category-title">{t("summary.tasks")}</div>}
          {actionError && <div className="taskmonitor__state taskmonitor__state--error">{actionError}</div>}
          {actionMessage && <div className="taskmonitor__state">{actionMessage}</div>}
          {loading && (
            <div className="taskmonitor__state">
              <Loader2 size={16} className="taskmonitor__spinner" />
              <span>{t("common.loading")}</span>
            </div>
          )}

          {error && (
            <div className="taskmonitor__state taskmonitor__state--error">
              <AlertCircle size={16} />
              <span>{error}</span>
            </div>
          )}

          {!loading && !error && sorted.length === 0 && (
            <div className="taskmonitor__state taskmonitor__state--empty">
              <Clock size={16} />
              <span>{t("summary.noTasks")}</span>
            </div>
          )}

          {!loading &&
            sorted.map((task) => {
              const cfg = stateConfig(task.state, t);
              const runtime = runtimeConfig(task.runtime_state, t);
              const isOpen = expanded.has(task.task_id);
              const terminal = isTerminalState(task.state);
              const evs = taskEvents.get(task.task_id) ?? [];
              const evLoading = eventsLoading.has(task.task_id);
              const evError = eventsError.get(task.task_id);

              return (
                <div
                  key={task.task_id}
                  className={`taskmonitor__task taskmonitor__task--${safeStateClass(task.state)}`}
                >
                  <div className="taskmonitor__task-head">
                    <button
                      className="taskmonitor__expand"
                      onClick={() => toggleTask(task.task_id)}
                      aria-expanded={isOpen}
                      aria-label={t("summary.taskLabel", { id: shortID(task.task_id), state: cfg.label })}
                    >
                      <span
                        className="taskmonitor__dot"
                        style={{ color: cfg.color }}
                      >
                        {cfg.dot}
                      </span>
                      <span className="taskmonitor__id">
                        {shortID(task.task_id)}
                      </span>
                      <span
                        className="taskmonitor__badge"
                        style={{
                          backgroundColor: cfg.color + "18",
                          color: cfg.color,
                        }}
                      >
                        {cfg.label}
                      </span>
                      <span
                        className="taskmonitor__runtime"
                        style={{ color: runtime.color }}
                        title="Runtime process state"
                      >
                        <span aria-hidden="true">{task.runtime_state === "alive" ? "●" : "○"}</span>
                        {runtime.label}
                      </span>
                      {terminal && (
                        <XCircle size={12} className="taskmonitor__terminal" />
                      )}
                      <span className="taskmonitor__time">
                        {elapsed(task, nowMs)}
                      </span>
                      {isOpen ? (
                        <ChevronDown size={12} />
                      ) : (
                        <ChevronRight size={12} />
                      )}
                    </button>
                  </div>

                  {isOpen && (
                    <div className="taskmonitor__detail">
                      <dl>
                        <dt>{t("summary.taskId")}</dt>
                        <dd>{task.task_id}</dd>
                        <dt>{t("summary.sessionId")}</dt>
                        <dd>{task.session_id || "—"}</dd>
                        <dt>{t("summary.state")}</dt>
                        <dd>{task.state}</dd>
                        <dt>{t("summary.runtime")}</dt>
                        <dd>{runtime.label}</dd>
                        <dt>{t("summary.updated")}</dt>
                        <dd>{new Date(task.updated_at).toLocaleString()}</dd>
                        {task.error_code && (
                          <>
                            <dt>{t("summary.errorCode")}</dt>
                            <dd className="taskmonitor__err">{task.error_code}</dd>
                          </>
                        )}
                        {task.error_summary && (
                          <>
                            <dt>{t("summary.detail")}</dt>
                            <dd className="taskmonitor__err-summary">
                              {task.error_summary}
                            </dd>
                          </>
                        )}
                      </dl>

                      {/* Events section */}
                      <div className="taskmonitor__events">
                        <div className="taskmonitor__events-head">
                          <List size={12} />
                          <span>{t("summary.recentEvents")}</span>
                          {evs.length > 0 && (
                            <span className="taskmonitor__events-count">
                              {evs.length}
                            </span>
                          )}
                        </div>

                        {evLoading && evs.length === 0 && (
                          <div className="taskmonitor__state">
                            <Loader2
                              size={12}
                              className="taskmonitor__spinner"
                            />
                            <span>{t("summary.loadingEvents")}</span>
                          </div>
                        )}

                        {evError && (
                          <div className="taskmonitor__state taskmonitor__state--error">
                            <AlertCircle size={12} />
                            <span>{evError}</span>
                          </div>
                        )}

                        {!evLoading && !evError && evs.length === 0 && (
                          <div className="taskmonitor__state taskmonitor__state--empty">
                            <span>{t("summary.noEvents")}</span>
                          </div>
                        )}

                        {evs.length > 0 && (
                          <ul className="taskmonitor__event-list">
                            {evs.map((ev) => (
                              <li
                                key={ev.sequence}
                                className="taskmonitor__event"
                              >
                                <span className="taskmonitor__event-seq">
                                  #{ev.sequence}
                                </span>
                                <span className="taskmonitor__event-type">
                                  {eventSummary(ev, t)}
                                </span>
                                <span className="taskmonitor__event-time">
                                  {new Date(ev.timestamp).toLocaleTimeString()}
                                </span>
                              </li>
                            ))}
                          </ul>
                        )}
                      </div>
                      <div className="taskmonitor__actions">
                        {(task.state === "queued" || task.state === "running" || task.state === "waiting") && (
                          <>
                            <button disabled={actionTask === task.task_id} onClick={() => void controlTask(task, "stop")}>{t("summary.stop")}</button>
                            <button disabled={actionTask === task.task_id} onClick={() => void controlTask(task, "cancel")}>{t("summary.cancel")}</button>
                          </>
                        )}
                        {(task.state === "failed" || task.state === "stale") && (
                          <button disabled={actionTask === task.task_id || task.runtime_state === "alive"} onClick={() => void controlTask(task, "requeue")}>{t("summary.requeue")}</button>
                        )}
                        <button disabled={actionTask === task.task_id} onClick={() => void controlTask(task, "open")}>{t("summary.openSession")}</button>
                      </div>
                      {pendingAction?.task.task_id === task.task_id && (
                        <div className="taskmonitor__confirm">
                          <span>{t(pendingAction.action === "stop" ? "summary.confirmStop" : "summary.confirmCancel")}</span>
                          <button type="button" onClick={() => void controlTask(task, pendingAction.action)}>{t("common.confirm")}</button>
                          <button type="button" onClick={() => setPendingAction(null)}>{t("summary.keep")}</button>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
        </div>
      )}
    </div>
  );
}
