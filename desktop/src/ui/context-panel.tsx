import { invoke } from "@tauri-apps/api/core";
import { openPath } from "@tauri-apps/plugin-opener";
<<<<<<< feat/desktop-markdown-plan-memory
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ActivePlan, ArchivedPlan, PendingPlan, SessionFile, Settings, UsageStats } from "../App";
=======
import { useMemo, useState } from "react";
import type { SessionFile, Settings, UsageStats } from "../App";
>>>>>>> main
import { Markdown } from "../Markdown";
import { t, useLang } from "../i18n";
import { I } from "../icons";
import type { McpSpecInfo, MemoryDetail, MemoryEntryInfo } from "../protocol";
import { PanelErrorBoundary } from "./error-boundary";

export type CtxTab = "files" | "tools" | "memory" | "rules" | "plan";

const CONTEXT_MAX_TOKENS = 1_000_000;

export function ContextPanel({
  settings,
  usage,
  mcpSpecs,
  mcpBridged,
  sessionFiles,
  memory,
  memoryDetail,
  onReadMemory,
  activePlan,
  pendingPlans,
  planArchived,
  activeTab,
  onTabChange,
  selectedPlanIdx: externalSelectedPlanIdx,
  onPlanSelect,
}: {
  settings: Settings | null;
  usage: UsageStats;
  mcpSpecs: McpSpecInfo[];
  mcpBridged: boolean;
  sessionFiles: SessionFile[];
  memory: MemoryEntryInfo[];
  memoryDetail: MemoryDetail | null;
  onReadMemory: (path: string) => void;
  activePlan: ActivePlan | null;
  pendingPlans: PendingPlan[];
  planArchived: ArchivedPlan[];
  activeTab?: CtxTab;
  onTabChange?: (tab: CtxTab) => void;
  selectedPlanIdx?: number | null;
  onPlanSelect?: (idx: number | null) => void;
}) {
  useLang();
  const [tab, setTabInternal] = useState<CtxTab>("files");
  const [internalSelectedPlanIdx, setInternalSelectedPlanIdx] = useState<number | null>(null);

  const selectedPlanIdx = externalSelectedPlanIdx !== undefined ? externalSelectedPlanIdx : internalSelectedPlanIdx;
  const setSelectedPlanIdx = onPlanSelect ?? setInternalSelectedPlanIdx;

  const setTab = useCallback(
    (next: CtxTab) => {
      setTabInternal(next);
      onTabChange?.(next);
    },
    [onTabChange],
  );

  // Sync external tab override
  useEffect(() => {
    if (activeTab && activeTab !== tab) setTabInternal(activeTab);
  }, [activeTab]); // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-load first memory entry when switching to the memory tab.
  const prevTabRef = useRef<CtxTab>("files");
  useEffect(() => {
    if (tab === "memory" && prevTabRef.current !== "memory" && memory.length > 0 && !memoryDetail) {
      onReadMemory(memory[0]!.path);
    }
    prevTabRef.current = tab;
  }, [tab, memory, memoryDetail, onReadMemory]);
  const reserved = usage.reservedTokens;
  const lastHit = usage.lastCallCacheHit ?? 0;
  const lastMiss = usage.lastCallCacheMiss ?? 0;
  const observedLog = Math.max(0, lastHit + lastMiss - reserved);
  const logTokens = Math.max(usage.liveLogTokens, observedLog);
  const cached = Math.min(logTokens, Math.max(0, lastHit - reserved));
  const used = Math.max(0, logTokens - cached);
  const reservedPct = Math.min(100, (reserved / CONTEXT_MAX_TOKENS) * 100);
  const usedPct = Math.min(100, (used / CONTEXT_MAX_TOKENS) * 100);
  const cachedPct = Math.min(100, (cached / CONTEXT_MAX_TOKENS) * 100);
  const free = Math.max(0, CONTEXT_MAX_TOKENS - reserved - used - cached);
  return (
    <aside className="ctx">
      <div className="ctx-tabs">
        <div className="ctx-tab" data-active={tab === "files"} onClick={() => setTab("files")}>
          {t("contextPanel.filesTab")}
        </div>
        <div className="ctx-tab" data-active={tab === "tools"} onClick={() => setTab("tools")}>
          {t("contextPanel.toolsTab")}
        </div>
        <div className="ctx-tab" data-active={tab === "memory"} onClick={() => setTab("memory")}>
          {t("contextPanel.memoryTab")}
        </div>
        <div className="ctx-tab" data-active={tab === "rules"} onClick={() => setTab("rules")}>
          {t("contextPanel.rulesTab")}
        </div>
        <div className="ctx-tab" data-active={tab === "plan"} onClick={() => setTab("plan")}>
          {t("contextPanel.planTab")}
        </div>
      </div>

      <div className="ctx-body">
        <div className="ctx-block ctx-body-tokens">
          <div className="h">
            <span>{t("contextPanel.contextTokens")}</span>
            <span className="right">
              {(reserved + used + cached).toLocaleString()} /{" "}
              {CONTEXT_MAX_TOKENS.toLocaleString()}
            </span>
          </div>
          <div className="meter">
            <span className="rsvd" style={{ width: `${reservedPct}%` }} />
            <span className="cached" style={{ width: `${cachedPct}%` }} />
            <span className="used" style={{ width: `${usedPct}%` }} />
          </div>
          <div className="legend">
            <span className="l">
              <span className="sw r" />
              {t("contextPanel.reservedKey")} <span className="v">{reserved.toLocaleString()}</span>
            </span>
            <span className="l">
              <span className="sw c" />
              {t("contextPanel.cacheKey")} <span className="v">{cached.toLocaleString()}</span>
            </span>
            <span className="l">
              <span className="sw u" />
              {t("contextPanel.usedKey")} <span className="v">{used.toLocaleString()}</span>
            </span>
            <span className="l">
              {t("contextPanel.freeKey")} <span className="v">{free.toLocaleString()}</span>
            </span>
          </div>
        </div>

        <div className="ctx-body-tab">
          <PanelErrorBoundary key={tab} label={tab}>
            {tab === "files" && <CtxFiles files={sessionFiles} settings={settings} />}
            {tab === "tools" && <CtxTools specs={mcpSpecs} bridged={mcpBridged} />}
            {tab === "memory" && (
              <CtxMemory entries={memory} detail={memoryDetail} onRead={onReadMemory} />
            )}
            {tab === "rules" && <CtxRules settings={settings} />}
            {tab === "plan" && (
              <CtxPlan
                activePlan={activePlan}
                pendingPlans={pendingPlans}
                planArchived={planArchived}
                selectedIdx={selectedPlanIdx}
                onSelect={(idx) => setSelectedPlanIdx(idx === selectedPlanIdx ? null : idx)}
              />
            )}
          </PanelErrorBoundary>
        </div>
      </div>
    </aside>
  );
}

type TreeNode =
  | { kind: "dir"; depth: number; name: string; key: string }
  | { kind: "file"; depth: number; name: string; path: string; key: string; status: "c" | "m" };

async function openContextFile(path: string, settings: Settings | null): Promise<void> {
  const workspaceDir = settings?.workspaceDir;
  const isWindows = workspaceDir?.includes("\\") ?? false;
  const sep = isWindows ? "\\" : "/";
  const abs =
    workspaceDir && !/^[a-zA-Z]:[\\/]/.test(path) && !path.startsWith("/")
      ? `${workspaceDir.replace(/[\\/]$/, "")}${sep}${path.replace(/^[\\/]+/, "").replace(/\//g, sep)}`
      : isWindows
        ? path.replace(/\//g, "\\")
        : path;
  const editor = settings?.editor?.trim();
  if (editor) {
    await invoke("open_in_editor", { command: editor, path: abs, line: null });
    return;
  }
  await openPath(abs);
}

function buildSessionTree(files: SessionFile[]): TreeNode[] {
  const sorted = [...files].sort((a, b) =>
    a.path.replace(/\\/g, "/").localeCompare(b.path.replace(/\\/g, "/")),
  );
  const out: TreeNode[] = [];
  const seenDirs = new Set<string>();
  for (const f of sorted) {
    const displayPath = f.path.replace(/\\/g, "/");
    const parts = displayPath.split("/").filter(Boolean);
    if (parts.length === 0) continue;
    let prefix = "";
    for (let i = 0; i < parts.length - 1; i++) {
      const seg = parts[i] ?? "";
      prefix = prefix ? `${prefix}/${seg}` : seg;
      if (!seenDirs.has(prefix)) {
        seenDirs.add(prefix);
        out.push({ kind: "dir", depth: i, name: seg, key: `d:${prefix}` });
      }
    }
    const leaf = parts[parts.length - 1] ?? "";
    out.push({
      kind: "file",
      depth: parts.length - 1,
      name: leaf,
      path: displayPath,
      key: `f:${f.path}`,
      status: f.status,
    });
  }
  return out;
}

function CtxFiles({ files, settings }: { files: SessionFile[]; settings: Settings | null }) {
  const tree = useMemo(() => buildSessionTree(files), [files]);
  return (
    <div className="ctx-block">
      <div className="h">
        <span>{t("contextPanel.filesTitle")}</span>
        <span className="right">
          {files.length === 0 ? "—" : t("contextPanel.filesCount", { count: files.length })}
        </span>
      </div>
      <div className="tree">
        {files.length === 0 ? (
          <div className="ctx-empty">{t("contextPanel.noFilesMsg")}</div>
        ) : (
          tree.map((n) =>
            n.kind === "dir" ? (
              <div
                className="node"
                key={n.key}
                data-d={n.depth}
                data-kind="dir"
                style={{ paddingLeft: 4 + n.depth * 14 }}
              >
                <span className="ico">
                  <I.folder size={12} />
                </span>
                <span className="nm">{n.name}/</span>
              </div>
            ) : (
              <div
                className="node"
                key={n.key}
                data-d={n.depth}
                data-kind="file"
                title={n.path}
                style={{ paddingLeft: 4 + n.depth * 14 }}
              >
                <span className="ico">
                  <I.file size={12} />
                </span>
                <span className="node-text">
                  <span className="nm">{n.name}</span>
                  <span className="full-path">{n.path}</span>
                </span>
                <span
                  className="dot"
                  data-s={n.status}
                  title={n.status === "m" ? t("contextPanel.fileModified") : t("contextPanel.fileInContext")}
                />
                <button
                  type="button"
                  className="tree-action"
                  aria-label={t("contextPanel.openFile", { path: n.path })}
                  title={t("contextPanel.openFile", { path: n.path })}
                  onClick={() => void openContextFile(n.path, settings)}
                >
                  <I.file size={12} />
                </button>
                <button
                  type="button"
                  className="tree-action"
                  aria-label={t("contextPanel.copyPath", { path: n.path })}
                  title={t("contextPanel.copyPath", { path: n.path })}
                  onClick={() => void navigator.clipboard?.writeText(n.path)}
                >
                  <I.copy size={12} />
                </button>
              </div>
            ),
          )
        )}
      </div>
    </div>
  );
}

function CtxTools({ specs, bridged }: { specs: McpSpecInfo[]; bridged: boolean }) {
  const readyCount = specs.filter((s) => s.status === "connected").length;
  return (
    <div className="ctx-block">
      <div className="h">
        <span>{t("contextPanel.mcpTitle")}</span>
        <span className="right">
          {specs.length === 0
            ? "—"
            : bridged
              ? t("contextPanel.mcpReadyAll", { count: specs.length })
              : t("contextPanel.mcpReadySome", { ready: readyCount, count: specs.length })}
        </span>
      </div>
      {specs.length === 0 ? (
        <div className="ctx-empty">{t("contextPanel.mcpEmpty")}</div>
      ) : (
        specs.map((s) => {
          const dot =
            s.status === "connected"
              ? "ok"
              : s.status === "failed" || s.parseError
                ? "off"
                : "pending";
          const suffix = s.statusReason
            ? ` · ${s.statusReason}`
            : s.status === "connected"
              ? typeof s.toolCount === "number"
                ? ` · ${t("contextPanel.mcpTools", { count: s.toolCount })}`
                : ` · ${t("contextPanel.mcpReady")}`
              : s.status === "handshake"
                ? ` · ${t("contextPanel.mcpConnecting")}`
                : s.status === "disabled"
                  ? ` · ${t("contextPanel.mcpDisabled")}`
                  : s.status === "failed"
                    ? ` · ${t("contextPanel.mcpFailed")}`
                    : ` · ${t("contextPanel.mcpConfigured")}`;
          return (
            <div className="mcp-row" key={s.raw}>
              <span className="ico">
                <I.wrench size={12} />
              </span>
              <div className="body">
                <div className="n">{s.name ?? s.summary}</div>
                <div className="m">
                  {s.transport}
                  {suffix}
                </div>
              </div>
              <span className="status" data-s={dot} />
            </div>
          );
        })
      )}
    </div>
  );
}

function CtxMemory({
  entries,
  detail,
  onRead,
}: {
  entries: MemoryEntryInfo[];
  detail: MemoryDetail | null;
  onRead: (path: string) => void;
}) {
  return (
    <div className="ctx-block" style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }}>
      <div className="h">
        <span>{t("contextPanel.memoryTitle")}</span>
        <span className="right">
          {entries.length === 0 ? "—" : t("contextPanel.itemCount", { count: entries.length })}
        </span>
      </div>
      {entries.length === 0 ? (
        <div className="ctx-empty">{t("contextPanel.noMemoriesMsg")}</div>
      ) : (
        <div className="mem" style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }}>
          <div style={{ flexShrink: 0 }}>
            {entries.map((m) => (
              <button
                type="button"
                className="mem-row"
                data-active={detail?.path === m.path}
                key={m.path}
                onClick={() => onRead(m.path)}
              >
                <span className="scope" data-s={m.scope}>
                  {m.scope === "project" ? t("contextPanel.scopeProject") : t("contextPanel.scopeGlobal")}
                </span>
                <span className="txt">{m.description || m.name}</span>
              </button>
            ))}
          </div>
          {detail ? (
            <div className="mem-detail">
              <Markdown source={detail.body} />
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}

function CtxRules({ settings }: { settings: Settings | null }) {
  const editMode = settings?.editMode ?? "review";
  const items: { p: string; allow: boolean; desc: string }[] =
    editMode === "yolo"
      ? [{ p: "*", allow: true, desc: t("contextPanel.ruleYolo") }]
      : editMode === "auto"
        ? [
            { p: "read_file, list_directory, search_files, *", allow: true, desc: t("contextPanel.ruleReadOnly") },
            { p: "run_command (allowlist)", allow: true, desc: t("contextPanel.ruleShellAllowlist") },
            { p: "edit_file, write_file, run_command (other)", allow: false, desc: t("contextPanel.ruleWritesAsk") },
          ]
        : [
            { p: "*", allow: false, desc: t("contextPanel.ruleReview") },
          ];
  return (
    <div className="ctx-block">
      <div className="h">
        <span>{t("contextPanel.autoApproveTitle")}</span>
        <span className="right">{editMode}</span>
      </div>
      {items.map((r) => (
        <div className="rule" key={r.p}>
          <div className="top">
            <span className={`pat ${r.allow ? "" : "deny"}`}>{r.p}</span>
            <span className={`sw ${r.allow ? "" : "deny"}`}>
              {r.allow ? t("contextPanel.allow") : t("contextPanel.ask")}
            </span>
          </div>
          <div className="desc">{r.desc}</div>
        </div>
      ))}
    </div>
  );
}

type PlanEntry =
  | { kind: "active"; plan: ActivePlan }
  | { kind: "pending"; plan: PendingPlan }
  | { kind: "archived"; plan: ArchivedPlan; index: number };

function CtxPlan({
  activePlan,
  pendingPlans,
  planArchived,
  selectedIdx,
  onSelect,
}: {
  activePlan: ActivePlan | null;
  pendingPlans: PendingPlan[];
  planArchived: ArchivedPlan[];
  selectedIdx: number | null;
  onSelect: (idx: number) => void;
}) {
  const [tooltip, setTooltip] = useState<{ text: string; x: number; y: number } | null>(null);

  const entries: PlanEntry[] = [];
  if (activePlan) entries.push({ kind: "active", plan: activePlan });
  for (const p of pendingPlans) entries.push({ kind: "pending", plan: p });
  for (let i = 0; i < planArchived.length; i++) {
    entries.push({ kind: "archived", plan: planArchived[i]!, index: i });
  }

  // Highlight: first active/pending plan when nothing explicit is selected.
  const effectiveIdx =
    selectedIdx !== null
      ? selectedIdx
      : entries.findIndex((e) => e.kind === "active" || e.kind === "pending");

  // Detail is only shown when the user (or system) explicitly selects an entry.
  const showDetail = selectedIdx !== null && effectiveIdx >= 0;
  const selected = showDetail ? (entries[effectiveIdx] ?? null) : null;

  const detailBody =
    selected?.kind === "active" || selected?.kind === "pending"
      ? (selected.plan as ActivePlan | PendingPlan).plan
      : selected?.kind === "archived"
        ? (selected.plan as ArchivedPlan).plan
        : null;

  const isSelectedRefining =
    selected?.kind === "pending" && (selected.plan as PendingPlan).refining === true;

  return (
    <div
      className="ctx-block"
      style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }}
    >
      <div className="h">
        <span>{t("contextPanel.planTitle")}</span>
        <span className="right">
          {entries.length === 0 ? "—" : t("contextPanel.itemCount", { count: entries.length })}
        </span>
      </div>
      {entries.length === 0 ? (
        <div className="ctx-empty">{t("contextPanel.noPlansMsg")}</div>
      ) : (
        <div className={`mem${showDetail ? " mem--split" : ""}`}>
          <div className="mem-list">
            {entries.map((e, i) => {
              const summary =
                e.kind === "active" || e.kind === "pending"
                  ? (e.plan as ActivePlan | PendingPlan).summary
                  : (e.plan as ArchivedPlan).summary;
              const isRefining =
                e.kind === "pending" && (e.plan as PendingPlan).refining === true;
              const label =
                e.kind === "active"
                  ? t("contextPanel.planActive")
                  : isRefining
                    ? t("contextPanel.planRefining")
                    : e.kind === "pending"
                      ? t("contextPanel.planPending")
                      : (e.plan as ArchivedPlan).status === "cancelled"
                        ? t("contextPanel.planCancelled")
                        : t("contextPanel.planArchived");
              const tone =
                e.kind === "active"
                  ? "ok"
                  : isRefining
                    ? "accent"
                    : e.kind === "pending"
                      ? "warn"
                      : (e.kind === "archived" && (e.plan as ArchivedPlan).status === "cancelled")
                        ? "err"
                        : "muted";
              const tipText = summary || t("contextPanel.planUntitled");
              return (
                <button
                  type="button"
                  className="mem-row"
                  data-active={effectiveIdx === i}
                  data-compact={showDetail}
                  key={`${e.kind}-${i}`}
                  onClick={() => onSelect(i)}
                  onMouseEnter={
                    showDetail
                      ? (ev) => {
                          const r = ev.currentTarget.getBoundingClientRect();
                          setTooltip({ text: tipText, x: r.right + 8, y: r.top + r.height / 2 });
                        }
                      : undefined
                  }
                  onMouseLeave={showDetail ? () => setTooltip(null) : undefined}
                >
                  <span className="scope" data-s={tone}>
                    {label}
                  </span>
                  <span className="txt">{tipText}</span>
                </button>
              );
            })}
          </div>
          {detailBody ? (
            <div className="mem-detail">
              {isSelectedRefining && (
                <div className="plan-refining-bar">
                  <span className="plan-refining-dot" />
                  {t("contextPanel.planRefiningInProgress")}
                </div>
              )}
              <Markdown source={detailBody} />
            </div>
          ) : null}
        </div>
      )}
      {tooltip && (
        <div
          className="plan-name-tooltip"
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          {tooltip.text}
        </div>
      )}
    </div>
  );
}
