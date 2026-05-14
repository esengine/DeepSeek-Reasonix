import { useMemo, useState } from "react";
import type { SessionFile, Settings, UsageStats } from "../App";
import { t, useLang } from "../i18n";
import { I } from "../icons";
import type { McpSpecInfo, MemoryEntryInfo } from "../protocol";

type Tab = "files" | "tools" | "memory" | "rules";

const CONTEXT_MAX_TOKENS = 1_000_000;

export function ContextPanel({
  settings,
  usage,
  mcpSpecs,
  mcpBridged,
  sessionFiles,
  memory,
}: {
  settings: Settings | null;
  usage: UsageStats;
  mcpSpecs: McpSpecInfo[];
  mcpBridged: boolean;
  sessionFiles: SessionFile[];
  memory: MemoryEntryInfo[];
}) {
  useLang();
  const [tab, setTab] = useState<Tab>("files");
  const reserved = usage.reservedTokens;
  // After a warm cache turn the API counts the reserved prefix inside cacheHit;
  // subtract to keep the bar segments visually disjoint. Cold cache shows the
  // reserved portion in cacheMiss instead, so do the same for `used`.
  const cached = Math.max(0, usage.cacheHitTokens - reserved);
  const used = Math.max(0, usage.cacheMissTokens - Math.max(0, reserved - usage.cacheHitTokens));
  const reservedPct = Math.min(100, (reserved / CONTEXT_MAX_TOKENS) * 100);
  const usedPct = Math.min(100, (used / CONTEXT_MAX_TOKENS) * 100);
  const cachedPct = Math.min(100, (cached / CONTEXT_MAX_TOKENS) * 100);
  const free = Math.max(0, CONTEXT_MAX_TOKENS - reserved - used - cached);
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
              缓存 <span className="v">{cached.toLocaleString()}</span>
            </span>
            <span className="l">
              <span className="sw u" />
              {t("contextPanel.usedKey")} <span className="v">{used.toLocaleString()}</span>
            </span>
            <span className="l">
              余 <span className="v">{free.toLocaleString()}</span>
            </span>
          </div>
        </div>

        {tab === "files" && <CtxFiles files={sessionFiles} />}
        {tab === "tools" && <CtxTools specs={mcpSpecs} bridged={mcpBridged} />}
        {tab === "memory" && <CtxMemory entries={memory} />}
        {tab === "rules" && <CtxRules settings={settings} />}
      </div>
    </aside>
  );
}

type TreeNode =
  | { kind: "dir"; depth: number; name: string; key: string }
  | { kind: "file"; depth: number; name: string; key: string; status: "c" | "m" };

function buildSessionTree(files: SessionFile[]): TreeNode[] {
  const sorted = [...files].sort((a, b) =>
    a.path.replace(/\\/g, "/").localeCompare(b.path.replace(/\\/g, "/")),
  );
  const out: TreeNode[] = [];
  const seenDirs = new Set<string>();
  for (const f of sorted) {
    const parts = f.path.replace(/\\/g, "/").split("/").filter(Boolean);
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
      key: `f:${f.path}`,
      status: f.status,
    });
  }
  return out;
}

function CtxFiles({ files }: { files: SessionFile[] }) {
  const tree = useMemo(() => buildSessionTree(files), [files]);
  return (
    <div className="ctx-block">
      <div className="h">
        <span>上下文中的文件</span>
        <span className="right">{files.length === 0 ? "—" : `${files.length} files`}</span>
      </div>
      <div className="tree">
        {files.length === 0 ? (
          <div className="ctx-empty">{t("contextPanel.noFilesMsg")}</div>
        ) : (
          tree.map((n) =>
            n.kind === "dir" ? (
              <div className="node" key={n.key} data-d={n.depth} data-kind="dir">
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
                title={n.name}
              >
                <span className="ico">
                  <I.file size={12} />
                </span>
                <span className="nm">{n.name}</span>
                <span
                  className="dot"
                  data-s={n.status}
                  title={n.status === "m" ? "modified" : "in context"}
                />
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
        <span>MCP 服务器</span>
        <span className="right">
          {specs.length === 0
            ? "—"
            : bridged
              ? `${specs.length} ready`
              : `${readyCount}/${specs.length} ready`}
        </span>
      </div>
      {specs.length === 0 ? (
        <div className="ctx-empty">未配置 MCP 服务器</div>
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
                ? ` · ${s.toolCount} tools`
                : " · ready"
              : s.status === "handshake"
                ? " · connecting"
                : s.status === "disabled"
                  ? " · disabled"
                  : s.status === "failed"
                    ? " · failed"
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
              <span className="status" data-s={dot} />
            </div>
          );
        })
      )}
    </div>
  );
}

function CtxMemory({ entries }: { entries: MemoryEntryInfo[] }) {
  return (
    <div className="ctx-block">
      <div className="h">
        <span>长期记忆</span>
        <span className="right">{entries.length === 0 ? "—" : `${entries.length} 项`}</span>
      </div>
      {entries.length === 0 ? (
        <div className="ctx-empty">{t("contextPanel.noMemoriesMsg")}</div>
      ) : (
        <div className="mem">
          {entries.map((m) => (
            <div className="mem-row" key={`${m.scope}/${m.name}`}>
              <span className="scope" data-s={m.scope}>
                {m.scope === "project" ? "项目" : "全局"}
              </span>
              <span className="txt">{m.description || m.name}</span>
            </div>
          ))}
        </div>
      )}
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
