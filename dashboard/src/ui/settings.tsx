import { type ReactNode, useEffect, useState } from "react";
import type { Balance, Settings as SettingsType, UsageStats } from "../App";
import { getLangLabel, getSupportedLangs, setLang, t, useLang } from "../i18n";
import { I } from "../icons";
import type {
  McpSpecInfo,
  MemoryDetail,
  MemoryEntryInfo,
  SettingsPatch,
  SkillInfo,
} from "../protocol";
import {
  describeQQRowSummary,
  getQQConnectIntent,
  getQQStatusLabel,
  type QQDesktopSettingsState,
} from "../qq-settings";
import {
  FONT_FAMILY,
  FONT_SCALE,
  type FontFamily,
  type FontScale,
  THEME,
  THEME_STYLES,
  type Theme,
  type ThemeStyle,
  themeForStyle,
} from "../theme";
import { Shortcut, type ShortcutKey } from "./shortcut";

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
  theme,
  themeStyle,
  onSetTheme,
  onSetThemeStyle,
  fontScale,
  onSetFontScale,
  fontFamily,
  onSetFontFamily,
  initialPage,
  mcpSpecs,
  mcpBridged,
  skills,
  memory,
  memoryDetail,
  qq,
  onClose,
  onSave,
  onSaveApiKey,
  onLoadQQ,
  onConnectQQ,
  onDisconnectQQ,
  onSaveQQConfig,
  onOpenQQApplyLink,
  onPickWorkspace,
  onAddMcpSpec,
  onRemoveMcpSpec,
  onReadMemory,
}: {
  settings: SettingsType;
  balance: Balance | null;
  usage: UsageStats;
  currency: "CNY" | "USD";
  theme: Theme;
  themeStyle: ThemeStyle;
  onSetTheme: (theme: Theme) => void;
  onSetThemeStyle: (style: ThemeStyle) => void;
  fontScale: FontScale;
  onSetFontScale: (scale: FontScale) => void;
  fontFamily: FontFamily;
  onSetFontFamily: (family: FontFamily) => void;
  initialPage?: PageId;
  mcpSpecs: McpSpecInfo[];
  mcpBridged: boolean;
  skills: SkillInfo[];
  memory: MemoryEntryInfo[];
  memoryDetail: MemoryDetail | null;
  qq: QQDesktopSettingsState | null;
  onClose: () => void;
  onSave: (patch: SettingsPatch) => void;
  onSaveApiKey: (key: string) => void;
  onLoadQQ: () => void;
  onConnectQQ: () => void;
  onDisconnectQQ: () => void;
  onSaveQQConfig: (patch: { appId?: string; appSecret?: string; sandbox: boolean }) => void;
  onOpenQQApplyLink: () => void;
  onPickWorkspace: () => void;
  onAddMcpSpec: (spec: string) => void;
  onRemoveMcpSpec: (spec: string) => void;
  onReadMemory: (path: string) => void;
}) {
  const [page, setPage] = useState<PageId>(initialPage ?? "general");
  const [qqConfigureOpen, setQQConfigureOpen] = useState(false);
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
              <PageGeneral
                settings={settings}
                theme={theme}
                themeStyle={themeStyle}
                onSetTheme={onSetTheme}
                onSetThemeStyle={onSetThemeStyle}
                fontScale={fontScale}
                onSetFontScale={onSetFontScale}
                fontFamily={fontFamily}
                onSetFontFamily={onSetFontFamily}
                onSave={onSave}
                onPickWorkspace={onPickWorkspace}
              />
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
            {page === "skills" && (
              <PageSkills
                skills={skills}
                subagentModels={settings.subagentModels ?? {}}
                onSave={onSave}
              />
            )}
            {page === "memory" && (
              <PageMemory entries={memory} detail={memoryDetail} onRead={onReadMemory} />
            )}
            {page === "rules" && <PageRules settings={settings} onSave={onSave} />}
            {page === "billing" && (
              <PageBilling balance={balance} usage={usage} currency={currency} />
            )}
            {page === "shortcuts" && <PageShortcuts />}
            {page === "general" ? (
              <>
                <ApiKeySection
                  baseUrl={settings.baseUrl}
                  apiKeyPrefix={settings.apiKeyPrefix}
                  onSave={onSave}
                  onSaveApiKey={onSaveApiKey}
                />
                <QQChannelSection
                  qq={qq}
                  configureOpen={qqConfigureOpen}
                  onOpenConfigure={() => {
                    onLoadQQ();
                    setQQConfigureOpen(true);
                  }}
                  onCloseConfigure={() => setQQConfigureOpen(false)}
                  onConnect={onConnectQQ}
                  onDisconnect={onDisconnectQQ}
                  onSaveConfig={onSaveQQConfig}
                  onSaveAndConnect={(patch) => {
                    onSaveQQConfig(patch);
                    onConnectQQ();
                  }}
                  onOpenApplyLink={onOpenQQApplyLink}
                />
              </>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}

export function QQChannelSection({
  qq,
  configureOpen,
  onOpenConfigure,
  onCloseConfigure,
  onConnect,
  onDisconnect,
  onSaveConfig,
  onSaveAndConnect,
  onOpenApplyLink,
}: {
  qq: QQDesktopSettingsState | null;
  configureOpen: boolean;
  onOpenConfigure: () => void;
  onCloseConfigure: () => void;
  onConnect: () => void;
  onDisconnect: () => void;
  onSaveConfig: (patch: { appId?: string; appSecret?: string; sandbox: boolean }) => void;
  onSaveAndConnect: (patch: { appId?: string; appSecret?: string; sandbox: boolean }) => void;
  onOpenApplyLink: () => void;
}) {
  const current = qq ?? {
    appId: undefined,
    appSecret: undefined,
    sandbox: true,
    enabled: false,
    configured: false,
    runtimeState: "disconnected",
    access: "open (unbound)",
  };
  const [appId, setAppId] = useState(current.appId ?? "");
  const [appSecret, setAppSecret] = useState(current.appSecret ?? "");
  const [sandbox, setSandbox] = useState(current.sandbox ?? true);

  useEffect(() => {
    setAppId(current.appId ?? "");
    setAppSecret(current.appSecret ?? "");
    setSandbox(current.sandbox ?? true);
  }, [current.appId, current.appSecret, current.sandbox, configureOpen]);

  const savePatch = { appId, appSecret, sandbox };

  return (
    <section className="section">
      <div className="stitle">{t("settings.qqSection")}</div>
      {!configureOpen ? (
        <div className="setting-row qq-setting-row">
          <div className="l">
            <div className="n">{t("settings.qqTitle")}</div>
            <div className="h">{describeQQRowSummary(current)}</div>
          </div>
          <div className="qq-row-actions">
            <button
              type="button"
              className={`btn qq-status-btn qq-status-${
                current.runtimeState === "connected"
                  ? "on"
                  : current.runtimeState === "connecting"
                    ? "connecting"
                    : current.runtimeState === "failed"
                      ? "failed"
                      : "off"
              }`}
              onClick={() => {
                if (getQQConnectIntent(current) === "configure") {
                  onOpenConfigure();
                  return;
                }
                if (current.runtimeState === "connected") {
                  onDisconnect();
                  return;
                }
                onConnect();
              }}
            >
              {getQQStatusLabel(current)}
            </button>
            <button type="button" className="btn" onClick={onOpenConfigure}>
              {t("settings.qqConfigure")}
            </button>
          </div>
        </div>
      ) : (
        <div className="qq-config-card">
          <div className="qq-config-head">
            <div>
              <div className="n">{t("settings.qqConfigureTitle")}</div>
              <div className="h">{t("settings.qqConfigureHint")}</div>
            </div>
            <button type="button" className="btn" onClick={onCloseConfigure}>
              {t("settings.qqBack")}
            </button>
          </div>
          <div className="setting-row">
            <div className="l">
              <div className="n">{t("settings.qqAppId")}</div>
            </div>
            <input
              className="field mono"
              value={appId}
              onChange={(e) => setAppId(e.target.value)}
              placeholder="QQ Open Platform App ID"
            />
          </div>
          <div className="setting-row">
            <div className="l">
              <div className="n">{t("settings.qqAppSecret")}</div>
            </div>
            <input
              className="field mono"
              type="password"
              value={appSecret}
              onChange={(e) => setAppSecret(e.target.value)}
              placeholder="QQ Open Platform App Secret"
            />
          </div>
          <div className="setting-row">
            <div className="l">
              <div className="n">{t("settings.qqEnvironment")}</div>
            </div>
            <div className="seg-ctrl">
              <button type="button" data-on={sandbox} onClick={() => setSandbox(true)}>
                {t("settings.qqSandbox")}
              </button>
              <button type="button" data-on={!sandbox} onClick={() => setSandbox(false)}>
                {t("settings.qqProduction")}
              </button>
            </div>
          </div>
          <div className="setting-row">
            <div className="l">
              <div className="n">{t("settings.qqApplyLabel")}</div>
            </div>
            <button type="button" className="btn" onClick={onOpenApplyLink}>
              {t("settings.qqApplyAction")}
            </button>
          </div>
          <div className="qq-config-actions">
            <button
              type="button"
              className="btn"
              onClick={() => {
                onSaveConfig(savePatch);
                onCloseConfigure();
              }}
            >
              {t("settings.qqSave")}
            </button>
            <button
              type="button"
              className="btn primary"
              onClick={() => {
                onSaveAndConnect(savePatch);
                onCloseConfigure();
              }}
            >
              {t("settings.qqSaveAndConnect")}
            </button>
          </div>
        </div>
      )}
    </section>
  );
}

function PageGeneral({
  settings,
  theme,
  themeStyle,
  onSetTheme,
  onSetThemeStyle,
  fontScale,
  onSetFontScale,
  fontFamily,
  onSetFontFamily,
  onSave,
  onPickWorkspace,
}: {
  settings: SettingsType;
  theme: Theme;
  themeStyle: ThemeStyle;
  onSetTheme: (theme: Theme) => void;
  onSetThemeStyle: (style: ThemeStyle) => void;
  fontScale: FontScale;
  onSetFontScale: (scale: FontScale) => void;
  fontFamily: FontFamily;
  onSetFontFamily: (family: FontFamily) => void;
  onSave: (patch: SettingsPatch) => void;
  onPickWorkspace: () => void;
}) {
  const [editorDraft, setEditorDraft] = useState(settings.editor ?? "");
  const lang = useLang();
  return (
    <>
      <section className="section">
        <div className="stitle">{t("settings.appearanceSection")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.theme")}</div>
            <div className="h">{t("settings.themeHint")}</div>
          </div>
          <div className="seg-ctrl">
            <button
              type="button"
              data-on={theme === THEME.DARK}
              onClick={() => onSetTheme(THEME.DARK)}
            >
              {t("settings.themeDark")}
            </button>
            <button
              type="button"
              data-on={theme === THEME.LIGHT}
              onClick={() => onSetTheme(THEME.LIGHT)}
            >
              {t("settings.themeLight")}
            </button>
          </div>
        </div>
        <div className="setting-row theme-style-row">
          <div className="l">
            <div className="n">{t("settings.themeStyle")}</div>
            <div className="h">{t("settings.themeStyleHint")}</div>
          </div>
          <div className="style-grid">
            {THEME_STYLES.map((style) => (
              <button
                key={style}
                type="button"
                className="style-card"
                data-on={themeStyle === style}
                data-style={style}
                onClick={() => onSetThemeStyle(style)}
              >
                <span className="style-card-head">
                  <span className="style-name">
                    {t(`settings.themeStyle${style[0]!.toUpperCase()}${style.slice(1)}` as any)}
                  </span>
                  <span className="style-mode">
                    {themeForStyle(style) === THEME.DARK
                      ? t("settings.themeDark")
                      : t("settings.themeLight")}
                  </span>
                </span>
                <span className="style-swatches" aria-hidden="true">
                  <span />
                  <span />
                  <span />
                </span>
                <span className="style-desc">
                  {t(`settings.themeStyle${style[0]!.toUpperCase()}${style.slice(1)}Desc` as any)}
                </span>
              </button>
            ))}
          </div>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.fontScale")}</div>
            <div className="h">{t("settings.fontScaleHint")}</div>
          </div>
          <div className="seg-ctrl">
            <button
              type="button"
              data-on={fontScale === FONT_SCALE.SMALL}
              onClick={() => onSetFontScale(FONT_SCALE.SMALL)}
            >
              {t("settings.fontScaleSmall")}
            </button>
            <button
              type="button"
              data-on={fontScale === FONT_SCALE.MEDIUM}
              onClick={() => onSetFontScale(FONT_SCALE.MEDIUM)}
            >
              {t("settings.fontScaleMedium")}
            </button>
            <button
              type="button"
              data-on={fontScale === FONT_SCALE.LARGE}
              onClick={() => onSetFontScale(FONT_SCALE.LARGE)}
            >
              {t("settings.fontScaleLarge")}
            </button>
          </div>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.fontFamily")}</div>
            <div className="h">{t("settings.fontFamilyHint")}</div>
          </div>
          <div className="seg-ctrl">
            <button
              type="button"
              data-on={fontFamily === FONT_FAMILY.SANS}
              onClick={() => onSetFontFamily(FONT_FAMILY.SANS)}
            >
              {t("settings.fontFamilySans")}
            </button>
            <button
              type="button"
              data-on={fontFamily === FONT_FAMILY.SYSTEM}
              onClick={() => onSetFontFamily(FONT_FAMILY.SYSTEM)}
            >
              {t("settings.fontFamilySystem")}
            </button>
            <button
              type="button"
              data-on={fontFamily === FONT_FAMILY.SERIF}
              onClick={() => onSetFontFamily(FONT_FAMILY.SERIF)}
            >
              {t("settings.fontFamilySerif")}
            </button>
          </div>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.language")}</div>
            <div className="h">{t("settings.languageHint")}</div>
          </div>
          <div className="seg-ctrl">
            {getSupportedLangs().map((code) => (
              <button type="button" key={code} data-on={lang === code} onClick={() => setLang(code)}>
                {getLangLabel(code)}
              </button>
            ))}
          </div>
        </div>
      </section>

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
            {(["low", "medium", "high", "max"] as const).map((e) => (
              <button
                type="button"
                key={e}
                data-on={settings.reasoningEffort === e}
                onClick={() => onSave({ reasoningEffort: e })}
              >
                {e}
              </button>
            ))}
          </div>
        </div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.editMode")}</div>
            <div className="h">{t("settings.editModeHint")}</div>
          </div>
          <div className="seg-ctrl">
            {(["plan", "review", "auto", "yolo"] as const).map((m) => (
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
            <div className="n">{t("settings.showSystemEvents")}</div>
            <div className="h">{t("settings.showSystemEventsHint")}</div>
          </div>
          <div className="seg-ctrl">
            <button
              type="button"
              data-on={settings.showSystemEvents !== false}
              onClick={() => onSave({ showSystemEvents: true })}
            >
              {t("settings.shown")}
            </button>
            <button
              type="button"
              data-on={settings.showSystemEvents === false}
              onClick={() => onSave({ showSystemEvents: false })}
            >
              {t("settings.hidden")}
            </button>
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
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.webSearchEngine")}</div>
            <div className="h">{t("settings.webSearchEngineNote")}</div>
          </div>
          <select
            className="field"
            value={settings.webSearchEngine ?? "bing"}
            onChange={(e) =>
              onSave({
                webSearchEngine: e.target.value as
                  | "bing"
                  | "searxng"
                  | "metaso"
                  | "tavily"
                  | "perplexity"
                  | "exa"
                  | "ollama",
              })
            }
          >
            <option value="bing">{t("settings.webSearchEngineBing")}</option>
            <option value="searxng">{t("settings.webSearchEngineSearxng")}</option>
            <option value="metaso">{t("settings.webSearchEngineMetaso")}</option>
            <option value="tavily">{t("settings.webSearchEngineTavily")}</option>
            <option value="perplexity">{t("settings.webSearchEnginePerplexity")}</option>
            <option value="exa">{t("settings.webSearchEngineExa")}</option>
            <option value="ollama">{t("settings.webSearchEngineOllama")}</option>
          </select>
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

const KNOWN_MODELS = ["deepseek-v4-flash", "deepseek-v4-pro"] as const;

const EFFORT_VALUES = ["low", "medium", "high", "max"] as const;
type EffortValue = (typeof EFFORT_VALUES)[number];

function PageModels({
  settings,
  onSave,
}: {
  settings: SettingsType;
  onSave: (patch: SettingsPatch) => void;
}) {
  const [draft, setDraft] = useState(settings.model);
  useEffect(() => setDraft(settings.model), [settings.model]);
  const isKnown = (KNOWN_MODELS as readonly string[]).includes(settings.model);
  return (
    <>
      <section className="section">
        <div className="stitle">{t("settings.defaultModelCurrent", { model: settings.model })}</div>
        <div className="model-grid">
          {KNOWN_MODELS.map((id) => (
            <div
              key={id}
              className="mcard"
              data-on={settings.model === id}
              onClick={() => onSave({ model: id })}
            >
              <div className="nm">{id}</div>
            </div>
          ))}
        </div>
        <div className="setting-row" style={{ marginTop: 12 }}>
          <div className="l">
            <div className="n">{t("settings.modelCustom")}</div>
            <div className="h">{t("settings.modelCustomHint")}</div>
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <input
              className="field mono"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="deepseek-v4-flash"
            />
            <button
              type="button"
              className="btn primary"
              disabled={!draft.trim() || draft.trim() === settings.model}
              onClick={() => onSave({ model: draft.trim() })}
            >
              {t("settings.apiKeySave")}
            </button>
          </div>
        </div>
        {!isKnown ? (
          <div className="h" style={{ marginTop: 6 }}>
            {t("settings.modelCustomActive", { model: settings.model })}
          </div>
        ) : null}
      </section>
      <section className="section">
        <div className="stitle">{t("settings.effortSection")}</div>
        <div className="setting-row">
          <div className="l">
            <div className="n">{t("settings.reasoningEffort")}</div>
            <div className="h">{t("settings.reasoningEffortHint")}</div>
          </div>
          <div className="seg-ctrl">
            {EFFORT_VALUES.map((e) => (
              <button
                type="button"
                key={e}
                data-on={settings.reasoningEffort === e}
                onClick={() => onSave({ reasoningEffort: e as EffortValue })}
              >
                {e}
              </button>
            ))}
          </div>
        </div>
      </section>
    </>
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

function PageSkills({
  skills,
  subagentModels,
  onSave,
}: {
  skills: SkillInfo[];
  subagentModels: Record<string, "flash" | "pro">;
  onSave: (patch: SettingsPatch) => void;
}) {
  const setSubagentModel = (name: string, value: "flash" | "pro") => {
    onSave({ subagentModels: { ...subagentModels, [name]: value } });
  };
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
                      fontFamily: "Geist Mono, monospace",
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
              {s.runAs === "subagent" ? (
                <select
                  className="field"
                  style={{ marginLeft: "auto", minWidth: 96 }}
                  value={subagentModels[s.name] ?? "flash"}
                  onChange={(e) =>
                    setSubagentModel(s.name, e.target.value as "flash" | "pro")
                  }
                  title={t("settings.subagentModelHint")}
                >
                  <option value="flash">{t("settings.subagentModelFlash")}</option>
                  <option value="pro">{t("settings.subagentModelPro")}</option>
                </select>
              ) : null}
            </div>
            <div className="desc">{s.description}</div>
            <div
              style={{
                fontFamily: "Geist Mono, monospace",
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

function PageMemory({
  entries,
  detail,
  onRead,
}: {
  entries: MemoryEntryInfo[];
  detail: MemoryDetail | null;
  onRead: (path: string) => void;
}) {
  return (
    <section className="section">
      <div className="stitle">{t("settings.memorySection")}</div>
      {entries.length === 0 ? (
        <div className="muted-card">{t("settings.memoryDesc")}</div>
      ) : (
        <div className="memory-browser">
          <div className="memory-list">
            {entries.map((m) => (
              <button
                type="button"
                className="memory-item"
                data-active={detail?.path === m.path}
                key={m.path}
                onClick={() => onRead(m.path)}
              >
                <span className="memory-kind">{m.kind.replace("_", " ")}</span>
                <span className="memory-name">{m.description || m.name}</span>
                <span className="memory-path">{m.path}</span>
              </button>
            ))}
          </div>
          <pre className="memory-detail">
            {detail ? detail.body : t("settings.memoryDesc")}
          </pre>
        </div>
      )}
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
            {(["plan", "review", "auto", "yolo"] as const).map((m) => (
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
  const sessionCost = currency === "CNY" ? usage.totalCostUsd * 7.2 : usage.totalCostUsd;
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
            {symbol} {sessionCost.toFixed(4)}
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
  const rows: { nm: string; keys: ShortcutKey[] }[] = [
    { nm: t("settings.shortcutNewChat"), keys: ["mod", "N"] },
    { nm: t("settings.shortcutNewTab"), keys: ["mod", "T"] },
    { nm: t("settings.shortcutCloseTab"), keys: ["mod", "W"] },
    { nm: t("settings.shortcutCommandPalette"), keys: ["mod", "K"] },
    { nm: t("settings.shortcutFocusComposer"), keys: ["mod", "L"] },
    { nm: t("settings.shortcutSwitchTab"), keys: ["mod", "tab"] },
    { nm: t("settings.shortcutAbort"), keys: ["esc"] },
    { nm: t("settings.shortcutSettings"), keys: ["mod", ","] },
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

function SectionRow({ nm, keys }: { nm: string; keys: ShortcutKey[] }): ReactNode {
  return (
    <>
      <div className="nm">{nm}</div>
      <div className="keys">
        <Shortcut keys={keys} />
      </div>
    </>
  );
}
