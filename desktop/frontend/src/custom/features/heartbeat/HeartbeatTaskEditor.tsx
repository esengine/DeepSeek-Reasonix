import { useManagementT } from "../../../lib/managementLocale";
import { automationDraftDirty, useAutomationDraftStore } from "../../../store/automationDrafts";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { Check, ChevronsUpDown, CirclePause, Play, Trash2, X } from "lucide-react";
import { Tooltip } from "../../../components/Tooltip";
import { app } from "../../../lib/bridge";
import type { WorkspaceView } from "../../../lib/types";
import { CycleEditor } from "./HeartbeatCycleEditor";
import { CirclePlaySolid, mergeEngineRunState } from "./HeartbeatShared";
import { useHeartbeatT } from "./heartbeat.i18n";
import { changeHeartbeatFrequency, describeCron, formatCronNext, formatRelativeTime, isCronExpr, nextCronRunAt, type HeartbeatFrequencyType } from "./heartbeat.presentation";
import type { HeartbeatTask } from "./heartbeat.types";

function normalizeMode(mode: "ask" | "auto" | "yolo" | undefined): "ask" | "auto" | "yolo" {
  if (mode === "ask" || mode === "auto" || mode === "yolo") return mode;
  return "yolo"; // default
}

export function TaskEditor({
  task,
  onSave,
  onDelete,
  onCloseDetail,
  onDirtyChange,
  onOpenTopic,
  onTrigger, onDiscard, onToggleEnabled, onSaveAsNew,
}: {
  onDiscard?: () => void;
  onToggleEnabled?: () => Promise<boolean>;
  onSaveAsNew?: (task: HeartbeatTask) => Promise<boolean>;
  task: HeartbeatTask;
  onSave: (t: HeartbeatTask) => Promise<boolean>;
  onDelete: () => Promise<boolean>;
  onCloseDetail: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onOpenTopic?: (scope: string, workspaceRoot: string, topicId: string) => void;
  onTrigger?: (id: string) => void;
}) {
  const t = useHeartbeatT();
  const m = useManagementT();
  const entry = useAutomationDraftStore((state) => state.entries[task.id]);
  const managed = Boolean(onDiscard);
  const [localTab, setLocalTab] = useState<"configuration" | "history">("configuration");
  const tab = managed ? entry?.tab ?? "configuration" : localTab;
  const setTab = (value: "configuration" | "history") => managed ? useAutomationDraftStore.getState().ui(task.id, { tab: value }) : setLocalTab(value);
  const titleRef = useRef<HTMLInputElement>(null);
  const [workspaces, setWorkspaces] = useState<WorkspaceView[]>([]);
  const [projectOpen, setProjectOpen] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [localSaving, setSaving] = useState(false);
  const saving = localSaving || (managed && Boolean(entry?.busy));
  const [saveError, setSaveError] = useState(false);
  const [frequencyError, setFrequencyError] = useState(false);
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

  const [localDraft, setLocalDraft] = useState(task);
  const draft = managed && entry ? entry.draft : localDraft;
  const setDraft = useCallback((update: HeartbeatTask | ((task: HeartbeatTask) => HeartbeatTask)) => {
    if (managed) useAutomationDraftStore.getState().edit(task.id, (current) => typeof update === "function" ? update(current) : update);
    else setLocalDraft(update);
  }, [managed, task.id]);
  const initialTaskRef = useRef(task);
  // 保存后父组件 setEditing({...task}) 传入新引用，同步基线使 isDirty
  // 复位（保存按钮与 dirtyRef 不再保持脏状态）。
  useEffect(() => {
    initialTaskRef.current = task;
  }, [task]);
  // Sync fields owned outside this editor without replacing user-owned draft
  // input. In particular, TriggerNow may advance topicId/lastRunAt/runHistory
  // while the user is editing title, prompt, or schedule.
  useEffect(() => {
    if (managed) return;
    setDraft((current) => mergeEngineRunState({ ...current, enabled: task.enabled }, task));
  }, [managed, setDraft, task.enabled, task.lastRunAt, task.runHistory, task.topicId]);
  const isNew = managed ? !entry?.baseline : !task.createdAt;
  const isDirty = managed && entry ? automationDraftDirty(entry) : draft.title !== initialTaskRef.current.title
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

  // 手动保存（ChatGPT 式）：修改后底部出现取消/保存，无修改时不显示。
  // enabled 开关走头部即时保存并同步基线，不进入 isDirty。
  const handleCancel = useCallback(() => {
    setSaveError(false);
    if (onDiscard) onDiscard(); else setDraft(initialTaskRef.current);
  }, [onDiscard, setDraft]);

  const handleSave = useCallback(async () => {
    if (!draft.title.trim() || !draft.prompt.trim()) return;
    setSaving(true);
    const saved = await onSave(draft);
    setSaving(false);
    setSaveError(!saved);
  }, [draft, onSave]);
  const set = useCallback((field: keyof HeartbeatTask, value: string | boolean) => {
    setDraft((prev) => ({ ...prev, [field]: value }));
  }, [setDraft]);

  // 启用/暂停切换（状态文字入口 + 右侧按钮共用）：
  // 只持久化 enabled 变更，基于最近保存基线（initialTaskRef）翻转，
  // 不携带 draft 中尚未保存的 title/prompt/schedule 编辑；同时保留草稿，
  // 等待中的用户输入不被基线快照覆盖。
  const toggleEnabled = useCallback(async () => {
    const saved = initialTaskRef.current;
    const updated = { ...saved, enabled: !saved.enabled };
    setSaving(true);
    const persisted = onToggleEnabled ? await onToggleEnabled() : await onSave(updated);
    setSaving(false);
    setSaveError(!persisted);
    if (persisted && !managed) {
      // 只同步 enabled（磁盘已持久化），title/prompt/interval 等草稿编辑保留。
      setDraft((prev) => ({ ...prev, enabled: updated.enabled }));
      initialTaskRef.current = { ...saved, enabled: updated.enabled };
    }
  }, [managed, onSave, onToggleEnabled, setDraft]);

  // Detect frequency type from interval value
  const [localFrequency, setLocalFrequency] = useState<HeartbeatFrequencyType>(
    (() => {
      const iv = task.interval || "";
      if (isCronExpr(iv)) return "cron";
      const m = iv.match(/^(\d+)[smh]\|(daily|weekly|biweekly|monthly|yearly)/);
      if (m) return m[2] as "daily" | "weekly" | "biweekly" | "monthly" | "yearly";
      return "interval";
    })()
  );

  const freqType = managed ? entry?.frequency ?? localFrequency : localFrequency;
  const setFreqType = useCallback((frequency: HeartbeatFrequencyType) => {
    if (managed) useAutomationDraftStore.getState().ui(task.id, { frequency }); else setLocalFrequency(frequency);
  }, [managed, task.id]);

  // 切换频率类型时重建 interval（摊开的 7 个选项）
  const onFreqSelect = useCallback((ft: HeartbeatFrequencyType) => {
    const converted = changeHeartbeatFrequency(draft, ft);
    if (converted === null) {
      setFrequencyError(true);
      return;
    }
    setFrequencyError(false);
    setDraft(converted);
    setFreqType(ft);
  }, [draft, setDraft, setFreqType]);

  const selectedWorkspace = draft.scope === "project" && draft.workspaceRoot
    ? workspaces.find((w) => w.path === draft.workspaceRoot)
    : null;

  return (
    <div className="heartbeat-editor">
      {/* Header: 状态文字（点击切换，CodeX 式）+ 操作菜单 + 关闭 */}
      <header className="heartbeat-editor__header">
        {isNew ? (
          <span className="heartbeat-editor__status heartbeat-editor__status--new">{t("heartbeat.newTask")}</span>
        ) : (
          <button
            className={`heartbeat-editor__status${draft.enabled ? " heartbeat-editor__status--on" : ""}`}
            type="button"
            title={draft.enabled ? t("heartbeat.statusDisabled") : t("heartbeat.statusEnabled")}
            disabled={saving}
            onClick={() => void toggleEnabled()}
          >
            {draft.enabled ? t("heartbeat.statusEnabled") : t("heartbeat.statusDisabled")}
          </button>
        )}
        <span className="heartbeat-editor__header-spacer" />
        {!isNew && (
          <Tooltip
            label={t("heartbeat.runNow")}
            side="top"
            delay={60}
          >
            <button
              className="heartbeat-editor__header-action"
              type="button"
              onClick={() => { if (onTrigger) onTrigger(task.id); }}
            >
              <Play size={14} strokeWidth={1.9} />
              {t("heartbeat.runNow")}
            </button>
          </Tooltip>
        )}
        {!isNew && (
          <Tooltip
            label={draft.enabled ? t("heartbeat.clickPause") : t("heartbeat.clickStart")}
            side="top"
            delay={60}
          >
            <button
              className={`heartbeat-editor__header-action${draft.enabled ? " heartbeat-editor__header-action--on" : ""}`}
              type="button"
              disabled={saving}
              onClick={() => void toggleEnabled()}
            >
              {draft.enabled ? (
                <CirclePause size={14} strokeWidth={2.4} />
              ) : (
                <CirclePlaySolid size={14} />
              )}
              {draft.enabled ? t("heartbeat.btnPause") : t("heartbeat.btnStart")}
            </button>
          </Tooltip>
        )}
        {!isNew && (
          <Tooltip
            label={confirmingDelete ? t("heartbeat.confirmDelete") : t("heartbeat.delete")}
            side="top"
            delay={60}
          >
            <button
              className={`heartbeat-editor__header-action heartbeat-editor__header-action--danger${confirmingDelete ? " heartbeat-editor__header-action--confirm" : ""}`}
              type="button"
              disabled={saving}
              onClick={() => {
                if (managed || confirmingDelete) {
                  void onDelete();
                } else {
                  setConfirmingDelete(true);
                  window.setTimeout(() => setConfirmingDelete(false), 3000);
                }
              }}
            >
              <Trash2 size={14} />
              {confirmingDelete ? t("heartbeat.confirmDelete") : t("heartbeat.delete")}
            </button>
          </Tooltip>
        )}
        <button className="heartbeat-editor__close" type="button" onClick={onCloseDetail} title={t("common.close")}>
          <X size={14} />
        </button>
      </header>

      {/* Fields: 表单滚动区 */}
      {isDirty && !isNew && <div className="management-notice">{m("savedRun")}</div>}
      {managed && entry && (entry.missing || entry.conflicts.length > 0) && <div className="management-notice" role="alert">
        {m(entry.missing ? "missingTask" : "conflict")}
        {!entry.missing && <button className="btn btn--small" disabled={saving} onClick={handleCancel}>{m("reloadTask")}</button>}
        <button className="btn btn--small" disabled={saving || !draft.title.trim() || !draft.prompt.trim()} onClick={() => void onSaveAsNew?.(draft)}>{m("saveAsNew")}</button>
      </div>}
      <div className="heartbeat-detail-tabs" role="tablist" aria-label={t("heartbeat.detailTitle")}>
        <button role="tab" aria-selected={tab === "configuration"} onClick={() => setTab("configuration")}>{m("configuration")}</button>
        <button role="tab" aria-selected={tab === "history"} onClick={() => setTab("history")}>{t("heartbeat.runHistory")}</button>
      </div>
      <div className="heartbeat-editor__fields">
      <fieldset className="automation-fields" disabled={saving} hidden={tab !== "configuration"}>
      {/* Title: 隐形输入框——无边框大标题样式，点击仍可直接编辑 */}
      <input
        ref={titleRef}
        className="heartbeat-editor__title"
        value={draft.title}
        onChange={(e) => set("title", e.target.value)}
        placeholder={t("heartbeat.titlePlaceholder")}
        aria-label={t("heartbeat.fieldTitle")}
      />

      {/* Scope：仅新建任务时可选项目，保存后锁定（已创建任务不显示项目字段） */}
      {isNew && (
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
      )}

      {/* Prompt（无字段标题） */}
      <div className="heartbeat-editor__field">
        <textarea
          className="heartbeat-editor__textarea"
          value={draft.prompt}
          onChange={(e) => set("prompt", e.target.value)}
          placeholder={t("heartbeat.promptPlaceholder")}
          rows={5}
        />
      </div>

      {/* Approval Mode（竖排） */}
      <div className="heartbeat-editor__field">
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
        <div className="heartbeat-editor__field">
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
        <div className="set-seg" style={{ alignSelf: "flex-start", flexWrap: "wrap" }}>
          {([
            ["interval", t("heartbeat.freqInterval")],
            ["daily", t("heartbeat.cycleDaily")],
            ["weekly", t("heartbeat.cycleWeekly")],
            ["biweekly", t("heartbeat.cycleBiweekly")],
            ["monthly", t("heartbeat.cycleMonthly")],
            ["yearly", t("heartbeat.cycleYearly")],
            ["cron", t("heartbeat.freqCron")],
          ] as const).map(([v, label]) => (
            <button
              key={v}
              type="button"
              className={`set-seg__btn${freqType === v ? " set-seg__btn--on" : ""}`}
              onClick={() => onFreqSelect(v)}
            >
              {label}
            </button>
          ))}
        </div>
        {frequencyError && (
          <span className="heartbeat-editor__inline-error" role="status">{t("heartbeat.frequencyConversionFailed")}</span>
        )}

        {freqType === "cron" ? (
          <div className="heartbeat-editor__freq-interval">
            <input
              className="heartbeat-editor__freq-input heartbeat-editor__freq-input--cron"
              value={draft.interval}
              onChange={(e) => setDraft((prev) => ({ ...prev, interval: e.target.value }))}
              placeholder={t("heartbeat.cronPlaceholder")}
            />
            <span className="heartbeat-editor__cron-hint">
              {describeCron(draft.interval, t)}
              {nextCronRunAt(draft.interval) ? ` ${t("heartbeat.cronNextRun")} ${formatCronNext(nextCronRunAt(draft.interval))}` : ""}
            </span>
          </div>
        ) : freqType === "interval" ? (
          <div className="heartbeat-editor__freq-interval">
            <span className="heartbeat-editor__freq-label">{t("heartbeat.freqEvery")}</span>
            <input
              className="heartbeat-editor__freq-input"
              value={(() => {
                const m = (draft.interval || "").match(/^(\d+)/);
                return m ? m[1] : "";
              })()}
              onChange={(e) => {
                const num = e.target.value.replace(/\D/g, "");
                const unit = (draft.interval || "").match(/([smh])$/)?.[1] || "h";
                setDraft((prev) => ({ ...prev, interval: num + unit }));
              }}
              placeholder="1"
            />
            <div className="set-seg">
              <button
                className={`set-seg__btn${(() => {
                  const m = (draft.interval || "").match(/^(\d+)([smh])/);
                  return (m ? m[2] : "h") === "m" ? " set-seg__btn--on" : "";
                })()}`}
                onClick={() => {
                  const num = (draft.interval || "").match(/^(\d+)/)?.[1] || "1";
                  setDraft((prev) => ({ ...prev, interval: num + "m" }));
                }}
              >
                {t("heartbeat.unitMin")}
              </button>
              <button
                className={`set-seg__btn${(() => {
                  const m = (draft.interval || "").match(/^(\d+)([smh])/);
                  return (m ? m[2] : "h") === "h" ? " set-seg__btn--on" : "";
                })()}`}
                onClick={() => {
                  const num = (draft.interval || "").match(/^(\d+)/)?.[1] || "1";
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
        ) : (
          <CycleEditor
            key={freqType}
            draft={draft}
            setDraft={set}
            cycleType={freqType as "daily" | "weekly" | "biweekly" | "monthly" | "yearly"}
          />
        )}
      </div>

      {/* 运行历史记录：每次成功执行的记录，点击可打开对应对话
          历史为空但有最近会话（task.topicId）时，用最近会话合成一条——旧任务
          在 runHistory 字段引入前执行过，topicId 仍指向最近对话 */}
      </fieldset>
      <div className="heartbeat-run-history" hidden={tab !== "history"}>
        <div className="heartbeat-run-history__header">
          <span>{t("heartbeat.runHistory")}</span>
        </div>
        {(() => {
          const history = (task.runHistory || []).length > 0
            ? [...task.runHistory!].reverse()
            : task.topicId
              ? [{ at: task.lastRunAt || task.createdAt || Date.now(), topicId: task.topicId }]
              : [];
          if (history.length === 0) {
            return <div className="heartbeat-run-history__empty">{t("heartbeat.runHistoryEmpty")}</div>;
          }
          return (
            <div className="heartbeat-run-history__list">
              {history.map((run, i) => (
                <button
                  key={`${run.at}-${i}`}
                  className="heartbeat-run-history__item"
                  type="button"
                  disabled={!run.topicId}
                  onClick={() => {
                    if (run.topicId && onOpenTopic) {
                      onOpenTopic(task.scope || "global", task.workspaceRoot || "", run.topicId);
                    }
                  }}
                  title={run.topicId ? t("heartbeat.openTopic") : ""}
                >
                  <span className="heartbeat-run-history__title">{task.title || t("heartbeat.untitled")}</span>
                  <span className="heartbeat-run-history__scope">
                    {task.scope === "project" && task.workspaceRoot
                      ? (workspaces.find((w) => w.path === task.workspaceRoot)?.name
                        || task.workspaceRoot.split("/").pop() || task.workspaceRoot)
                      : t("heartbeat.scopeGlobal")}
                  </span>
                  <span className="heartbeat-run-history__rel">{formatRelativeTime(run.at, Date.now(), t)}</span>
                  {!run.topicId && (
                    <span className="heartbeat-run-history__notopic">{t("heartbeat.runHistoryNoTopic")}</span>
                  )}
                </button>
              ))}
            </div>
          );
        })()}
      </div>
      </div>

      {/* 保存/取消：仅在有未保存修改时显示（ChatGPT 式），固定在面板底部 */}
      {(saveError || (managed && entry?.error)) && (
        <div className="heartbeat-editor__save-notice">
          <span className="heartbeat-editor__save-error" role="alert">{t("heartbeat.saveFailed")}</span>
        </div>
      )}
      {(managed || isNew || isDirty) && (
        <div className="heartbeat-editor__actions">
          {managed && <span className="automation-save-status" role="status">{m(saving ? "saving" : isDirty ? "unsaved" : "saved")}</span>}
          <button
            className="heartbeat-editor__action-btn"
            type="button"
            disabled={saving || (!isNew && !isDirty)}
            onClick={handleCancel}
          >
            {managed ? m("discard") : t("common.cancel")}
          </button>
          <button
            className="heartbeat-editor__action-btn heartbeat-editor__action-btn--primary"
            type="button"
            disabled={saving || (!isNew && !isDirty) || Boolean(managed && (entry?.missing || entry?.conflicts.length)) || !draft.title.trim() || !draft.prompt.trim()}
            onClick={() => void handleSave()}
          >
            {t("common.save")}
          </button>
        </div>
      )}
    </div>
  );
}
