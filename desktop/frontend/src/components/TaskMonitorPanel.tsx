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

type CatalogTask = TaskSnapshot & { __projectKey: string; __projectLabel: string; __catalogKey: string };

function hasTaskCatalogBinding(): boolean {
  const bound = (window as unknown as { go?: { main?: { App?: { ListTaskPage?: unknown } } } }).go?.main?.App?.ListTaskPage;
  return typeof bound === "function";
}

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

function isStoppableState(state: string): boolean {
  return state === "queued" || state === "running" || state === "waiting";
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
      return t("task.event.stateChange", { state: stateConfig(ev.state, t).label, runtime: runtimeConfig(ev.runtime_state, t).label });
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
  initialScope = "session",
  popover = false,
  summaryMode = false,
}: {
  tabID: string;
  onClose?: () => void;
  onOpenSession?: (tabID: string, taskID: string) => Promise<boolean> | boolean;
  initialOpen?: boolean;
  initialScope?: "session" | "project" | "all";
  popover?: boolean;
  summaryMode?: boolean;
}) {
  const t = useT();
  const [tasks, setTasks] = useState<CatalogTask[]>([]);
	const [scope, setScope] = useState<"session" | "project" | "all">(initialScope);
	const [query, setQuery] = useState("");
	const [nextCursor, setNextCursor] = useState("");
	const [indexProgress, setIndexProgress] = useState<{ indexed: number; total: number; partial: boolean }>({ indexed: 0, total: 0, partial: true });
	const requestSeq = useRef(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState(initialOpen);
  const [actionTask, setActionTask] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [nowMs, setNowMs] = useState(() => Date.now());
  const [pendingStop, setPendingStop] = useState<CatalogTask | null>(null);
  const stopButtonRefs = useRef<Map<string, HTMLButtonElement>>(new Map());
  const confirmStopRef = useRef<HTMLButtonElement | null>(null);

  // Per-task event state
  const [taskEvents, setTaskEvents] = useState<Map<string, TaskEvent[]>>(
    () => new Map(),
  );
  const [eventsLoading, setEventsLoading] = useState<Set<string>>(new Set());
  const [eventsError, setEventsError] = useState<Map<string, string>>(
    () => new Map(),
  );
  const eventCursors = useRef<Map<string, number>>(new Map());

  const fetchTasks = useCallback(async (cursor = "") => {
	const seq = ++requestSeq.current;
    try {
      setError(null);
			if (!hasTaskCatalogBinding()) {
				const legacy = await app.ListTasksForTab(tabID);
				if (seq !== requestSeq.current) return;
				const filtered = legacy.filter((task) => !query.trim() || [task.task_id, task.session_id, task.error_code, task.error_summary].some((value) => (value || "").toLowerCase().includes(query.trim().toLowerCase())));
				setTasks(filtered.map((task) => ({ ...task, __projectKey: "", __projectLabel: "", __catalogKey: task.task_id })));
				setNextCursor("");
				setIndexProgress({ indexed: filtered.length, total: filtered.length, partial: false });
				return;
			}
			const page = await app.ListTaskPage({ scope, tabId: tabID, projectKey: "", states: [], query, cursor, limit: 50 });
			if (seq !== requestSeq.current) return;
			const decorated = (page.items ?? []).map((item) => ({ ...item.task, __projectKey: item.projectKey, __projectLabel: item.projectLabel, __catalogKey: `${item.projectKey}:${item.task.task_id}` }));
			setTasks((current) => cursor ? [...current, ...decorated.filter((item) => !current.some((existing) => existing.__catalogKey === item.__catalogKey))] : decorated);
			setNextCursor(page.nextCursor || "");
			setIndexProgress({ indexed: page.status.indexed, total: page.status.total, partial: page.partial });
    } catch (e) {
			if (seq !== requestSeq.current) return;
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, [query, scope, tabID]);

  // Fetch events for a single task, using afterSequence for incremental load.
	const fetchEvents = useCallback(async (task: CatalogTask) => {
		const taskID = task.__catalogKey;
    setEventsLoading((prev) => new Set(prev).add(taskID));
    setEventsError((prev) => {
      const next = new Map(prev);
      next.delete(taskID);
      return next;
    });
    try {
      const cursor = eventCursors.current.get(taskID) ?? 0;
			const events = hasTaskCatalogBinding()
				? (await app.ListTaskEventPage({ projectKey: task.__projectKey, taskId: task.task_id, after: cursor, limit: 50 })).items ?? []
				: await app.ListTaskEventsForTab(tabID, task.task_id, cursor);
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
		void fetchTasks("");
    const interval = setInterval(() => {
			void fetchTasks("");
    }, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
	}, [fetchTasks]);

  // Live tasks need a ticking clock; terminal and queued tasks stay frozen at
  // their persisted end/update time.
  useEffect(() => {
    if (!tasks.some((task) => task.runtime_state === "alive" && !isTerminalState(task.state))) return;
    const interval = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(interval);
  }, [tasks]);

  useEffect(() => {
    if (pendingStop) confirmStopRef.current?.focus();
  }, [pendingStop]);

  useEffect(() => {
    if (!pendingStop) return;
    const current = tasks.find((task) => task.__catalogKey === pendingStop.__catalogKey);
    if (!current || !isStoppableState(current.state)) setPendingStop(null);
  }, [pendingStop, tasks]);

  const dismissStopConfirmation = () => {
    const taskKey = pendingStop?.__catalogKey;
    setPendingStop(null);
    if (taskKey) {
      requestAnimationFrame(() => stopButtonRefs.current.get(taskKey)?.focus());
    }
  };

  const toggleTask = (task: CatalogTask) => {
    const id = task.__catalogKey;
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
        // Load events on first expand
        if (!taskEvents.has(id)) {
			void fetchEvents(task);
        }
      }
      return next;
    });
  };

  const controlTask = async (task: CatalogTask, action: "stop" | "requeue" | "open") => {
    setPendingStop(null);
    setActionTask(task.__catalogKey);
    setActionError(null);
    setActionMessage(null);
    try {
			if (action === "open" && onOpenSession && scope === "session") {
        const opened = await onOpenSession(tabID, task.task_id);
        if (opened) onClose?.();
        return;
      }
			const request = { projectKey: task.__projectKey, taskId: task.task_id, expectedVersion: task.version, reason: "desktop request", idempotencyKey: `desktop-${action}-${task.task_id}-${task.version}` };
			const result = hasTaskCatalogBinding()
				? action === "stop"
					? await app.StopTaskByKey(request)
					: action === "requeue"
						? await app.RequeueTaskByKey(request)
						: await app.OpenTaskSessionByKey({ projectKey: task.__projectKey, taskId: task.task_id })
				: action === "stop"
					? await app.StopTaskForTab(tabID, task.task_id, task.version, request.reason, request.idempotencyKey)
					: action === "requeue"
						? await app.RequeueTaskForTab(tabID, task.task_id, task.version, request.idempotencyKey)
						: await app.OpenTaskSessionForTab(tabID, task.task_id);
      if (result.error) {
        setActionError(`${result.error.code}: ${result.error.message}`);
      } else if (action === "open") {
        const sessionID = result.session_id?.trim();
        if (!sessionID) throw new Error("Task session is unavailable");
        setActionMessage(`Session: ${sessionID}`);
      } else {
        setActionMessage(result.idempotent ? "Already applied" : "Task updated");
				await fetchTasks("");
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
				void fetchTasks("");
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
			{!summaryMode && (
				<div className="taskmonitor__filters">
					<select value={scope} onChange={(event) => setScope(event.target.value as "session" | "project" | "all")} aria-label="Task scope">
						<option value="session">Current session</option>
						<option value="project">Current project</option>
						<option value="all">All projects</option>
					</select>
					<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Filter tasks" aria-label="Filter tasks" />
				</div>
			)}
			{indexProgress.partial && <div className="taskmonitor__indexing">Indexing tasks ({indexProgress.indexed}/{indexProgress.total})</div>}
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
				const taskKey = task.__catalogKey;
				const isOpen = expanded.has(taskKey);
              const terminal = isTerminalState(task.state);
				const evs = taskEvents.get(taskKey) ?? [];
				const evLoading = eventsLoading.has(taskKey);
				const evError = eventsError.get(taskKey);

              return (
                <div
					key={taskKey}
                  className={`taskmonitor__task taskmonitor__task--${safeStateClass(task.state)}`}
                >
                  <div className="taskmonitor__task-head">
                    <button
                      className="taskmonitor__expand"
						onClick={() => toggleTask(task)}
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
							{scope === "all" && <span className="taskmonitor__project">{task.__projectLabel}</span>}
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
                        <dd>{cfg.label}</dd>
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
                      {pendingStop?.__catalogKey === taskKey ? (
                        <div
                          className="taskmonitor__confirm"
                          role="group"
                          aria-label={t("summary.confirmStop")}
                          onKeyDown={(event) => {
                            if (event.key === "Escape") {
                              event.preventDefault();
                              dismissStopConfirmation();
                            }
                          }}
                        >
                          <span className="taskmonitor__confirm-copy">{t("summary.confirmStop")}</span>
                          <div className="taskmonitor__confirm-actions">
                            <button
                              ref={confirmStopRef}
                              type="button"
                              className="taskmonitor__confirm-stop"
                              disabled={actionTask === taskKey}
                              onClick={() => void controlTask(task, "stop")}
                            >
                              {t("summary.stop")}
                            </button>
                            <button type="button" onClick={dismissStopConfirmation}>{t("summary.keep")}</button>
                          </div>
                        </div>
                      ) : (
                        <div className="taskmonitor__actions">
                          {isStoppableState(task.state) && (
                            <button
                              ref={(node) => {
                                if (node) stopButtonRefs.current.set(taskKey, node);
                                else stopButtonRefs.current.delete(taskKey);
                              }}
                              className="taskmonitor__stop"
                              disabled={actionTask === taskKey}
                              onClick={() => setPendingStop(task)}
                            >
                              {t("summary.stop")}
                            </button>
                          )}
                          {(task.state === "failed" || task.state === "stale") && (
                            <button disabled={actionTask === taskKey || task.runtime_state === "alive"} onClick={() => void controlTask(task, "requeue")}>{t("summary.requeue")}</button>
                          )}
                          <button disabled={actionTask === taskKey} onClick={() => void controlTask(task, "open")}>{t("summary.openSession")}</button>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
			{nextCursor && !loading && !error && (
				<button className="taskmonitor__load-more" onClick={() => void fetchTasks(nextCursor)}>
					Load more
				</button>
			)}
        </div>
      )}
    </div>
  );
}
