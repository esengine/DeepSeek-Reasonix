import { invoke } from "@tauri-apps/api/core";
import { useEffect, useMemo, useState } from "react";
import type { Settings, UsageStats } from "../App";
import { I } from "../icons";
import type { McpSpecInfo } from "../protocol";

type Tab = "files" | "tools" | "memory" | "rules";

type FileEntry = {
  path: string;
  depth: number;
  kind: "dir" | "file";
  name: string;
};

const CONTEXT_MAX_TOKENS = 1_000_000;

export function ContextPanel({
  settings,
  usage,
  workspaceDir,
  mcpSpecs,
  mcpBridged,
}: {
  settings: Settings | null;
  usage: UsageStats;
  workspaceDir?: string;
  mcpSpecs: McpSpecInfo[];
  mcpBridged: boolean;
}) {
  const [tab, setTab] = useState<Tab>("files");
  const used = usage.cacheMissTokens;
  const cached = usage.cacheHitTokens;
  const usedPct = Math.min(100, (used / CONTEXT_MAX_TOKENS) * 100);
  const cachedPct = Math.min(100, (cached / CONTEXT_MAX_TOKENS) * 100);
  const free = Math.max(0, CONTEXT_MAX_TOKENS - used - cached);
  return (
    <aside className="ctx">
      <div className="ctx-tabs">
        <div className="ctx-tab" data-active={tab === "files"} onClick={() => setTab("files")}>
          文件
        </div>
        <div className="ctx-tab" data-active={tab === "tools"} onClick={() => setTab("tools")}>
          工具
        </div>
        <div className="ctx-tab" data-active={tab === "memory"} onClick={() => setTab("memory")}>
          记忆
        </div>
        <div className="ctx-tab" data-active={tab === "rules"} onClick={() => setTab("rules")}>
          规则
        </div>
      </div>

      <div className="ctx-body">
        <div className="ctx-block">
          <div className="h">
            <span>上下文 · tokens</span>
            <span className="right">
              {(used + cached).toLocaleString()} / {CONTEXT_MAX_TOKENS.toLocaleString()}
            </span>
          </div>
          <div className="meter">
            <span className="used" style={{ width: `${usedPct}%` }} />
            <span className="cached" style={{ width: `${cachedPct}%` }} />
          </div>
          <div className="legend">
            <span className="l">
              <span className="sw u" />
              已用 <span className="v">{used.toLocaleString()}</span>
            </span>
            <span className="l">
              <span className="sw c" />
              缓存 <span className="v">{cached.toLocaleString()}</span>
            </span>
            <span className="l">
              余 <span className="v">{free.toLocaleString()}</span>
            </span>
          </div>
        </div>

        {tab === "files" && <CtxFiles workspaceDir={workspaceDir} />}
        {tab === "tools" && <CtxTools specs={mcpSpecs} bridged={mcpBridged} />}
        {tab === "memory" && <CtxMemory />}
        {tab === "rules" && <CtxRules settings={settings} />}
      </div>
    </aside>
  );
}

function CtxFiles({ workspaceDir }: { workspaceDir?: string }) {
  const [entries, setEntries] = useState<FileEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    if (!workspaceDir) {
      setEntries(null);
      setError(null);
      setCollapsed(new Set());
      return;
    }
    let cancelled = false;
    setEntries(null);
    setError(null);
    setCollapsed(new Set());
    invoke<FileEntry[]>("list_workspace_tree", { root: workspaceDir, maxDepth: 2 })
      .then((rows) => {
        if (!cancelled) setEntries(rows);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(String((err as { message?: string })?.message ?? err));
      });
    return () => {
      cancelled = true;
    };
  }, [workspaceDir]);

  // depth-first traversal — each entry's parent is the nearest dir above it
  // at depth-1, which is what walk_dir on the Rust side guarantees.
  const parentByPath = useMemo(() => {
    const map = new Map<string, string | null>();
    if (!entries) return map;
    const stack: string[] = [];
    for (const e of entries) {
      while (stack.length > e.depth) stack.pop();
      map.set(e.path, e.depth === 0 ? null : (stack[e.depth - 1] ?? null));
      if (e.kind === "dir") stack[e.depth] = e.path;
    }
    return map;
  }, [entries]);

  const isVisible = (path: string): boolean => {
    const parent = parentByPath.get(path);
    if (!parent) return true;
    if (collapsed.has(parent)) return false;
    return isVisible(parent);
  };

  const toggle = (path: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  const fileCount = entries?.filter((e) => e.kind === "file").length ?? 0;
  const visible = entries?.filter((e) => isVisible(e.path)) ?? [];

  return (
    <div className="ctx-block">
      <div className="h">
        <span>当前工作区</span>
        <span className="right">{entries ? `${fileCount} files` : "—"}</span>
      </div>
      <div className="tree">
        {!workspaceDir ? (
          <div className="ctx-empty">未选择工作区</div>
        ) : error ? (
          <div className="ctx-empty">读取失败：{error}</div>
        ) : entries === null ? (
          <div className="ctx-empty">加载中…</div>
        ) : entries.length === 0 ? (
          <div className="ctx-empty">空目录</div>
        ) : (
          visible.map((n) => (
            <div
              className="node"
              key={n.path}
              data-d={n.depth}
              data-kind={n.kind}
              title={n.path}
              onClick={n.kind === "dir" ? () => toggle(n.path) : undefined}
            >
              <span className="caret">
                {n.kind === "dir" ? (
                  collapsed.has(n.path) ? (
                    <I.chevR size={10} />
                  ) : (
                    <I.chev size={10} />
                  )
                ) : null}
              </span>
              <span className="ico">
                {n.kind === "dir" ? <I.folder size={12} /> : <I.file size={12} />}
              </span>
              <span className="nm">
                {n.name}
                {n.kind === "dir" ? "/" : ""}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function CtxTools({ specs, bridged }: { specs: McpSpecInfo[]; bridged: boolean }) {
  return (
    <div className="ctx-block">
      <div className="h">
        <span>MCP 服务器</span>
        <span className="right">
          {specs.length === 0 ? "—" : `${specs.length} ${bridged ? "ready" : "configured"}`}
        </span>
      </div>
      {specs.length === 0 ? (
        <div className="ctx-empty">未配置 MCP 服务器</div>
      ) : (
        specs.map((s) => {
          const ok = !s.parseError;
          const suffix = s.parseError
            ? ` · ${s.parseError}`
            : bridged
              ? " · ready"
              : " · configured";
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
              <span className="status" data-s={ok ? "ok" : "off"} />
            </div>
          );
        })
      )}
    </div>
  );
}

function CtxMemory() {
  return (
    <div className="ctx-block">
      <div className="h">
        <span>长期记忆</span>
        <span className="right">—</span>
      </div>
      <div className="ctx-empty">当前会话尚未记录长期记忆。</div>
    </div>
  );
}

function CtxRules({ settings }: { settings: Settings | null }) {
  const editMode = settings?.editMode ?? "review";
  const items: { p: string; allow: boolean; desc: string }[] =
    editMode === "yolo"
      ? [{ p: "*", allow: true, desc: "YOLO 模式 · 所有工具调用自动批准" }]
      : editMode === "auto"
        ? [
            { p: "read_file, list_directory, search_files, *", allow: true, desc: "只读工具自动批准" },
            { p: "run_command (allowlist)", allow: true, desc: "命中 shell 白名单的命令自动批准" },
            { p: "edit_file, write_file, run_command (其他)", allow: false, desc: "写入与未知 shell 命令需确认" },
          ]
        : [
            { p: "*", allow: false, desc: "Review 模式 · 每个工具调用都需确认" },
          ];
  return (
    <div className="ctx-block">
      <div className="h">
        <span>自动批准</span>
        <span className="right">{editMode}</span>
      </div>
      {items.map((r) => (
        <div className="rule" key={r.p}>
          <div className="top">
            <span className={`pat ${r.allow ? "" : "deny"}`}>{r.p}</span>
            <span className={`sw ${r.allow ? "" : "deny"}`}>{r.allow ? "ALLOW" : "ASK"}</span>
          </div>
          <div className="desc">{r.desc}</div>
        </div>
      ))}
    </div>
  );
}
