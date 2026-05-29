import { invoke } from "@tauri-apps/api/core";
import { openPath } from "@tauri-apps/plugin-opener";
import { useEffect, useMemo, useState } from "react";
import type { SessionFile, Settings, UsageStats } from "../App";
import { Markdown } from "../Markdown";
import { t, useLang } from "../i18n";
import { I } from "../icons";
import type { McpSpecInfo, MemoryDetail, MemoryEntryInfo } from "../protocol";
import { PanelErrorBoundary } from "./error-boundary";
import { McpServerCard } from "./mcp-server-card";

export type ContextPanelTab = "files" | "tools" | "memory" | "rules";

const CONTEXT_MAX_TOKENS = 1_000_000;

export function ContextPanel({
  settings,
  usage,
  mcpSpecs,
  mcpBridged,
  sessionFiles,
  memory,
  memoryDetail,
  activeTab,
  activeTabNonce,
  onReadMemory,
  onOpenMcpSettings,
  onEditMcpSpec,
  onRetryMcpSpec,
}: {
  settings: Settings | null;
  usage: UsageStats;
  mcpSpecs: McpSpecInfo[];
  mcpBridged: boolean;
  sessionFiles: SessionFile[];
  memory: MemoryEntryInfo[];
  memoryDetail: MemoryDetail | null;
  activeTab?: ContextPanelTab;
  activeTabNonce?: number;
  onReadMemory: (path: string) => void;
  onOpenMcpSettings?: () => void;
  onEditMcpSpec?: (spec: McpSpecInfo) => void;
  onRetryMcpSpec?: (raw: string) => void;
}) {
  useLang();
  const [tab, setTab] = useState<ContextPanelTab>("files");
  useEffect(() => {
    if (activeTab) setTab(activeTab);
  }, [activeTab, activeTabNonce]);
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
            {tab === "tools" && (
              <CtxTools
                specs={mcpSpecs}
                bridged={mcpBridged}
                onOpenSettings={onOpenMcpSettings}
                onEdit={onEditMcpSpec}
                onRetry={onRetryMcpSpec}
              />
            )}
            {tab === "memory" && (
              <CtxMemory entries={memory} detail={memoryDetail} onRead={onReadMemory} />
            )}
            {tab === "rules" && <CtxRules settings={settings} />}
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

type McpFilter = "all" | "ready" | "failed";

function CtxTools({
  specs,
  bridged,
  onOpenSettings,
  onEdit,
  onRetry,
}: {
  specs: McpSpecInfo[];
  bridged: boolean;
  onOpenSettings?: () => void;
  onEdit?: (spec: McpSpecInfo) => void;
  onRetry?: (raw: string) => void;
}) {
  const [filter, setFilter] = useState<McpFilter>("all");
  const readyCount = specs.filter((s) => s.status === "connected").length;
  const failed = specs.filter((s) => s.status === "failed");
  const failedCount = failed.length;
  const toolCount = specs.reduce((sum, s) => sum + (s.toolCount ?? s.tools?.length ?? 0), 0);
  const filtered =
    filter === "ready"
      ? specs.filter((s) => s.status === "connected")
      : filter === "failed"
        ? failed
        : [...failed, ...specs.filter((s) => s.status !== "failed")];
  const retryAll = () => {
    if (!onRetry) return;
    for (const spec of failed) onRetry(spec.raw);
  };
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
        <>
          <div className="mcp-health-strip">
            <span>{t("contextPanel.mcpHealthTotal", { count: specs.length })}</span>
            <span data-kind="ok">{t("contextPanel.mcpHealthReady", { count: readyCount })}</span>
            <span data-kind={failedCount > 0 ? "failed" : "muted"}>
              {t("contextPanel.mcpHealthFailed", { count: failedCount })}
            </span>
            <span>{t("contextPanel.mcpHealthTools", { count: toolCount })}</span>
          </div>
          <div className="mcp-filter-row">
            {(["all", "ready", "failed"] as const).map((kind) => (
              <button
                key={kind}
                type="button"
                className="mcp-filter"
                data-active={filter === kind}
                onClick={() => setFilter(kind)}
              >
                {kind === "all"
                  ? t("contextPanel.mcpFilterAll")
                  : kind === "ready"
                    ? t("contextPanel.mcpFilterReady")
                    : t("contextPanel.mcpFilterFailed")}
              </button>
            ))}
            <span className="spacer" />
            {failedCount > 0 && onRetry ? (
              <button type="button" className="mcp-mini-action" onClick={retryAll}>
                <I.refresh size={12} />
                {t("contextPanel.mcpRetryAll")}
              </button>
            ) : null}
            {onOpenSettings ? (
              <button type="button" className="mcp-mini-action" onClick={onOpenSettings}>
                <I.cog size={12} />
                {t("contextPanel.mcpSettings")}
              </button>
            ) : null}
          </div>
          {filtered.length === 0 ? (
            <div className="ctx-empty">{t("contextPanel.mcpFilterEmpty")}</div>
          ) : (
            filtered.map((s) => (
              <McpServerCard
                key={s.raw}
                spec={s}
                mode="context"
                onRetry={onRetry}
                onEdit={onEdit ?? (onOpenSettings ? () => onOpenSettings() : undefined)}
              />
            ))
          )}
        </>
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
