import { type ReactNode, useState } from "react";
import type { Balance, Settings as SettingsType, UsageStats } from "../App";
import { t } from "../i18n";
import { I } from "../icons";
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

const PAGE_META: ReadonlyArray<{ id: PageId; icon: keyof typeof I }> = [
  { id: "general", icon: "cog" },
  { id: "models", icon: "brain" },
  { id: "mcp", icon: "wrench" },
  { id: "skills", icon: "zap" },
  { id: "memory", icon: "bookmark" },
  { id: "rules", icon: "shield" },
  { id: "billing", icon: "coin" },
  { id: "shortcuts", icon: "cpu" },
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
  const currentMeta = PAGE_META.find((p) => p.id === page) ?? PAGE_META[0]!;
  return (
    <div className="settings-mask" onClick={onClose}>
      <div className="settings" onClick={(e) => e.stopPropagation()}>
        <nav className="settings-side">
          <div className="sg">{t("settings.title")}</div>
          {PAGE_META.map((p) => (
            <div
              key={p.id}
              className="row"
              data-active={page === p.id}
              onClick={() => setPage(p.id)}
            >
              <span className="ico">{I[p.icon]({ size: 13 })}</span>
              <span>{t(`settings.page${p.id[0]!.toUpperCase()}${p.id.slice(1)}Label` as any)}</span>
            </div>
          ))}
        </nav>
        <div className="settings-main">
          <div className="settings-head">
            <div>
              <h2>
                {t(
                  `settings.page${currentMeta.id[0]!.toUpperCase()}${currentMeta.id.slice(1)}Label` as any,
                )}
              </h2>
              <div className="desc">
                {t(
                  `settings.page${currentMeta.id[0]!.toUpperCase()}${currentMeta.id.slice(1)}Desc` as any,
                )}
              </div>
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
        <div className="stitle">{t("settings.workspaceSection")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.currentWorkspace")}</div>
            <div className="h">{settings.workspaceDir || t("settings.notSelected")}</div>
          </div>
          <button type="button" className="btn" onClick={onPickWorkspace}>
            {t("settings.workspaceChange")}
          </button>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.editor")}</div>
            <div className="h">{t("settings.editorHint")}</div>
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
        <div className="stitle">{t("settings.behaviorSection")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.reasoningEffort")}</div>
            <div className="h">{t("settings.reasoningEffortHint")}</div>
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
            <div className="n">{t("settings.editMode")}</div>
            <div className="h">{t("settings.editModeHint")}</div>
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
            <div className="n">{t("settings.budget")}</div>
            <div className="h">{t("settings.budgetHint")}</div>
          </div>
          <input
            className="field"
            type="number"
            defaultValue={settings.budgetUsd ?? ""}
            placeholder={t("settings.budgetPlaceholder")}
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
      <div className="stitle">{t("settings.apiSection")}</div>
      <div className="setting-row">
        <div className="l">
          <div className="n">{t("settings.apiKey")}</div>
          <div className="h">
            {apiKeyPrefix
              ? t("settings.apiKeySet", { prefix: apiKeyPrefix })
              : t("settings.apiKeyNotSet")}
          </div>
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
            {t("settings.apiKeySave")}
          </button>
        </div>
      </div>
      <div className="setting-row">
        <div className="l">
          <div className="n">{t("settings.baseUrl")}</div>
          <div className="h">{t("settings.baseUrlHint")}</div>
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
      desc: t("settings.modelAutoDesc"),
      ctx: "—",
      out: "—",
    },
    {
      id: "flash" as const,
      name: "deepseek-v4-flash",
      badge: "FLASH",
      desc: t("settings.modelFlashDesc"),
      ctx: "1M",
      out: "8K",
    },
    {
      id: "pro" as const,
      name: "deepseek-v4-pro",
      badge: "PRO",
      desc: t("settings.modelProDesc"),
      ctx: "1M",
      out: "32K",
    },
  ];
  return (
    <section className="section">
      <div className="stitle">{t("settings.defaultModelCurrent", { model: settings.model })}</div>
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
                <span className="k">{t("settings.ctxWindow")} </span>
                <span className="v">{m.ctx}</span>
              </div>
              <div>
                <span className="k">{t("settings.maxOutput")} </span>
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
          {t("settings.mcpConfigured", { count: specs.length })}
          {bridged ? (
            <span style={{ color: "var(--accent)", marginLeft: 8, fontSize: 11 }}>
              {t("settings.mcpBridged")}
            </span>
          ) : (
            <span style={{ color: "var(--muted)", marginLeft: 8, fontSize: 11 }}>
              {t("settings.mcpNotBridged")}
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
            {t("settings.mcpEmpty")}
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
                  {t("settings.mcpRemove")}
                </button>
              </div>
              {s.parseError ? (
                <div className="desc" style={{ color: "var(--danger)" }}>
                  {t("settings.parseError", { error: s.parseError })}
                </div>
              ) : null}
            </div>
          ))
        )}
      </section>
      <section className="section">
        <div className="stitle">{t("settings.mcpAddSection")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.mcpSpecLabel")}</div>
            <div className="h" dangerouslySetInnerHTML={{ __html: t("settings.mcpSpecFormat") }} />
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
            <button type="button" className="btn primary" disabled={!draft.trim()} onClick={submit}>
              {t("settings.mcpAdd")}
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
      <div className="stitle">{t("settings.skillsLoaded", { count: skills.length })}</div>
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
          {t("settings.skillsEmpty")}
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
      <div className="stitle">{t("settings.memorySection")}</div>
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
        {t("settings.memoryDesc")}
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
        <div className="stitle">{t("settings.editMode")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.appMode")}</div>
            <div className="h">{t("settings.editModeHint")}</div>
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
        <div className="stitle">{t("settings.ruleAutoApprovalSection")}</div>
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
          {t("settings.ruleAutoApprovalHint")}
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
          <div className="l">{t("settings.balanceLabel")}</div>
          <div className="v ok">
            {balance
              ? `${balance.currency === "USD" ? "$" : "¥"} ${balance.total.toFixed(2)}`
              : "—"}
          </div>
          <div className="sub">
            {balance && !balance.isAvailable
              ? t("settings.balanceLow")
              : t("settings.balanceAvailable")}
          </div>
        </div>
        <div className="bill-card">
          <div className="l">{t("settings.sessionCost")}</div>
          <div className="v">
            {symbol} {usage.totalCostUsd.toFixed(4)}
          </div>
          <div className="sub">prompt {usage.totalPromptTokens.toLocaleString()} t</div>
        </div>
        <div className="bill-card">
          <div className="l">{t("settings.cacheHitRate")}</div>
          <div className="v acc">{hitPct}%</div>
          <div className="sub">
            hit {usage.cacheHitTokens.toLocaleString()} / miss{" "}
            {usage.cacheMissTokens.toLocaleString()}
          </div>
        </div>
      </div>
    </>
  );
}

function PageShortcuts() {
  const rows: { nm: string; keys: string[] }[] = [
    { nm: t("settings.shortcutNewChat"), keys: ["⌘", "N"] },
    { nm: t("settings.shortcutNewTab"), keys: ["⌘", "T"] },
    { nm: t("settings.shortcutCloseTab"), keys: ["⌘", "W"] },
    { nm: t("settings.shortcutCommandPalette"), keys: ["⌘", "K"] },
    { nm: t("settings.shortcutFocusComposer"), keys: ["⌘", "L"] },
    { nm: t("settings.shortcutSwitchTab"), keys: ["⌘", "⇥"] },
    { nm: t("settings.shortcutAbort"), keys: ["esc"] },
    { nm: t("settings.shortcutSettings"), keys: ["⌘", ","] },
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
