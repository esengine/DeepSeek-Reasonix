import { useState } from "react";
import { I } from "../icons";
import type { Settings, UsageStats } from "../App";

type Tab = "files" | "tools" | "memory" | "rules";

export function ContextPanel({
  settings,
  usage,
  workspaceDir,
}: {
  settings: Settings | null;
  usage: UsageStats;
  workspaceDir?: string;
}) {
  const [tab, setTab] = useState<Tab>("files");
  const used = usage.cacheMissTokens;
  const cached = usage.cacheHitTokens;
  const max = 128_000;
  const usedPct = Math.min(100, (used / max) * 100);
  const cachedPct = Math.min(100, (cached / max) * 100);
  const free = Math.max(0, max - used - cached);
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
              {(used + cached).toLocaleString()} / {max.toLocaleString()}
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
        {tab === "tools" && <CtxTools />}
        {tab === "memory" && <CtxMemory />}
        {tab === "rules" && <CtxRules settings={settings} />}
      </div>
    </aside>
  );
}

function CtxFiles({ workspaceDir }: { workspaceDir?: string }) {
  return (
    <div className="ctx-block">
      <div className="h">
        <span>当前工作区</span>
        <span className="right">—</span>
      </div>
      <div className="tree">
        {workspaceDir ? (
          <div className="node">
            <span className="ico">
              <I.folder size={12} />
            </span>
            <span className="nm">{workspaceDir.split(/[\\/]/).pop()}</span>
          </div>
        ) : (
          <div
            style={{
              padding: "8px 4px",
              fontSize: 11,
              color: "var(--muted-2)",
              fontFamily: "IBM Plex Mono, monospace",
            }}
          >
            未选择工作区
          </div>
        )}
      </div>
    </div>
  );
}

function CtxTools() {
  return (
    <div className="ctx-block">
      <div className="h">
        <span>内置工具</span>
        <span className="right">connected</span>
      </div>
      {[
        { id: "fs", name: "filesystem", tools: 12 },
        { id: "shell", name: "shell", tools: 2 },
        { id: "search", name: "search", tools: 4 },
        { id: "edit", name: "edit", tools: 5 },
      ].map((m) => (
        <div className="mcp-row" key={m.id}>
          <span className="ico">
            <I.wrench size={12} />
          </span>
          <div className="body">
            <div className="n">{m.name}</div>
            <div className="m">{m.tools} tools · ready</div>
          </div>
          <span className="status" />
        </div>
      ))}
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
      <div
        style={{
          padding: "8px",
          fontSize: 11.5,
          color: "var(--muted)",
          fontFamily: "IBM Plex Mono, monospace",
        }}
      >
        当前会话尚未记录长期记忆。
      </div>
    </div>
  );
}

function CtxRules({ settings }: { settings: Settings | null }) {
  const editMode = settings?.editMode ?? "review";
  const items: { p: string; allow: boolean; desc: string }[] = [
    { p: editMode === "yolo" ? "* (YOLO)" : editMode === "auto" ? "read_*, search_*" : "需要确认", allow: editMode !== "review", desc: `当前模式：${editMode}` },
    { p: "run_command, run_background", allow: false, desc: "执行 shell 前需确认" },
  ];
  return (
    <div className="ctx-block">
      <div className="h">
        <span>自动批准</span>
        <span className="right">{items.length}</span>
      </div>
      {items.map((r, i) => (
        <div className="rule" key={i}>
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
