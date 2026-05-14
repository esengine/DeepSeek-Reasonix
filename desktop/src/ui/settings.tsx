import { type ReactNode, useState } from "react";
import type { Balance, Settings as SettingsType, UsageStats } from "../App";
import { type Lang, setLang, t, useLang } from "../i18n";
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

const PAGE_KEYS: { id: PageId; labelKey: string; descKey: string; icon: keyof typeof I }[] = [
  {
    id: "general",
    labelKey: "settings.navGeneral",
    descKey: "settings.navGeneralDesc",
    icon: "cog",
  },
  {
    id: "models",
    labelKey: "settings.navModels",
    descKey: "settings.navModelsDesc",
    icon: "brain",
  },
  { id: "mcp", labelKey: "settings.navMcp", descKey: "settings.navMcpDesc", icon: "wrench" },
  { id: "skills", labelKey: "settings.navSkills", descKey: "settings.navSkillsDesc", icon: "zap" },
  {
    id: "memory",
    labelKey: "settings.navMemory",
    descKey: "settings.navMemoryDesc",
    icon: "bookmark",
  },
  { id: "rules", labelKey: "settings.navRules", descKey: "settings.navRulesDesc", icon: "shield" },
  {
    id: "billing",
    labelKey: "settings.navBilling",
    descKey: "settings.navBillingDesc",
    icon: "coin",
  },
  {
    id: "shortcuts",
    labelKey: "settings.navShortcuts",
    descKey: "settings.navShortcutsDesc",
    icon: "cpu",
  },
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
  useLang();
  const [page, setPage] = useState<PageId>(initialPage ?? "general");
  const current = PAGE_KEYS.find((p) => p.id === page) ?? PAGE_KEYS[0]!;
  return (
    <div className="settings-mask" onClick={onClose}>
      <div className="settings" onClick={(e) => e.stopPropagation()}>
        <nav className="settings-side">
          <div className="sg">{t("settings.title")}</div>
          {PAGE_KEYS.map((p) => (
            <div
              key={p.id}
              className="row"
              data-active={page === p.id}
              onClick={() => setPage(p.id)}
            >
              <span className="ico">{I[p.icon]({ size: 13 })}</span>
              <span>{t(p.labelKey as never)}</span>
            </div>
          ))}
        </nav>
        <div className="settings-main">
          <div className="settings-head">
            <div>
              <h2>{t(current.labelKey as never)}</h2>
              <div className="desc">{t(current.descKey as never)}</div>
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
  const lang = useLang();
  const [editorDraft, setEditorDraft] = useState(settings.editor ?? "");
  return (
    <>
      <section className="section">
        <div className="stitle">{t("settings.language")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.language")}</div>
            <div className="h">{t("settings.languageHint")}</div>
          </div>
          <div className="seg-ctrl">
            {(["en", "zh-CN"] as const).map((code) => (
              <button
                type="button"
                key={code}
                data-on={lang === code}
                onClick={() => setLang(code as Lang)}
              >
                {code === "en" ? t("settings.langEn") : t("settings.langZhCn")}
              </button>
            ))}
          </div>
        </div>
      </section>

      <section className="section">
        <div className="stitle">{t("settings.workspace")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.workspace")}</div>
            <div className="h">{settings.workspaceDir || t("settings.workspaceUnset")}</div>
          </div>
          <button type="button" className="btn" onClick={onPickWorkspace}>
            {t("settings.pickWorkspaceBtn")}
          </button>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.editor")}</div>
            <div className="h">{t("settings.editorHintShort")}</div>
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
            <div className="h">{t("settings.reasoningEffortShort")}</div>
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
            <div className="h">{t("settings.editModeShort")}</div>
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
            <div className="n">{t("settings.budgetUsdLabel")}</div>
            <div className="h">{t("settings.budgetUsdHint")}</div>
          </div>
          <input
            className="field"
            type="number"
            defaultValue={settings.budgetUsd ?? ""}
            placeholder={t("settings.budgetUsdPlaceholder")}
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
  useLang();
  const [key, setKey] = useState("");
  const [urlDraft, setUrlDraft] = useState(baseUrl ?? "");
  return (
    <section className="section">
      <div className="stitle">{t("settings.apiSectionTitle")}</div>
      <div className="setting-row">
        <div className="l">
          <div className="n">{t("settings.apiKey")}</div>
          <div className="h">
            {apiKeyPrefix
              ? t("settings.apiKeyConfiguredPrefix", { prefix: apiKeyPrefix })
              : t("settings.apiKeyUnconfigured")}
          </div>
        </div>
        <div style={{ display: "flex", gap: 6 }}>
          <input
            className="field mono"
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder={t("settings.apiKeyPlaceholder")}
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
            {t("settings.apiKeySaveBtn")}
          </button>
        </div>
      </div>
      <div className="setting-row">
        <div className="l">
          <div className="n">{t("settings.baseUrl")}</div>
          <div className="h">{t("settings.baseUrlHintShort")}</div>
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
  useLang();
  const presets = [
    {
      id: "auto" as const,
      name: t("settings.modelAutoName"),
      badge: "AUTO",
      desc: t("settings.modelAutoDesc"),
      ctx: "—",
      out: "—",
    },
    {
      id: "flash" as const,
      name: t("settings.modelFlashName"),
      badge: "FLASH",
      desc: t("settings.modelFlashDesc"),
      ctx: "1M",
      out: "8K",
    },
    {
      id: "pro" as const,
      name: t("settings.modelProName"),
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
                <span className="k">{t("settings.modelCtxLabel")} </span>
                <span className="v">{m.ctx}</span>
              </div>
              <div>
                <span className="k">{t("settings.modelOutLabel")} </span>
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
  useLang();
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
          {t("settings.mcpConfiguredCount", { count: specs.length })}
          {bridged ? (
            <span style={{ color: "var(--accent)", marginLeft: 8, fontSize: 11 }}>
              · {t("settings.mcpBridgedTag")}
            </span>
          ) : (
            <span style={{ color: "var(--muted)", marginLeft: 8, fontSize: 11 }}>
              · {t("settings.mcpNotBridgedHint")}
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
                  <div className="nm">{s.name ?? t("settings.mcpAnonymous")}</div>
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
                  {t("settings.mcpParseErrorPrefix")}
                  {s.parseError}
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
            <div className="h">
              {t("settings.mcpSpecHintFormat")}
              <code>name=command args</code>
              {t("settings.mcpOr")}
              <code>name=https://host/sse</code>
            </div>
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <input
              className="field mono"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={t("settings.mcpSpecPlaceholder")}
              onKeyDown={(e) => {
                if (e.key === "Enter") submit();
              }}
            />
            <button type="button" className="btn primary" disabled={!draft.trim()} onClick={submit}>
              {t("settings.mcpAddBtn")}
            </button>
          </div>
        </div>
      </section>
    </>
  );
}

function PageSkills({ skills }: { skills: SkillInfo[] }) {
  useLang();
  return (
    <section className="section">
      <div className="stitle">{t("settings.skillsLoadedCount", { count: skills.length })}</div>
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
  useLang();
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
        {t("settings.memoryBody")}
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
  useLang();
  return (
    <>
      <section className="section">
        <div className="stitle">{t("settings.rulesEditModeSection")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.rulesApplyMode")}</div>
            <div className="h">{t("settings.rulesApplyModeHint")}</div>
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
        <div className="stitle">{t("settings.rulesCommandAutoSection")}</div>
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
          {t("settings.rulesCommandAutoBody")}
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
  useLang();
  const symbol = currency === "CNY" ? "¥" : "$";
  const totalTokens = usage.cacheHitTokens + usage.cacheMissTokens;
  const hitPct = totalTokens > 0 ? Math.round((usage.cacheHitTokens / totalTokens) * 100) : 0;
  return (
    <>
      <div className="bill-grid">
        <div className="bill-card">
          <div className="l">{t("settings.billingWalletBalance")}</div>
          <div className="v ok">
            {balance
              ? `${balance.currency === "USD" ? "$" : "¥"} ${balance.total.toFixed(2)}`
              : "—"}
          </div>
          <div className="sub">
            {balance && !balance.isAvailable
              ? t("settings.billingInsufficient")
              : t("settings.billingAvailable")}
          </div>
        </div>
        <div className="bill-card">
          <div className="l">{t("settings.billingSessionSpent")}</div>
          <div className="v">
            {symbol} {usage.totalCostUsd.toFixed(4)}
          </div>
          <div className="sub">
            {t("settings.billingPromptTokens", { n: usage.totalPromptTokens.toLocaleString() })}
          </div>
        </div>
        <div className="bill-card">
          <div className="l">{t("settings.billingCacheHitRate")}</div>
          <div className="v acc">{hitPct}%</div>
          <div className="sub">
            {t("settings.billingHitMiss", {
              hit: usage.cacheHitTokens.toLocaleString(),
              miss: usage.cacheMissTokens.toLocaleString(),
            })}
          </div>
        </div>
      </div>
    </>
  );
}

function PageShortcuts() {
  useLang();
  const rows: { nm: string; keys: string[] }[] = [
    { nm: t("settings.scNewSession"), keys: ["⌘", "N"] },
    { nm: t("settings.scNewTab"), keys: ["⌘", "T"] },
    { nm: t("settings.scCloseTab"), keys: ["⌘", "W"] },
    { nm: t("settings.scPalette"), keys: ["⌘", "K"] },
    { nm: t("settings.scFocusInput"), keys: ["⌘", "L"] },
    { nm: t("settings.scSwitchTab"), keys: ["⌘", "⇥"] },
    { nm: t("settings.scAbortStream"), keys: ["esc"] },
    { nm: t("settings.scOpenSettings"), keys: ["⌘", ","] },
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
