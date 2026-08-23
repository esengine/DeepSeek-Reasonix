// Heartbeat Panel — Modal for configuring scheduled heartbeat tasks.
//
// Renders a list of tasks with add/edit/delete controls, plus a manual
// "run now" button for each. The panel is opened from the sidebar nav item.

import { useCallback, useEffect, useRef, useState } from "react";
import {
  Activity,
  Check,
  ChevronDown,
  ChevronRight,
  ChevronsUpDown,
  Circle,
  CirclePause,
  FolderTree,
  Heart,
  Lightbulb,
  List,
  MessageSquare,
  MoreHorizontal,
  Play,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { app } from "../../../lib/bridge";
import { AnchoredPopover } from "../../../components/AnchoredPopover";
import { Tooltip } from "../../../components/Tooltip";
import {
  heartbeatListTasks,
  heartbeatMutateTasks,
  heartbeatTriggerNow,
  heartbeatGenerateID,
} from "./heartbeat.bridge";
import type { HeartbeatTask } from "./heartbeat.types";
import { useHeartbeatT, type HeartbeatTranslator } from "./heartbeat.i18n";
// 样式跟着组件走：App.tsx 通过 React.lazy 按需加载本组件，CSS 在此动态
// 静态导入：Vite 保证 CSS 在模块 evaluate 前注入 DOM，避免首次访问自动化页
// 无样式闪烁（FOUC）。node 单测通过 css-stub-register.mjs 的 loader hook 解析。
import "./heartbeat.css";
import { formatInterval, formatTaskNextRun, prepareTasksByNextRun } from "./heartbeat.presentation";
import { TaskEditor } from "./HeartbeatTaskEditor";
import { CirclePlaySolid, mergeEngineRunState } from "./HeartbeatShared";
export { changeHeartbeatFrequency, cronToInterval, heartbeatNextRunAt, intervalToCron, nextCycleRunAt, prepareTasksByNextRun } from "./heartbeat.presentation";
export { heartbeatBuildCycleInterval } from "./HeartbeatCycleEditor";
export { TaskEditor } from "./HeartbeatTaskEditor";
export { mergeEngineRunState } from "./HeartbeatShared";

interface HeartbeatPanelProps {
  onOpenTopic?: (scope: string, workspaceRoot: string, topicId: string) => void;
}

export function HeartbeatView({ onOpenTopic }: HeartbeatPanelProps) {
  const t = useHeartbeatT();
  const [tasks, setTasks] = useState<HeartbeatTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [mutationError, setMutationError] = useState(false);
  const [editing, setEditing] = useState<HeartbeatTask | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "enabled" | "disabled">("all");
  const [scopeFilter, setScopeFilter] = useState<string>("all");
  const [scopeFilterOpen, setScopeFilterOpen] = useState(false);
  const scopeFilterRef = useRef<HTMLButtonElement>(null);
  const [expandedProjects, setExpandedProjects] = useState<Set<string> | null>(null);
  const [workspaceMap, setWorkspaceMap] = useState<Record<string, string>>({});
  // Left list view: flat (default) or grouped by project scope.
  const [listView, setListView] = useState<"flat" | "grouped">("grouped");
  // Detail panel: hidden by default (the list fills the pane, like ChatGPT),
  // opens on task click with a 50/50 split and a draggable divider.
  const [detailOpen, setDetailOpen] = useState(false);
  // 分割线拖拽宽度持久化：localStorage 缓存上次拖拽比例（30-70），
  // 重新打开面板/切换视图后恢复，无需每次重拖。
  const [listWidthPct, setListWidthPct] = useState(() => {
    try {
      const cached = Number(localStorage.getItem("reasonix-heartbeat-list-width"));
      return Number.isFinite(cached) ? Math.min(70, Math.max(30, cached)) : 50;
    } catch {
      return 50;
    }
  });
  const dirtyRef = useRef(false);
  // IDs of drafts created via Add/scoped-Add/startNew that have not been saved yet.
  // They are intentionally absent from `tasks`, so the "clear editing" effect
  // below must not close the editor for them.
  const unsavedDraftIdsRef = useRef<Set<string>>(new Set());

  // ── 自定义滚动条：隐藏系统滚动条，DOM 直接驱动 + rAF 帧同步 ──
  const listRef = useRef<HTMLDivElement>(null);
  const thumbRef = useRef<HTMLDivElement>(null);
  const scrollbarRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const list = listRef.current;
    const thumb = thumbRef.current;
    const track = scrollbarRef.current;
    if (!list || !thumb || !track) return;

    // 滚动条自动隐藏：滚动/拖动时显示，停止 1.2s 后淡出
    let hideTimer = 0;
    const show = () => {
      track.classList.add("heartbeat-scrollbar--visible");
      window.clearTimeout(hideTimer);
      hideTimer = window.setTimeout(() => {
        track.classList.remove("heartbeat-scrollbar--visible");
      }, 1200);
    };

    const update = () => {
      const { scrollTop, scrollHeight, clientHeight } = list;
      if (scrollHeight <= clientHeight + 1) {
        thumb.style.display = "none";
        return;
      }
      thumb.style.display = "block";
      // 轨道（.heartbeat-scrollbar）覆盖整个 left 列，高度 ≠ 列表可视区；
      // thumb 尺寸/行程按轨道实际高度计算，保证滚到底时 thumb 也到底。
      const trackHeight = track.clientHeight;
      const height = Math.max(24, (clientHeight / scrollHeight) * trackHeight);
      const maxTop = trackHeight - height;
      const top = maxTop <= 0 ? 0 : (scrollTop / (scrollHeight - clientHeight)) * maxTop;
      thumb.style.height = `${Math.round(height)}px`;
      thumb.style.top = `${Math.round(top)}px`;
    };

    // rAF 节流：滚动/尺寸变化时在下一帧同步一次滑块位置
    let raf = 0;
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(() => { raf = 0; update(); });
    };
    // 滚动（含拖动 thumb 触发的 scroll）时显示滚动条并重置隐藏计时
    const onScroll = () => {
      show();
      schedule();
    };

    update();
    list.addEventListener("scroll", onScroll, { passive: true });
    const ro = new ResizeObserver(schedule);
    ro.observe(list);

    // 内容变化后 scrollHeight 更新，下一帧校准（含首次挂载）
    const t = window.setTimeout(update, 50);
    return () => {
      if (raf) cancelAnimationFrame(raf);
      window.clearTimeout(t);
      window.clearTimeout(hideTimer);
      list.removeEventListener("scroll", onScroll);
      ro.disconnect();
    };
  }, [tasks]);

  const onScrollThumbMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    const list = listRef.current;
    const track = scrollbarRef.current;
    if (!list || !track) return;
    // 拖动开始即显示滚动条，拖动过程中保持可见（scroll 事件会重置隐藏计时）
    track.classList.add("heartbeat-scrollbar--visible");
    const startY = e.clientY;
    const startScroll = list.scrollTop;
    const { scrollHeight, clientHeight } = list;
    // 与 update() 一致：thumb 行程按轨道实际高度映射
    const trackHeight = track.clientHeight;
    const height = Math.max(24, (clientHeight / scrollHeight) * trackHeight);
    const maxTop = trackHeight - height;
    const maxScroll = scrollHeight - clientHeight;
    const ratio = maxTop > 0 ? maxScroll / maxTop : 1;
    const onMove = (ev: MouseEvent) => {
      list.scrollTop = startScroll + (ev.clientY - startY) * ratio;
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  }, []);

  // Reset dirty ref when leaving edit mode
  useEffect(() => {
    if (!editing) dirtyRef.current = false;
  }, [editing]);

  const loadTasks = useCallback(async (): Promise<HeartbeatTask[] | null> => {
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
      return taskList;
    } catch {
      return null;
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    setEditing(null);
    setSearchQuery("");
    setStatusFilter("all");
    void loadTasks();
  }, [loadTasks]);

  // Clear editing when the edited task is no longer in the filtered list.
  // Unsaved drafts (created via Add/scoped-Add/startNew) are not yet in
  // `tasks`; keep their editor open until the user saves or cancels.
  // 列表切换任务：未保存修改直接放弃（ChatGPT 式，不弹确认）
  useEffect(() => {
    if (!editing) return;
    if (unsavedDraftIdsRef.current.has(editing.id)) return;
    // 过滤变化导致编辑任务不可见时，直接关闭（丢弃未保存修改）。
    const closeIfNotDirty = () => {
      dirtyRef.current = false;
      return true;
    };
    const match = tasks.find(t => t.id === editing.id);
    if (!match) { if (closeIfNotDirty()) setEditing(null); return; }
    // 状态筛选切换（全部↔已开启↔已暂停）不关闭已打开的详情——右侧详情跟随任务，
    // 与左侧列表筛选解耦（用户明确要求：已激活任务的详情不受筛选影响）。
    if (searchQuery && !match.title.toLowerCase().includes(searchQuery.toLowerCase())) { if (closeIfNotDirty()) setEditing(null); return; }
    // 与列表过滤一致：scopeFilter 过滤掉正在编辑的任务时关闭编辑器。
    if (scopeFilter === "global" && (match.scope === "project" && match.workspaceRoot)) { if (closeIfNotDirty()) setEditing(null); return; }
    if (scopeFilter !== "all" && scopeFilter !== "global" && (match.scope !== "project" || match.workspaceRoot !== scopeFilter)) { if (closeIfNotDirty()) setEditing(null); }
  }, [tasks, editing?.id, statusFilter, searchQuery, scopeFilter, t]);

  const save = useCallback(
    async (mutate: (current: HeartbeatTask[]) => HeartbeatTask[]): Promise<HeartbeatTask[] | null> => {
      try {
        const persisted = await heartbeatMutateTasks(mutate);
        setTasks(persisted);
        setMutationError(false);
        return persisted;
      } catch {
        setMutationError(true);
        // A concurrent external edit wins; reload the authoritative config so
        // the list cannot continue from a stale baseline. The editor keeps its
        // local draft and reports the failure instead of pretending it saved.
        await loadTasks();
        return null;
      }
    },
    [loadTasks],
  );

  const openUnsavedDraft = useCallback((task: HeartbeatTask) => {
    unsavedDraftIdsRef.current.add(task.id);
    setEditing(task);
    setDetailOpen(true);
  }, []);

  const handleAdd = useCallback(async () => {
    let id = `hb-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    try {
      id = await heartbeatGenerateID();
    } catch {
      // 生成失败时使用本地 fallback id，仍可正常打开新建详情
    }
    // 不设 createdAt：isNew=true，详情显示项目字段、删除按钮禁用
    openUnsavedDraft({
      id,
      title: "",
      prompt: "",
      interval: "30m",
      enabled: true,
      approvalMode: "yolo",
      newConversationEachRun: false,
      notifyChannels: false,
    });
  }, [openUnsavedDraft]);

  const handleAddToScope = useCallback(async (scopeKey: string) => {
    let id = `hb-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    try {
      id = await heartbeatGenerateID();
    } catch {      // 生成失败时使用本地 fallback id
    }
    const isProject = scopeKey !== "global";
    openUnsavedDraft({
      id,
      title: "",
      prompt: "",
      interval: "30m",
      enabled: true,
      scope: isProject ? "project" : "global",
      workspaceRoot: isProject ? scopeKey : "",
    });
  }, [openUnsavedDraft]);

  // 建议区：用预设内容打开未保存草稿，由用户确认后再创建。
  // 动态推荐：建议只在"对应标题的任务不存在"时显示——添加后消失，删除后恢复。
  const handleAddSuggestion = useCallback(
    async (sug: Suggestion) => {
      let id = `hb-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
      try {
        id = await heartbeatGenerateID();
      } catch {
        // 生成失败时使用本地 fallback id
      }
      // 推荐卡片只打开编辑器，不直接创建并启用任务：预置 prompt 可能涉及
      // 读取对话/扫描本地文件/访问网络等敏感操作，默认禁用 + ask 审批，
      // 由用户在编辑器中确认作用域与权限后再启用。
      const task: HeartbeatTask = {
        id,
        title: sug.title,
        prompt: sug.prompt,
        interval: sug.interval,
        enabled: false,
        approvalMode: "ask",
        newConversationEachRun: false,
        notifyChannels: false,
        scope: "global",
        workspaceRoot: "",
      };
      openUnsavedDraft(task);
    },
    [openUnsavedDraft],
  );

  const handleEdit = useCallback((task: HeartbeatTask) => {
    // 分栏模式下切换任务直接丢弃当前编辑器未保存改动（ChatGPT 式，不确认）。
    dirtyRef.current = false;
    unsavedDraftIdsRef.current.delete(task.id);
    setEditing({ ...task });
    setDetailOpen(true);
  }, []);

  // 列表行点击状态图标切换任务启用/暂停（即时保存）
  const handleToggle = useCallback(async (task: HeartbeatTask) => {
    const saved = await save((current) => current.map((item) => (
      item.id === task.id ? { ...item, enabled: !item.enabled } : item
    )));
    if (saved) {
      const authoritative = saved.find((item) => item.id === task.id);
      if (!authoritative) return;
      setEditing((prev) => (prev && prev.id === task.id ? mergeEngineRunState({ ...prev, enabled: authoritative.enabled }, authoritative) : prev));
    }
  }, [save]);

  const handleDelete = useCallback(
    async (id: string) => (await save((current) => current.filter((task) => task.id !== id))) !== null,
    [save],
  );

  // 任务行 ⋯ 菜单（仅删除任务）：单个 popover，anchor 动态指向当前点击的按钮
  const [menuTaskId, setMenuTaskId] = useState<string | null>(null);
  const menuAnchorRef = useRef<HTMLButtonElement | null>(null);

  const handleTrigger = useCallback(
    async (id: string) => {
      try {
        await heartbeatTriggerNow(id);
        const fresh = await loadTasks();
        // 详情页正在编辑该任务时，同步最新 run 状态（runHistory/topicId/lastRunAt），
        // 否则详情页 runHistory 区域停留在触发前的旧快照，看不到新记录。
        // 只合并引擎拥有的字段：等待期间用户对 title/prompt/schedule 的草稿
        // 编辑不能被磁盘旧快照覆盖。
        if (fresh) {
          const updated = fresh.find((t) => t.id === id);
          if (updated) {
            setEditing((prev) => (prev && prev.id === id ? mergeEngineRunState(prev, updated) : prev));
          }
        }
      } catch {
        // ignore
      }
    },
    [loadTasks],
  );

  const handleSaveEdit = useCallback(
    async (task: HeartbeatTask) => {
      // 新建任务（无 createdAt，isNew）保存时补 createdAt，使其成为正式任务：
      // isNew=false 后详情头部出现启停/删除按钮，呈现与常规任务完全一致。
      const persisted = task.createdAt ? task : { ...task, createdAt: Date.now() };
      const saved = await save((current) => {
        const idx = current.findIndex((item) => item.id === task.id);
        if (idx < 0) return [...current, persisted];
        const next = [...current];
        next[idx] = mergeEngineRunState(persisted, current[idx]);
        return next;
      });
      if (!saved) return false;
      // The draft is now persisted; stop treating it as an unsaved draft.
      unsavedDraftIdsRef.current.delete(task.id);
      setEditing({ ...(saved.find((item) => item.id === task.id) ?? persisted) });
      return true;
    },
    [save],
  );

  const onDividerMouseDown = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    const splitEl = e.currentTarget.parentElement;
    if (!splitEl) return;
    const rect = splitEl.getBoundingClientRect();
    const onMove = (ev: MouseEvent) => {
      const pct = ((ev.clientX - rect.left) / rect.width) * 100;
      const clamped = Math.min(70, Math.max(30, pct));
      setListWidthPct(clamped);
      // 拖拽过程中同步缓存（最后一次 onMove 即松手时的值，无需在 onUp 再写）
      try {
        localStorage.setItem("reasonix-heartbeat-list-width", String(clamped));
      } catch {
        // Storage may be unavailable in hardened webviews; in-memory state still works.
      }
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  }, []);

  const scopeFilterLabel = (filter: string, map: Record<string, string>): string => {
    if (filter === "all") return t("heartbeat.filterAllProjects");
    if (filter === "global") return t("heartbeat.scopeGlobal");
    return map[filter] || filter.split("/").pop() || filter;
  };

  return (
    <div className="heartbeat-page">
      {/* 无详情时：页面顶部一条全宽透明窗口拖拽条 */}
      {!detailOpen && <div className="heartbeat-drag-strip" />}
      <div className="heartbeat-split">
          {/* ── Left column: task list（含列表区头部工具栏） ── */}
          <div className={`heartbeat-split__left${detailOpen ? "" : " heartbeat-split__left--full"}`} style={{ width: detailOpen ? `${listWidthPct}%` : "100%" }}>
            {!detailOpen && (
              <div className="heartbeat-hero">
                <h1 className="heartbeat-hero__title">{t("heartbeat.heroTitle")}</h1>
                <p className="heartbeat-hero__subtitle">{t("heartbeat.heroSubtitle")}</p>
              </div>
            )}
            <div className="heartbeat-toolbar">
              <div className="heartbeat-status-tabs" role="tablist" aria-label={t("heartbeat.filterStatus")}>
                {(["all", "enabled", "disabled"] as const).map((key) => (
                  <button
                    key={key}
                    type="button"
                    role="tab"
                    aria-selected={statusFilter === key}
                    className={`heartbeat-status-tabs__tab${statusFilter === key ? " heartbeat-status-tabs__tab--on" : ""}`}
                    onClick={() => setStatusFilter(key)}
                  >
                    {key === "all" ? t("heartbeat.filterAll") : key === "enabled" ? t("heartbeat.filterEnabled") : t("heartbeat.filterDisabled")}
                  </button>
                ))}
              </div>
              <div className="heartbeat-toolbar__view" style={{ marginLeft: "auto" }}>
                {listView === "flat" && (
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
                )}
                <button
                  className="heartbeat-toolbar__btn heartbeat-toolbar__btn--icon"
                  type="button"
                  onClick={() => setListView(listView === "flat" ? "grouped" : "flat")}
                  title={listView === "flat" ? t("heartbeat.viewGrouped") : t("heartbeat.viewFlat")}
                >
                  {listView === "flat" ? <FolderTree size={14} /> : <List size={14} />}
                </button>
                <button
                  className="heartbeat-toolbar__btn heartbeat-toolbar__btn--primary heartbeat-toolbar__btn--add"
                  type="button"
                  onClick={() => void handleAdd()}
                  title={t("heartbeat.addTask")}
                >
                  <Plus size={14} strokeWidth={2.2} />
                  {t("heartbeat.btnNew")}
                </button>
              </div>
            </div>

            <div className="heartbeat-split__list-wrap">
              <div className="heartbeat-list-search">
                <Search size={13} className="heartbeat-list-search__icon" />
                <input
                  className="heartbeat-list-search__input"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") setSearchQuery("");
                  }}
                  placeholder={t("heartbeat.searchPlaceholder")}
                />
                {searchQuery && (
                  <button className="heartbeat-list-search__clear" onClick={() => setSearchQuery("")}>
                    <X size={12} />
                  </button>
                )}
              </div>
              <div className="heartbeat-split__list" ref={listRef}>
                {(() => {
                const now = Date.now();
                const filtered = prepareTasksByNextRun(
                  tasks.filter((task) => {
                    if (statusFilter === "enabled" && !task.enabled) return false;
                    if (statusFilter === "disabled" && task.enabled) return false;
                    if (searchQuery && !task.title.toLowerCase().includes(searchQuery.toLowerCase())) return false;
                    if (scopeFilter === "global" && (task.scope === "project" && task.workspaceRoot)) return false;
                    if (scopeFilter !== "all" && scopeFilter !== "global") {
                      if (task.scope !== "project" || task.workspaceRoot !== scopeFilter) return false;
                    }
                    return true;
                  }),
                  now,
                );

                // Group tasks by scope
                const groups = new Map<string, (typeof filtered)[number][]>();
                for (const entry of filtered) {
                  const { task } = entry;
                  const key = task.scope === "project" && task.workspaceRoot
                    ? task.workspaceRoot : "global";
                  if (!groups.has(key)) groups.set(key, []);
                  groups.get(key)!.push(entry);
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
                  tasks.length === 0 ? (
                    <div className="heartbeat-empty heartbeat-empty--guided">
                      <Heart size={28} />
                      <span>{t("heartbeat.noTasks")}</span>
                      <button className="heartbeat-btn heartbeat-btn--primary" type="button" onClick={handleAdd}>
                        <Plus size={13} />
                        {t("heartbeat.addTask")}
                      </button>
                    </div>
                  ) : (
                    <div className="heartbeat-empty">
                      <Heart size={24} />
                      <span>{t("heartbeat.noMatchingTasks")}</span>
                    </div>
                  )
                ) : listView === "flat" ? (
                  <div className="worktree-tree heartbeat-flat">
                    {filtered.map(({ task, nextRunAt }) => {
                      const isSelected = detailOpen && editing?.id === task.id;
                      const nextRun = formatTaskNextRun(nextRunAt, now, t);
                      const scopeLabel = task.scope === "project" && task.workspaceRoot
                        ? (workspaceMap[task.workspaceRoot] || task.workspaceRoot.split("/").pop() || task.workspaceRoot)
                        : t("heartbeat.scopeGlobal");
                      return (
                        <div
                          key={task.id}
                          className={`worktree-node worktree-node--task${task.enabled ? "" : " worktree-node--paused"}${isSelected ? " worktree-node--selected" : ""}`}
                          style={{ paddingLeft: "21px" }}
                          onClick={() => handleEdit(task)}
                        >
                          <div className="worktree-node__main">
                            <span className="worktree-node__marker">
                              <Tooltip
                                label={task.enabled ? t("heartbeat.clickPause") : t("heartbeat.clickStart")}
                                side="top"
                                delay={60}
                              >
                                <button
                                  className={`worktree-node__toggle${task.enabled ? " worktree-node__toggle--on" : ""}`}
                                  type="button"
                                  onClick={(e) => { e.stopPropagation(); void handleToggle(task); }}
                                >
                                  {task.enabled ? (
                                    <>
                                      <Circle className="worktree-node__toggle-circle" size={15} strokeWidth={2.4} />
                                      <CirclePause className="worktree-node__toggle-hover" size={15} strokeWidth={2.4} />
                                    </>
                                  ) : (
                                    <CirclePlaySolid size={15} />
                                  )}
                                </button>
                              </Tooltip>
                            </span>
                            <span className="worktree-node__label">{task.title || t("heartbeat.untitled")}</span>
                            <span className="worktree-node__actions">
                              <button
                                className="worktree-node__action-btn"
                                type="button"
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
                                    onOpenTopic(task.scope || "global", task.workspaceRoot || "", task.topicId);
                                  }
                                }}
                                title={task.topicId ? t("heartbeat.openTopic") : ""}
                              >
                                <MessageSquare size={14} strokeWidth={1.9} />
                              </button>
                              <button
                                className="worktree-node__action-btn"
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  menuAnchorRef.current = e.currentTarget;
                                  setMenuTaskId(task.id);
                                }}
                                title={t("common.moreActions")}
                              >
                                <MoreHorizontal size={14} strokeWidth={1.9} />
                              </button>
                            </span>
                          </div>
                          <div className="worktree-node__meta">
                            <span className="worktree-node__scope-tag">{scopeLabel}</span>
                            <span className="worktree-node__interval">{formatInterval(task.interval, t)}{nextRun ? ` · ${nextRun}` : ""}</span>
                          </div>
                        </div>
                      );
                    })}
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
                            className={`worktree-node worktree-node--scope`}
                            style={{ paddingLeft: "8px" }}
                            onClick={() => toggleProject(key)}
                          >
                            <span className="worktree-node__icon">
                              {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                            </span>
                            <span className="worktree-node__label">{label}</span>
                            <span className="worktree-node__scope-add" onClick={(e) => { e.stopPropagation(); void handleAddToScope(key); }} title={t("heartbeat.addTaskToScope", { name: label })}>
                              <Plus size={12} strokeWidth={2.5} />
                            </span>
                          </div>

                          {/* ── Tasks under group (depth 1: 14 + 16 = 30px indent) ── */}
                          {isExpanded && groupTasks.map(({ task, nextRunAt }) => {
                            const isSelected = detailOpen && editing?.id === task.id;
                            const nextRun = formatTaskNextRun(nextRunAt, now, t);
                            return (
                              <div
                                key={task.id}
                                className={`worktree-node worktree-node--task${task.enabled ? "" : " worktree-node--paused"}${isSelected ? " worktree-node--selected" : ""}`}
                                style={{ paddingLeft: "21px" }}
                                onClick={() => handleEdit(task)}
                              >
                                <div className="worktree-node__main">
                                  <span className="worktree-node__marker">
                                    <Tooltip
                                      label={task.enabled ? t("heartbeat.clickPause") : t("heartbeat.clickStart")}
                                      side="top"
                                      delay={60}
                                    >
                                      <button
                                        className={`worktree-node__toggle${task.enabled ? " worktree-node__toggle--on" : ""}`}
                                        type="button"
                                        onClick={(e) => { e.stopPropagation(); void handleToggle(task); }}
                                      >
                                        {task.enabled ? (
                                          <>
                                            <Circle className="worktree-node__toggle-circle" size={15} strokeWidth={2.4} />
                                            <CirclePause className="worktree-node__toggle-hover" size={15} strokeWidth={2.4} />
                                          </>
                                        ) : (
                                          <CirclePlaySolid size={15} />
                                        )}
                                      </button>
                                    </Tooltip>
                                  </span>
                                  <span className="worktree-node__label">{task.title || t("heartbeat.untitled")}</span>
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
                                        onOpenTopic(task.scope || "global", task.workspaceRoot || "", task.topicId);
                                      }
                                    }}
                                    title={task.topicId ? t("heartbeat.openTopic") : ""}
                                  >
                                    <MessageSquare size={14} strokeWidth={1.9} />
                                  </button>
                                  <button
                                    className="worktree-node__action-btn"
                                    type="button"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      menuAnchorRef.current = e.currentTarget;
                                      setMenuTaskId(task.id);
                                    }}
                                    title={t("common.moreActions")}
                                  >
                                    <MoreHorizontal size={14} strokeWidth={1.9} />
                                  </button>
                                </span>
                                </div>
                                <div className="worktree-node__meta">
                                  <span className="worktree-node__interval">{formatInterval(task.interval, t)}{nextRun ? ` · ${nextRun}` : ""}</span>
                                </div>
                              </div>
                            );
                          })}
                        </div>
                      );
                    })}
                  </div>
                );
              })()}
              {statusFilter === "all" && !searchQuery && scopeFilter === "all" && suggestions(t).some((sug) => !tasks.some((task) => task.title === sug.title)) && (
                <div className="heartbeat-suggestions">
                  <div className="heartbeat-suggestions__header">
                    <Lightbulb size={13} />
                    <span>{t("heartbeat.suggestions")}</span>
                    <span className="heartbeat-suggestions__hint">{t("heartbeat.suggestionsHint")}</span>
                  </div>
                  {suggestions(t)
                    .filter((sug) => !tasks.some((task) => task.title === sug.title))
                    .map((sug) => (
                      <button
                        key={sug.id}
                        className="heartbeat-suggestion"
                        type="button"
                        onClick={() => void handleAddSuggestion(sug)}
                      >
                        <div className="heartbeat-suggestion__main">
                          <span className="heartbeat-suggestion__title">{sug.title}</span>
                          <span className="heartbeat-suggestion__freq">{sug.freqLabel}</span>
                        </div>
                        <span className="heartbeat-suggestion__desc">{sug.desc}</span>
                      </button>
                    ))}
                </div>
              )}
              </div>
            </div>
            <div className={`heartbeat-scrollbar${detailOpen ? "" : " heartbeat-scrollbar--edge"}`} aria-hidden="true" ref={scrollbarRef}>
              <div
                ref={thumbRef}
                className="heartbeat-scrollbar__thumb"
                onMouseDown={onScrollThumbMouseDown}
              />
            </div>
          </div>

          {/* 任务行 ⋯ 菜单：仅删除任务 */}
          <AnchoredPopover
            open={menuTaskId !== null}
            anchorRef={menuAnchorRef}
            onClose={() => setMenuTaskId(null)}
            className="heartbeat-task-menu"
            placement="bottom"
          >
            <button
              className="heartbeat-task-menu__item heartbeat-task-menu__item--danger"
              type="button"
              onClick={() => {
                if (menuTaskId) void handleDelete(menuTaskId);
                setMenuTaskId(null);
              }}
            >
              <Trash2 size={13} />
              <span>{t("common.delete")}</span>
            </button>
          </AnchoredPopover>

          {/* ── Vertical divider (draggable, visible only when detail open) ── */}
          {detailOpen && (
            <div className="heartbeat-split__divider" onMouseDown={onDividerMouseDown} />
          )}

          {/* ── Right column: detail / editor (ChatGPT-style, opens on task click) ── */}
          {detailOpen && (
            <div className="heartbeat-split__right">
              {editing ? (
                <TaskEditor key={editing.id} task={editing} onSave={handleSaveEdit} onDelete={async () => {
                  const deleted = await handleDelete(editing.id);
                  if (deleted) {
                    setEditing(null);
                    setDetailOpen(false);
                  }
                  return deleted;
                }} onCloseDetail={() => { setDetailOpen(false); }} onDirtyChange={(d) => { dirtyRef.current = d; }} onOpenTopic={onOpenTopic} onTrigger={handleTrigger} />
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
          )}
        </div>
        {mutationError && (
          <div className="heartbeat-editor__save-notice" role="alert">
            <span className="heartbeat-editor__save-error">{t("heartbeat.saveFailed")}</span>
          </div>
        )}
      </div>
  );
}

// ── Cycle Editor ──────────────────────────────────────────────────────────────


// ── 建议区：预填的自动化任务草稿 ──
interface Suggestion {
  id: string;
  title: string;
  desc: string;
  prompt: string;
  interval: string;
  freqLabel: string;
}

function suggestions(t: HeartbeatTranslator): Suggestion[] {
  return [
    {
      id: "daily-review",
      title: t("heartbeat.sugDailyReview"),
      desc: t("heartbeat.sugDailyReviewDesc"),
      prompt: t("heartbeat.sugDailyReviewPrompt"),
      interval: "24h|daily@20:00",
      freqLabel: t("heartbeat.sugDailyReviewFreq"),
    },
    {
      id: "product-update",
      title: t("heartbeat.sugProductUpdate"),
      desc: t("heartbeat.sugProductUpdateDesc"),
      prompt: t("heartbeat.sugProductUpdatePrompt"),
      interval: "24h|daily@12:00",
      freqLabel: t("heartbeat.sugProductUpdateFreq"),
    },
    {
      id: "downloads-report",
      title: t("heartbeat.sugDownloads"),
      desc: t("heartbeat.sugDownloadsDesc"),
      prompt: t("heartbeat.sugDownloadsPrompt"),
      interval: "168h|weekly:fri@16:00",
      freqLabel: t("heartbeat.sugDownloadsFreq"),
    },
  ];
}

// ── Editor ─────────────────────────────────────────────────────────────────────
