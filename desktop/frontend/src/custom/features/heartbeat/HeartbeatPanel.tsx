// Heartbeat Panel — Modal for configuring scheduled heartbeat tasks.
//
// Renders a list of tasks with add/edit/delete controls, plus a manual
// "run now" button for each. The panel is opened from the sidebar nav item.

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  Activity,
  ChevronLeft,
  ChevronsUpDown,
  Clock,
  Check,
  Heart,
  MessageSquare,
  Play,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { app } from "../../../lib/bridge";
import { useT } from "../../../lib/i18n";
import { AnchoredPopover } from "../../../components/AnchoredPopover";
import {
  heartbeatListTasks,
  heartbeatSaveTasks,
  heartbeatTriggerNow,
  heartbeatGenerateID,
} from "./heartbeat.bridge";
import type { HeartbeatTask } from "./heartbeat.types";
import type { WorkspaceView } from "../../../lib/types";

const INTERVAL_MS: Record<"s" | "m" | "h", number> = {
  s: 1000,
  m: 60_000,
  h: 3_600_000,
};

function heartbeatIntervalMs(interval?: string): number | null {
  const clean = (interval || "").replace(/\|.*$/, "");
  const m = clean.match(/^(\d+)([smh])$/);
  if (!m) return null;
  return parseInt(m[1], 10) * INTERVAL_MS[m[2] as "s" | "m" | "h"];
}

function heartbeatClockMinutes(value?: string): number | null {
  const m = (value || "").match(/^(\d{2}):(\d{2})$/);
  if (!m) return null;
  const hour = parseInt(m[1], 10);
  const minute = parseInt(m[2], 10);
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59) return null;
  return hour * 60 + minute;
}

function dateAtMinutes(base: Date, minutes: number): Date {
  const d = new Date(base);
  d.setHours(Math.floor(minutes / 60), minutes % 60, 0, 0);
  return d;
}

function heartbeatWithinWindow(date: Date, start: number | null, end: number | null): boolean {
  if (start === null && end === null) return true;
  const minutes = date.getHours() * 60 + date.getMinutes();
  if (start !== null && end === null) return minutes >= start;
  if (start === null && end !== null) return minutes < end;
  if (start === end) return true;
  if (start! < end!) return minutes >= start! && minutes < end!;
  return minutes >= start! || minutes < end!;
}

function nextHeartbeatWindowTime(from: Date, start: number | null, end: number | null): Date {
  if (heartbeatWithinWindow(from, start, end)) return from;
  if (start !== null && end === null) return dateAtMinutes(from, start);
  if (start === null && end !== null) {
    const next = new Date(from);
    next.setDate(next.getDate() + 1);
    next.setHours(0, 0, 0, 0);
    return next;
  }
  const minutes = from.getHours() * 60 + from.getMinutes();
  if (start! < end! && minutes < start!) return dateAtMinutes(from, start!);
  if (start! > end! && minutes < start! && minutes >= end!) return dateAtMinutes(from, start!);
  const next = dateAtMinutes(from, start!);
  next.setDate(next.getDate() + 1);
  return next;
}

export function heartbeatNextRunAt(task: Pick<HeartbeatTask, "interval" | "lastRunAt" | "timeWindowStart" | "timeWindowEnd">, now = Date.now()): number | null {
  if (!task.lastRunAt) return null;
  const intervalMs = heartbeatIntervalMs(task.interval);
  if (intervalMs === null) return null;
  const rawNext = task.lastRunAt + intervalMs;
  if ((task.interval || "").includes("|")) return rawNext;
  const start = heartbeatClockMinutes(task.timeWindowStart);
  const end = heartbeatClockMinutes(task.timeWindowEnd);
  if (start === null && end === null) return rawNext;
  const candidate = new Date(Math.max(rawNext, now));
  return nextHeartbeatWindowTime(candidate, start, end).getTime();
}

function heartbeatIntervalLabel(interval: string | undefined, t: ReturnType<typeof useT>): string {
  const cycleMatch = (interval || "").match(/^(\d+)[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(\d{2}:\d{2}))?$/);
  if (cycleMatch) {
    const [, , type, days, time] = cycleMatch;
    const timeStr = time ? ` ${time}` : "";
    if (type === "daily") return `${t("heartbeat.cycleDaily")}${timeStr}`;
    if (type === "weekly") return `${t("heartbeat.cycleWeekly")}${timeStr}`;
    if (type === "biweekly") return `${t("heartbeat.cycleBiweekly")}${timeStr}`;
    if (type === "monthly") return `${t("heartbeat.cycleMonthly")}${days ? ` ${days}` : ""}${timeStr}`;
    if (type === "yearly") {
      const parts = (days || "").split("-");
      return `${t("heartbeat.cycleYearly")} ${parts[0] || "1"}/${parts[1] || "1"}${timeStr}`;
    }
  }
  const clean = (interval || "").replace(/\|.*$/, "");
  const m = clean.match(/^(\d+)([smh])$/);
  if (!m) return clean;
  const unitLabels: Record<string, string> = {
    s: t("heartbeat.unitSec"),
    m: t("heartbeat.unitMin"),
    h: t("heartbeat.unitHour"),
  };
  return `${t("heartbeat.freqEvery")}${t("heartbeat.everyJoiner")}${m[1]}${unitLabels[m[2]] || m[2]}`;
}

interface HeartbeatPanelProps {
  open: boolean;
  onClose: () => void;
  startNew?: boolean;
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string) => void;
}

export function HeartbeatPanel({ open, onClose, startNew, onOpenTopic }: HeartbeatPanelProps) {
  const t = useT();
  const [tasks, setTasks] = useState<HeartbeatTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState<HeartbeatTask | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "enabled" | "disabled">("all");
  const [scopeFilter, setScopeFilter] = useState<string>("all");
  const [scopeFilterOpen, setScopeFilterOpen] = useState(false);
  const scopeFilterRef = useRef<HTMLButtonElement>(null);
  const [statusFilterOpen, setStatusFilterOpen] = useState(false);
  const statusFilterRef = useRef<HTMLButtonElement>(null);
  const [workspaceMap, setWorkspaceMap] = useState<Record<string, string>>({});
  const backdropRef = useRef<HTMLDivElement>(null);
  const startedRef = useRef(false);

  const loadTasks = useCallback(async () => {
    setLoading(true);
    try {
      const [taskList, wsList] = await Promise.all([
        heartbeatListTasks(),
        app.ListWorkspaces(),
      ]);
      setTasks(taskList);
      const map: Record<string, string> = {};
      if (wsList) {
        wsList.forEach((ws) => { if (ws.path) map[ws.path] = ws.name; });
      }
      setWorkspaceMap(map);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) {
      setEditing(null);
      setSearchQuery("");
      setStatusFilter("all");
      setScopeFilter("all");
      startedRef.current = false;
      void loadTasks();
    }
  }, [open, loadTasks]);

  // Open directly in add mode when startNew is true
  useEffect(() => {
    if (open && startNew && !startedRef.current) {
      startedRef.current = true;
      void heartbeatGenerateID().then((id) => {
        setEditing({
          id,
          title: "",
          prompt: "",
          interval: "30m",
          enabled: true,
          approvalMode: "yolo",
          newConversationEachRun: false,
          notifyChannels: false,
          createdAt: Date.now(),
        });
      });
    }
  }, [open, startNew]);

  const save = useCallback(
    async (next: HeartbeatTask[]) => {
      setTasks(next);
      try {
        await heartbeatSaveTasks(next);
      } catch {
        // ignore
      }
    },
    [],
  );

  const handleAdd = useCallback(async () => {
    const id = await heartbeatGenerateID();
    setEditing({
      id,
      title: "",
      prompt: "",
      interval: "30m",
      enabled: true,
      approvalMode: "yolo",
      newConversationEachRun: false,
      notifyChannels: false,
      createdAt: Date.now(),
    });
  }, []);

  const handleEdit = useCallback((task: HeartbeatTask) => {
    setEditing({ ...task });
  }, []);

  const handleDelete = useCallback(
    async (id: string) => {
      const next = tasks.filter((t) => t.id !== id);
      await save(next);
    },
    [tasks, save],
  );

  const handleTrigger = useCallback(
    async (id: string) => {
      await heartbeatTriggerNow(id);
      void loadTasks();
    },
    [loadTasks],
  );

  const handleSaveEdit = useCallback(
    async (task: HeartbeatTask) => {
      const idx = tasks.findIndex((t) => t.id === task.id);
      const next = [...tasks];
      if (idx >= 0) {
        next[idx] = task;
      } else {
        next.push(task);
      }
      await save(next);
      setEditing(null);
    },
    [tasks, save],
  );

  const handleBackdrop = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === backdropRef.current) onClose();
    },
    [onClose],
  );

  useEffect(() => {
    if (!open) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  const scopeFilterLabel = (filter: string, map: Record<string, string>): string => {
    if (filter === "all") return "全部项目";
    if (filter === "global") return "全局";
    return map[filter] || filter.split("/").pop() || filter;
  };

  const statusFilterLabel = (filter: string): string => {
    if (filter === "all") return t("heartbeat.filterAll" as any);
    if (filter === "enabled") return t("heartbeat.filterEnabled" as any);
    return t("heartbeat.filterDisabled" as any);
  };

  return (
    <div ref={backdropRef} className="heartbeat-backdrop" onClick={handleBackdrop}>
      <div className="heartbeat-modal">
        <header className="heartbeat-modal__header">
          {editing ? (
            <button className="heartbeat-modal__back" onClick={() => setEditing(null)}>
              <ChevronLeft size={16} />
            </button>
          ) : (
            <Activity size={16} />
          )}
          <span>{editing ? t("heartbeat.editTask") : "自动化任务"}</span>
          <button
            className="heartbeat-modal__close"
            onClick={onClose}
            aria-label={t("common.close")}
          >
            <X size={16} />
          </button>
        </header>

        {editing ? (
          <TaskEditor key={editing.id} task={editing} onSave={handleSaveEdit} onCancel={() => setEditing(null)} onDelete={() => { handleDelete(editing.id); setEditing(null); }} />
        ) : (
          <div className="heartbeat-modal__body">
            <div className="heartbeat-toolbar">
              <div className="heartbeat-toolbar__search">
                <Search size={13} className="heartbeat-toolbar__search-icon" />
                <input
                  className="heartbeat-toolbar__search-input"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder={t("heartbeat.searchPlaceholder" as any)}
                />
                {searchQuery && (
                  <button className="heartbeat-toolbar__search-clear" onClick={() => setSearchQuery("")}>
                    <X size={12} />
                  </button>
                )}
              </div>
              <div className="heartbeat-scope-filter">
                <button
                  ref={statusFilterRef}
                  className="heartbeat-toolbar__btn heartbeat-toolbar__btn--select"
                  type="button"
                  onClick={() => setStatusFilterOpen((v) => !v)}
                >
                  <span>{statusFilterLabel(statusFilter)}</span>
                  <ChevronsUpDown size={12} />
                </button>
                <AnchoredPopover
                  open={statusFilterOpen}
                  anchorRef={statusFilterRef}
                  onClose={() => setStatusFilterOpen(false)}
                  className="heartbeat-filter-menu"
                  placement="bottom"
                >
                  <div className="heartbeat-filter-menu__list" role="listbox">
                    {(["all", "enabled", "disabled"] as const).map((key) => (
                      <button
                        key={key}
                        className={`heartbeat-filter-menu__option${statusFilter === key ? " heartbeat-filter-menu__option--selected" : ""}`}
                        role="option"
                        aria-selected={statusFilter === key}
                        type="button"
                        onClick={() => { setStatusFilter(key); setStatusFilterOpen(false); }}
                      >
                        <span>{key === "all" ? t("heartbeat.filterAll" as any) : key === "enabled" ? t("heartbeat.filterEnabled" as any) : t("heartbeat.filterDisabled" as any)}</span>
                        {statusFilter === key && <Check size={12} className="heartbeat-filter-menu__check" />}
                      </button>
                    ))}
                  </div>
                </AnchoredPopover>
              </div>
              <div className="heartbeat-scope-filter">
                <button
                  ref={scopeFilterRef}
                  className="heartbeat-toolbar__btn heartbeat-toolbar__btn--select"
                  type="button"
                  onClick={() => setScopeFilterOpen((v) => !v)}
                >
                  <span>{scopeFilterLabel(scopeFilter, workspaceMap)}</span>
                  <ChevronsUpDown size={12} />
                </button>
                <AnchoredPopover
                  open={scopeFilterOpen}
                  anchorRef={scopeFilterRef}
                  onClose={() => setScopeFilterOpen(false)}
                  className="heartbeat-filter-menu"
                  placement="bottom"
                >
                  <div className="heartbeat-filter-menu__list" role="listbox">
                    <button
                      className={`heartbeat-filter-menu__option${scopeFilter === "all" ? " heartbeat-filter-menu__option--selected" : ""}`}
                      role="option"
                      aria-selected={scopeFilter === "all"}
                      type="button"
                      onClick={() => { setScopeFilter("all"); setScopeFilterOpen(false); }}
                    >
                      <span>全部项目</span>
                      {scopeFilter === "all" && <Check size={12} className="heartbeat-filter-menu__check" />}
                    </button>
                    <button
                      className={`heartbeat-filter-menu__option${scopeFilter === "global" ? " heartbeat-filter-menu__option--selected" : ""}`}
                      role="option"
                      aria-selected={scopeFilter === "global"}
                      type="button"
                      onClick={() => { setScopeFilter("global"); setScopeFilterOpen(false); }}
                    >
                      <span>全局</span>
                      {scopeFilter === "global" && <Check size={12} className="heartbeat-filter-menu__check" />}
                    </button>
                    {(() => {
                      const seen = new Set<string>();
                      const items: { value: string; label: string }[] = [];
                      for (const task of tasks) {
                        const key = task.scope !== "project" || !task.workspaceRoot ? "global" : task.workspaceRoot;
                        if (seen.has(key)) continue;
                        seen.add(key);
                        if (key !== "global") {
                          items.push({
                            value: key,
                            label: workspaceMap[key] || key.split("/").pop() || key,
                          });
                        }
                      }
                      return items.map((item) => (
                        <button
                          key={item.value}
                          className={`heartbeat-filter-menu__option${scopeFilter === item.value ? " heartbeat-filter-menu__option--selected" : ""}`}
                          role="option"
                          aria-selected={scopeFilter === item.value}
                          type="button"
                          onClick={() => { setScopeFilter(item.value); setScopeFilterOpen(false); }}
                        >
                          <span>{item.label}</span>
                          {scopeFilter === item.value && <Check size={12} className="heartbeat-filter-menu__check" />}
                        </button>
                      ));
                    })()}
                  </div>
                </AnchoredPopover>
              </div>
              <button className="heartbeat-toolbar__btn heartbeat-toolbar__btn--primary" style={{ marginLeft: "auto" }} onClick={handleAdd}>
                <Plus size={14} />
                {t("heartbeat.addTask")}
              </button>
            </div>

            {(() => {
              const filtered = tasks
                .filter((task) => {
                  if (statusFilter === "enabled" && !task.enabled) return false;
                  if (statusFilter === "disabled" && task.enabled) return false;
                  if (searchQuery && !task.title.toLowerCase().includes(searchQuery.toLowerCase())) return false;
                  if (scopeFilter === "global" && (task.scope === "project" && task.workspaceRoot)) return false;
                  if (scopeFilter !== "all" && scopeFilter !== "global") {
                    if (task.scope !== "project" || task.workspaceRoot !== scopeFilter) return false;
                  }
                  return true;
                })
                .sort((a, b) => {
                  if (a.enabled && !b.enabled) return -1;
                  if (!a.enabled && b.enabled) return 1;
                  return 0;
                });

              const scopeLabel = (task: HeartbeatTask): string => {
                if (task.scope !== "project" || !task.workspaceRoot) return t("heartbeat.scopeGlobal");
                return workspaceMap[task.workspaceRoot] || task.workspaceRoot.split("/").pop() || task.workspaceRoot;
              };

              return loading ? (
                <div className="heartbeat-empty">
                  <Heart size={24} className="heartbeat-pulse" />
                  <span>{t("workspace.loading")}</span>
                </div>
              ) : filtered.length === 0 ? (
                <div className="heartbeat-empty">
                  <Heart size={24} />
                  <span>{tasks.length === 0 ? t("heartbeat.noTasks") : "没有匹配的任务"}</span>
                </div>
              ) : (
                <ul className="heartbeat-tasklist">
                  {filtered.map((task) => (
                    <TaskCard
                      key={task.id}
                      task={task}
                      scopeLabel={scopeLabel(task)}
                      onToggle={() => {
                        const next = tasks.map((t) =>
                          t.id === task.id ? { ...t, enabled: !t.enabled } : t,
                        );
                        save(next);
                      }}
                      onEdit={() => handleEdit(task)}
                      onTrigger={() => void handleTrigger(task.id)}
                      onOpenTopic={onOpenTopic}
                      onClose={onClose}
                    />
                  ))}
                </ul>
              );
            })()}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Task Card ─────────────────────────────────────────────────────────────────

function TaskCard({
  task,
  scopeLabel,
  onToggle,
  onEdit,
  onTrigger,
  onOpenTopic,
  onClose,
}: {
  task: HeartbeatTask;
  scopeLabel: string;
  onToggle: () => void;
  onEdit: () => void;
  onTrigger: () => void;
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string) => void;
  onClose: () => void;
}) {
  const t = useT();

  const intervalLabel = heartbeatIntervalLabel(task.interval, t);

  const nextRunLabel = (() => {
    if (!task.enabled) return t("heartbeat.disabled");
    const now = Date.now();
    const next = heartbeatNextRunAt(task, now);
    if (next === null) return task.lastRunAt ? "" : t("heartbeat.neverRun");
    const diff = next - now;
    if (diff <= 0) return t("heartbeat.due" as any);
    if (diff < 60000) return t("heartbeat.soon" as any);
    if (diff < 3600000) return `${Math.floor(diff / 60000)}${t("heartbeat.minLater" as any)}`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}${t("heartbeat.hourLater" as any)}`;
    const d = new Date(next);
    return `${d.getMonth() + 1}/${d.getDate()} ${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
  })();

  const lastRunLabel = task.lastRunAt
    ? (() => {
        const d = new Date(task.lastRunAt);
        const now = new Date();
        const diff = now.getTime() - task.lastRunAt;
        if (diff < 60000) return t("heartbeat.justNow" as any);
        if (diff < 3600000) return `${Math.floor(diff / 60000)}${t("heartbeat.minAgo" as any)}`;
        if (diff < 86400000) return `${Math.floor(diff / 3600000)}${t("heartbeat.hourAgo" as any)}`;
        return `${d.getMonth() + 1}/${d.getDate()} ${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
      })()
    : t("heartbeat.neverRun");

  return (
    <li className={`heartbeat-card${!task.enabled ? " heartbeat-card--disabled" : ""}`}>
      <div className="heartbeat-card__head">
        <span className={`heartbeat-card__dot${task.enabled ? " heartbeat-card__dot--on" : ""}`} />
        <span className="heartbeat-card__title">
          <button
            type="button"
            className="heartbeat-card__title-btn"
            onClick={onEdit}
          >
            <span className="heartbeat-card__title-text">{task.title || t("heartbeat.untitled")}</span>
            <span className="heartbeat-card__title-scope">{scopeLabel}</span>
          </button>
        </span>
        <span className="heartbeat-card__meta-item heartbeat-card__meta-item--compact">
          <Clock size={10} />
          {intervalLabel}
          <span className="heartbeat-card__meta-sep">·</span>
          {task.enabled ? nextRunLabel : lastRunLabel}
        </span>
        <span className="heartbeat-card__head-actions">
          <button
            className="heartbeat-card__open-btn heartbeat-card__open-btn--play"
            onClick={onTrigger}
            title={t("heartbeat.runNow")}
          >
            <Play size={12} />
          </button>
          <button
            className="heartbeat-card__open-btn"
            type="button"
            disabled={!task.topicId}
            onClick={() => {
              if (task.topicId) {
                onClose();
                onOpenTopic(task.scope || "global", task.workspaceRoot || "", task.topicId);
              }
            }}
            title={task.topicId ? (t("heartbeat.openTopic" as any)) : ""}
          >
            <MessageSquare size={13} />
          </button>
          <button
            className={`heartbeat-card__toggle${task.enabled ? " heartbeat-card__toggle--on" : ""}`}
            onClick={onToggle}
            aria-label={task.enabled ? t("heartbeat.disable") : t("heartbeat.enabled")}
          >
            <span className="heartbeat-card__toggle-knob" />
          </button>
        </span>
      </div>
    </li>
  );
}

// ── Cycle Editor ──────────────────────────────────────────────────────────────

const WEEKDAYS = [
  { key: "mon", label: "周一" },
  { key: "tue", label: "周二" },
  { key: "wed", label: "周三" },
  { key: "thu", label: "周四" },
  { key: "fri", label: "周五" },
  { key: "sat", label: "周六" },
  { key: "sun", label: "周日" },
] as const;

const ALL_WEEKDAYS = WEEKDAYS.map(w => w.key);
const DEFAULT_WEEKLY_DAY = "mon";

function defaultHeartbeatCycleDays(cycleType: string): string[] {
  if (cycleType === "daily") return [...ALL_WEEKDAYS];
  if (cycleType === "weekly" || cycleType === "biweekly") return [DEFAULT_WEEKLY_DAY];
  return [];
}

export function heartbeatBuildCycleInterval(cycleType: string, days: string[], time: string): string {
  const base: Record<string, string> = {
    daily: "24h",
    weekly: "168h",
    biweekly: "336h",
    monthly: "720h",
    yearly: "8760h",
  };
  const selectedDays = days.filter(Boolean);
  const isDailyWithSelection = cycleType === "daily" && selectedDays.length > 0 && selectedDays.length < 7;
  const isDailyWithoutSelection = cycleType === "daily" && selectedDays.length === 0;
  const effectiveType = isDailyWithoutSelection || isDailyWithSelection ? "weekly" : cycleType;
  const scheduleDays =
    (effectiveType === "weekly" || effectiveType === "biweekly") && selectedDays.length === 0
      ? defaultHeartbeatCycleDays(effectiveType)
      : selectedDays;

  let suffix = `|${effectiveType}`;
  if (effectiveType === "weekly" || effectiveType === "biweekly") {
    suffix += `:${scheduleDays.join(",")}`;
  } else if (effectiveType === "monthly") {
    suffix += `:${scheduleDays[0] || "1"}`;
  } else if (effectiveType === "yearly") {
    suffix += `:${scheduleDays[0] || "1"}-${scheduleDays[1] || "1"}`;
  }
  suffix += `@${time}`;
  return (base[cycleType] || "24h") + suffix;
}

function CycleEditor({
  draft,
  setDraft,
}: {
  draft: HeartbeatTask;
  setDraft: (field: keyof HeartbeatTask, value: string | boolean) => void;
}) {
  const t = useT();
  const cycleMatch = (draft.interval || "").match(/^(\d+)[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(\d{2}:\d{2}))?$/);
  const [cycleType, setCycleType] = useState<string>(
    cycleMatch ? cycleMatch[2] : "daily"
  );
  const cycleDays = cycleMatch?.[3] || "";
  const cycleTime = cycleMatch?.[4] || "09:00";
  const [selectedDays, setSelectedDays] = useState<string[]>(
    cycleDays ? cycleDays.split(",").filter(Boolean) :
    defaultHeartbeatCycleDays(cycleMatch ? cycleMatch[2] : "daily")
  );
  const [monthDay, setMonthDay] = useState(cycleDays || "1");
  const [yearMonth, setYearMonth] = useState(cycleDays.split("-")[0] || "1");
  const [yearDay, setYearDay] = useState(cycleDays.split("-")[1] || "1");
  const [timeVal, setTimeVal] = useState(cycleTime);

  const hasWeekdays = cycleType === "daily" || cycleType === "weekly" || cycleType === "biweekly";

  // Build interval string when config changes
  const buildInterval = useCallback(heartbeatBuildCycleInterval, []);

  const onCycleTypeChange = useCallback((ct: string) => {
    setCycleType(ct);
    const days = defaultHeartbeatCycleDays(ct);
    setSelectedDays(days);
    setMonthDay("1");
    setYearMonth("1");
    setYearDay("1");
    setDraft("interval", buildInterval(ct, days, timeVal));
  }, [buildInterval, setDraft, timeVal]);

  const onDayToggle = useCallback((day: string) => {
    setSelectedDays((prev) => {
      if (prev.includes(day) && prev.length <= 1) return prev;
      const next = prev.includes(day) ? prev.filter((d) => d !== day) : [...prev, day];
      setDraft("interval", buildInterval(cycleType, next, timeVal));
      return next;
    });
  }, [buildInterval, cycleType, setDraft, timeVal]);

  const onMonthDayChange = useCallback((d: string) => {
    setMonthDay(d);
    setDraft("interval", buildInterval(cycleType, [d], timeVal));
  }, [buildInterval, cycleType, setDraft, timeVal]);

  const onYearMonthChange = useCallback((m: string) => {
    setYearMonth(m);
    setDraft("interval", buildInterval(cycleType, [m, yearDay], timeVal));
  }, [buildInterval, cycleType, setDraft, timeVal, yearDay]);

  const onYearDayChange = useCallback((d: string) => {
    setYearDay(d);
    setDraft("interval", buildInterval(cycleType, [yearMonth, d], timeVal));
  }, [buildInterval, cycleType, setDraft, timeVal, yearMonth]);

  const onTimeChange = useCallback((tm: string) => {
    setTimeVal(tm);
    const days = hasWeekdays ? selectedDays
      : cycleType === "monthly" ? [monthDay]
      : cycleType === "yearly" ? [yearMonth, yearDay]
      : [];
    setDraft("interval", buildInterval(cycleType, days, tm));
  }, [buildInterval, cycleType, selectedDays, monthDay, yearMonth, yearDay, setDraft]);

  const MONTHS = Array.from({ length: 12 }, (_, i) => ({
    value: String(i + 1),
    label: `${i + 1}月`,
  }));
  const DAYS = Array.from({ length: 31 }, (_, i) => ({
    value: String(i + 1),
    label: `${i + 1}日`,
  }));

  return (
    <div className="heartbeat-editor__cycle-wrap">
      <div className="heartbeat-editor__cycle-row">
        <select
          className="heartbeat-editor__freq-select"
          value={cycleType}
          onChange={(e) => onCycleTypeChange(e.target.value)}
        >
          <option value="daily">{t("heartbeat.cycleDaily")}</option>
          <option value="weekly">{t("heartbeat.cycleWeekly")}</option>
          <option value="biweekly">{t("heartbeat.cycleBiweekly")}</option>
          <option value="monthly">{t("heartbeat.cycleMonthly")}</option>
          <option value="yearly">{t("heartbeat.cycleYearly")}</option>
        </select>

        {cycleType === "monthly" && (
          <select
            className="heartbeat-editor__freq-select"
            value={monthDay}
            onChange={(e) => onMonthDayChange(e.target.value)}
          >
            {DAYS.map((d) => (
              <option key={d.value} value={d.value}>{d.label}</option>
            ))}
          </select>
        )}

        {cycleType === "yearly" && (
          <>
            <select
              className="heartbeat-editor__freq-select"
              value={yearMonth}
              onChange={(e) => onYearMonthChange(e.target.value)}
            >
              {MONTHS.map((m) => (
                <option key={m.value} value={m.value}>{m.label}</option>
              ))}
            </select>
            <select
              className="heartbeat-editor__freq-select"
              value={yearDay}
              onChange={(e) => onYearDayChange(e.target.value)}
            >
              {DAYS.map((d) => (
                <option key={d.value} value={d.value}>{d.label}</option>
              ))}
            </select>
          </>
        )}

        <input
          className="heartbeat-editor__freq-input heartbeat-editor__freq-input--time"
          type="time"
          value={timeVal}
          onChange={(e) => onTimeChange(e.target.value)}
        />

        {hasWeekdays && (
          <div className="set-seg">
            {WEEKDAYS.map((wd) => (
              <button
                key={wd.key}
                type="button"
                className={`set-seg__btn${selectedDays.includes(wd.key) ? " set-seg__btn--on" : ""}`}
                onClick={() => onDayToggle(wd.key)}
                aria-pressed={selectedDays.includes(wd.key)}
              >
                {wd.label}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Editor ─────────────────────────────────────────────────────────────────────

function normalizeMode(mode: "ask" | "auto" | "yolo" | undefined): "ask" | "auto" | "yolo" {
  if (mode === "ask" || mode === "auto" || mode === "yolo") return mode;
  return "yolo"; // default
}

function TaskEditor({
  task,
  onSave,
  onCancel,
  onDelete,
}: {
  task: HeartbeatTask;
  onSave: (t: HeartbeatTask) => void;
  onCancel: () => void;
  onDelete: () => void;
}) {
  const t = useT();
  const titleRef = useRef<HTMLInputElement>(null);
  const [workspaces, setWorkspaces] = useState<WorkspaceView[]>([]);
  const [projectOpen, setProjectOpen] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const projectRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    titleRef.current?.focus();
    app.ListWorkspaces().then((list) => setWorkspaces(list ?? [])).catch(() => {});
  }, []);

  useEffect(() => {
    if (!projectOpen) return;
    const close = (e: MouseEvent) => {
      if (projectRef.current && !projectRef.current.contains(e.target as Node)) {
        setProjectOpen(false);
      }
    };
    document.addEventListener("click", close);
    return () => document.removeEventListener("click", close);
  }, [projectOpen]);

  const [draft, setDraft] = useState(task);
  const intervalBeforeCycle = useRef<string | null>(null);
  const promptRef = useRef<HTMLTextAreaElement>(null);

  // Auto-grow prompt textarea: shrink-to-fit then cap at 180px
  const autoGrowPrompt = useCallback(() => {
    const el = promptRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 180) + "px";
  }, []);

  useLayoutEffect(() => {
    autoGrowPrompt();
  }, [draft.prompt, autoGrowPrompt]);
  const set = useCallback((field: keyof HeartbeatTask, value: string | boolean) => {
    setDraft((prev) => ({ ...prev, [field]: value }));
  }, []);

  // Detect frequency type from interval value
  const [freqType, setFreqType] = useState<"cycle" | "interval">(
    (task.interval && task.interval.includes("|")) ? "cycle" : "interval"
  );

  const isNew = !task.createdAt;
  const selectedWorkspace = draft.scope === "project" && draft.workspaceRoot
    ? workspaces.find((w) => w.path === draft.workspaceRoot)
    : null;

  return (
    <div className="heartbeat-editor">
      <div className="heartbeat-editor__fields">
        {/* Title */}
        <div className="heartbeat-editor__field">
        <label>{t("heartbeat.fieldTitle")}</label>
        <input
          ref={titleRef}
          className="heartbeat-editor__input"
          value={draft.title}
          onChange={(e) => set("title", e.target.value)}
          placeholder={t("heartbeat.titlePlaceholder")}
        />
      </div>

      {/* Scope */}
      <div className="heartbeat-editor__field">
        <label>{t("heartbeat.fieldScope")} <span className="heartbeat-editor__optional">{t("heartbeat.optional")}</span></label>
        <div className="heartbeat-editor__scope-row">
          <button
            className={`heartbeat-scope-btn${draft.scope !== "project" ? " heartbeat-scope-btn--active" : ""}`}
            onClick={() => setDraft((prev) => ({ ...prev, scope: "global", workspaceRoot: "" }))}
          >
            {t("heartbeat.scopeGlobal")}
          </button>
          <div className="heartbeat-project-wrap" ref={projectRef}>
            <button
              className={`heartbeat-scope-btn${draft.scope === "project" ? " heartbeat-scope-btn--active" : ""}`}
              onClick={() => setProjectOpen((v) => !v)}
            >
              {selectedWorkspace ? selectedWorkspace.name : t("heartbeat.scopeProject")}
              <ChevronsUpDown size={12} />
            </button>
            {projectOpen && (
              <div className="heartbeat-project-menu">
                {workspaces.length === 0 ? (
                  <div className="heartbeat-project-menu__empty">{t("heartbeat.noProjects")}</div>
                ) : (
                  workspaces.map((ws) => (
                    <button
                      key={ws.path}
                      className={`heartbeat-project-menu__item${draft.workspaceRoot === ws.path ? " heartbeat-project-menu__item--active" : ""}`}
                      onClick={() => {
                        setDraft((prev) => ({ ...prev, scope: "project", workspaceRoot: ws.path }));
                        setProjectOpen(false);
                      }}
                    >
                      {ws.name}
                      {ws.current && <span className="heartbeat-project-menu__current">{t("heartbeat.currentWorkspace")}</span>}
                    </button>
                  ))
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Prompt */}
      <div className="heartbeat-editor__field">
        <label>{t("heartbeat.fieldPrompt")}</label>
        <textarea
          ref={promptRef}
          className="heartbeat-editor__textarea"
          value={draft.prompt}
          onChange={(e) => {
            set("prompt", e.target.value);
            // autoGrowPrompt is called via useEffect watching draft.prompt
          }}
          placeholder={t("heartbeat.promptPlaceholder")}
        />
      </div>

      {/* Approval Mode + Push to bot (side by side) */}
      <div style={{ display: "flex", gap: "16px", flexWrap: "wrap" }}>
        <div className="heartbeat-editor__field" style={{ flex: "1 1 45%", minWidth: "200px" }}>
          <label>{t("heartbeat.fieldApprovalMode")}</label>
          <div className="set-seg" style={{ alignSelf: "flex-start" }}>
            <button
              className={`set-seg__btn${normalizeMode(draft.approvalMode) === "ask" ? " set-seg__btn--on" : ""}`}
              onClick={() => setDraft((prev) => ({ ...prev, approvalMode: "ask" }))}
              title={t("heartbeat.approvalModeAskTooltip")}
            >
              {t("heartbeat.approvalModeAsk")}
            </button>
            <button
              className={`set-seg__btn${normalizeMode(draft.approvalMode) === "auto" ? " set-seg__btn--on" : ""}`}
              onClick={() => setDraft((prev) => ({ ...prev, approvalMode: "auto" }))}
              title={t("heartbeat.approvalModeAutoTooltip")}
            >
              {t("heartbeat.approvalModeAuto")}
            </button>
            <button
              className={`set-seg__btn${normalizeMode(draft.approvalMode) === "yolo" ? " set-seg__btn--on" : ""}`}
              onClick={() => setDraft((prev) => ({ ...prev, approvalMode: "yolo" }))}
              title={t("heartbeat.approvalModeYoloTooltip")}
            >
              {t("heartbeat.approvalModeYolo")}
            </button>
          </div>
          <span className="heartbeat-editor__mode-hint">
            {normalizeMode(draft.approvalMode) === "yolo" ? t("heartbeat.approvalModeYoloHint") :
             normalizeMode(draft.approvalMode) === "auto" ? t("heartbeat.approvalModeAutoHint") :
             t("heartbeat.approvalModeAskHint")}
          </span>
        </div>

        {/* Push to bot channels */}
        <div className="heartbeat-editor__field" style={{ flex: "1 1 45%", minWidth: "200px", textAlign: "left" }}>
          <label>{t("heartbeat.notifyChannels")} <span className="heartbeat-editor__optional">{t("heartbeat.optional")}</span></label>
          <div className="set-seg" style={{ alignSelf: "flex-start" }}>
            <button
              className={`set-seg__btn${draft.notifyChannels === true ? " set-seg__btn--on" : ""}`}
              onClick={() => setDraft((prev) => ({ ...prev, notifyChannels: true }))}
            >
              {t("heartbeat.notifyChannelsOn")}
            </button>
            <button
              className={`set-seg__btn${draft.notifyChannels !== true ? " set-seg__btn--on" : ""}`}
              onClick={() => setDraft((prev) => ({ ...prev, notifyChannels: false }))}
            >
              {t("heartbeat.notifyChannelsOff")}
            </button>
          </div>
          <span className="heartbeat-editor__mode-hint">
            {draft.notifyChannels === true
              ? t("heartbeat.notifyChannelsOnHint")
              : t("heartbeat.notifyChannelsOffHint")}
          </span>
        </div>
      </div>

      {/* New conversation per run */}
      <div className="heartbeat-editor__field">
        <label>{t("heartbeat.fieldNewConversation")}</label>
        <div className="set-seg" style={{ alignSelf: "flex-start" }}>
          <button
            className={`set-seg__btn${!draft.newConversationEachRun ? " set-seg__btn--on" : ""}`}
            onClick={() => setDraft((prev) => ({ ...prev, newConversationEachRun: false }))}
          >
            {t("heartbeat.newConversationEachRunOff")}
          </button>
          <button
            className={`set-seg__btn${draft.newConversationEachRun ? " set-seg__btn--on" : ""}`}
            onClick={() => setDraft((prev) => ({ ...prev, newConversationEachRun: true }))}
          >
            {t("heartbeat.newConversationEachRunOn")}
          </button>
        </div>
      </div>

      {/* Frequency */}
      <div className="heartbeat-editor__field">
        <label>{t("heartbeat.fieldInterval")}</label>
        <div className="set-seg" style={{ alignSelf: "flex-start" }}>
          <button
            className={`set-seg__btn${freqType === "cycle" ? " set-seg__btn--on" : ""}`}
            onClick={() => {
              setFreqType("cycle");
              // Save the original interval so switching back can restore it
              const cur = draft.interval || "";
              const nextInterval = cur.includes("|") ? cur : "24h|daily@09:00";
              if (!cur.includes("|")) {
                intervalBeforeCycle.current = cur;
              }
              setDraft((prev) => ({ ...prev, interval: nextInterval, timeWindowStart: undefined, timeWindowEnd: undefined }));
            }}
          >
            {t("heartbeat.freqCycle")}
          </button>
          <button
            className={`set-seg__btn${freqType === "interval" ? " set-seg__btn--on" : ""}`}
            onClick={() => {
              setFreqType("interval");
              // Restore original interval if user toggled cycle and back without saving
              if (intervalBeforeCycle.current !== null) {
                setDraft((prev) => ({ ...prev, interval: intervalBeforeCycle.current! }));
                intervalBeforeCycle.current = null;
              } else if ((draft.interval || "").includes("|")) {
                // Fallback: strip cycle suffix
                setDraft((prev) => ({ ...prev, interval: (prev.interval || "").replace(/\|.*$/, "") }));
              }
            }}
          >
            {t("heartbeat.freqInterval")}
          </button>
        </div>

        {freqType === "cycle" ? <CycleEditor draft={draft} setDraft={set} /> : (
          <div className="heartbeat-editor__freq-interval">
            <span className="heartbeat-editor__freq-label">{t("heartbeat.freqEvery")}</span>
            <input
              className="heartbeat-editor__freq-input"
              value={(() => {
                const m = (draft.interval || "").match(/^(\d+)/);
                return m ? m[1] : "1";
              })()}
              onChange={(e) => {
                const num = e.target.value.replace(/\D/g, "");
                const mUnit = (draft.interval || "").match(/^(\d+)([smh])/);
                const unit = mUnit ? mUnit[2] : "h";
                // Guard: never save a bare unit string like "h" or "m"
                setDraft((prev) => ({ ...prev, interval: num ? num + unit : "1" + unit }));
              }}
              placeholder="1"
            />
            <select
              className="heartbeat-editor__freq-select"
              value={(() => {
                const m = (draft.interval || "").match(/^(\d+)([smh])/);
                return m ? m[2] : "h";
              })()}
              onChange={(e) => {
                const num = (draft.interval || "").match(/^(\d+)/)?.[1] || "1";
                setDraft((prev) => ({ ...prev, interval: num + e.target.value }));
              }}
            >
              <option value="m">{t("heartbeat.unitMin")}</option>
              <option value="h">{t("heartbeat.unitHour")}</option>
            </select>
            <span className="heartbeat-editor__freq-label" style={{ marginLeft: "6px" }}>
              {draft.timeWindowStart || draft.timeWindowEnd ? (
                <>{t("heartbeat.timeWindow")}</>
              ) : (
                <span className="heartbeat-editor__tw-add"
                  onClick={() => setDraft((prev) => ({ ...prev, timeWindowStart: "09:00", timeWindowEnd: "17:00" }))}
                >
                  + {t("heartbeat.timeWindow")}
                </span>
              )}
            </span>
            {(draft.timeWindowStart || draft.timeWindowEnd) && (
              <>
                <input
                  className="heartbeat-editor__freq-input heartbeat-editor__freq-input--time"
                  type="time"
                  value={draft.timeWindowStart || ""}
                  onChange={(e) => setDraft((prev) => ({ ...prev, timeWindowStart: e.target.value || undefined }))}
                  placeholder="09:00"
                />
                <span className="heartbeat-editor__freq-label heartbeat-editor__tw-sep">—</span>
                <input
                  className="heartbeat-editor__freq-input heartbeat-editor__freq-input--time"
                  type="time"
                  value={draft.timeWindowEnd || ""}
                  onChange={(e) => setDraft((prev) => ({ ...prev, timeWindowEnd: e.target.value || undefined }))}
                  placeholder="17:00"
                />
                <button
                  className="heartbeat-card__open-btn heartbeat-editor__tw-clear"
                  onClick={() => setDraft((prev) => ({ ...prev, timeWindowStart: undefined, timeWindowEnd: undefined }))}
                  title={t("heartbeat.clearTimeWindow")}
                >
                  ×
                </button>
              </>
            )}
          </div>
        )}
      </div>

      </div>

      {/* Actions */}
      <div className="heartbeat-editor__actions">
        {!isNew && !confirmingDelete && (
          <button className="heartbeat-btn heartbeat-btn--danger" onClick={() => setConfirmingDelete(true)} style={{ marginRight: "auto" }}>
            <Trash2 size={13} />
            {t("heartbeat.delete")}
          </button>
        )}
        {!isNew && confirmingDelete && (
          <span className="heartbeat-editor__confirm-del" style={{ marginRight: "auto" }}>
            <span>{t("heartbeat.confirmDelete")}</span>
            <button className="heartbeat-btn heartbeat-btn--danger" onClick={onDelete}>
              {t("common.delete")}
            </button>
            <button className="heartbeat-btn" onClick={() => setConfirmingDelete(false)}>
              {t("common.cancel")}
            </button>
          </span>
        )}
        <button
          className="heartbeat-btn heartbeat-btn--primary"
          onClick={() => onSave(draft)}
          disabled={!draft.title.trim() || !draft.prompt.trim()}
        >
          {isNew ? t("heartbeat.add") : t("heartbeat.save")}
        </button>
        <button className="heartbeat-btn" onClick={onCancel}>
          {t("common.cancel")}
        </button>
      </div>
    </div>
  );
}
