// Heartbeat Panel — Modal for configuring scheduled heartbeat tasks.
//
// Renders a list of tasks with add/edit/delete controls, plus a manual
// "run now" button for each. The panel is opened from the sidebar nav item.

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  Activity,
  ChevronDown,
  ChevronRight,
  ChevronsUpDown,
  Check,
  Filter,
  Folder,
  Globe,
  Heart,
  MessageSquare,
  Play,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { app } from "../../../lib/bridge";
import { useT, type Translator } from "../../../lib/i18n";
import { AnchoredPopover } from "../../../components/AnchoredPopover";
import {
  heartbeatListTasks,
  heartbeatSaveTasks,
  heartbeatTriggerNow,
  heartbeatGenerateID,
} from "./heartbeat.bridge";
import type { HeartbeatTask } from "./heartbeat.types";
import type { WorkspaceView } from "../../../lib/types";

interface HeartbeatPanelProps {
  open: boolean;
  onClose: () => void;
  startNew?: boolean;
  onOpenTopic?: (scope: string, workspaceRoot: string, topicId: string) => void;
}

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

function formatInterval(interval: string, t: Translator): string {
  const cycleMatch = interval.match(/^(\d+)[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(\d{2}:\d{2}))?$/);
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
  const simple = interval.match(/^(\d+)([smh])$/);
  if (simple) {
    const unitLabels: Record<string, string> = {
      s: t("heartbeat.unitSec"),
      m: t("heartbeat.unitMin"),
      h: t("heartbeat.unitHour"),
    };
    return `${simple[1]}${unitLabels[simple[2]] || simple[2]}`;
  }
  return interval;
}

function taskNextRun(task: HeartbeatTask): string | null {
  if (!task.enabled || !task.lastRunAt) return null;
  const cleaned = task.interval.replace(/\|.*$/, "");
  const m = cleaned.match(/^(\d+)([smh])$/);
  if (!m) return null;
  const ms = parseInt(m[1]) * { s: 1000, m: 60000, h: 3600000 }[m[2] as "s" | "m" | "h"];
  const next = task.lastRunAt + ms;
  if (next <= Date.now()) return null;
  const diff = next - Date.now();
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h`;
  const d = new Date(next);
  return `${(d.getMonth() + 1).toString().padStart(2, "0")}/${d.getDate().toString().padStart(2, "0")} ${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
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
  const [expandedProjects, setExpandedProjects] = useState<Set<string> | null>(null);
  const [workspaceMap, setWorkspaceMap] = useState<Record<string, string>>({});
  const backdropRef = useRef<HTMLDivElement>(null);
  const dirtyRef = useRef(false);
  const startedRef = useRef(false);
  // IDs of drafts created via Add/scoped-Add/startNew that have not been saved yet.
  // They are intentionally absent from `tasks`, so the "clear editing" effect
  // below must not close the editor for them.
  const unsavedDraftIdsRef = useRef<Set<string>>(new Set());

  // Reset dirty ref when leaving edit mode
  useEffect(() => {
    if (!editing) dirtyRef.current = false;
  }, [editing]);

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
      startedRef.current = false;
      void loadTasks();
    }
  }, [open, loadTasks]);

  // Open directly in add mode when startNew is true
  useEffect(() => {
    if (open && startNew && !startedRef.current) {
      startedRef.current = true;
      void heartbeatGenerateID().then((id) => {
        unsavedDraftIdsRef.current.add(id);
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
      }).catch(() => {});
    }
  }, [open, startNew]);

  // Clear editing when the edited task is no longer in the filtered list.
  // Unsaved drafts (created via Add/scoped-Add/startNew) are not yet in
  // `tasks`; keep their editor open until the user saves or cancels.
  useEffect(() => {
    if (!editing) return;
    if (unsavedDraftIdsRef.current.has(editing.id)) return;
    // 过滤变化导致编辑任务不可见时，若有未保存改动先确认再关闭。
    const closeIfNotDirty = () => {
      if (dirtyRef.current) {
        if (!window.confirm(t("heartbeat.discardChanges"))) return false;
        dirtyRef.current = false;
      }
      return true;
    };
    const match = tasks.find(t => t.id === editing.id);
    if (!match) { if (closeIfNotDirty()) setEditing(null); return; }
    if (statusFilter === "enabled" && !match.enabled) { if (closeIfNotDirty()) setEditing(null); return; }
    if (statusFilter === "disabled" && match.enabled) { if (closeIfNotDirty()) setEditing(null); return; }
    if (searchQuery && !match.title.toLowerCase().includes(searchQuery.toLowerCase())) { if (closeIfNotDirty()) setEditing(null); return; }
    // 与列表过滤一致：scopeFilter 过滤掉正在编辑的任务时关闭编辑器。
    if (scopeFilter === "global" && (match.scope === "project" && match.workspaceRoot)) { if (closeIfNotDirty()) setEditing(null); return; }
    if (scopeFilter !== "all" && scopeFilter !== "global" && (match.scope !== "project" || match.workspaceRoot !== scopeFilter)) { if (closeIfNotDirty()) setEditing(null); }
  }, [tasks, editing?.id, statusFilter, searchQuery, scopeFilter, t]);

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
    try {
      const id = await heartbeatGenerateID();
      unsavedDraftIdsRef.current.add(id);
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
    } catch {
      // ignore
    }
  }, []);

  const handleAddToScope = useCallback(async (scopeKey: string) => {
    const id = await heartbeatGenerateID();
    const isProject = scopeKey !== "global";
    unsavedDraftIdsRef.current.add(id);
    setEditing({
      id,
      title: "",
      prompt: "",
      interval: "30m",
      enabled: true,
      createdAt: Date.now(),
      scope: isProject ? "project" : "global",
      workspaceRoot: isProject ? scopeKey : "",
    });
  }, []);

  const handleEdit = useCallback((task: HeartbeatTask) => {
    // 分栏模式下切换任务会丢弃当前编辑器未保存改动，先确认。
    if (dirtyRef.current) {
      if (!window.confirm(t("heartbeat.discardChanges"))) return;
      dirtyRef.current = false;
    }
    unsavedDraftIdsRef.current.delete(task.id);
    setEditing({ ...task });
  }, [t]);

  const handleDelete = useCallback(
    async (id: string) => {
      const next = tasks.filter((t) => t.id !== id);
      await save(next);
    },
    [tasks, save],
  );

  const handleTrigger = useCallback(
    async (id: string) => {
      try {
        await heartbeatTriggerNow(id);
        void loadTasks();
      } catch {
        // ignore
      }
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
      // The draft is now persisted; stop treating it as an unsaved draft.
      unsavedDraftIdsRef.current.delete(task.id);
      setEditing({ ...task });
    },
    [tasks, save],
  );

  const handleBackdrop = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === backdropRef.current && !dirtyRef.current) onClose();
    },
    [onClose],
  );

  useEffect(() => {
    if (!open) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape" && !dirtyRef.current && !document.querySelector("[data-anchored-popover='active']")) onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  const scopeFilterLabel = (filter: string, map: Record<string, string>): string => {
    if (filter === "all") return t("heartbeat.filterAllProjects");
    if (filter === "global") return t("heartbeat.scopeGlobal");
    return map[filter] || filter.split("/").pop() || filter;
  };

  const statusFilterLabel = (filter: string): string => {
    if (filter === "all") return t("heartbeat.filterAll" as any);
    if (filter === "enabled") return t("heartbeat.filterEnabled" as any);
    return t("heartbeat.filterDisabled" as any);
  };

  return (
    <div ref={backdropRef} className="heartbeat-backdrop" onMouseDown={handleBackdrop}>
      <div className="heartbeat-modal">
        <header className="heartbeat-modal__header">
          <Activity size={16} />
          <button
            className="heartbeat-modal__close"
            onClick={onClose}
            aria-label={t("common.close")}
          >
            <X size={16} />
          </button>
        </header>

        <div className="heartbeat-split">
          {/* ── Left column: task list ── */}
          <div className="heartbeat-split__left">
            <div className="heartbeat-toolbar">
              <div className="heartbeat-toolbar__search heartbeat-toolbar__search--active">
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
                  className={`heartbeat-toolbar__btn heartbeat-toolbar__btn--icon${statusFilter !== "all" ? " heartbeat-toolbar__btn--active" : ""}`}
                  type="button"
                  onClick={() => setStatusFilterOpen((v) => !v)}
                  title={statusFilterLabel(statusFilter)}
                >
                  <Filter size={13} />
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
                      <span>{t("heartbeat.filterAllProjects")}</span>
                      {scopeFilter === "all" && <Check size={12} className="heartbeat-filter-menu__check" />}
                    </button>
                    <button
                      className={`heartbeat-filter-menu__option${scopeFilter === "global" ? " heartbeat-filter-menu__option--selected" : ""}`}
                      role="option"
                      aria-selected={scopeFilter === "global"}
                      type="button"
                      onClick={() => { setScopeFilter("global"); setScopeFilterOpen(false); }}
                    >
                      <span>{t("heartbeat.scopeGlobal")}</span>
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
              <button className="heartbeat-toolbar__btn heartbeat-toolbar__btn--icon" style={{ marginLeft: "auto" }} onClick={handleAdd} title={t("heartbeat.addTask")}>
                <Plus size={14} />
              </button>
            </div>

            <div className="heartbeat-split__list">
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

                // Group tasks by scope
                const groups = new Map<string, HeartbeatTask[]>();
                for (const task of filtered) {
                  const key = task.scope === "project" && task.workspaceRoot
                    ? task.workspaceRoot : "global";
                  if (!groups.has(key)) groups.set(key, []);
                  groups.get(key)!.push(task);
                }

                const sortedGroups = Array.from(groups.entries()).sort(([a], [b]) => {
                  if (a === "global") return -1;
                  if (b === "global") return 1;
                  return (workspaceMap[a] || a).localeCompare(workspaceMap[b] || b);
                });

                const toggleProject = (key: string) => {
                  setExpandedProjects((prev) => {
                    if (prev === null) {
                      // All groups are currently expanded (null = all). The
                      // first click must collapse the clicked group while
                      // keeping every other group expanded, so seed the set
                      // with all groups minus the clicked one.
                      const next = new Set<string>();
                      for (const [k] of groups) {
                        if (k !== key) next.add(k);
                      }
                      return next;
                    }
                    const next = new Set(prev);
                    if (next.has(key)) next.delete(key);
                    else next.add(key);
                    return next;
                  });
                };

                const isGroupExpanded = (key: string): boolean => {
                  if (expandedProjects === null) return true;
                  return expandedProjects.has(key);
                };

                return loading ? (
                  <div className="heartbeat-empty">
                    <Heart size={24} className="heartbeat-pulse" />
                    <span>{t("workspace.loading")}</span>
                  </div>
                ) : filtered.length === 0 ? (
                  <div className="heartbeat-empty">
                    <Heart size={24} />
                    <span>{tasks.length === 0 ? t("heartbeat.noTasks") : t("heartbeat.noMatchingTasks")}</span>
                  </div>
                ) : (
                  <div className="worktree-tree">
                    {sortedGroups.map(([key, groupTasks]) => {
                      const isExpanded = isGroupExpanded(key);
                      const label = key === "global"
                        ? t("heartbeat.scopeGlobal")
                        : workspaceMap[key] || key.split("/").pop() || key;

                      return (
                        <div key={key}>
                          {/* ── Group header (depth 0: 8px indent) ── */}
                          <div
                            className={`worktree-node worktree-node--scope${editing && groupTasks.some(t => t.id === editing.id) ? " worktree-node--scope-active" : ""}`}
                            style={{ paddingLeft: "8px" }}
                            onClick={() => toggleProject(key)}
                          >
                            <span className="worktree-node__icon">
                              {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                            </span>
                            <span className="worktree-node__marker">
                              {key === "global" ? <Globe size={13} /> : <Folder size={13} />}
                            </span>
                            <span className="worktree-node__label">{label}</span>
                            <span className="worktree-node__scope-add" onClick={(e) => { e.stopPropagation(); void handleAddToScope(key); }} title={t("heartbeat.addTaskToScope", { name: label })}>
                              <Plus size={12} strokeWidth={2.5} />
                            </span>
                          </div>

                          {/* ── Tasks under group (depth 1: 14 + 16 = 30px indent) ── */}
                          {isExpanded && groupTasks.map((task) => {
                            const isSelected = editing?.id === task.id;
                            return (
                              <div
                                key={task.id}
                                className={`worktree-node worktree-node--task${isSelected ? " worktree-node--selected" : ""}`}
                                style={{ paddingLeft: "21px" }}
                                onClick={() => handleEdit(task)}
                              >
                                <span className="worktree-node__marker">
                                  <span className={`worktree-node__dot${task.enabled ? " worktree-node__dot--on" : ""}`} />
                                </span>
                                <span className="worktree-node__label">{task.title || "(untitled)"}</span>
                                <span className="worktree-node__tail">
                                  <span className="worktree-node__interval">{formatInterval(task.interval, t)}{taskNextRun(task) ? ` ${taskNextRun(task)}` : ""}</span>
                                  <span className="worktree-node__actions">
                                  <button
                                    className="worktree-node__action-btn"
                                    onClick={(e) => { e.stopPropagation(); void handleTrigger(task.id); }}
                                    title={t("heartbeat.runNow")}
                                  >
                                    <Play size={14} strokeWidth={1.9} />
                                  </button>
                                  <button
                                    className="worktree-node__action-btn"
                                    type="button"
                                    disabled={!task.topicId}
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      if (task.topicId && onOpenTopic) {
                                        onClose();
                                        onOpenTopic(task.scope || "global", task.workspaceRoot || "", task.topicId);
                                      }
                                    }}
                                    title={task.topicId ? t("heartbeat.openTopic" as any) : ""}
                                  >
                                    <MessageSquare size={14} strokeWidth={1.9} />
                                  </button>
                                </span>
                                </span>
                              </div>
                            );
                          })}
                        </div>
                      );
                    })}
                  </div>
                );
              })()}
            </div>
          </div>

          {/* ── Vertical divider ── */}
          <div className="heartbeat-split__divider" />

          {/* ── Right column: detail / editor ── */}
          <div className="heartbeat-split__right">
            {editing ? (
              <TaskEditor key={editing.id} task={editing} onSave={handleSaveEdit} onCancel={() => setEditing(null)} onDelete={() => { handleDelete(editing.id); setEditing(null); }} onDirtyChange={(d) => { dirtyRef.current = d; }} />
            ) : (
              <div className="heartbeat-split__empty">
                <div className="heartbeat-split__empty-inner">
                  <Activity size={28} />
                  <span>{t("heartbeat.selectTask")}</span>
                  <span className="heartbeat-split__empty-hint">{t("heartbeat.configHint")}</span>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Cycle Editor ──────────────────────────────────────────────────────────────

const WEEKDAYS = [
  { key: "mon", labelKey: "heartbeat.weekdayMon" },
  { key: "tue", labelKey: "heartbeat.weekdayTue" },
  { key: "wed", labelKey: "heartbeat.weekdayWed" },
  { key: "thu", labelKey: "heartbeat.weekdayThu" },
  { key: "fri", labelKey: "heartbeat.weekdayFri" },
  { key: "sat", labelKey: "heartbeat.weekdaySat" },
  { key: "sun", labelKey: "heartbeat.weekdaySun" },
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
  const cycleMatch = draft.interval.match(/^(\d+)[smh]\|(daily|weekly|biweekly|monthly|yearly)(?::([^@]*))?(?:@(\d{2}:\d{2}))?$/);
  const [cycleType, setCycleType] = useState<string>(
    cycleMatch ? cycleMatch[2] : "daily"
  );
  const cycleDays = cycleMatch?.[3] || "";
  const cycleTime = cycleMatch?.[4] || "09:00";
  const [selectedDays, setSelectedDays] = useState<string[]>(
    cycleDays ? cycleDays.split(",") : ["mon","tue","wed","thu","fri","sat","sun"]
  );
  const [monthDay, setMonthDay] = useState(cycleDays || "1");
  const [yearMonth, setYearMonth] = useState(cycleDays.split("-")[0] || "1");
  const [yearDay, setYearDay] = useState(cycleDays.split("-")[1] || "1");
  const [timeVal, setTimeVal] = useState(cycleTime);
  const [cycleOpen, setCycleOpen] = useState(false);
  const cycleRef = useRef<HTMLDivElement>(null);

  // Close dropdown on outside click
  useEffect(() => {
    if (!cycleOpen) return;
    const close = (e: MouseEvent) => {
      if (cycleRef.current && !cycleRef.current.contains(e.target as Node)) {
        setCycleOpen(false);
      }
    };
    document.addEventListener("click", close);
    return () => document.removeEventListener("click", close);
  }, [cycleOpen]);

  // Build interval string when config changes
  const buildInterval = useCallback((ct: string, days: string[], tm: string) => {
    const base: Record<string, string> = {
      daily: "24h",
      weekly: "168h",
      biweekly: "336h",
      monthly: "720h",
      yearly: "8760h",
    };
    let suffix = `|${ct}`;
    if (ct === "daily" || ct === "weekly" || ct === "biweekly") {
      suffix += `:${days.join(",")}`;
    } else if (ct === "monthly") {
      suffix += `:${days[0] || "1"}`;
    } else if (ct === "yearly") {
      // days[0] = month, days[1] = day — each is a plain number, no dash
      suffix += `:${days[0] || "1"}-${days[1] || "1"}`;
    }
    suffix += `@${tm}`;
    return (base[ct] || "24h") + suffix;
  }, []);

  const onCycleTypeChange = useCallback((ct: string) => {
    setCycleType(ct);
    const days: string[] = [];
    setSelectedDays(days);
    setMonthDay("1");
    setYearMonth("1");
    setYearDay("1");
    if (ct !== "daily" && ct !== "weekly" && ct !== "biweekly") {
      setSelectedDays([]);
    }
    setDraft("interval", buildInterval(ct, days, timeVal));
  }, [buildInterval, setDraft, timeVal]);

  const onDayToggle = useCallback((day: string) => {
    setSelectedDays((prev) => {
      // Weekly/biweekly schedules must keep at least one weekday selected;
      // an empty weekday rule is rejected by the backend's schedule parser,
      // silently turning the task into a rolling interval.
      const isWeeklyLike = cycleType === "weekly" || cycleType === "biweekly";
      const wouldBeEmpty = prev.includes(day) && prev.length === 1 && isWeeklyLike;
      if (wouldBeEmpty) return prev;
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
    const days = cycleType === "daily" || cycleType === "weekly" || cycleType === "biweekly" ? selectedDays
      : cycleType === "monthly" ? [monthDay]
      : cycleType === "yearly" ? [yearMonth, yearDay]
      : [];
    setDraft("interval", buildInterval(cycleType, days, tm));
  }, [buildInterval, cycleType, selectedDays, monthDay, yearMonth, yearDay, setDraft]);

  const MONTHS = Array.from({ length: 12 }, (_, i) => ({
    value: String(i + 1),
    label: t("heartbeat.monthOption", { n: i + 1 }),
  }));
  const DAYS = Array.from({ length: 31 }, (_, i) => ({
    value: String(i + 1),
    label: t("heartbeat.dayOption", { n: i + 1 }),
  }));

  return (
    <div className="heartbeat-editor__cycle-wrap">
      <div className="heartbeat-editor__cycle-row">
        <div className="heartbeat-scope-wrap" ref={cycleRef}>
          <button
            className="heartbeat-scope-select"
            onClick={() => setCycleOpen((v) => !v)}
          >
            {t(`heartbeat.cycle${cycleType.charAt(0).toUpperCase() + cycleType.slice(1)}` as any)}
            <ChevronsUpDown size={12} />
          </button>
          {cycleOpen && (
            <div className="heartbeat-project-menu heartbeat-project-menu--up">
              {["daily", "weekly", "biweekly", "monthly", "yearly"].map((ct) => (
                <button
                  key={ct}
                  className={`heartbeat-project-menu__item${cycleType === ct ? " heartbeat-project-menu__item--active" : ""}`}
                  onClick={() => { onCycleTypeChange(ct); setCycleOpen(false); }}
                >
                  {t(`heartbeat.cycle${ct.charAt(0).toUpperCase() + ct.slice(1)}` as any)}
                  {cycleType === ct && <Check size={12} className="heartbeat-filter-menu__check" />}
                </button>
              ))}
            </div>
          )}
        </div>

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

        {(cycleType === "weekly" || cycleType === "biweekly") && (
          <div className="set-seg">
            {WEEKDAYS.map((wd) => (
              <button
                key={wd.key}
                type="button"
                className={`set-seg__btn${selectedDays.includes(wd.key) ? " set-seg__btn--on" : ""}`}
                onClick={() => onDayToggle(wd.key)}
                aria-pressed={selectedDays.includes(wd.key)}
              >
                {t(wd.labelKey)}
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
  onDirtyChange,
}: {
  task: HeartbeatTask;
  onSave: (t: HeartbeatTask) => void;
  onCancel: () => void;
  onDelete: () => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const t = useT();
  const titleRef = useRef<HTMLInputElement>(null);
  const [workspaces, setWorkspaces] = useState<WorkspaceView[]>([]);
  const [projectOpen, setProjectOpen] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const projectRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
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
  const initialTaskRef = useRef(task);
  // 保存后父组件 setEditing({...task}) 传入新引用，同步基线使 isDirty
  // 复位（保存按钮与 dirtyRef 不再保持脏状态）。
  useEffect(() => {
    initialTaskRef.current = task;
  }, [task]);
  const isDirty = draft.title !== initialTaskRef.current.title
    || draft.prompt !== initialTaskRef.current.prompt
    || draft.interval !== initialTaskRef.current.interval
    || draft.enabled !== initialTaskRef.current.enabled
    || draft.approvalMode !== initialTaskRef.current.approvalMode
    || draft.newConversationEachRun !== initialTaskRef.current.newConversationEachRun
    || draft.notifyChannels !== initialTaskRef.current.notifyChannels
    || draft.scope !== initialTaskRef.current.scope
    || draft.workspaceRoot !== initialTaskRef.current.workspaceRoot
    || draft.timeWindowStart !== initialTaskRef.current.timeWindowStart
    || draft.timeWindowEnd !== initialTaskRef.current.timeWindowEnd;

  useEffect(() => {
    onDirtyChange?.(isDirty);
  }, [isDirty, onDirtyChange]);

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
    task.interval.includes("|") ? "cycle" : "interval"
  );

  const isNew = !task.createdAt;
  const selectedWorkspace = draft.scope === "project" && draft.workspaceRoot
    ? workspaces.find((w) => w.path === draft.workspaceRoot)
    : null;

  return (
    <div className="heartbeat-editor">
      {/* Title */}
      <div className="heartbeat-editor__field">
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
        <label>{t("heartbeat.scopeProject")}</label>
        <div className="heartbeat-scope-wrap" ref={projectRef}>
          <button
            className="heartbeat-scope-select"
            onClick={() => setProjectOpen((v) => !v)}
          >
            {selectedWorkspace ? selectedWorkspace.name : t("heartbeat.scopeGlobal")}
            <ChevronsUpDown size={12} />
          </button>
          {projectOpen && (
            <div className="heartbeat-project-menu">
              {workspaces.length === 0 ? (
                <div className="heartbeat-project-menu__empty">{t("heartbeat.noProjects")}</div>
              ) : (
                <>
                  <button
                    className={`heartbeat-project-menu__item${!draft.scope || draft.scope === "global" || !draft.workspaceRoot ? " heartbeat-project-menu__item--active" : ""}`}
                    onClick={() => {
                      setDraft((prev) => ({ ...prev, scope: "global", workspaceRoot: "" }));
                      setProjectOpen(false);
                    }}
                  >
                    {t("heartbeat.scopeGlobal")}
                    {(!draft.scope || draft.scope === "global" || !draft.workspaceRoot) && <Check size={12} className="heartbeat-filter-menu__check" />}
                  </button>
                  {workspaces.map((ws) => (
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
                      {draft.workspaceRoot === ws.path && <Check size={12} className="heartbeat-filter-menu__check" />}
                    </button>
                  ))}
                </>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Prompt */}
      <div className="heartbeat-editor__field">
        <label>{t("heartbeat.fieldPrompt")}</label>
        <textarea
          className="heartbeat-editor__textarea"
          value={draft.prompt}
          onChange={(e) => set("prompt", e.target.value)}
          placeholder={t("heartbeat.promptPlaceholder")}
          rows={5}
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
          <button
            className={`set-seg__btn${freqType === "cycle" ? " set-seg__btn--on" : ""}`}
            onClick={() => {
              setFreqType("cycle");
              // Initialize interval to daily schedule when switching to cycle mode
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
        </div>

        {freqType === "cycle" ? <CycleEditor draft={draft} setDraft={set} /> : (
          <div className="heartbeat-editor__freq-interval">
            <span className="heartbeat-editor__freq-label">{t("heartbeat.freqEvery")}</span>
            <input
              className="heartbeat-editor__freq-input"
              value={(() => {
                const m = draft.interval.match(/^(\d+)/);
                return m ? m[1] : "1";
              })()}
              onChange={(e) => {
                const num = e.target.value.replace(/\D/g, "");
                const mUnit = draft.interval.match(/^(\d+)([smh])/);
                const unit = mUnit ? mUnit[2] : "h";
                setDraft((prev) => ({ ...prev, interval: num ? num + unit : "1" + unit }));
              }}
              placeholder="1"
            />
            <div className="set-seg">
              <button
                className={`set-seg__btn${(() => {
                  const m = draft.interval.match(/^(\d+)([smh])/);
                  return (m ? m[2] : "h") === "m" ? " set-seg__btn--on" : "";
                })()}`}
                onClick={() => {
                  const num = draft.interval.match(/^(\d+)/)?.[1] || "1";
                  setDraft((prev) => ({ ...prev, interval: num + "m" }));
                }}
              >
                {t("heartbeat.unitMin")}
              </button>
              <button
                className={`set-seg__btn${(() => {
                  const m = draft.interval.match(/^(\d+)([smh])/);
                  return (m ? m[2] : "h") === "h" ? " set-seg__btn--on" : "";
                })()}`}
                onClick={() => {
                  const num = draft.interval.match(/^(\d+)/)?.[1] || "1";
                  setDraft((prev) => ({ ...prev, interval: num + "h" }));
                }}
              >
                {t("heartbeat.unitHour")}
              </button>
            </div>
            {draft.timeWindowStart || draft.timeWindowEnd ? (
              <div className="heartbeat-editor__tw-inputs" style={{ marginLeft: "8px" }}>
                <input
                  className="heartbeat-editor__freq-input heartbeat-editor__freq-input--time"
                  type="time"
                  value={draft.timeWindowStart || ""}
                  onChange={(e) => setDraft((prev) => ({ ...prev, timeWindowStart: e.target.value || undefined }))}
                  style={{ width: "90px" }}
                />
                <span className="heartbeat-editor__freq-label heartbeat-editor__tw-sep">—</span>
                <input
                  className="heartbeat-editor__freq-input heartbeat-editor__freq-input--time"
                  type="time"
                  value={draft.timeWindowEnd || ""}
                  onChange={(e) => setDraft((prev) => ({ ...prev, timeWindowEnd: e.target.value || undefined }))}
                  style={{ width: "90px" }}
                />
                <button
                  className="heartbeat-editor__tw-remove"
                  onClick={() => setDraft((prev) => ({ ...prev, timeWindowStart: undefined, timeWindowEnd: undefined }))}
                  title={t("heartbeat.removeTimeWindow")}
                >
                  <X size={12} />
                </button>
              </div>
            ) : (
              <span className="heartbeat-editor__tw-add" style={{ marginLeft: "8px" }}
                onClick={() => setDraft((prev) => ({ ...prev, timeWindowStart: "09:00", timeWindowEnd: "17:00" }))}
              >
                + {t("heartbeat.timeWindow")}
              </span>
            )}
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="heartbeat-editor__actions">
        {!isNew && (
          <button
            className={`heartbeat-btn${confirmingDelete ? "" : " heartbeat-btn--danger"}`}
            onClick={() => {
              if (confirmingDelete) {
                onDelete();
              } else {
                setConfirmingDelete(true);
              }
            }}
          >
            <Trash2 size={13} />
            {confirmingDelete ? t("heartbeat.confirmDelete") : t("heartbeat.delete")}
          </button>
        )}
        <button
          className="heartbeat-btn"
          onClick={() => {
            const updated = { ...draft, enabled: !draft.enabled };
            setDraft(updated);
            // enabled 切换即时保存（onSave 立即持久化），同步基线使
            // isDirty 不再把 enabled 当作未保存改动，保存按钮状态不受影响。
            initialTaskRef.current = updated;
            onSave(updated);
          }}
        >
          {draft.enabled ? t("heartbeat.disable") : t("heartbeat.enabled")}
        </button>
        <span style={{ marginLeft: "auto", display: "flex", gap: "8px" }}>
          <button
            className={`heartbeat-btn${isDirty ? " heartbeat-btn--primary" : ""}`}
            onClick={() => onSave(draft)}
            disabled={!draft.title.trim() || !draft.prompt.trim() || (!isDirty && !isNew)}
          >
            {isNew ? t("heartbeat.add") : t("heartbeat.save")}
          </button>
          <button className="heartbeat-btn" onClick={onCancel}>
            {t("common.cancel")}
          </button>
        </span>
      </div>
    </div>
  );
}
