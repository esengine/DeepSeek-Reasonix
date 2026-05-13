import { useState, type ReactNode } from "react";
import { I } from "../icons";
import type { Balance, Settings as SettingsType, UsageStats } from "../App";
import type { McpSpecInfo, SettingsPatch, SkillInfo } from "../protocol";

export type PageId =
  | "general"
  | "models"
  | "mcp"
  | "skills"
  | "memory"
  | "rules"
  | "billing"
  | "shortcuts";

const SETTING_PAGES: { id: PageId; label: string; icon: keyof typeof I; desc: string }[] = [
  { id: "general", label: "通用", icon: "cog", desc: "外观、语言、行为" },
  { id: "models", label: "模型", icon: "brain", desc: "选择默认模型与采样参数" },
  { id: "mcp", label: "MCP 服务器", icon: "wrench", desc: "管理 MCP 协议工具服务器" },
  { id: "skills", label: "技能 / Skills", icon: "zap", desc: "为 / 命令绑定的可复用提示集" },
  { id: "memory", label: "记忆", icon: "bookmark", desc: "CLAUDE.md / AGENTS.md 注入说明" },
  { id: "rules", label: "审批规则", icon: "shield", desc: "自动批准、拒绝、需确认命令模式" },
  { id: "billing", label: "账户 & 计费", icon: "coin", desc: "账户余额、用量与发票" },
  { id: "shortcuts", label: "快捷键", icon: "cpu", desc: "键盘快捷键总览" },
];

export function SettingsModal({
  settings,
  balance,
  usage,
  currency,
  initialPage,
  mcpSpecs,
  mcpBridged,
  skills,
  onClose,
  onSave,
  onSaveApiKey,
  onPickWorkspace,
  onAddMcpSpec,
  onRemoveMcpSpec,
}: {
  settings: SettingsType;
  balance: Balance | null;
  usage: UsageStats;
  currency: "CNY" | "USD";
  initialPage?: PageId;
  mcpSpecs: McpSpecInfo[];
  mcpBridged: boolean;
  skills: SkillInfo[];
  onClose: () => void;
  onSave: (patch: SettingsPatch) => void;
  onSaveApiKey: (key: string) => void;
  onPickWorkspace: () => void;
  onAddMcpSpec: (spec: string) => void;
  onRemoveMcpSpec: (spec: string) => void;
}) {
  const [page, setPage] = useState<PageId>(initialPage ?? "general");
  const current = SETTING_PAGES.find((p) => p.id === page) ?? SETTING_PAGES[0]!;
  return (
    <div className="settings-mask" onClick={onClose}>
      <div className="settings" onClick={(e) => e.stopPropagation()}>
        <nav className="settings-side">
          <div className="sg">设置</div>
          {SETTING_PAGES.map((p) => (
            <div
              key={p.id}
              className="row"
              data-active={page === p.id}
              onClick={() => setPage(p.id)}
            >
              <span className="ico">{I[p.icon]({ size: 13 })}</span>
              <span>{p.label}</span>
            </div>
          ))}
        </nav>
        <div className="settings-main">
          <div className="settings-head">
            <div>
              <h2>{current.label}</h2>
              <div className="desc">{current.desc}</div>
            </div>
            <span className="grow" />
            <button type="button" className="close-btn" onClick={onClose}>
              <I.x size={14} />
            </button>
          </div>
          <div className="settings-body">
            {page === "general" && (
              <PageGeneral settings={settings} onSave={onSave} onPickWorkspace={onPickWorkspace} />
            )}
            {page === "models" && <PageModels settings={settings} onSave={onSave} />}
            {page === "mcp" && (
              <PageMCP
                specs={mcpSpecs}
                bridged={mcpBridged}
                onAdd={onAddMcpSpec}
                onRemove={onRemoveMcpSpec}
              />
            )}
            {page === "skills" && <PageSkills skills={skills} />}
            {page === "memory" && <PageMemory />}
            {page === "rules" && <PageRules settings={settings} onSave={onSave} />}
            {page === "billing" && (
              <PageBilling balance={balance} usage={usage} currency={currency} />
            )}
            {page === "shortcuts" && <PageShortcuts />}
            {page === "general" && settings.baseUrl !== undefined ? (
              <ApiKeySection
                baseUrl={settings.baseUrl}
                apiKeyPrefix={settings.apiKeyPrefix}
                onSave={onSave}
                onSaveApiKey={onSaveApiKey}
              />
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}

function PageGeneral({
  settings,
  onSave,
  onPickWorkspace,
}: {
  settings: SettingsType;
  onSave: (patch: SettingsPatch) => void;
  onPickWorkspace: () => void;
}) {
  const [editorDraft, setEditorDraft] = useState(settings.editor ?? "");
  return (
    <>
      <section className="section">
        <div className="stitle">工作区</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">当前工作目录</div>
            <div className="h">{settings.workspaceDir || "未选择"}</div>
          </div>
          <button type="button" className="btn" onClick={onPickWorkspace}>
            选择…
          </button>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">外部编辑器</div>
            <div className="h">用于 file:line 链接打开，如 cursor / code / idea</div>
          </div>
          <input
            className="field mono"
            value={editorDraft}
            placeholder="cursor --goto"
            onChange={(e) => setEditorDraft(e.target.value)}
            onBlur={() => onSave({ editor: editorDraft || undefined })}
          />
        </div>
      </section>

      <section className="section">
        <div className="stitle">行为</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">推理强度</div>
            <div className="h">high — 性价比；max — 复杂任务</div>
          </div>
          <div className="seg-ctrl">
            <button
              type="button"
              data-on={settings.reasoningEffort === "high"}
              onClick={() => onSave({ reasoningEffort: "high" })}
            >
              high
            </button>
            <button
              type="button"
              data-on={settings.reasoningEffort === "max"}
              onClick={() => onSave({ reasoningEffort: "max" })}
            >
              max
            </button>
          </div>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">编辑模式</div>
            <div className="h">review — 每次确认；auto — 文件操作自动；yolo — 全部自动</div>
          </div>
          <div className="seg-ctrl">
            {(["review", "auto", "yolo"] as const).map((m) => (
              <button
                type="button"
                key={m}
                data-on={settings.editMode === m}
                onClick={() => onSave({ editMode: m })}
              >
                {m}
              </button>
            ))}
          </div>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">预算上限 (USD)</div>
            <div className="h">超过此值自动暂停；留空为无限制</div>
          </div>
          <input
            className="field"
            type="number"
            defaultValue={settings.budgetUsd ?? ""}
            placeholder="无限制"
            onBlur={(e) => {
              const v = e.target.value.trim();
              onSave({ budgetUsd: v === "" ? null : Number(v) });
            }}
          />
        </div>
      </section>
    </>
  );
}

function ApiKeySection({
  baseUrl,
  apiKeyPrefix,
  onSave,
  onSaveApiKey,
}: {
  baseUrl?: string;
  apiKeyPrefix?: string;
  onSave: (patch: SettingsPatch) => void;
  onSaveApiKey: (key: string) => void;
}) {
  const [key, setKey] = useState("");
  const [urlDraft, setUrlDraft] = useState(baseUrl ?? "");
  return (
    <section className="section">
      <div className="stitle">DeepSeek API</div>
      <div className="setting-row">
        <div className="l">
          <div className="n">API Key</div>
          <div className="h">{apiKeyPrefix ? `已设置 · ${apiKeyPrefix}…` : "未配置"}</div>
        </div>
        <div style={{ display: "flex", gap: 6 }}>
          <input
            className="field mono"
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="sk-…"
          />
          <button
            type="button"
            className="btn primary"
            disabled={!key}
            onClick={() => {
              if (!key) return;
              onSaveApiKey(key);
              setKey("");
            }}
          >
            保存
          </button>
        </div>
      </div>
      <div className="setting-row">
        <div className="l">
          <div className="n">Base URL</div>
          <div className="h">默认 api.deepseek.com — 可改为兼容端点</div>
        </div>
        <input
          className="field mono"
          value={urlDraft}
          onChange={(e) => setUrlDraft(e.target.value)}
          onBlur={() => onSave({ baseUrl: urlDraft.trim() || undefined })}
        />
      </div>
    </section>
  );
}

function PageModels({
  settings,
  onSave,
}: {
  settings: SettingsType;
  onSave: (patch: SettingsPatch) => void;
}) {
  const presets = [
    {
      id: "auto" as const,
      name: "auto (flash → pro)",
      badge: "AUTO",
      desc: "自动从 flash 起步，遇复杂任务升级到 pro，兼顾速度与质量。",
      ctx: "—",
      out: "—",
    },
    {
      id: "flash" as const,
      name: "deepseek-v4-flash",
      badge: "FLASH",
      desc: "通用对话模型，速度快、长上下文、价格友好。",
      ctx: "1M",
      out: "8K",
    },
    {
      id: "pro" as const,
      name: "deepseek-v4-pro",
      badge: "PRO",
      desc: "深度推理模型，先生成可解释的思考链，再给最终答案。",
      ctx: "1M",
      out: "32K",
    },
  ];
  return (
    <section className="section">
      <div className="stitle">默认模型 · 当前 {settings.model}</div>
      <div className="model-grid">
        {presets.map((m) => (
          <div
            key={m.id}
            className="mcard"
            data-on={settings.preset === m.id}
            onClick={() => onSave({ preset: m.id })}
          >
            <div className="nm">
              {m.name}
              <span className="badge">{m.badge}</span>
            </div>
            <div className="desc">{m.desc}</div>
            <div className="spec">
              <div>
                <span className="k">上下文 </span>
                <span className="v">{m.ctx}</span>
              </div>
              <div>
                <span className="k">输出 </span>
                <span className="v">{m.out}</span>
              </div>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

function PageMCP({
  specs,
  bridged,
  onAdd,
  onRemove,
}: {
  specs: McpSpecInfo[];
  bridged: boolean;
  onAdd: (spec: string) => void;
  onRemove: (spec: string) => void;
}) {
  const [draft, setDraft] = useState("");
  const submit = () => {
    const v = draft.trim();
    if (!v) return;
    onAdd(v);
    setDraft("");
  };
  return (
    <>
      <section className="section">
        <div className="stitle">
          已配置 · {specs.length}
          {bridged ? (
            <span style={{ color: "var(--accent)", marginLeft: 8, fontSize: 11 }}>
              · 已桥接
            </span>
          ) : (
            <span style={{ color: "var(--muted)", marginLeft: 8, fontSize: 11 }}>
              · 当前桌面会话未桥接，重启 reasonix code (TUI) 后生效
            </span>
          )}
        </div>
        {specs.length === 0 ? (
          <div
            style={{
              padding: 16,
              background: "var(--card)",
              border: "1px solid var(--border)",
              borderRadius: 10,
              fontSize: 12,
              color: "var(--muted)",
            }}
          >
            还没有配置 MCP 服务器。下面输入 spec 添加。
          </div>
        ) : (
          specs.map((s) => (
            <div className="scard" key={s.raw}>
              <div className="top">
                <span className="ico">
                  <I.wrench size={14} />
                </span>
                <div>
                  <div className="nm">{s.name ?? "(anonymous)"}</div>
                  <div className="sub">{s.summary}</div>
                </div>
                <span className="grow" />
                <button
                  type="button"
                  className="btn ghost"
                  style={{ color: "var(--danger)" }}
                  onClick={() => onRemove(s.raw)}
                >
                  移除
                </button>
              </div>
              {s.parseError ? (
                <div className="desc" style={{ color: "var(--danger)" }}>
                  解析失败：{s.parseError}
                </div>
              ) : null}
            </div>
          ))
        )}
      </section>
      <section className="section">
        <div className="stitle">添加服务器</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">spec 字符串</div>
            <div className="h">
              格式：<code>name=command args</code> 或 <code>name=https://host/sse</code>
            </div>
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <input
              className="field mono"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="github=npx -y @smithery/cli ..."
              onKeyDown={(e) => {
                if (e.key === "Enter") submit();
              }}
            />
            <button
              type="button"
              className="btn primary"
              disabled={!draft.trim()}
              onClick={submit}
            >
              添加
            </button>
          </div>
        </div>
      </section>
    </>
  );
}

function PageSkills({ skills }: { skills: SkillInfo[] }) {
  return (
    <section className="section">
      <div className="stitle">已加载 · {skills.length} · 通过 / 命令调用</div>
      {skills.length === 0 ? (
        <div
          style={{
            padding: 16,
            background: "var(--card)",
            border: "1px solid var(--border)",
            borderRadius: 10,
            fontSize: 12,
            color: "var(--muted)",
          }}
        >
          没有可用技能。可在 ~/.reasonix/skills/ 或 项目根/.reasonix/skills/ 下创建 SKILL.md。
        </div>
      ) : (
        skills.map((s) => (
          <div className="scard" key={`${s.scope}:${s.name}`}>
            <div className="top">
              <span className="ico">
                <I.zap size={14} />
              </span>
              <div>
                <div className="nm">
                  <span
                    style={{
                      fontFamily: "IBM Plex Mono, monospace",
                      color: "var(--accent)",
                    }}
                  >
                    /{s.name}
                  </span>
                </div>
                <div className="sub">
                  {s.scope} · {s.runAs}
                  {s.model ? ` · ${s.model}` : ""}
                </div>
              </div>
            </div>
            <div className="desc">{s.description}</div>
            <div
              style={{
                fontFamily: "IBM Plex Mono, monospace",
                fontSize: 10.5,
                color: "var(--muted-2)",
                marginTop: 4,
              }}
            >
              {s.path}
            </div>
          </div>
        ))
      )}
    </section>
  );
}

function PageMemory() {
  return (
    <section className="section">
      <div className="stitle">长期记忆</div>
      <div
        style={{
          padding: 16,
          background: "var(--card)",
          border: "1px solid var(--border)",
          borderRadius: 10,
          fontSize: 12,
          color: "var(--muted)",
        }}
      >
        当前版本依赖内置 CLAUDE.md / AGENTS.md 注入；项目级别记忆在内核侧维护。
      </div>
    </section>
  );
}

function PageRules({
  settings,
  onSave,
}: {
  settings: SettingsType;
  onSave: (patch: SettingsPatch) => void;
}) {
  return (
    <>
      <section className="section">
        <div className="stitle">编辑模式</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">应用模式</div>
            <div className="h">review 每次确认，auto 文件自动，yolo 全部自动</div>
          </div>
          <div className="seg-ctrl">
            {(["review", "auto", "yolo"] as const).map((m) => (
              <button
                type="button"
                key={m}
                data-on={settings.editMode === m}
                onClick={() => onSave({ editMode: m })}
              >
                {m}
              </button>
            ))}
          </div>
        </div>
      </section>
      <section className="section">
        <div className="stitle">命令自动批准</div>
        <div
          style={{
            padding: 12,
            background: "var(--card)",
            border: "1px solid var(--border)",
            borderRadius: 10,
            fontSize: 12,
            color: "var(--muted)",
          }}
        >
          在 ApprovalCard 中点击 "始终允许" 可向白名单添加命令前缀；后续匹配命令将不再询问。
        </div>
      </section>
    </>
  );
}

function PageBilling({
  balance,
  usage,
  currency,
}: {
  balance: Balance | null;
  usage: UsageStats;
  currency: "CNY" | "USD";
}) {
  const symbol = currency === "CNY" ? "¥" : "$";
  const totalTokens = usage.cacheHitTokens + usage.cacheMissTokens;
  const hitPct = totalTokens > 0 ? Math.round((usage.cacheHitTokens / totalTokens) * 100) : 0;
  return (
    <>
      <div className="bill-grid">
        <div className="bill-card">
          <div className="l">钱包余额</div>
          <div className="v ok">
            {balance
              ? `${balance.currency === "USD" ? "$" : "¥"} ${balance.total.toFixed(2)}`
              : "—"}
          </div>
          <div className="sub">
            {balance && !balance.isAvailable ? "余额不足" : "可用"}
          </div>
        </div>
        <div className="bill-card">
          <div className="l">本会话花费</div>
          <div className="v">
            {symbol} {usage.totalCostUsd.toFixed(4)}
          </div>
          <div className="sub">prompt {usage.totalPromptTokens.toLocaleString()} t</div>
        </div>
        <div className="bill-card">
          <div className="l">缓存命中率</div>
          <div className="v acc">{hitPct}%</div>
          <div className="sub">
            hit {usage.cacheHitTokens.toLocaleString()} / miss {usage.cacheMissTokens.toLocaleString()}
          </div>
        </div>
      </div>
    </>
  );
}

function PageShortcuts() {
  const rows: { nm: string; keys: string[] }[] = [
    { nm: "新建会话", keys: ["⌘", "N"] },
    { nm: "新建标签", keys: ["⌘", "T"] },
    { nm: "关闭标签", keys: ["⌘", "W"] },
    { nm: "命令面板", keys: ["⌘", "K"] },
    { nm: "聚焦输入框", keys: ["⌘", "L"] },
    { nm: "切换标签", keys: ["⌘", "⇥"] },
    { nm: "中断流式输出", keys: ["esc"] },
    { nm: "设置", keys: ["⌘", ","] },
  ];
  return (
    <section className="section">
      <div className="kbd-grid">
        {rows.map((s, i) => (
          <SectionRow key={i} nm={s.nm} keys={s.keys} />
        ))}
      </div>
    </section>
  );
}

function SectionRow({ nm, keys }: { nm: string; keys: string[] }): ReactNode {
  return (
    <>
      <div className="nm">{nm}</div>
      <div className="keys">
        {keys.map((k, j) => (
          <kbd key={j}>{k}</kbd>
        ))}
      </div>
    </>
  );
}
