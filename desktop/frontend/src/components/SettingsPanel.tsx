import { lazy, memo, Suspense, useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent, type ReactNode } from "react";
import { Check, CheckCircle2, ChevronDown, ChevronUp, Clipboard, GripVertical, KeyRound, Loader2, Play, QrCode, RefreshCw, Send } from "lucide-react";
import { asArray } from "../lib/array";
import { useDeferredClose } from "../lib/useMountTransition";
import { app } from "../lib/bridge";
import { normalizeLangPref, useI18n, useT, type DictKey, type LangPref } from "../lib/i18n";
import { apiKeyEnvFromProviderName, inferredVisionModels, mergedFetchedProviderModels, providerApiKeyEnvForSave, providerDefaultModel, providerIsConfigured, providerModelCandidates, providerRequiresKey } from "../lib/providerModels";
import { useUpdater } from "../lib/useUpdater";
import {
  THEME_STYLES,
  applyTheme,
  getTheme,
  getThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  type Theme,
  type ThemeStyle,
} from "../lib/theme";
import { TEXT_SIZES, applyTextSize, getTextSize, type TextSize } from "../lib/textSize";
import {
  applyFontFamily,
  applyMonoFontFamily,
  getFontFamily,
  getMonoFontFamily,
  getCustomFontName,
  getCustomMonoFontName,
  setCustomFontName,
  setCustomMonoFontName,
  type FontFamily,
  type MonoFontFamily,
} from "../lib/fontFamily";
import { getAvailableFontFamilies, getAvailableMonoFontFamilies } from "../lib/fontAvailability";
import { getDisplayMode, onDisplayModeChange, setDisplayMode as setLocalDisplayMode } from "../lib/displayMode";
import { DEFAULT_STATUS_BAR_ITEMS, normalizeStatusBarItems, type StatusBarItemId } from "../lib/statusBarItems";
import { normalizeToolApprovalMode } from "../lib/types";
import {
  comboFromKeyboardEvent,
  detectShortcutPlatform,
  formatShortcutCombo,
  onShortcutsChanged,
  resetCustomShortcuts,
  resolvedShortcutCombo,
  saveCustomShortcut,
  shortcutConflict,
  shortcutDefinitions,
  type ShortcutAction,
} from "../lib/keyboardShortcuts";
import type { BotAllowlistView, BotConnectionDiagnostic, BotConnectionView, BotInstallStartResult, BotSettingsView, HookConfigView, HooksSettingsView, NetworkView, ProviderView, SettingsTab, SettingsView } from "../lib/types";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";
import { getGenerativePreset, setGenerativePreset, generativeMusic, type GenerativePreset } from "../lib/generative-music";
import { SoundSelect } from "./SoundSelect";
import { getSuccessPreference, setSuccessPreference, getAttentionPreference, setAttentionPreference, playSuccessChime, playAttentionChime, type SoundWavPref } from "../lib/sound";
import { ModalCloseButton } from "./ModalCloseButton";
import { ShortcutComboDisplay } from "./ShortcutComboDisplay";

const SETTINGS_TABS: SettingsTab[] = ["general", "models", "bots", "mcp", "skills", "memory", "hooks", "shortcuts", "permissions", "sandbox", "network", "appearance", "updates"];
export type SettingsInitialFocus = { target: "bot-allowlist"; connectionId?: string };

const MCPServersSettingsPage = lazy(() => import("./CapabilitiesPanel").then((module) => ({ default: module.MCPServersSettingsPage })));
const SkillsSettingsPage = lazy(() => import("./CapabilitiesPanel").then((module) => ({ default: module.SkillsSettingsPage })));
const MemorySettingsPage = lazy(() => import("./MemoryPanel").then((module) => ({ default: module.MemorySettingsPage })));
const QRCodeSVG = lazy(() => import("qrcode.react").then((module) => ({ default: module.QRCodeSVG })));

// SettingsPanel is the desktop settings centre — a centred modal with left
// navigation and a right content area. It hosts all settings pages plus MCP,
// Skills, and Memory management, replacing the old per-feature drawers.
export function SettingsPanel({
  onClose,
  onChanged,
  initialTab,
  initialFocus,
  agentRunning = false,
}: {
  onClose: () => void;
  onChanged: (settings?: SettingsView | null) => void;
  initialTab?: SettingsTab;
  initialFocus?: SettingsInitialFocus;
  agentRunning?: boolean;
}) {
  const t = useT();
  const [s, setS] = useState<SettingsView | null>(null);
  const [loadingSettings, setLoadingSettings] = useState(true);
  const [settingsLoadFailed, setSettingsLoadFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [warning, setWarning] = useState<string | null>(null);
  const [theme, setThemeState] = useState<Theme>(getTheme());
  const [themeStyle, setThemeStyleState] = useState<ThemeStyle>(() => getThemeStyle(getTheme()));
  const [textSize, setTextSizeState] = useState<TextSize>(getTextSize());
  const [fontFamily, setFontFamilyState] = useState<FontFamily>(getFontFamily());
  const [monoFontFamily, setMonoFontFamilyState] = useState<MonoFontFamily>(getMonoFontFamily());
  const [customFontName, setCustomFontNameState] = useState<string>(getCustomFontName());
  const [customMonoFontName, setCustomMonoFontNameState] = useState<string>(getCustomMonoFontName());
  const [tab, setTab] = useState<SettingsTab>(initialTab === "providers" ? "models" : initialTab ?? "general");
  // Play the modal exit animation, then let the parent unmount us.
  const { status, requestClose } = useDeferredClose(onClose, 240);

  const reload = useCallback(async () => {
    setLoadingSettings(true);
    setSettingsLoadFailed(false);
    try {
      const next = normalizeSettingsView(await app.Settings());
      setS(next);
      return next;
    } catch {
      setS(null);
      setSettingsLoadFailed(true);
      return null;
    } finally {
      setLoadingSettings(false);
    }
  }, []);
  useEffect(() => {
    void reload();
    if (initialTab) setTab(initialTab === "providers" ? "models" : initialTab);
  }, [initialTab, reload]);
  useEffect(() => {
    if (!s) return;
    const nextTheme = normalizeThemePreference(s.desktopTheme);
    const nextStyle = normalizeThemeStyleForTheme(s.desktopThemeStyle, nextTheme);
    setThemeState(nextTheme);
    setThemeStyleState(nextStyle);
  }, [s?.desktopTheme, s?.desktopThemeStyle]);

  // apply runs a mutation, re-reads settings, and refreshes the topbar/model.
  const apply = useCallback(async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(null);
    setWarning(null);
    try {
      const result = await fn();
      const next = await reload();
      onChanged(next);
      if (typeof result === "string" && result.trim()) {
        setWarning(result.trim());
      }
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  }, [reload, onChanged]);
  const backgroundApply = useCallback(async (fn: () => Promise<void>) => {
    setErr(null);
    setWarning(null);
    try {
      await fn();
      const next = await reload();
      onChanged(next);
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    }
  }, [reload, onChanged]);

  // Close on Esc
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("[data-anchored-popover='active']")) requestClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [requestClose]);

  // The settings-reliant pages (general, models, network, permissions,
  // sandbox, appearance, updates) need SettingsView loaded. MCP, Skills, and Memory
  // load their own data and render regardless.
  const needsSettings = tab === "general" || tab === "models" || tab === "bots" || tab === "network" || tab === "permissions" || tab === "sandbox" || tab === "appearance" || tab === "updates";
  const lazySettingsPageFallback = <div className="empty">{t("settings.loading")}</div>;

  return (
    <div className="management-modal-backdrop settings-modal-backdrop" data-state={status} onClick={(e) => { if (e.target === e.currentTarget) requestClose(); }}>
      <div className="management-modal settings-modal" data-state={status}>
        <header className="management-modal__head settings-modal__head">
          <div className="management-modal__title settings-modal__title">{t("settings.title")}</div>
          <ModalCloseButton label={t("common.close")} onClick={requestClose} />
        </header>

        <div className="settings-center">
          <nav className="settings-center__nav" aria-label={t("settings.title")}>
            {SETTINGS_TABS.map((id) => (
              <button
                key={id}
                className={`settings-center__navitem${tab === id ? " settings-center__navitem--active" : ""}`}
                onClick={() => setTab(id)}
              >
                <span>{settingsTabLabel(id, t)}</span>
                {s && <small>{settingsTabMeta(id, s, t)}</small>}
              </button>
            ))}
          </nav>
          <main className="settings-center__content">
            {needsSettings && settingsLoadFailed && (
              <div className="banner banner--error settings-load-error" role="alert">
                <span>{t("settings.loadFailed")}</span>
                <button className="btn btn--small" type="button" onClick={() => void reload()}>{t("common.retry")}</button>
              </div>
            )}
            {needsSettings && err && <div className="banner banner--error">{err}</div>}
            {needsSettings && warning && <div className="banner banner--warning">{warning}</div>}
            {needsSettings && !s ? (
              loadingSettings ? <div className="empty">{t("settings.loading")}</div> : null
            ) : (
              <>
                {tab === "general" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><GeneralSection s={s} busy={busy} apply={apply} agentRunning={agentRunning} /></SettingsPageShell>}
                {tab === "models" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><ModelsSection s={s} busy={busy} apply={apply} backgroundApply={backgroundApply} /></SettingsPageShell>}
                {tab === "bots" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><BotsSection s={s} busy={busy} apply={apply} initialFocus={initialFocus} /></SettingsPageShell>}
                {tab === "mcp" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><Suspense fallback={lazySettingsPageFallback}><MCPServersSettingsPage /></Suspense></SettingsPageShell>}
                {tab === "skills" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><Suspense fallback={lazySettingsPageFallback}><SkillsSettingsPage /></Suspense></SettingsPageShell>}
                {tab === "memory" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><Suspense fallback={lazySettingsPageFallback}><MemorySettingsPage /></Suspense></SettingsPageShell>}
                {tab === "hooks" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><HooksSection onChanged={onChanged} /></SettingsPageShell>}
                {tab === "shortcuts" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><ShortcutsSection /></SettingsPageShell>}
                {tab === "permissions" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><PermissionsSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "sandbox" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><SandboxSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "network" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><NetworkSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "appearance" && s && (
                  <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}>
                    <AppearanceSection
                      theme={theme}
                      themeStyle={themeStyle}
                      textSize={textSize}
                      fontFamily={fontFamily}
                      monoFontFamily={monoFontFamily}
                      customFontName={customFontName}
                      customMonoFontName={customMonoFontName}
                      onTheme={(nextTheme) => {
                        applyTheme(nextTheme, themeStyle, { persist: false });
                        setThemeState(nextTheme);
                        void apply(() => app.SetDesktopAppearance(nextTheme, themeStyle));
                      }}
                      onThemeStyle={(style) => {
                        applyTheme(theme, style, { persist: false });
                        setThemeStyleState(style);
                        void apply(() => app.SetDesktopAppearance(theme, style));
                      }}
                      onTextSize={(size) => {
                        applyTextSize(size);
                        setTextSizeState(size);
                      }}
                      onFontFamily={(font) => {
                        applyFontFamily(font);
                        setFontFamilyState(font);
                      }}
                      onMonoFontFamily={(font) => {
                        applyMonoFontFamily(font);
                        setMonoFontFamilyState(font);
                      }}
                      onCustomFontNameChange={(name) => {
                        setCustomFontNameState(name);
                        setCustomFontName(name);
                        applyFontFamily("custom");
                      }}
                      onCustomMonoFontNameChange={(name) => {
                        setCustomMonoFontNameState(name);
                        setCustomMonoFontName(name);
                        applyMonoFontFamily("custom");
                      }}
                    />
                  </SettingsPageShell>
                )}
                {tab === "updates" && s && (
                  <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}>
                    <UpdatesSection
                      configPath={s.configPath}
                      checkUpdates={s.checkUpdates}
                      telemetry={s.telemetry !== false}
                      metrics={s.metrics !== false}
                      settingsBusy={busy}
                      applySettings={apply}
                    />
                  </SettingsPageShell>
                )}
              </>
            )}
          </main>
        </div>
      </div>
    </div>
  );
}

function SettingsPageShell({ s: _s, tab, children }: { s: SettingsView | null; tab: SettingsTab; busy: boolean; apply: (fn: () => Promise<unknown>) => Promise<void>; children: ReactNode }) {
  const t = useT();
  const descKey = `settings.pageDesc.${tab}` as keyof typeof import("../locales/en").en;
  const desc = t(descKey as any);
  return (
    <div className={`settings-page settings-page--${settingsPageKind(tab)} settings-page--${tab}`}>
      <div className="settings-page__header">
        <h2 className="settings-page__title">{settingsTabPageTitle(tab, t)}</h2>
        {typeof desc === "string" && desc !== `settings.pageDesc.${tab}` && <p className="settings-page__desc">{desc}</p>}
      </div>
      {children}
    </div>
  );
}

function settingsPageKind(tab: SettingsTab): "form" | "manager" {
  switch (tab) {
    case "models":
    case "mcp":
    case "skills":
    case "memory":
      return "manager";
    default:
      return "form";
  }
}

function SettingsSection({
  title,
  description,
  actions,
  children,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const hasHead = Boolean(title || description || actions);
  return (
    <section className="settings-section">
      {hasHead && (
        <div className="settings-section__head">
          <div>
            {title && <div className="settings-section__title">{title}</div>}
            {description && (
              <div className="settings-section__desc">
                <SettingsHint hint={description} />
              </div>
            )}
          </div>
          {actions && <div className="settings-section__actions">{actions}</div>}
        </div>
      )}
      <div className="settings-section__body">{children}</div>
    </section>
  );
}

function SettingsField({
  label,
  hint,
  children,
  className,
  stacked = false,
}: {
  label: ReactNode;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
  stacked?: boolean;
}) {
  return (
    <div className={`settings-field${stacked ? " settings-field--stacked" : ""}${className ? ` ${className}` : ""}`}>
      <div className="settings-field__copy">
        <div className="settings-field__label">{label}</div>
        {hint && (
          <div className="settings-field__hint">
            <SettingsHint hint={hint} />
          </div>
        )}
      </div>
      <div className="settings-field__control">{children}</div>
    </div>
  );
}

function SettingsHint({ hint }: { hint: ReactNode }) {
  if (typeof hint === "string" || typeof hint === "number") {
    const label = String(hint);
    return (
      <Tooltip label={label} fill block className="settings-field__hint-tooltip">
        <span className="settings-field__hint-line">{label}</span>
      </Tooltip>
    );
  }
  return hint;
}

function settingsTabPageTitle(id: SettingsTab, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "mcp": return t("settings.tab.mcp");
    case "skills": return t("settings.tab.skills");
    case "memory": return t("settings.tab.memory");
    case "shortcuts": return t("settings.tab.shortcuts");
    default: return settingsTabLabel(id, t);
  }
}

type SectionProps = {
  s: SettingsView;
  busy: boolean;
  apply: (fn: () => Promise<unknown>) => Promise<void>;
};

type ModelsSectionProps = SectionProps & {
  backgroundApply: (fn: () => Promise<void>) => Promise<void>;
};

function settingsTabLabel(id: SettingsTab, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "general":
      return t("settings.tab.general");
    case "models":
      return t("settings.tab.models");
    case "providers":
      return t("settings.tab.providers");
    case "bots":
      return t("settings.tab.bots");
    case "mcp":
      return t("settings.tab.mcp");
    case "skills":
      return t("settings.tab.skills");
    case "memory":
      return t("settings.tab.memory");
    case "hooks":
      return t("settings.tab.hooks");
    case "shortcuts":
      return t("settings.tab.shortcuts");
    case "network":
      return t("settings.tab.network");
    case "permissions":
      return t("settings.tab.permissions");
    case "sandbox":
      return t("settings.tab.sandbox");
    case "appearance":
      return t("settings.tab.appearance");
    case "updates":
      return t("settings.tab.updates");
  }
}

function settingsTabMeta(id: SettingsTab, s: SettingsView, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "models":
      return settingsModelMeta(s, t);
    case "general":
      return `${desktopLayoutStyleLabel(normalizeDesktopLayoutStyle(s.desktopLayoutStyle), t)} · ${closeBehaviorLabel(normalizeCloseBehavior(s.closeBehavior), t)}`;
    case "providers":
      return t("settings.providerCount", { n: s.providers.length });
    case "bots":
      return botSettingsMeta(s.bot, t);
    case "mcp":
      return t("caps.connectorsTab");
    case "skills":
      return t("caps.skillsTab");
    case "memory":
      return t("settings.tabSub.memory");
    case "hooks":
      return t("settings.tabSub.hooks");
    case "shortcuts":
      return t("settings.tabSub.shortcuts");
    case "network":
      return proxyModeLabel(normalizeProxyMode(s.network.proxyMode), t);
    case "permissions":
      return permissionModeLabel(s.permissions.mode, t);
    case "sandbox":
      return sandboxModeLabel(s.sandbox.bash, t);
    case "appearance":
      return t("settings.appearanceMeta");
    case "updates":
      return t("settings.updatesMeta");
  }
}

function settingsModelMeta(s: SettingsView, t: ReturnType<typeof useT>): string {
  const ref = toRef(s.defaultModel, s);
  if (!ref) return t("common.none");
  if (!ref.includes("/")) return ref;
  const [provider, ...modelParts] = ref.split("/");
  const model = modelParts.join("/") || ref;
  const providerView = s.providers.find((p) => p.name === provider);
  return `${modelProviderLabel(provider, providerView, t)} · ${model}`;
}

function botSettingsMeta(bot: BotSettingsView, t: ReturnType<typeof useT>): string {
  const normalized = normalizeBotSettings(bot);
  const connections = normalized.connections.length + (qqBotAdded(normalized.qq) ? 1 : 0);
  if (connections === 0) return t("settings.botNoConnections");
  if (!normalized.enabled) return t("settings.botDisabledWithConnections", { n: connections });
  return t("settings.botConnectionCount", { n: connections });
}

function ShortcutsSection() {
  const t = useT();
  const [platform] = useState(() => detectShortcutPlatform());
  const [revision, setRevision] = useState(0);
  const [recording, setRecording] = useState<ShortcutAction | null>(null);
  const [conflict, setConflict] = useState<{ action: ShortcutAction; conflictAction: ShortcutAction } | null>(null);

  useEffect(() => onShortcutsChanged(() => setRevision((value) => value + 1)), []);

  const definitions = shortcutDefinitions();
  const commitShortcut = (action: ShortcutAction, event: ReactKeyboardEvent<HTMLButtonElement>) => {
    const combo = comboFromKeyboardEvent(event.nativeEvent);
    if (!combo) return;
    event.preventDefault();
    event.stopPropagation();
    const conflictDefinition = shortcutConflict(action, combo, platform);
    if (conflictDefinition) {
      setConflict({ action, conflictAction: conflictDefinition.action });
      return;
    }
    saveCustomShortcut(action, combo);
    setConflict(null);
    setRecording(null);
    setRevision((value) => value + 1);
  };

  return (
    <SettingsSection
      title={t("settings.shortcutsTitle")}
      description={t("settings.shortcutsHint")}
      actions={
        <button
          className="chip chip--icon"
          type="button"
          title={t("settings.shortcutsResetAll")}
          aria-label={t("settings.shortcutsResetAll")}
          onClick={() => {
            resetCustomShortcuts();
            setConflict(null);
            setRecording(null);
            setRevision((value) => value + 1);
          }}
        >
          <RefreshCw size={14} />
        </button>
      }
    >
      <div className="shortcuts-settings" data-revision={revision}>
        {conflict && (
          <div className="shortcuts-settings__conflict" role="alert">
            {t("settings.shortcutsConflict", {
              action: t(definitions.find((definition) => definition.action === conflict.action)?.labelKey ?? "settings.tab.shortcuts"),
              conflict: t(definitions.find((definition) => definition.action === conflict.conflictAction)?.labelKey ?? "settings.tab.shortcuts"),
            })}
          </div>
        )}
        {definitions.map((definition) => {
          const resolved = resolvedShortcutCombo(definition.action, platform);
          const defaultCombo = definition.defaults[platform];
          const display = formatShortcutCombo(resolved, platform);
          const isCustom = formatShortcutCombo(resolved, platform) !== formatShortcutCombo(defaultCombo, platform);
          const isRecording = recording === definition.action;
          return (
            <div className="shortcuts-settings__row" key={definition.action}>
              <div className="shortcuts-settings__copy">
                <div className="shortcuts-settings__label">{t(definition.labelKey)}</div>
                <div className="shortcuts-settings__desc">{t(definition.descriptionKey)}</div>
              </div>
              <div className="shortcuts-settings__control">
                <button
                  className={`shortcuts-settings__key${isRecording ? " shortcuts-settings__key--recording" : ""}${definition.configurable === false ? " shortcuts-settings__key--locked" : ""}`}
                  type="button"
                  disabled={definition.configurable === false}
                  aria-label={isRecording ? t("settings.shortcutsRecording") : display}
                  aria-pressed={isRecording}
                  onClick={() => {
                    setRecording(definition.action);
                    setConflict(null);
                  }}
                  onKeyDown={(event) => isRecording && commitShortcut(definition.action, event)}
                >
                  {isRecording ? t("settings.shortcutsRecording") : <ShortcutComboDisplay combo={resolved} platform={platform} />}
                </button>
                <button
                  className="chip"
                  type="button"
                  disabled={!isCustom}
                  onClick={() => {
                    saveCustomShortcut(definition.action, null);
                    setConflict(null);
                    setRecording(null);
                    setRevision((value) => value + 1);
                  }}
                >
                  {t("settings.shortcutsReset")}
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </SettingsSection>
  );
}

// allRefs flattens providers into "provider/model" refs for the model selectors.
function allRefs(s: SettingsView): string[] {
  const out: string[] = [];
  for (const p of s.providers) {
    if (!p.added || !providerIsConfigured(p)) continue;
    for (const m of p.models) out.push(`${p.name}/${m}`);
  }
  return out;
}

// toRef normalises a stored model id (a provider name, a bare model, or a ref) to
// a "provider/model" ref so a <select> of refs can show it selected.
function toRef(model: string, s: SettingsView): string {
  if (!model) return "";
  if (model.includes("/")) return model;
  const byName = s.providers.find((p) => p.name === model);
  if (byName) return `${byName.name}/${byName.default || byName.models[0] || ""}`;
  const byModel = s.providers.find((p) => p.models.includes(model));
  if (byModel) return `${byModel.name}/${model}`;
  return model;
}

const PROXY_MODES = ["auto", "custom", "off"] as const;

// EFFORT_PRESETS is the canonical union of /effort levels the kernel
// recognises. The settings UI exposes these as toggleable checkboxes; users
// can additionally add arbitrary custom names via the "Add" input. The order
// here is what the user sees in the dropdown.
const EFFORT_PRESETS: readonly string[] = ["low", "medium", "high", "xhigh", "max"];
const REASONING_PROTOCOLS: readonly string[] = ["", "deepseek", "openai", "none"];
const PROXY_TYPES = ["http", "https", "socks5", "socks5h"] as const;
const LANGUAGE_PREFS: LangPref[] = ["", "zh", "en"];
const AUTO_PLAN_MODES = ["off", "on"] as const;
const TOOL_APPROVAL_MODES = ["ask", "auto", "yolo"] as const;
const BOT_TOOL_APPROVAL_MODES = ["", "ask", "auto", "yolo"] as const;

type ProxyMode = (typeof PROXY_MODES)[number];
type AutoPlanMode = (typeof AUTO_PLAN_MODES)[number];

function normalizeProxyMode(mode: string): ProxyMode {
  switch (mode) {
    case "custom":
      return "custom";
    case "off":
      return "off";
    default:
      return "auto";
  }
}

function normalizeNetworkView(network: NetworkView): NetworkView {
  return { ...network, proxyMode: normalizeProxyMode(network.proxyMode) };
}

function normalizeAutoPlan(mode: string | undefined): AutoPlanMode {
  return mode === "ask" || mode === "on" ? "on" : "off";
}

function normalizeReasoningProtocol(protocol: string | undefined): string {
  return REASONING_PROTOCOLS.includes(protocol ?? "") ? protocol ?? "" : "";
}

function normalizeReasoningLanguage(lang: string | undefined): string {
  const v = String(lang ?? "").trim().toLowerCase();
  return v === "zh" || v === "en" ? v : "auto";
}

function defaultBotSettings(): BotSettingsView {
  return {
    enabled: false,
    model: "",
    toolApprovalMode: "ask",
    maxSteps: 0,
    debounceMs: 1500,
    allowlist: {
      enabled: true,
      allowAll: false,
      qqUsers: [],
      feishuUsers: [],
      weixinUsers: [],
      qqGroups: [],
      feishuGroups: [],
      weixinGroups: [],
    },
    qq: { enabled: false, appId: "", appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: false, sandbox: false },
    feishu: {
      enabled: false,
      domain: "feishu",
      appId: "",
      appSecretEnv: "FEISHU_BOT_APP_SECRET",
      secretSet: false,
      verificationToken: "",
      mode: "webhook",
      webhookPort: 8080,
      requireMention: true,
    },
    weixin: {
      enabled: false,
      accountId: "default",
      tokenEnv: "WEIXIN_BOT_TOKEN",
      tokenSet: false,
      apiBase: "https://ilinkai.weixin.qq.com",
    },
    connections: [],
  };
}

function normalizeBotSettings(bot: BotSettingsView | null | undefined): BotSettingsView {
  const fallback = defaultBotSettings();
  const allowlist = bot?.allowlist ?? fallback.allowlist;
  const mode = bot?.feishu?.mode === "websocket" ? "websocket" : "webhook";
  return {
    ...fallback,
    ...bot,
    toolApprovalMode: normalizeBotToolApprovalMode(bot?.toolApprovalMode),
    maxSteps: Math.max(0, Number(bot?.maxSteps ?? fallback.maxSteps) || 0),
    debounceMs: Number(bot?.debounceMs) || fallback.debounceMs,
    allowlist: {
      ...fallback.allowlist,
      ...allowlist,
      qqUsers: asArray(allowlist.qqUsers),
      feishuUsers: asArray(allowlist.feishuUsers),
      weixinUsers: asArray(allowlist.weixinUsers),
      qqGroups: asArray(allowlist.qqGroups),
      feishuGroups: asArray(allowlist.feishuGroups),
      weixinGroups: asArray(allowlist.weixinGroups),
    },
    qq: { ...fallback.qq, ...bot?.qq },
    feishu: { ...fallback.feishu, ...bot?.feishu, domain: bot?.feishu?.domain === "lark" ? "lark" : "feishu", mode },
    weixin: { ...fallback.weixin, ...bot?.weixin },
    connections: asArray(bot?.connections).map(normalizeBotConnection),
  };
}

function normalizeBotConnection(raw: any) {
  const credential = raw?.credential ?? {};
  const workspaceRoot = String(raw?.workspaceRoot ?? "").trim();
  return {
    id: String(raw?.id ?? "").trim(),
    provider: String(raw?.provider ?? "").trim(),
    domain: String(raw?.domain ?? "").trim(),
    label: String(raw?.label ?? "").trim(),
    enabled: raw?.enabled !== false,
    status: String(raw?.status ?? "disconnected").trim(),
    model: String(raw?.model ?? "").trim(),
    toolApprovalMode: normalizeBotToolApprovalMode(raw?.toolApprovalMode, true),
    workspaceRoot,
    credential: {
      appId: String(credential.appId ?? "").trim(),
      appSecretEnv: String(credential.appSecretEnv ?? "").trim(),
      accountId: String(credential.accountId ?? "").trim(),
      tokenEnv: String(credential.tokenEnv ?? "").trim(),
      secretSet: Boolean(credential.secretSet),
    },
    sessionMappings: asArray(raw?.sessionMappings).map((item: any) => ({
      remoteId: String(item?.remoteId ?? "").trim(),
      sessionId: String(item?.sessionId ?? "").trim(),
      sessionSource: String(item?.sessionSource ?? "").trim(),
      chatType: String(item?.chatType ?? "").trim(),
      userId: String(item?.userId ?? "").trim(),
      threadId: String(item?.threadId ?? "").trim(),
      scope: normalizeBotMappingScope(item?.scope, item?.workspaceRoot ?? workspaceRoot),
      workspaceRoot: normalizeBotMappingScope(item?.scope, item?.workspaceRoot ?? workspaceRoot) === "project"
        ? String(item?.workspaceRoot ?? workspaceRoot).trim()
        : "",
      updatedAt: String(item?.updatedAt ?? "").trim(),
    })),
    lastError: String(raw?.lastError ?? "").trim(),
    createdAt: String(raw?.createdAt ?? "").trim(),
    updatedAt: String(raw?.updatedAt ?? "").trim(),
  };
}

function normalizeBotToolApprovalMode(mode: unknown, allowEmpty = false): "ask" | "auto" | "yolo" | "" {
  const raw = String(mode ?? "").trim().toLowerCase();
  if (raw === "") return allowEmpty ? "" : "ask";
  if (raw === "ask") return "ask";
  if (raw === "auto") return "auto";
  if (raw === "yolo" || raw === "full" || raw === "full-access" || raw === "bypass") return "yolo";
  return allowEmpty ? "" : "ask";
}

function normalizeBotMappingScope(scope: unknown, workspaceRoot: unknown): "global" | "project" {
  if (String(scope ?? "").trim() === "project") return "project";
  return String(workspaceRoot ?? "").trim() ? "project" : "global";
}

function normalizeProviderView(p: ProviderView): ProviderView {
  const visionModels = asArray(p.visionModels);
  const requiresKey = providerRequiresKey(p);
  return {
    ...p,
    builtIn: Boolean(p.builtIn),
    added: Boolean(p.added),
    models: asArray(p.models),
    visionModels,
    visionModelsConfigured: Boolean(p.visionModelsConfigured ?? visionModels.length > 0),
    modelsUrl: p.modelsUrl ?? "",
    reasoningProtocol: normalizeReasoningProtocol(p.reasoningProtocol),
    supportedEfforts: asArray(p.supportedEfforts),
    requiresKey,
    configured: providerIsConfigured({ ...p, requiresKey }),
    keySource: p.keySource ?? "",
    keySourcePath: p.keySourcePath ?? "",
  };
}

function normalizeSettingsView(view: SettingsView | null | undefined): SettingsView | null {
  if (!view) return null;
  const permissions = view.permissions ?? { mode: "ask", allow: [], ask: [], deny: [] };
  const sandbox = view.sandbox ?? { bash: "enforce", network: false, workspaceRoot: "", allowWrite: [], shell: "auto" };
  const network = view.network ?? {
    proxyMode: "auto",
    proxyUrl: "",
    noProxy: "",
    proxy: { type: "socks5", server: "", port: 0, username: "", password: "" },
  };
  const agent = view.agent ?? { temperature: 0, maxSteps: 0, plannerMaxSteps: 0, systemPrompt: "", coldResumePrune: true, reasoningLanguage: "auto" };
  agent.plannerMaxSteps = Number.isFinite(agent.plannerMaxSteps) ? Math.max(0, Math.trunc(agent.plannerMaxSteps)) : 0;
  agent.maxSteps = Number.isFinite(agent.maxSteps) ? Math.max(0, Math.trunc(agent.maxSteps)) : 0;
  agent.reasoningLanguage = normalizeReasoningLanguage(agent.reasoningLanguage);
  return {
    ...view,
    providers: asArray(view.providers).map(normalizeProviderView),
    officialProviders: asArray(view.officialProviders).map(normalizeProviderView),
    providerKinds: asArray(view.providerKinds),
    permissions: {
      ...permissions,
      allow: asArray(permissions.allow),
      ask: asArray(permissions.ask),
      deny: asArray(permissions.deny),
    },
    sandbox: {
      ...sandbox,
      allowWrite: asArray(sandbox.allowWrite),
    },
    network: {
      ...network,
      proxy: network.proxy ?? { type: "socks5", server: "", port: 0, username: "", password: "" },
    },
    agent,
    bot: normalizeBotSettings(view.bot),
    autoPlan: normalizeAutoPlan(view.autoPlan),
    defaultToolApprovalMode: normalizeToolApprovalMode(view.defaultToolApprovalMode),
    autoApproveTools: Boolean(view.autoApproveTools ?? view.bypass),
    bypass: Boolean(view.autoApproveTools ?? view.bypass),
    desktopLanguage: normalizeLangPref(view.desktopLanguage),
    desktopLayoutStyle: normalizeDesktopLayoutStyle(view.desktopLayoutStyle),
    desktopTheme: normalizeThemePreference(view.desktopTheme),
    desktopThemeStyle: normalizeThemeStyleForTheme(view.desktopThemeStyle, normalizeThemePreference(view.desktopTheme)),
    closeBehavior: normalizeCloseBehavior(view.closeBehavior),
    displayMode: normalizeDisplayMode(view.displayMode),
    statusBarStyle: normalizeStatusBarStyle(view.statusBarStyle),
    statusBarItems: normalizeStatusBarItems(view.statusBarItems),
    checkUpdates: view.checkUpdates !== false,
    memoryCompilerEnabled: view.memoryCompilerEnabled !== false,
  };
}

type CloseBehavior = "background" | "quit";

function normalizeCloseBehavior(mode: string | undefined): CloseBehavior {
  return mode === "quit" ? "quit" : "background";
}

type DisplayMode = "standard" | "compact";

function normalizeDisplayMode(mode: string | undefined): DisplayMode {
  return mode === "standard" || mode === "compact" ? mode : "standard";
}

type DesktopLayoutStyle = "classic" | "workbench" | "creation";

function normalizeDesktopLayoutStyle(style: string | undefined): DesktopLayoutStyle {
  if (style === "classic") return "classic";
  if (style === "creation") return "creation";
  return "workbench";
}

function desktopLayoutStyleLabel(style: DesktopLayoutStyle, t: ReturnType<typeof useT>): string {
  return t(`settings.desktopLayoutStyle.${style}`);
}

type StatusBarStyle = "icon" | "text";
type StatusBarDropPlacement = "before" | "after";
type StatusBarDragTarget = {
  id: StatusBarItemId;
  placement: StatusBarDropPlacement;
};

function normalizeStatusBarStyle(style: string | undefined): StatusBarStyle {
  return style === "icon" ? "icon" : "text";
}

function statusBarItemLabel(id: StatusBarItemId, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "model":
      return t("settings.statusBarItem.model");
    case "workspace":
      return t("settings.statusBarItem.workspace");
    case "git_branch":
      return t("settings.statusBarItem.gitBranch");
    case "cache":
      return t("status.cacheLabel");
    case "cache_avg":
      return t("status.cacheAvgLabel");
    case "session_tokens":
      return t("status.sessionTokensLabel");
    case "turn_tokens":
      return t("status.turnTokensLabel");
    case "turn_cost":
      return t("status.turnCostLabel");
    case "session_turns":
      return t("status.sessionTurnsLabel");
    case "context":
      return t("status.ctxLabel");
    case "compact":
      return t("status.compactLabel");
    case "cost":
      return t("status.costLabel");
    case "balance":
      return t("status.balanceLabel");
  }
}

function closeBehaviorLabel(mode: CloseBehavior, t: ReturnType<typeof useT>): string {
  return mode === "quit" ? t("settings.closeBehavior.quit") : t("settings.closeBehavior.background");
}

function permissionModeLabel(mode: string, t: ReturnType<typeof useT>): string {
  switch (mode) {
    case "allow":
      return t("settings.modeAllowShort");
    case "deny":
      return t("settings.modeDenyShort");
    default:
      return t("settings.modeAskShort");
  }
}

function sandboxModeLabel(mode: string, t: ReturnType<typeof useT>): string {
  return mode === "off" ? t("settings.bashOffShort") : t("settings.bashEnforceShort");
}

function reasoningProtocolLabel(protocol: string, t: ReturnType<typeof useT>): string {
  switch (protocol) {
    case "deepseek":
      return t("settings.reasoningProtocol.deepseek");
    case "openai":
      return t("settings.reasoningProtocol.openai");
    case "none":
      return t("settings.reasoningProtocol.none");
    default:
      return t("settings.reasoningProtocol.auto");
  }
}

function GeneralSection({ s, busy, apply, agentRunning }: SectionProps & { agentRunning: boolean }) {
  const { t, setPref } = useI18n();
  const closeBehavior = normalizeCloseBehavior(s.closeBehavior);
  const [displayMode, setDisplayMode] = useState<DisplayMode>(() => normalizeDisplayMode(getDisplayMode()));
  const [statusBarItemsExpanded, setStatusBarItemsExpanded] = useState(false);
  const [draggingStatusBarItem, setDraggingStatusBarItem] = useState<StatusBarItemId | null>(null);
  const [statusBarDragTarget, setStatusBarDragTargetState] = useState<StatusBarDragTarget | null>(null);
  const draggingStatusBarItemRef = useRef<StatusBarItemId | null>(null);
  const statusBarDragTargetRef = useRef<StatusBarDragTarget | null>(null);
  const mouseDragCleanupRef = useRef<(() => void) | null>(null);
  const soundPanelId = useId();
  const statusBarItemsPanelId = useId();
  useEffect(() => onDisplayModeChange((mode) => setDisplayMode(mode)), []);
  useEffect(() => () => mouseDragCleanupRef.current?.(), []);
  const autoPlan = normalizeAutoPlan(s.autoPlan);
  const defaultToolApprovalMode = normalizeToolApprovalMode(s.defaultToolApprovalMode);
  const memoryCompilerEnabled = s.memoryCompilerEnabled !== false;
  const languagePref = normalizeLangPref(s.desktopLanguage);
  const desktopLayoutStyle = normalizeDesktopLayoutStyle(s.desktopLayoutStyle);
  const [genMusicPreset, setGenMusicPreset] = useState<GenerativePreset>(getGenerativePreset());
  const [soundPref, setSoundPref] = useState<SoundWavPref>(getSuccessPreference());
  const [attentionPref, setAttentionPref] = useState<SoundWavPref>(getAttentionPreference());
  const [soundExpanded, setSoundExpanded] = useState(false);
  const statusBarStyle = normalizeStatusBarStyle(s.statusBarStyle);
  const statusBarItems = normalizeStatusBarItems(s.statusBarItems);
  const soundStatus = summarizeSoundStatus(genMusicPreset, soundPref, attentionPref);
  const visibleStatusItems = new Set<StatusBarItemId>(statusBarItems);
  const orderedStatusItems = [
    ...statusBarItems,
    ...DEFAULT_STATUS_BAR_ITEMS.filter((id) => !visibleStatusItems.has(id)),
  ];
  const applyStatusBarItems = (items: StatusBarItemId[]) => {
    const contentScrollTop = document.querySelector<HTMLElement>(".settings-center__content")?.scrollTop ?? 0;
    const navScrollTop = document.querySelector<HTMLElement>(".settings-center__nav")?.scrollTop ?? 0;
    const active = document.activeElement;
    if (active instanceof HTMLElement && active.closest(".status-bar-items-editor")) active.blur();
    void apply(() => app.SetStatusBarItems(items)).finally(() => {
      window.scrollTo(0, 0);
      requestAnimationFrame(() => {
        window.scrollTo(0, 0);
        const content = document.querySelector<HTMLElement>(".settings-center__content");
        const nav = document.querySelector<HTMLElement>(".settings-center__nav");
        if (content) content.scrollTop = Math.min(contentScrollTop, Math.max(0, content.scrollHeight - content.clientHeight));
        if (nav) nav.scrollTop = navScrollTop;
      });
    });
  };
  const toggleStatusBarItem = (id: StatusBarItemId) => {
    if (visibleStatusItems.has(id)) {
      if (statusBarItems.length <= 1) return;
      applyStatusBarItems(statusBarItems.filter((item) => item !== id));
      return;
    }
    applyStatusBarItems([...statusBarItems, id]);
  };
  const moveStatusBarItem = (id: StatusBarItemId, direction: -1 | 1) => {
    const idx = statusBarItems.indexOf(id);
    const nextIdx = idx + direction;
    if (idx < 0 || nextIdx < 0 || nextIdx >= statusBarItems.length) return;
    const next = [...statusBarItems];
    [next[idx], next[nextIdx]] = [next[nextIdx], next[idx]];
    applyStatusBarItems(next);
  };
  const reorderStatusBarItem = (fromId: StatusBarItemId, toId: StatusBarItemId, placement: StatusBarDropPlacement) => {
    const fromIdx = statusBarItems.indexOf(fromId);
    const toIdx = statusBarItems.indexOf(toId);
    if (fromIdx < 0 || toIdx < 0 || fromIdx === toIdx) return;
    const next = statusBarItems.filter((item) => item !== fromId);
    const insertAt = next.indexOf(toId);
    if (insertAt < 0) return;
    next.splice(placement === "after" ? insertAt + 1 : insertAt, 0, fromId);
    if (next.every((item, index) => item === statusBarItems[index])) return;
    applyStatusBarItems(next);
  };
  const statusBarItemFromPoint = (x: number, y: number): StatusBarDragTarget | null => {
    const row = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-statusbar-setting-item]");
    const id = row?.dataset.statusbarSettingItem as StatusBarItemId | undefined;
    if (!row || !id || !statusBarItems.includes(id)) return null;
    const rect = row.getBoundingClientRect();
    return { id, placement: y < rect.top + rect.height / 2 ? "before" : "after" };
  };
  const setStatusBarDragTarget = (target: StatusBarDragTarget | null) => {
    const current = statusBarDragTargetRef.current;
    if (current?.id === target?.id && current?.placement === target?.placement) return;
    statusBarDragTargetRef.current = target;
    setStatusBarDragTargetState(target);
  };
  const beginStatusBarDrag = (id: StatusBarItemId, visible: boolean): boolean => {
    if (busy || !visible) return false;
    mouseDragCleanupRef.current?.();
    mouseDragCleanupRef.current = null;
    draggingStatusBarItemRef.current = id;
    statusBarDragTargetRef.current = null;
    setDraggingStatusBarItem(id);
    setStatusBarDragTargetState(null);
    return true;
  };
  const updateStatusBarDrag = (clientX: number, clientY: number) => {
    const draggingId = draggingStatusBarItemRef.current;
    if (!draggingId) return;
    const target = statusBarItemFromPoint(clientX, clientY);
    setStatusBarDragTarget(target && target.id !== draggingId ? target : null);
  };
  const finishStatusBarDrag = (clientX?: number, clientY?: number) => {
    const draggingId = draggingStatusBarItemRef.current;
    let target = statusBarDragTargetRef.current;
    if (draggingId && clientX !== undefined && clientY !== undefined) {
      const pointerTarget = statusBarItemFromPoint(clientX, clientY);
      if (pointerTarget && pointerTarget.id !== draggingId) target = pointerTarget;
    }
    if (draggingId && target) reorderStatusBarItem(draggingId, target.id, target.placement);
    draggingStatusBarItemRef.current = null;
    statusBarDragTargetRef.current = null;
    setDraggingStatusBarItem(null);
    setStatusBarDragTargetState(null);
  };
  const cancelStatusBarDrag = () => {
    mouseDragCleanupRef.current?.();
    mouseDragCleanupRef.current = null;
    draggingStatusBarItemRef.current = null;
    statusBarDragTargetRef.current = null;
    setDraggingStatusBarItem(null);
    setStatusBarDragTargetState(null);
  };
  const startStatusBarPointerDrag = (event: PointerEvent<HTMLElement>, id: StatusBarItemId, visible: boolean) => {
    if (event.button !== 0 || !beginStatusBarDrag(id, visible)) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const moveStatusBarPointerDrag = (event: PointerEvent<HTMLElement>) => {
    if (!draggingStatusBarItemRef.current) return;
    event.preventDefault();
    updateStatusBarDrag(event.clientX, event.clientY);
  };
  const endStatusBarPointerDrag = (event: PointerEvent<HTMLElement>) => {
    if (!draggingStatusBarItemRef.current) return;
    event.preventDefault();
    try {
      event.currentTarget.releasePointerCapture(event.pointerId);
    } catch {
      // Pointer capture may already be released by the browser.
    }
    finishStatusBarDrag(event.clientX, event.clientY);
  };
  const cancelStatusBarPointerDrag = (event: PointerEvent<HTMLElement>) => {
    event.preventDefault();
    cancelStatusBarDrag();
  };
  const startStatusBarMouseDrag = (event: ReactMouseEvent<HTMLElement>, id: StatusBarItemId, visible: boolean) => {
    if (event.button !== 0 || !beginStatusBarDrag(id, visible)) return;
    event.preventDefault();
    const handleMove = (moveEvent: MouseEvent) => {
      moveEvent.preventDefault();
      updateStatusBarDrag(moveEvent.clientX, moveEvent.clientY);
    };
    const cleanup = () => {
      window.removeEventListener("mousemove", handleMove);
      window.removeEventListener("mouseup", handleUp);
    };
    const handleUp = (upEvent: MouseEvent) => {
      upEvent.preventDefault();
      cleanup();
      mouseDragCleanupRef.current = null;
      finishStatusBarDrag(upEvent.clientX, upEvent.clientY);
    };
    window.addEventListener("mousemove", handleMove);
    window.addEventListener("mouseup", handleUp);
    mouseDragCleanupRef.current = cleanup;
  };
  const setLanguage = (next: LangPref) => {
    setPref(next);
    void apply(() => app.SetDesktopLanguage(next));
  };
  return (
    <SettingsSection>
      <SettingsField label={t("settings.language")}>
        <div className="set-seg">
          {LANGUAGE_PREFS.map((pref) => (
            <button
              key={pref || "auto"}
              className={`set-seg__btn${languagePref === pref ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => setLanguage(pref)}
            >
              {pref === "" ? t("settings.langAuto") : pref === "zh" ? "中文" : "English"}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.desktopLayoutStyle")}>
        <div className="set-seg">
          {(["classic", "workbench", "creation"] as const).map((style) => (
            <button
              key={style}
              className={`set-seg__btn${desktopLayoutStyle === style ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetDesktopLayoutStyle(style))}
            >
              {desktopLayoutStyleLabel(style, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.closeBehavior")}>
        <div className="set-seg">
          {(["background", "quit"] as const).map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${closeBehavior === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetCloseBehavior(mode))}
            >
              {closeBehaviorLabel(mode, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.displayMode")}>
        <div className="set-seg">
          {(["standard", "compact"] as const).map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${displayMode === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => {
                setLocalDisplayMode(mode);
                void apply(() => app.SetDisplayMode(mode));
              }}
            >
              {t(`settings.displayMode.${mode}`)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.defaultToolApprovalMode")} hint={t("settings.defaultToolApprovalModeHint")}>
        <div className="set-seg">
          {TOOL_APPROVAL_MODES.map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${defaultToolApprovalMode === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetDefaultToolApprovalMode(mode))}
            >
              {t(`settings.defaultToolApprovalMode.${mode}`)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.autoPlan")}>
        <div className="set-seg">
          {AUTO_PLAN_MODES.map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${autoPlan === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetAutoPlan(mode))}
            >
              {t(`settings.autoPlan.${mode}`)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.memoryCompiler")} hint={t("settings.memoryCompilerHint")}>
        <ToggleSegment
          value={memoryCompilerEnabled}
          disabled={busy}
          onChange={(enabled) => void apply(() => app.SetMemoryCompilerEnabled(enabled))}
        />
      </SettingsField>
      <SettingsField label={t("settings.sound")} hint={t("settings.soundHint")} stacked>
        <div className={`settings-sound-editor${soundExpanded ? " settings-sound-editor--expanded" : ""}`}>
          <div className="settings-sound-editor__summary">
            <span className={`settings-sound-editor__status settings-sound-editor__status--${soundStatus}`}>
              {t(`settings.soundStatus.${soundStatus}`)}
            </span>
            <Tooltip label={t(soundExpanded ? "settings.soundCollapse" : "settings.soundExpand")}>
              <button
                type="button"
                className="settings-sound-editor__toggle"
                aria-expanded={soundExpanded}
                aria-controls={soundPanelId}
                aria-label={t(soundExpanded ? "settings.soundCollapse" : "settings.soundExpand")}
                onClick={() => setSoundExpanded((open) => !open)}
              >
                {soundExpanded ? <ChevronUp size={15} aria-hidden="true" /> : <ChevronDown size={15} aria-hidden="true" />}
              </button>
            </Tooltip>
          </div>
          {soundExpanded && (
            <div className="settings-sound-editor__list" id={soundPanelId}>
              <div className="settings-sound-row">
                <span className="settings-sound-row__label">{t("settings.generativeMusic")}</span>
                <GenMusicSelect
                  value={genMusicPreset}
                  onChange={(next) => {
                    setGenMusicPreset(next);
                    setGenerativePreset(next);
                    if (next === "off") {
                      generativeMusic.stop();
                    } else {
                      if (generativeMusic.isRunning) {
                        generativeMusic.setPreset(next);
                      } else if (agentRunning) {
                        generativeMusic.start(next);
                      }
                      generativeMusic.playPreview(next);
                    }
                  }}
                  onPreview={() => { if (genMusicPreset !== "off") generativeMusic.playPreview(genMusicPreset); }}
                  previewDisabled={genMusicPreset === "off"}
                />
              </div>
              <div className="settings-sound-row">
                <span className="settings-sound-row__label">{t("settings.notificationSoundSuccess")}</span>
                <SoundSelect
                  value={soundPref}
                  onChange={(next) => {
                    setSoundPref(next);
                    setSuccessPreference(next);
                    playSuccessChime();
                  }}
                  onPreview={playSuccessChime}
                  previewDisabled={soundPref === "off"}
                />
              </div>
              <div className="settings-sound-row">
                <span className="settings-sound-row__label">{t("settings.notificationSoundAttention")}</span>
                <SoundSelect
                  value={attentionPref}
                  onChange={(next) => {
                    setAttentionPref(next);
                    setAttentionPreference(next);
                    playAttentionChime();
                  }}
                  onPreview={playAttentionChime}
                  previewDisabled={attentionPref === "off"}
                />
              </div>
            </div>
          )}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.statusBarStyle")}>
        <div className="set-seg">
          {(["icon", "text"] as const).map((style) => (
            <button
              key={style}
              className={`set-seg__btn${statusBarStyle === style ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetStatusBarStyle(style))}
            >
              {t(`settings.statusBarStyle.${style}`)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.statusBarItems")} hint={t("settings.statusBarItemsHint")} stacked>
        <div className={`status-bar-items-editor${statusBarItemsExpanded ? " status-bar-items-editor--expanded" : ""}`}>
          <div className="status-bar-items-editor__summary">
            <span className="status-bar-items-editor__summary-text">
              {t("settings.statusBarItemsSummary", { visible: statusBarItems.length, total: DEFAULT_STATUS_BAR_ITEMS.length })}
            </span>
            <Tooltip label={t(statusBarItemsExpanded ? "settings.statusBarItemsCollapse" : "settings.statusBarItemsExpand")}>
              <button
                type="button"
                className="status-bar-items-editor__toggle"
                aria-expanded={statusBarItemsExpanded}
                aria-controls={statusBarItemsPanelId}
                aria-label={t(statusBarItemsExpanded ? "settings.statusBarItemsCollapse" : "settings.statusBarItemsExpand")}
                onClick={() => setStatusBarItemsExpanded((open) => !open)}
              >
                {statusBarItemsExpanded ? <ChevronUp size={15} aria-hidden="true" /> : <ChevronDown size={15} aria-hidden="true" />}
              </button>
            </Tooltip>
          </div>
          {statusBarItemsExpanded && (
            <div className="status-bar-items-editor__list" id={statusBarItemsPanelId}>
              {orderedStatusItems.map((id) => {
                const label = statusBarItemLabel(id, t);
                const visible = visibleStatusItems.has(id);
                const visibleIndex = statusBarItems.indexOf(id);
                const disableHide = visible && statusBarItems.length <= 1;
                const dragLabel = t("settings.statusBarItem.drag", { label });
                const moveUpLabel = t("settings.statusBarItem.moveUp", { label });
                const moveDownLabel = t("settings.statusBarItem.moveDown", { label });
                const dropPlacement = statusBarDragTarget?.id === id ? statusBarDragTarget.placement : null;
                return (
                  <div
                    className={[
                      "status-bar-item-row",
                      visible ? "" : "status-bar-item-row--hidden",
                      draggingStatusBarItem === id ? "status-bar-item-row--dragging" : "",
                      dropPlacement ? "status-bar-item-row--drag-over" : "",
                      dropPlacement === "before" ? "status-bar-item-row--drop-before" : "",
                      dropPlacement === "after" ? "status-bar-item-row--drop-after" : "",
                    ].filter(Boolean).join(" ")}
                    data-statusbar-setting-item={id}
                    key={id}
                  >
                    <Tooltip label={dragLabel}>
                      <button
                        type="button"
                        className="status-bar-item-row__drag"
                        disabled={!visible || busy}
                        aria-label={dragLabel}
                        title={dragLabel}
                        onPointerDown={(event) => startStatusBarPointerDrag(event, id, visible)}
                        onPointerMove={moveStatusBarPointerDrag}
                        onPointerUp={endStatusBarPointerDrag}
                        onPointerCancel={cancelStatusBarPointerDrag}
                        onMouseDown={(event) => startStatusBarMouseDrag(event, id, visible)}
                      >
                        <GripVertical size={14} aria-hidden="true" />
                      </button>
                    </Tooltip>
                    <label className="status-bar-item-row__toggle">
                      <input
                        type="checkbox"
                        checked={visible}
                        disabled={busy || disableHide}
                        onChange={() => toggleStatusBarItem(id)}
                      />
                      <span className="status-bar-item-row__check" aria-hidden="true">
                        {visible && <Check size={12} />}
                      </span>
                      <span className="status-bar-item-row__label">{label}</span>
                    </label>
                    <div className="status-bar-item-row__actions">
                      <Tooltip label={moveUpLabel}>
                        <button
                          type="button"
                          className="status-bar-item-row__order"
                          disabled={busy || !visible || visibleIndex <= 0}
                          onClick={() => moveStatusBarItem(id, -1)}
                          aria-label={moveUpLabel}
                        >
                          <ChevronUp size={14} aria-hidden="true" />
                        </button>
                      </Tooltip>
                      <Tooltip label={moveDownLabel}>
                        <button
                          type="button"
                          className="status-bar-item-row__order"
                          disabled={busy || !visible || visibleIndex < 0 || visibleIndex >= statusBarItems.length - 1}
                          onClick={() => moveStatusBarItem(id, 1)}
                          aria-label={moveDownLabel}
                        >
                          <ChevronDown size={14} aria-hidden="true" />
                        </button>
                      </Tooltip>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </SettingsField>
    </SettingsSection>
  );
}

const GENRE_OPTIONS: { value: GenerativePreset; labelKey: DictKey }[] = [
  { value: "off", labelKey: "settings.generativeMusic.off" },
  { value: "ethereal", labelKey: "settings.generativeMusic.presets.ethereal" },
  { value: "classic", labelKey: "settings.generativeMusic.presets.classic" },
  { value: "digital", labelKey: "settings.generativeMusic.presets.digital" },
  { value: "retro", labelKey: "settings.generativeMusic.presets.retro" },
];

function summarizeSoundStatus(
  music: GenerativePreset,
  success: SoundWavPref,
  attention: SoundWavPref,
): "allOff" | "enabled" | "custom" {
  const enabledCount = [music !== "off", success !== "off", attention !== "off"].filter(Boolean).length;
  if (enabledCount === 0) return "allOff";
  if (enabledCount === 1) return "enabled";
  return "custom";
}

function GenMusicSelect({
  value,
  onChange,
  onPreview,
  previewDisabled,
}: {
  value: GenerativePreset;
  onChange: (v: GenerativePreset) => void;
  onPreview: () => void;
  previewDisabled?: boolean;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const selected = GENRE_OPTIONS.find((o) => o.value === value) ?? GENRE_OPTIONS[0];

  return (
    <div className="sound-select">
      <button
        ref={triggerRef}
        className="sound-select__trigger"
        type="button"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="sound-select__label">{t(selected.labelKey)}</span>
        <ChevronDown
          size={16}
          className={`sound-select__chev${open ? " sound-select__chev--open" : ""}`}
        />
      </button>
      {!previewDisabled && (
        <button className="chip chip--icon" type="button" title={t("settings.generativeMusicPreview")} aria-label={t("settings.generativeMusicPreview")} onClick={onPreview}>
          <Play size={13} aria-hidden="true" />
        </button>
      )}
      <AnchoredPopover
        open={open}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="sound-select__menu"
        placement="bottom"
      >
        <div className="sound-select__list" role="listbox">
          {GENRE_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              className={`sound-select__option${opt.value === value ? " sound-select__option--selected" : ""}`}
              role="option"
              aria-selected={opt.value === value}
              type="button"
              onClick={() => {
                onChange(opt.value);
                setOpen(false);
              }}
            >
              <span>{t(opt.labelKey)}</span>
              {opt.value === value && <Check size={14} className="sound-select__check" />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function StepLimitControl({
  value,
  presets,
  busy,
  onChange,
}: {
  value: number;
  presets: number[];
  busy: boolean;
  onChange: (value: number) => void;
}) {
  const t = useT();
  const normalized = normalizeStepLimit(value);
  const presetSet = new Set(presets.map(normalizeStepLimit));
  const [custom, setCustom] = useState(String(normalized));
  useEffect(() => setCustom(String(normalized)), [normalized]);
  const isCustom = !presetSet.has(normalized);
  const commitCustom = () => {
    const next = normalizeStepLimit(Number(custom));
    setCustom(String(next));
    if (next !== normalized) onChange(next);
  };
  return (
    <div className="step-limit-control">
      <div className="set-seg">
        {presets.map((preset) => {
          const n = normalizeStepLimit(preset);
          return (
            <button
              key={n}
              type="button"
              className={`set-seg__btn${normalized === n ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => n !== normalized && onChange(n)}
            >
              {stepLimitLabel(n, t)}
            </button>
          );
        })}
        <button
          type="button"
          className={`set-seg__btn${isCustom ? " set-seg__btn--on" : ""}`}
          disabled={busy}
          onClick={() => {
            if (!isCustom) setCustom(String(normalized || 12));
          }}
        >
          {t("settings.stepLimit.custom")}
        </button>
      </div>
      <input
        className="mem-input step-limit-control__custom"
        value={custom}
        disabled={busy}
        inputMode="numeric"
        aria-label={t("settings.stepLimit.custom")}
        onChange={(e) => setCustom(e.target.value.replace(/[^\d]/g, ""))}
        onBlur={commitCustom}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
        }}
      />
    </div>
  );
}

function normalizeStepLimit(value: number): number {
  return Number.isFinite(value) && value > 0 ? Math.trunc(value) : 0;
}

function stepLimitLabel(value: number, t: ReturnType<typeof useT>): string {
  return value === 0 ? t("settings.stepLimit.unlimited") : String(value);
}

function NetworkSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const savedNetwork = normalizeNetworkView(s.network);
  const [draft, setDraft] = useState<NetworkView>(savedNetwork);
  useEffect(() => setDraft(normalizeNetworkView(s.network)), [s.network]);
  const dirty = JSON.stringify(draft) !== JSON.stringify(savedNetwork);
  const setProxy = (next: Partial<NetworkView["proxy"]>) => {
    setDraft({ ...draft, proxy: { ...draft.proxy, ...next } });
  };

  return (
    <SettingsSection
      title={t("settings.tab.network")}
      actions={
        <button
          className="btn btn--primary btn--small"
          disabled={busy || !dirty}
          onClick={() => void apply(() => app.SetNetwork(draft))}
        >
          {t("settings.saveNetwork")}
        </button>
      }
    >
      <SettingsField label={t("settings.proxyMode")}>
        <div className="set-seg">
          {PROXY_MODES.map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${draft.proxyMode === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => setDraft({ ...draft, proxyMode: mode })}
            >
              {proxyModeLabel(mode, t)}
            </button>
          ))}
        </div>
      </SettingsField>

      {draft.proxyMode === "custom" && (
        <>
          <SettingsField label={t("settings.proxyType")}>
            <div className="set-seg">
              {PROXY_TYPES.map((typ) => (
                <button
                  key={typ}
                  className={`set-seg__btn${draft.proxy.type === typ ? " set-seg__btn--on" : ""}`}
                  disabled={busy}
                  onClick={() => setProxy({ type: typ })}
                >
                  {typ.toUpperCase()}
                </button>
              ))}
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyServer")}>
            <div className="settings-inline-controls">
            <input
              className="mem-input set-grow"
              placeholder="127.0.0.1"
              value={draft.proxy.server}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ server: e.target.value })}
            />
            <label className="set-label">{t("settings.proxyPort")}</label>
            <input
              className="mem-input set-narrow"
              placeholder="7890"
              value={draft.proxy.port ? String(draft.proxy.port) : ""}
              disabled={busy || !!draft.proxyUrl.trim()}
              inputMode="numeric"
              onChange={(e) => setProxy({ port: Number(e.target.value) || 0 })}
            />
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyUsername")}>
            <div className="settings-inline-controls">
            <input
              className="mem-input set-grow"
              value={draft.proxy.username}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ username: e.target.value })}
            />
            <label className="set-label">{t("settings.proxyPassword")}</label>
            <input
              className="mem-input set-grow"
              type="password"
              value={draft.proxy.password}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ password: e.target.value })}
            />
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyUrl")} hint={t("settings.proxyUrlHint")}>
              <input
                className="mem-input set-grow"
                placeholder="socks5://127.0.0.1:7890"
                value={draft.proxyUrl}
                disabled={busy}
                onChange={(e) => setDraft({ ...draft, proxyUrl: e.target.value })}
              />
          </SettingsField>
          <SettingsField label={t("settings.noProxy")}>
            <input
              className="mem-input set-grow"
              placeholder="localhost,127.0.0.1,.local"
              value={draft.noProxy}
              disabled={busy}
              onChange={(e) => setDraft({ ...draft, noProxy: e.target.value })}
            />
          </SettingsField>
        </>
      )}
    </SettingsSection>
  );
}

type BotInstallTarget = "qq" | "feishu" | "lark" | "weixin";
type BotOfficialInstallTarget = Exclude<BotInstallTarget, "qq">;
const BOT_ALLOWLIST_TEXT_KEYS = ["qqUsers", "feishuUsers", "weixinUsers", "qqGroups", "feishuGroups", "weixinGroups"] as const;
type BotAllowlistTextKey = typeof BOT_ALLOWLIST_TEXT_KEYS[number];
type BotInstallState = {
  target: BotInstallTarget | "";
  result: BotInstallStartResult | null;
  status: "idle" | "starting" | "showing" | "connected" | "error";
  timeLeft: number;
  message: string;
};
const BOT_INSTALL_TARGETS: BotInstallTarget[] = ["qq", "feishu", "lark", "weixin"];
const BOT_INSTALL_DEFAULT_TIMEOUT_SECONDS = 300;
const BOT_INSTALL_MIN_POLL_SECONDS = 3;
const DEFAULT_QQ_SECRET_ENV = "QQ_BOT_APP_SECRET";
const QQ_CONNECTION_ID = "__qq_bot__";

type BotConnectionListItem =
  | { kind: "qq" }
  | { kind: "connection"; connection: BotConnectionView };

type BotsSectionProps = SectionProps & { initialFocus?: SettingsInitialFocus };

function BotsSection({ s, busy, apply, initialFocus }: BotsSectionProps) {
  const t = useT();
  const savedBot = normalizeBotSettings(s.bot);
  const [draft, setDraft] = useState<BotSettingsView>(savedBot);
  const [allowlistText, setAllowlistText] = useState<Record<BotAllowlistTextKey, string>>(() => botAllowlistTextValues(savedBot.allowlist));
  const [allowlistFocused, setAllowlistFocused] = useState(false);
  const [allowlistOpen, setAllowlistOpen] = useState(false);
  const [installTarget, setInstallTarget] = useState<BotInstallTarget>("qq");
  const [install, setInstall] = useState<BotInstallState>({ target: "qq", result: null, status: "idle", timeLeft: 0, message: "" });
  const [diagnostics, setDiagnostics] = useState<Record<string, BotConnectionDiagnostic | string>>({});
  const [testTargets, setTestTargets] = useState<Record<string, string>>({});
  const [connectionSecrets, setConnectionSecrets] = useState<Record<string, string>>({});
  const [qqSecretValue, setQQSecretValue] = useState("");
  const [expandedConnectionId, setExpandedConnectionId] = useState("");
  const installRef = useRef(install);
  const installPollTimerRef = useRef<number | null>(null);
  const installCountdownTimerRef = useRef<number | null>(null);
  const installRequestInFlightRef = useRef(false);
  const installAttemptRef = useRef(0);
  const allowlistRef = useRef<HTMLDetailsElement | null>(null);
  const initialFocusHandledRef = useRef("");
  const pendingAllowlistFocusRef = useRef(false);
  const refs = allRefs(s);

  useEffect(() => {
    const nextBot = normalizeBotSettings(s.bot);
    setDraft(nextBot);
    setAllowlistText(botAllowlistTextValues(nextBot.allowlist));
    setConnectionSecrets({});
    setQQSecretValue("");
    setTestTargets({});
  }, [s.bot]);
  useEffect(() => {
    if (initialFocus?.target !== "bot-allowlist") return;
    const focusKey = `${initialFocus.target}:${initialFocus.connectionId ?? ""}`;
    if (initialFocusHandledRef.current === focusKey) return;
    let focusConnectionId = "";
    if (initialFocus.connectionId === QQ_CONNECTION_ID && qqBotAdded(draft.qq)) {
      focusConnectionId = QQ_CONNECTION_ID;
    } else if (initialFocus.connectionId && draft.connections.some((connection) => connection.id === initialFocus.connectionId)) {
      focusConnectionId = initialFocus.connectionId;
    } else {
      focusConnectionId = draft.connections[0]?.id ?? "";
    }
    if (!focusConnectionId) return;
    initialFocusHandledRef.current = focusKey;
    pendingAllowlistFocusRef.current = true;
    setExpandedConnectionId(focusConnectionId);
    setAllowlistOpen(false);
  }, [draft.connections, draft.qq, initialFocus]);
  useEffect(() => {
    setAllowlistOpen(false);
  }, [expandedConnectionId]);
  useEffect(() => {
    installRef.current = install;
  }, [install]);
  useEffect(() => {
    installAttemptRef.current += 1;
    installRequestInFlightRef.current = false;
    clearInstallTimers();
    setInstall({ target: installTarget, result: null, status: "idle", timeLeft: 0, message: "" });
  }, [installTarget]);
  useEffect(() => () => {
    installAttemptRef.current += 1;
    clearInstallTimers();
  }, []);

  const setConnections = (mapper: (connections: BotConnectionView[]) => BotConnectionView[]) =>
    setDraft((prev) => ({ ...prev, connections: mapper(prev.connections) }));
  const persistBotDraft = async (nextDraft: BotSettingsView) => {
    const nextBot = botDraftWithDerivedGatewayState(nextDraft);
    setDraft(nextBot);
    await apply(async () => {
      await app.SetBotSettings(nextBot);
    });
  };
  const persistConnections = (mapper: (connections: BotConnectionView[]) => BotConnectionView[]) =>
    persistBotDraft({ ...draft, connections: mapper(draft.connections) });
  const updateConnection = (id: string, patch: Partial<BotConnectionView>) =>
    setConnections((items) => items.map((item) => item.id === id ? { ...item, ...patch } : item));
  const persistConnection = (id: string, patch: Partial<BotConnectionView>) =>
    persistConnections((items) => items.map((item) => item.id === id ? { ...item, ...patch } : item));
  const persistConnectionToolApprovalMode = (id: string, mode: string) => {
    const normalizedMode = normalizeBotToolApprovalMode(mode, true);
    setConnections((items) => items.map((item) => item.id === id ? { ...item, toolApprovalMode: normalizedMode } : item));
    void apply(() => app.SetBotConnectionToolApprovalMode(id, normalizedMode));
  };
  const updateConnectionCredential = (id: string, patch: Partial<BotConnectionView["credential"]>) =>
    setConnections((items) => items.map((item) => item.id === id ? { ...item, credential: { ...item.credential, ...patch } } : item));
  const persistConnectionCredential = (id: string, patch: Partial<BotConnectionView["credential"]>) =>
    persistConnections((items) => items.map((item) => item.id === id ? { ...item, credential: { ...item.credential, ...patch } } : item));
  const updateAllowlist = (patch: Partial<BotAllowlistView>) =>
    setDraft((prev) => ({ ...prev, allowlist: { ...prev.allowlist, ...patch } }));
  const persistAllowlist = (patch: Partial<BotAllowlistView>) =>
    persistBotDraft({ ...draft, allowlist: { ...draft.allowlist, ...patch } });
  const persistAllowlistText = (key: BotAllowlistTextKey, value: string) => {
    const entries = parseBotListInput(value);
    setAllowlistText((prev) => ({ ...prev, [key]: entries.join("\n") }));
    void persistAllowlist({ [key]: entries } as Partial<BotAllowlistView>);
  };
  const updateQQ = (patch: Partial<BotSettingsView["qq"]>) =>
    setDraft((prev) => ({ ...prev, qq: { ...prev.qq, ...patch } }));
  const persistQQ = (patch: Partial<BotSettingsView["qq"]>) =>
    persistBotDraft({ ...draft, qq: { ...draft.qq, ...patch } });
  const removeConnection = async (connection: BotConnectionView) => {
    const nextDraft = botDraftWithDerivedGatewayState({
      ...draft,
      connections: draft.connections.filter((item) => item.id !== connection.id),
    });
    await apply(async () => {
      await app.SetBotSettings(nextDraft);
    });
  };
  const installQrURL = install.result?.url ?? "";
  const installQrIsImage = installQrURL.startsWith("data:image/");
  const isQQInstallTarget = installTarget === "qq";
  const selectedInstallConnection = isQQInstallTarget ? undefined : draft.connections.find((connection) => botInstallTargetMatchesConnection(installTarget, connection));
  const selectedInstallLabel = botTargetLabel(installTarget, t);
  const installUserCode = install.result?.userCode && installTarget !== "weixin" ? formatInstallUserCode(install.result.userCode) : "";
  const qqSecretEnv = draft.qq.appSecretEnv.trim() || DEFAULT_QQ_SECRET_ENV;
  const qqConfigured = draft.qq.enabled && draft.qq.appId.trim() && qqSecretEnv && draft.qq.secretSet;
  const qqCanEnableAccess = qqAccessReady(draft.allowlist);
  const qqCanSaveAndEnable = Boolean(draft.qq.appId.trim() && qqSecretEnv && (draft.qq.secretSet || qqSecretValue.trim()) && qqCanEnableAccess);
  const qqAdded = qqBotAdded(draft.qq);
  const nativeRuntimeAvailable = typeof window !== "undefined" && Boolean(window.runtime);
  const browserPreviewBotConfigured = !nativeRuntimeAvailable && (qqAdded || draft.connections.length > 0);
  const qqOnline = qqConfigured && nativeRuntimeAvailable;
  const connectionItems: BotConnectionListItem[] = [
    ...(qqAdded ? [{ kind: "qq" as const }] : []),
    ...draft.connections.map((connection) => ({ kind: "connection" as const, connection })),
  ];

  const saveBot = () => app.SetBotSettings(botDraftWithDerivedGatewayState(draft));
  function clearInstallTimers() {
    if (installPollTimerRef.current !== null) {
      window.clearTimeout(installPollTimerRef.current);
      installPollTimerRef.current = null;
    }
    if (installCountdownTimerRef.current !== null) {
      window.clearInterval(installCountdownTimerRef.current);
      installCountdownTimerRef.current = null;
    }
  }
  function beginInstallCountdown(attempt: number) {
    if (installCountdownTimerRef.current !== null) {
      window.clearInterval(installCountdownTimerRef.current);
    }
    installCountdownTimerRef.current = window.setInterval(() => {
      setInstall((prev) => {
        if (installAttemptRef.current !== attempt || prev.status !== "showing") return prev;
        return { ...prev, timeLeft: Math.max(0, prev.timeLeft - 1) };
      });
    }, 1000);
  }
  function scheduleInstallPoll(attempt: number, interval: number) {
    if (installPollTimerRef.current !== null) {
      window.clearTimeout(installPollTimerRef.current);
    }
    installPollTimerRef.current = window.setTimeout(() => void pollInstall(attempt), Math.max(interval || BOT_INSTALL_MIN_POLL_SECONDS, BOT_INSTALL_MIN_POLL_SECONDS) * 1000);
  }
  const startInstall = async (target: BotOfficialInstallTarget) => {
    if (installRequestInFlightRef.current) return;
    const existing = draft.connections.find((connection) => botInstallTargetMatchesConnection(target, connection));
    if (existing) {
      installAttemptRef.current += 1;
      clearInstallTimers();
      setInstall({ target, result: null, status: "connected", timeLeft: 0, message: t("settings.botInstallAlreadyConnected", { provider: botTargetLabel(target, t) }) });
      return;
    }
    clearInstallTimers();
    const attempt = installAttemptRef.current + 1;
    installAttemptRef.current = attempt;
    installRequestInFlightRef.current = true;
    setInstall({ target, result: null, status: "starting", timeLeft: 0, message: t("settings.botInstallStarting") });
    const provider = target === "weixin" ? "weixin" : "feishu";
    const domain = target === "lark" ? "lark" : target === "weixin" ? "weixin" : "feishu";
    try {
      const result = await app.StartBotConnectionInstall(provider, domain);
      if (installAttemptRef.current !== attempt) return;
      if (!result.ok) {
        setInstall({ target, result, status: "error", timeLeft: 0, message: result.message || t("settings.botInstallFailed") });
        return;
      }
      const timeLeft = result.expireIn > 0 ? result.expireIn : BOT_INSTALL_DEFAULT_TIMEOUT_SECONDS;
      setInstall({ target, result, status: "showing", timeLeft, message: result.message || t("settings.botInstallScanHint") });
      beginInstallCountdown(attempt);
      scheduleInstallPoll(attempt, result.interval);
    } catch (err) {
      if (installAttemptRef.current === attempt) {
        setInstall({ target, result: null, status: "error", timeLeft: 0, message: err instanceof Error ? err.message : t("settings.botInstallFailed") });
      }
    } finally {
      if (installAttemptRef.current === attempt) {
        installRequestInFlightRef.current = false;
      }
    }
  };
  const pollInstall = async (attempt = installAttemptRef.current) => {
    const current = installRef.current;
    if (installAttemptRef.current !== attempt || current.status !== "showing" || !current.result?.installId || !current.target) return;
    const poll = await app.PollBotConnectionInstall(current.result.installId);
    if (installAttemptRef.current !== attempt) return;
    if (poll.done) {
      clearInstallTimers();
      setDraft((prev) => ({
        ...prev,
        enabled: true,
        connections: [...prev.connections.filter((c) => c.id !== poll.connection.id), poll.connection],
      }));
      setInstall((prev) => ({ ...prev, status: "connected", timeLeft: 0, message: poll.message || t("settings.botInstallConnected") }));
      return;
    }
    if (poll.error) {
      clearInstallTimers();
      setInstall((prev) => ({ ...prev, status: "error", timeLeft: 0, message: poll.error }));
      return;
    }
    setInstall((prev) => ({ ...prev, message: poll.message || t("settings.botInstallWaiting") }));
    scheduleInstallPoll(attempt, current.result.interval);
  };
  useEffect(() => {
    if (install.status !== "showing" || install.timeLeft > 0) return;
    installAttemptRef.current += 1;
    clearInstallTimers();
    setInstall((prev) => prev.status === "showing" ? { ...prev, status: "error", message: t("settings.botInstallExpired") } : prev);
  }, [install.status, install.timeLeft]);
  const diagnoseConnection = async (id: string) => {
    const diag = await app.DiagnoseBotConnection(id);
    setDiagnostics((prev) => ({ ...prev, [id]: diag }));
    return diag;
  };
  const testConnection = async (connection: BotConnectionView) => {
    const target = (testTargets[connection.id] ?? firstConnectionRemote(connection)).trim();
    const diag = await app.TestBotConnection(connection.id, target);
    setDiagnostics((prev) => ({ ...prev, [connection.id]: diag }));
    if (diag.messageId && target) {
      const updatedAt = new Date().toISOString();
      await persistConnections((items) => items.map((item) => {
        if (item.id !== connection.id) return item;
        const scope = connection.workspaceRoot ? "project" : "global";
        const matchesTestMapping = (mapping: BotConnectionView["sessionMappings"][number]) =>
          mapping.remoteId === target &&
          !mapping.chatType.trim() &&
          !mapping.userId.trim() &&
          !mapping.threadId.trim();
        const sessionMappings = [
          ...item.sessionMappings.filter((mapping) => !matchesTestMapping(mapping)),
          { remoteId: target, sessionId: "", sessionSource: "", chatType: "", userId: "", threadId: "", scope, workspaceRoot: scope === "project" ? connection.workspaceRoot : "", updatedAt },
        ];
        return { ...item, sessionMappings, updatedAt };
      }));
    }
  };
  const ensureReportableDiagnostic = async (connection: BotConnectionView) => {
    return diagnoseConnection(connection.id);
  };
  const copyConnectionDiagnostic = async (connection: BotConnectionView) => {
    const diag = await ensureReportableDiagnostic(connection);
    if (!diag.reportDetail) return;
    try {
      await navigator.clipboard.writeText(diag.reportDetail);
      setDiagnostics((prev) => ({ ...prev, [connection.id]: { ...diag, message: t("settings.botDiagnosticCopied") } }));
    } catch (err) {
      setDiagnostics((prev) => ({
        ...prev,
        [connection.id]: { ...diag, status: "error", message: err instanceof Error ? err.message : t("settings.botDiagnosticCopyFailed") },
      }));
    }
  };
  const reportConnectionDiagnostic = async (connection: BotConnectionView) => {
    const diag = await ensureReportableDiagnostic(connection);
    if (!diag.reportDetail) return;
    try {
      await app.ReportCrash(diag.reportKind || "bot", diag.reportDetail);
      setDiagnostics((prev) => ({ ...prev, [connection.id]: { ...diag, status: "ok", message: t("settings.botDiagnosticReportSent") } }));
    } catch (err) {
      setDiagnostics((prev) => ({
        ...prev,
        [connection.id]: { ...diag, status: "error", message: err instanceof Error ? err.message : t("settings.botDiagnosticReportFailed") },
      }));
    }
  };
  const saveConnectionSecret = async (connection: BotConnectionView) => {
    const env = botConnectionSecretEnv(connection).trim();
    const value = (connectionSecrets[connection.id] ?? "").trim();
    if (!env || !value) return;
    await apply(async () => {
      await saveBot();
      await app.SetBotSecret(env, value);
    });
    setConnectionSecrets((prev) => ({ ...prev, [connection.id]: "" }));
  };
  const clearConnectionSecret = async (connection: BotConnectionView) => {
    const env = botConnectionSecretEnv(connection).trim();
    if (!env) return;
    await apply(async () => {
      await saveBot();
      await app.ClearBotSecret(env);
    });
  };
  const clearQQSecret = async () => {
    const env = draft.qq.appSecretEnv.trim() || DEFAULT_QQ_SECRET_ENV;
    if (!env) return;
    await apply(async () => {
      await saveBot();
      await app.ClearBotSecret(env);
    });
    setQQSecretValue("");
  };
  const focusQQAccessSettings = () => {
    pendingAllowlistFocusRef.current = true;
    setExpandedConnectionId(QQ_CONNECTION_ID);
    setAllowlistOpen(true);
    setAllowlistFocused(true);
    setDiagnostics((prev) => ({ ...prev, [QQ_CONNECTION_ID]: t("settings.botQQAccessRequired") }));
  };
  const saveQQAndEnable = async () => {
    if (!qqCanEnableAccess) {
      focusQQAccessSettings();
      return;
    }
    const env = draft.qq.appSecretEnv.trim() || DEFAULT_QQ_SECRET_ENV;
    const secret = qqSecretValue.trim();
    const nextDraft = botDraftWithDerivedGatewayState({
      ...draft,
      qq: {
        ...draft.qq,
        enabled: true,
        appId: draft.qq.appId.trim(),
        appSecretEnv: env,
        secretSet: draft.qq.secretSet || Boolean(secret),
      },
    });
    await apply(async () => {
      await app.SetBotSettings(nextDraft);
      if (secret) await app.SetBotSecret(env, secret);
    });
    setDraft(nextDraft);
    setQQSecretValue("");
  };
  const removeQQBot = async () => {
    const env = draft.qq.appSecretEnv.trim() || DEFAULT_QQ_SECRET_ENV;
    const nextDraft = botDraftWithDerivedGatewayState({
      ...draft,
      qq: { enabled: false, appId: "", appSecretEnv: DEFAULT_QQ_SECRET_ENV, secretSet: false, sandbox: false },
    });
    await apply(async () => {
      await app.SetBotSettings(nextDraft);
      if (draft.qq.secretSet) await app.ClearBotSecret(env);
    });
    setDraft(nextDraft);
    setQQSecretValue("");
    setExpandedConnectionId("");
  };
  const onlineConnections = (qqOnline ? 1 : 0) + draft.connections.filter((connection) => connection.enabled && connection.status === "connected").length;
  const selectedQQ = qqAdded && expandedConnectionId === QQ_CONNECTION_ID;
  const selectedConnection = selectedQQ ? null : draft.connections.find((connection) => connection.id === expandedConnectionId) ?? null;
  const selectedDiagnostic = selectedConnection ? diagnostics[selectedConnection.id] : undefined;
  const selectedDiagnosticDetail = diagnosticReportDetail(selectedDiagnostic);
  const selectedConnectionRemote = selectedConnection ? firstConnectionRemote(selectedConnection) : "";
  const selectedConnectionToolApprovalMode = selectedConnection ? normalizeBotToolApprovalMode(selectedConnection.toolApprovalMode, true) : "";
  const selectedAllowlistTargetReady = selectedQQ || Boolean(selectedConnection);
  useEffect(() => {
    if (!pendingAllowlistFocusRef.current || !selectedAllowlistTargetReady) return;
    setAllowlistOpen(true);
    const scrollTimer = window.setTimeout(() => {
      if (!allowlistRef.current) return;
      pendingAllowlistFocusRef.current = false;
      allowlistRef.current.scrollIntoView({ block: "center", behavior: "smooth" });
      setAllowlistFocused(true);
    }, 80);
    const clearTimer = window.setTimeout(() => setAllowlistFocused(false), 2100);
    return () => {
      window.clearTimeout(scrollTimer);
      window.clearTimeout(clearTimer);
    };
  }, [selectedAllowlistTargetReady]);

  return (
    <div className="bot-phone-connect">
        <div className="bot-connection-list">
          <div className="bot-connection-list__head">
            <div className="bot-connection-list__title">
              <strong>{t("settings.botConnectedBots")}</strong>
              <span>{t("settings.botConnectedBotsSummary", { online: onlineConnections, total: connectionItems.length })}</span>
            </div>
          </div>
          {browserPreviewBotConfigured ? (
            <div className="bot-connection-warning">{t("settings.botBrowserPreviewWarning")}</div>
          ) : null}
          {connectionItems.length === 0 ? (
            <div className="bot-connection-empty">{t("settings.botConnectionsEmpty")}</div>
          ) : (
            <div className="bot-connection-table" role="table" aria-label={t("settings.botConnectedBots")}>
              <div className="bot-connection-table__header" role="row">
                <span>{t("settings.botConnectionColumnChannel")}</span>
                <span>{t("settings.botConnectionColumnName")}</span>
                <span>{t("settings.botConnectionColumnStatus")}</span>
                <span>{t("settings.botConnectionColumnActions")}</span>
              </div>
              {connectionItems.map((item) => {
                if (item.kind === "qq") {
                  const appID = draft.qq.appId.trim();
                  const qqDiagMessage = diagnosticMessage(diagnostics[QQ_CONNECTION_ID]);
                  const statusText = qqOnline
                    ? t("settings.botConnectionConnected")
                    : qqConfigured
                      ? t("settings.botConnectionConfigured")
                      : draft.qq.secretSet
                      ? t("settings.botConnectionDisconnected")
                      : t("settings.botSecretMissing");
                  return (
                    <div key={QQ_CONNECTION_ID} className="bot-connection-row" role="rowgroup">
                      <div className="bot-connection-row__grid" role="row">
                      <div className="bot-connection-row__channel" role="cell">
                        <span>QQ</span>
                      </div>
                      <div className="bot-connection-row__identity-cell" role="cell">
                        <button
                          type="button"
                          className="bot-connection-identity"
                          disabled={busy}
                          onClick={() => setExpandedConnectionId((current) => current === QQ_CONNECTION_ID ? "" : QQ_CONNECTION_ID)}
                          title={appID || "QQ Bot"}
                        >
                          <span className="bot-connection-identity__main">
                            <strong>QQ Bot</strong>
                            <code>{appID || "—"}</code>
                          </span>
                        </button>
                      </div>
                      <div className="bot-connection-row__state" role="cell">
                        <span className={`bot-connection-row__status bot-connection-row__status--${qqOnline ? "connected" : qqConfigured ? "configured" : "disconnected"}`}>
                            {statusText}
                          </span>
                          <ToggleSegment
                            value={draft.qq.enabled}
                            disabled={busy}
                            onChange={(enabled) => {
                              if (enabled && !qqCanEnableAccess) {
                                focusQQAccessSettings();
                                return;
                              }
                              updateQQ({ enabled });
                              void persistQQ({ enabled });
                            }}
                          />
                        </div>
                        <div className="bot-connection-row__actions" role="cell">
                          <button
                            type="button"
                            className={`btn btn--small${selectedQQ ? " btn--primary" : " btn--secondary"}`}
                            disabled={busy}
                            onClick={() => setExpandedConnectionId((current) => current === QQ_CONNECTION_ID ? "" : QQ_CONNECTION_ID)}
                          >
                            {t("settings.botManage")}
                          </button>
                        </div>
                      </div>
                      {qqDiagMessage ? <em className="bot-connection-row__diag">{qqDiagMessage}</em> : null}
                    </div>
                  );
                }
                const connection = item.connection;
                const sessionID = firstConnectionRemote(connection);
                const diagMessage = diagnosticMessage(diagnostics[connection.id]);
                const connectionStatusClass = connection.status === "connected" ? "connected" : "disconnected";
                return (
                  <div key={connection.id} className="bot-connection-row" role="rowgroup">
                    <div className="bot-connection-row__grid" role="row">
                      <div className="bot-connection-row__channel" role="cell">
                        <span>{botConnectionLabel(connection, t)}</span>
                      </div>
                      <div className="bot-connection-row__identity-cell" role="cell">
                        <button
                          type="button"
                          className="bot-connection-identity"
                          disabled={busy}
                          onClick={() => setExpandedConnectionId((current) => current === connection.id ? "" : connection.id)}
                          title={sessionID || connection.label || botConnectionLabel(connection, t)}
                        >
                          <span className="bot-connection-identity__main">
                            <strong>{connection.label || botConnectionLabel(connection, t)}</strong>
                            <code>{sessionID || "—"}</code>
                          </span>
                        </button>
                      </div>
                      <div className="bot-connection-row__state" role="cell">
                        <span className={`bot-connection-row__status bot-connection-row__status--${connectionStatusClass}`}>
                          {connection.status === "connected" ? t("settings.botConnectionConnected") : connection.status || t("settings.botConnectionDisconnected")}
                        </span>
                        <ToggleSegment
                          value={connection.enabled}
                          disabled={busy}
                          onChange={(enabled) => void persistConnection(connection.id, { enabled })}
                        />
                      </div>
                      <div className="bot-connection-row__actions" role="cell">
                        <button
                          type="button"
                          className={`btn btn--small${expandedConnectionId === connection.id ? " btn--primary" : " btn--secondary"}`}
                          disabled={busy}
                          onClick={() => setExpandedConnectionId((current) => current === connection.id ? "" : connection.id)}
                        >
                          {t("settings.botManage")}
                        </button>
                      </div>
                    </div>
                    {diagMessage ? <em className="bot-connection-row__diag">{diagMessage}</em> : null}
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {selectedQQ ? (
          <article className="bot-detail-card" aria-labelledby="bot-detail-title">
            <div className="bot-detail-card__head">
              <div className="bot-detail-card__identity">
                <div className="bot-detail-card__title" id="bot-detail-title">
                  QQ Bot
                  <span className="badge badge--neutral">QQ</span>
                  <span className={`badge ${qqOnline ? "badge--project" : qqConfigured ? "badge--feedback" : "badge--feedback"}`}>
                    {qqOnline ? t("settings.botConnectionConnected") : qqConfigured ? t("settings.botConnectionConfigured") : t("settings.botConnectionDisconnected")}
                  </span>
                </div>
                <div className="bot-detail-card__desc">{t("settings.botAutoSaveHint")}</div>
              </div>
              <div className="bot-detail-card__actions">
                <button type="button" className="btn btn--small" onClick={() => setExpandedConnectionId("")}>
                  {t("common.collapse")}
                </button>
              </div>
            </div>

            <section className="bot-detail-section">
              <div className="bot-detail-section__head">{t("settings.botConnectionSummary")}</div>
              <div className="bot-detail-summary">
                <div>
                  <span>{t("settings.botConnectionColumnChannel")}</span>
                  <strong>QQ</strong>
                </div>
                <div>
                  <span>{t("settings.botConnectionColumnRemote")}</span>
                  <code title={draft.qq.appId.trim() || undefined}>{draft.qq.appId.trim() || "—"}</code>
                </div>
                <div>
                  <span>{t("settings.botConnectionColumnScope")}</span>
                  <strong>{t("settings.botScopeGlobal")}</strong>
                </div>
                <div>
                  <span>{t("settings.botConnectionColumnStatus")}</span>
                  <strong>{qqOnline ? t("settings.botConnectionConnected") : qqConfigured ? t("settings.botConnectionConfigured") : t("settings.botConnectionDisconnected")}</strong>
                </div>
              </div>
            </section>

            <section className="bot-detail-section">
              <div className="bot-detail-section__head">{t("settings.botRuntimeSettings")}</div>
              <SettingsField label={t("settings.botEnableBot")} hint={t("settings.botGatewayEnabled")}>
                <ToggleSegment
                  value={draft.qq.enabled}
                  disabled={busy}
                  onChange={(enabled) => {
                    if (enabled && !qqCanEnableAccess) {
                      focusQQAccessSettings();
                      return;
                    }
                    updateQQ({ enabled });
                    void persistQQ({ enabled });
                  }}
                />
              </SettingsField>
              <SettingsField label={t("settings.botSandbox")} hint={t("settings.botInstallQQHint")}>
                <ToggleSegment
                  value={draft.qq.sandbox}
                  disabled={busy}
                  onLabel={t("settings.toggleOn")}
                  offLabel={t("settings.toggleOff")}
                  onChange={(sandbox) => {
                    updateQQ({ sandbox });
                    void persistQQ({ sandbox });
                  }}
                />
              </SettingsField>
            </section>

            <section className="bot-detail-section">
              <div className="bot-detail-section__head">{t("settings.botCredential")}</div>
              <div className="bot-credential-stack">
                <div className="bot-credential-line">
                  <span>{draft.qq.appId.trim() ? t("settings.botCredentialApp", { value: draft.qq.appId.trim() }) : t("settings.botCredentialConfigured")}</span>
                  <strong>{draft.qq.secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing")}</strong>
                </div>
                <div className="bot-secret-row bot-secret-row--qq">
                  <input
                    className="mem-input"
                    value={draft.qq.appId}
                    disabled={busy}
                    placeholder={t("settings.botAppId")}
                    spellCheck={false}
                    aria-label={t("settings.botAppId")}
                    onChange={(event) => updateQQ({ appId: event.target.value })}
                    onBlur={(event) => void persistQQ({ appId: event.currentTarget.value })}
                  />
                  <input
                    className="mem-input"
                    value={draft.qq.appSecretEnv || DEFAULT_QQ_SECRET_ENV}
                    disabled={busy}
                    placeholder={DEFAULT_QQ_SECRET_ENV}
                    spellCheck={false}
                    aria-label={t("settings.botSecretEnv")}
                    onChange={(event) => updateQQ({ appSecretEnv: event.target.value })}
                    onBlur={(event) => void persistQQ({ appSecretEnv: event.currentTarget.value || DEFAULT_QQ_SECRET_ENV })}
                  />
                  <input
                    className="mem-input"
                    type="password"
                    value={qqSecretValue}
                    disabled={busy}
                    placeholder={draft.qq.secretSet ? t("settings.botSecretReplace") : t("settings.botSecretPaste")}
                    aria-label={t("settings.botSecretValue")}
                    onChange={(event) => setQQSecretValue(event.target.value)}
                  />
                  <button type="button" className="btn btn--secondary btn--small" disabled={busy || !qqCanSaveAndEnable} onClick={() => void saveQQAndEnable()}>
                    {draft.qq.secretSet ? t("settings.saveKey") : t("settings.botSaveAndEnable")}
                  </button>
                  <button type="button" className="btn btn--secondary btn--small" disabled={busy || !draft.qq.secretSet} onClick={() => void clearQQSecret()}>
                    {t("settings.clearKey")}
                  </button>
                </div>
                {!qqCanEnableAccess ? <div className="bot-connect-panel__hint bot-connect-panel__hint--warning">{t("settings.botQQAccessRequired")}</div> : null}
              </div>
            </section>

            <details
              ref={allowlistRef}
              className={`bot-access-panel${allowlistFocused ? " bot-access-panel--focused" : ""}`}
              data-focus-target="bot-allowlist"
              open={allowlistOpen}
              onToggle={(event) => setAllowlistOpen(event.currentTarget.open)}
            >
              <summary className="bot-access-panel__summary">
                <span>
                  <strong>{t("settings.botAccessControl")}</strong>
                  <small>{t("settings.botAllowlistHint")}</small>
                </span>
                <ChevronDown className="bot-access-panel__chevron" size={16} aria-hidden="true" />
              </summary>
              {allowlistOpen ? (
                <div className="bot-access-panel__body">
                  <SettingsField label={t("settings.botAccessMode")} hint={t("settings.botAccessControlHint")}>
                    <ToggleSegment
                      value={!draft.allowlist.allowAll}
                      disabled={busy}
                      onLabel={t("settings.botAccessWhitelist")}
                      offLabel={t("settings.botAccessAll")}
                      onChange={(whitelistOnly) => {
                        const patch = { enabled: whitelistOnly, allowAll: !whitelistOnly };
                        updateAllowlist(patch);
                        void persistAllowlist(patch);
                      }}
                    />
                  </SettingsField>
                  {draft.allowlist.allowAll ? <div className="bot-access-panel__warning">{t("settings.botAllowAllWarn")}</div> : null}
                  <SettingsField label={t("settings.botAllowlistEntries")} hint={t("settings.botListPlaceholder")}>
                    <div className="bot-list-grid bot-list-grid--qq">
                      <label className="bot-list-input">
                        <span>{t("settings.botQQUsers")}</span>
                        <textarea
                          className="mem-input bot-list-input__textarea"
                          value={allowlistText.qqUsers}
                          disabled={busy || draft.allowlist.allowAll}
                          placeholder={t("settings.botListPlaceholder")}
                          spellCheck={false}
                          onChange={(event) => setAllowlistText((prev) => ({ ...prev, qqUsers: event.target.value }))}
                          onBlur={(event) => persistAllowlistText("qqUsers", event.currentTarget.value)}
                        />
                      </label>
                      <label className="bot-list-input">
                        <span>{t("settings.botQQGroups")}</span>
                        <textarea
                          className="mem-input bot-list-input__textarea"
                          value={allowlistText.qqGroups}
                          disabled={busy || draft.allowlist.allowAll}
                          placeholder={t("settings.botListPlaceholder")}
                          spellCheck={false}
                          onChange={(event) => setAllowlistText((prev) => ({ ...prev, qqGroups: event.target.value }))}
                          onBlur={(event) => persistAllowlistText("qqGroups", event.currentTarget.value)}
                        />
                      </label>
                    </div>
                  </SettingsField>
                </div>
              ) : null}
            </details>

            <section className="bot-detail-section bot-detail-section--danger">
              <div>
                <div className="bot-detail-section__head">{t("settings.botDangerZone")}</div>
                <p>{t("settings.deleteBotHint")}</p>
              </div>
              <InlineConfirmButton
                label={t("settings.deleteBot")}
                confirmLabel={t("settings.confirmDeleteBot")}
                cancelLabel={t("common.cancel")}
                disabled={busy}
                danger
                onConfirm={() => void removeQQBot()}
              />
            </section>
          </article>
        ) : null}

        {selectedConnection ? (
          <article className="bot-detail-card" aria-labelledby="bot-detail-title">
            <div className="bot-detail-card__head">
              <div className="bot-detail-card__identity">
                <div className="bot-detail-card__title" id="bot-detail-title">
                  {selectedConnection.label || botConnectionLabel(selectedConnection, t)}
                  <span className="badge badge--neutral">{botConnectionLabel(selectedConnection, t)}</span>
                  <span className={`badge ${selectedConnection.status === "connected" ? "badge--project" : "badge--feedback"}`}>
                    {selectedConnection.status === "connected" ? t("settings.botConnectionConnected") : selectedConnection.status || t("settings.botConnectionDisconnected")}
                  </span>
                </div>
                <div className="bot-detail-card__desc">{t("settings.botAutoSaveHint")}</div>
              </div>
              <div className="bot-detail-card__actions">
                <button type="button" className="btn btn--small" disabled={busy} onClick={() => void diagnoseConnection(selectedConnection.id)}>
                  {t("settings.botDiagnose")}
                </button>
                {(selectedConnection.provider === "feishu" || selectedConnection.provider === "weixin") ? (
                  <button type="button" className="btn btn--small" disabled={busy || !selectedConnectionRemote} onClick={() => void testConnection(selectedConnection)}>
                    {t("settings.botTest")}
                  </button>
                ) : null}
                <button type="button" className="btn btn--small" onClick={() => setExpandedConnectionId("")}>
                  {t("common.collapse")}
                </button>
              </div>
            </div>

              {diagnosticMessage(selectedDiagnostic) ? (
                <div className="bot-detail-notice">
                  <span>{diagnosticMessage(selectedDiagnostic)}</span>
                  {selectedDiagnosticDetail ? (
                    <div className="bot-diagnostic-actions">
                      <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void copyConnectionDiagnostic(selectedConnection)}>
                        <Clipboard aria-hidden="true" />
                        {t("settings.botCopyDiagnostic")}
                      </button>
                      <button type="button" className="btn btn--primary btn--small" disabled={busy} onClick={() => void reportConnectionDiagnostic(selectedConnection)}>
                        <Send aria-hidden="true" />
                        {t("settings.botSendDiagnostic")}
                      </button>
                      <small>{t("settings.botDiagnosticPrivacy")}</small>
                    </div>
                  ) : null}
                </div>
              ) : null}

              <section className="bot-detail-section">
                <div className="bot-detail-section__head">{t("settings.botConnectionSummary")}</div>
                <div className="bot-detail-summary">
                  <div>
                    <span>{t("settings.botConnectionColumnChannel")}</span>
                    <strong>{botConnectionLabel(selectedConnection, t)}</strong>
                  </div>
                  <div>
                    <span>{t("settings.botConnectionColumnRemote")}</span>
                    <code title={selectedConnectionRemote || undefined}>{selectedConnectionRemote || "—"}</code>
                  </div>
                  <div>
                    <span>{t("settings.botConnectionColumnScope")}</span>
                    <strong>{botConnectionScopeLabel(selectedConnection, t)}</strong>
                  </div>
                  <div>
                    <span>{t("settings.botConnectionColumnStatus")}</span>
                    <strong>{selectedConnection.status === "connected" ? t("settings.botConnectionConnected") : selectedConnection.status || t("settings.botConnectionDisconnected")}</strong>
                  </div>
                </div>
              </section>

              <details
                ref={allowlistRef}
                className={`bot-access-panel${allowlistFocused ? " bot-access-panel--focused" : ""}`}
                data-focus-target="bot-allowlist"
                open={allowlistOpen}
                onToggle={(event) => setAllowlistOpen(event.currentTarget.open)}
              >
                <summary className="bot-access-panel__summary">
                  <span>
                    <strong>{t("settings.botAccessControl")}</strong>
                    <small>{t("settings.botAllowlistHint")}</small>
                  </span>
                  <ChevronDown className="bot-access-panel__chevron" size={16} aria-hidden="true" />
                </summary>
                {allowlistOpen ? (
                  <div className="bot-access-panel__body">
                    <SettingsField label={t("settings.botAccessMode")} hint={t("settings.botAccessControlHint")}>
                      <ToggleSegment
                        value={!draft.allowlist.allowAll}
                        disabled={busy}
                        onLabel={t("settings.botAccessWhitelist")}
                        offLabel={t("settings.botAccessAll")}
                        onChange={(whitelistOnly) => {
                          const patch = { enabled: whitelistOnly, allowAll: !whitelistOnly };
                          updateAllowlist(patch);
                          void persistAllowlist(patch);
                        }}
                      />
                    </SettingsField>
                    {draft.allowlist.allowAll ? <div className="bot-access-panel__warning">{t("settings.botAllowAllWarn")}</div> : null}
                    <SettingsField label={t("settings.botAllowlistEntries")} hint={t("settings.botListPlaceholder")}>
                      <div className="bot-list-grid">
                        <label className="bot-list-input">
                          <span>{t("settings.botQQUsers")}</span>
                          <textarea
                            className="mem-input bot-list-input__textarea"
                            value={allowlistText.qqUsers}
                            disabled={busy || draft.allowlist.allowAll}
                            placeholder={t("settings.botListPlaceholder")}
                            spellCheck={false}
                            onChange={(event) => setAllowlistText((prev) => ({ ...prev, qqUsers: event.target.value }))}
                            onBlur={(event) => persistAllowlistText("qqUsers", event.currentTarget.value)}
                          />
                        </label>
                        <label className="bot-list-input">
                          <span>{t("settings.botFeishuLarkUsers")}</span>
                          <textarea
                            className="mem-input bot-list-input__textarea"
                            value={allowlistText.feishuUsers}
                            disabled={busy || draft.allowlist.allowAll}
                            placeholder={t("settings.botListPlaceholder")}
                            spellCheck={false}
                            onChange={(event) => setAllowlistText((prev) => ({ ...prev, feishuUsers: event.target.value }))}
                            onBlur={(event) => persistAllowlistText("feishuUsers", event.currentTarget.value)}
                          />
                        </label>
                        <label className="bot-list-input">
                          <span>{t("settings.botWeixinUsers")}</span>
                          <textarea
                            className="mem-input bot-list-input__textarea"
                            value={allowlistText.weixinUsers}
                            disabled={busy || draft.allowlist.allowAll}
                            placeholder={t("settings.botListPlaceholder")}
                            spellCheck={false}
                            onChange={(event) => setAllowlistText((prev) => ({ ...prev, weixinUsers: event.target.value }))}
                            onBlur={(event) => persistAllowlistText("weixinUsers", event.currentTarget.value)}
                          />
                        </label>
                        <label className="bot-list-input">
                          <span>{t("settings.botQQGroups")}</span>
                          <textarea
                            className="mem-input bot-list-input__textarea"
                            value={allowlistText.qqGroups}
                            disabled={busy || draft.allowlist.allowAll}
                            placeholder={t("settings.botListPlaceholder")}
                            spellCheck={false}
                            onChange={(event) => setAllowlistText((prev) => ({ ...prev, qqGroups: event.target.value }))}
                            onBlur={(event) => persistAllowlistText("qqGroups", event.currentTarget.value)}
                          />
                        </label>
                        <label className="bot-list-input">
                          <span>{t("settings.botFeishuLarkGroups")}</span>
                          <textarea
                            className="mem-input bot-list-input__textarea"
                            value={allowlistText.feishuGroups}
                            disabled={busy || draft.allowlist.allowAll}
                            placeholder={t("settings.botListPlaceholder")}
                            spellCheck={false}
                            onChange={(event) => setAllowlistText((prev) => ({ ...prev, feishuGroups: event.target.value }))}
                            onBlur={(event) => persistAllowlistText("feishuGroups", event.currentTarget.value)}
                          />
                        </label>
                        <label className="bot-list-input">
                          <span>{t("settings.botWeixinGroups")}</span>
                          <textarea
                            className="mem-input bot-list-input__textarea"
                            value={allowlistText.weixinGroups}
                            disabled={busy || draft.allowlist.allowAll}
                            placeholder={t("settings.botListPlaceholder")}
                            spellCheck={false}
                            onChange={(event) => setAllowlistText((prev) => ({ ...prev, weixinGroups: event.target.value }))}
                            onBlur={(event) => persistAllowlistText("weixinGroups", event.currentTarget.value)}
                          />
                        </label>
                      </div>
                    </SettingsField>
                  </div>
                ) : null}
              </details>

              <section className="bot-detail-section">
                <div className="bot-detail-section__head">{t("settings.botRuntimeSettings")}</div>
                <SettingsField label={t("settings.botToolApprovalMode")} hint={t("settings.botToolApprovalModeHint")}>
                  <div className="provider-add-segmented" role="group" aria-label={t("settings.botToolApprovalMode")}>
                    {BOT_TOOL_APPROVAL_MODES.map((mode) => (
                      <button
                        key={mode || "inherit"}
                        type="button"
                        className={selectedConnectionToolApprovalMode === mode ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
                        disabled={busy}
                        onClick={() => persistConnectionToolApprovalMode(selectedConnection.id, mode)}
                      >
                        {t(`settings.botToolApprovalMode.${mode || "inherit"}` as DictKey)}
                      </button>
                    ))}
                  </div>
                </SettingsField>
                <SettingsField label={t("settings.botChannelModel")} hint={t("settings.botChannelModelHint")}>
                  <ModelPicker
                    s={s}
                    refs={refs}
                    value={toRef(selectedConnection.model, s)}
                    disabled={busy}
                    emptyOptionLabel={t("settings.botChannelModelAuto")}
                    emptyOptionHint={settingsModelMeta(s, t)}
                    onPick={(model) => void persistConnection(selectedConnection.id, { model })}
                  />
                </SettingsField>
                <SettingsField label={t("settings.botWorkspaceRoot")} hint={t("settings.botWorkspaceRootHint")}>
                  <input
                    className="mem-input"
                    value={selectedConnection.workspaceRoot}
                    disabled={busy}
                    placeholder={t("settings.botWorkspaceRootPlaceholder")}
                    spellCheck={false}
                    onChange={(event) => updateConnection(selectedConnection.id, { workspaceRoot: event.target.value })}
                    onBlur={(event) => void persistConnection(selectedConnection.id, { workspaceRoot: event.currentTarget.value })}
                  />
                </SettingsField>
              </section>

              <section className="bot-detail-section">
                <div className="bot-detail-section__head">{t("settings.botCredential")}</div>
                <div className="bot-credential-stack">
                  <div className="bot-credential-line">
                    <span>{botConnectionCredentialSummary(selectedConnection, t)}</span>
                    <strong>{selectedConnection.credential.secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing")}</strong>
                  </div>
                  {botConnectionSecretEnv(selectedConnection) ? (
                    <div className="bot-secret-row">
                      <input
                        className="mem-input"
                        value={botConnectionSecretEnv(selectedConnection)}
                        disabled={busy}
                        spellCheck={false}
                        onChange={(event) => updateConnectionCredential(selectedConnection.id, botConnectionSecretPatch(selectedConnection, event.target.value))}
                        onBlur={(event) => void persistConnectionCredential(selectedConnection.id, botConnectionSecretPatch(selectedConnection, event.currentTarget.value))}
                      />
                      <input
                        className="mem-input"
                        type="password"
                        value={connectionSecrets[selectedConnection.id] ?? ""}
                        disabled={busy}
                        placeholder={selectedConnection.credential.secretSet ? t("settings.botSecretReplace") : t("settings.botSecretPaste")}
                        onChange={(event) => setConnectionSecrets((prev) => ({ ...prev, [selectedConnection.id]: event.target.value }))}
                      />
                      <button type="button" className="btn btn--secondary btn--small" disabled={busy || !(connectionSecrets[selectedConnection.id] ?? "").trim()} onClick={() => void saveConnectionSecret(selectedConnection)}>
                        {t("settings.saveKey")}
                      </button>
                      <button type="button" className="btn btn--secondary btn--small" disabled={busy || !selectedConnection.credential.secretSet} onClick={() => void clearConnectionSecret(selectedConnection)}>
                        {t("settings.clearKey")}
                      </button>
                    </div>
                  ) : null}
                </div>
              </section>

              <section className="bot-detail-section bot-detail-section--danger">
                <div>
                  <div className="bot-detail-section__head">{t("settings.botDangerZone")}</div>
                  <p>{t("settings.deleteBotHint")}</p>
                </div>
                <InlineConfirmButton
                  label={t("settings.deleteBot")}
                  confirmLabel={t("settings.confirmDeleteBot")}
                  cancelLabel={t("common.cancel")}
                  disabled={busy}
                  danger
                  onConfirm={() => removeConnection(selectedConnection)}
                />
              </section>
          </article>
        ) : null}

        <div className="bot-add-panel">
          <div className="bot-phone-connect__top">
            <div className="bot-phone-connect__title">
              <strong>{t("settings.botConnectPhoneTitle")}</strong>
              <span>{t("settings.botConnectPhoneSubtitle")}</span>
            </div>
          </div>

          <div className="bot-phone-targets" role="tablist" aria-label={t("settings.botChannels")}>
            {BOT_INSTALL_TARGETS.map((target) => (
              <button
                key={target}
                type="button"
                role="tab"
                aria-selected={installTarget === target}
                className={`bot-phone-target${installTarget === target ? " bot-phone-target--active" : ""}`}
                disabled={busy || install.status === "starting"}
                onClick={() => setInstallTarget(target)}
              >
                <strong>{botTargetLabel(target, t)}</strong>
                <span>{botTargetHint(target, t)}</span>
              </button>
            ))}
          </div>

          {isQQInstallTarget ? (
            <div className="bot-connect-panel bot-connect-panel--manual bot-connect-panel--qq">
              <div className="bot-connect-panel__body">
                <div className="bot-qq-simple__head">
                  <div>
                    <strong>{selectedInstallLabel}</strong>
                    <p>{t("settings.botInstallManualQQ")}</p>
                  </div>
                  <span className={`bot-qq-simple__status${qqConfigured ? " bot-qq-simple__status--ready" : ""}`}>
                    {qqConfigured ? <CheckCircle2 aria-hidden="true" /> : <KeyRound aria-hidden="true" />}
                    {draft.qq.secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing")}
                  </span>
                </div>
                <div className="bot-manual-form bot-manual-form--qq">
                  <div className="bot-card-field">
                    <span>{t("settings.botAppId")}</span>
                    <div>
                      <input
                        className="mem-input"
                        aria-label={t("settings.botAppId")}
                        value={draft.qq.appId}
                        disabled={busy}
                        spellCheck={false}
                        onChange={(event) => updateQQ({ appId: event.target.value })}
                        onBlur={(event) => void persistQQ({ appId: event.currentTarget.value })}
                      />
                    </div>
                  </div>
                  <div className="bot-card-field">
                    <span>{t("settings.botAppSecret")}</span>
                    <div>
                      <input
                        className="mem-input"
                        type="password"
                        value={qqSecretValue}
                        disabled={busy}
                        placeholder={draft.qq.secretSet ? t("settings.botSecretSavedOptional") : t("settings.botSecretPaste")}
                        spellCheck={false}
                        aria-label={t("settings.botSecretValue")}
                        onChange={(event) => setQQSecretValue(event.target.value)}
                      />
                    </div>
                  </div>
                  <div className="bot-qq-simple__actions">
                    <button type="button" className="btn btn--primary btn--small" disabled={busy || !qqCanSaveAndEnable} onClick={() => void saveQQAndEnable()}>
                      {t("settings.botSaveAndEnable")}
                    </button>
                  </div>
                  {!qqCanEnableAccess ? <div className="bot-connect-panel__hint bot-connect-panel__hint--warning">{t("settings.botQQAccessRequired")}</div> : null}
                </div>
              </div>
            </div>
          ) : (
            <div className="bot-connect-panel bot-connect-panel--phone">
              <div className="bot-connect-panel__qr">
                {selectedInstallConnection ? (
                  <div className="bot-connect-panel__state bot-connect-panel__state--success">
                    <CheckCircle2 aria-hidden="true" />
                  </div>
                ) : install.status === "showing" && installQrURL ? (
                  installQrIsImage ? (
                    <img src={installQrURL} alt={t("settings.botInstallQrAlt")} />
                  ) : (
                    <Suspense fallback={<div className="bot-connect-panel__state"><QrCode aria-hidden="true" /></div>}>
                      <QRCodeSVG className="bot-connect-panel__qr-code" value={installQrURL} size={196} marginSize={1} />
                    </Suspense>
                  )
                ) : install.status === "starting" ? (
                  <div className="bot-connect-panel__state">
                    <Loader2 className="bot-spin" aria-hidden="true" />
                    <span>{t("settings.botInstallStarting")}</span>
                  </div>
                ) : install.status === "error" ? (
                  <div className="bot-connect-panel__state bot-connect-panel__state--error">
                    <RefreshCw aria-hidden="true" />
                  </div>
                ) : (
                  <div className="bot-connect-panel__state">
                    <QrCode aria-hidden="true" />
                  </div>
                )}
              </div>
              <div className="bot-connect-panel__body">
                <strong>{selectedInstallLabel}</strong>
                <p>
                  {selectedInstallConnection
                    ? t("settings.botInstallAlreadyConnected", { provider: selectedInstallLabel })
                    : install.message || botTargetHint(installTarget, t)}
                </p>
                {install.status === "showing" && install.timeLeft > 0 ? (
                  <span className="bot-connect-panel__timer">{t("settings.botInstallTimeLeft", { time: formatInstallTimeLeft(install.timeLeft) })}</span>
                ) : null}
                {installUserCode ? <code>{installUserCode}</code> : null}
                <div className="bot-connect-panel__actions">
                  {!selectedInstallConnection && install.status !== "showing" && install.status !== "starting" ? (
                    <button type="button" className="btn btn--primary btn--small" disabled={busy} onClick={() => void startInstall(installTarget)}>
                      {install.status === "error" ? <RefreshCw aria-hidden="true" /> : <QrCode aria-hidden="true" />}
                      {install.status === "error" ? t("settings.botInstallRetry") : t("settings.botInstallGenerate")}
                    </button>
                  ) : null}
                  {install.status === "showing" ? (
                    <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void pollInstall()}>
                      {t("settings.botInstallCheck")}
                    </button>
                  ) : null}
                  {selectedInstallConnection ? (
                    <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void diagnoseConnection(selectedInstallConnection.id)}>
                      {t("settings.botDiagnose")}
                    </button>
                  ) : null}
                </div>
              </div>
            </div>
          )}
        </div>
    </div>
  );
}

function diagnosticMessage(diag?: BotConnectionDiagnostic | string): string {
  if (typeof diag === "string") return diag;
  return diag?.message || diag?.status || "";
}

function diagnosticReportDetail(diag?: BotConnectionDiagnostic | string): string {
  if (typeof diag === "string") return "";
  return diag?.reportDetail || "";
}

function botTargetLabel(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return "QQ";
    case "lark": return "Lark";
    case "weixin": return t("settings.botWeixin");
    default: return t("settings.botFeishu");
  }
}

function botTargetHint(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return t("settings.botInstallQQHint");
    case "lark": return t("settings.botInstallLarkHint");
    case "weixin": return t("settings.botInstallWeixinHint");
    default: return t("settings.botInstallFeishuHint");
  }
}

function qqBotAdded(qq: BotSettingsView["qq"]): boolean {
  return Boolean(qq.enabled || qq.secretSet || qq.appId.trim());
}

function qqAccessReady(allowlist: BotAllowlistView): boolean {
  if (allowlist.allowAll) return true;
  if (!allowlist.enabled) return false;
  return asArray(allowlist.qqUsers).some((value) => value.trim()) || asArray(allowlist.qqGroups).some((value) => value.trim());
}

function botInstallTargetMatchesConnection(target: BotOfficialInstallTarget, connection: BotConnectionView): boolean {
  if (target === "weixin") return connection.provider === "weixin";
  if (target === "lark") return connection.provider === "feishu" && connection.domain === "lark";
  return connection.provider === "feishu" && connection.domain !== "lark";
}

function formatInstallUserCode(code: string): string {
  const compact = code.replace(/[^a-z0-9]/gi, "").toUpperCase().slice(0, 8);
  if (compact.length <= 4) return compact;
  return `${compact.slice(0, 4)}-${compact.slice(4)}`;
}

function formatInstallTimeLeft(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  const minutes = Math.floor(value / 60);
  const rest = value % 60;
  return `${minutes}:${String(rest).padStart(2, "0")}`;
}

function botConnectionLabel(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  if (connection.domain === "lark") return "Lark";
  if (connection.provider === "weixin") return t("settings.botWeixin");
  if (connection.provider === "qq") return "QQ";
  return t("settings.botFeishu");
}

function firstConnectionRemote(connection: BotConnectionView): string {
  return connection.sessionMappings.find((mapping) => mapping.remoteId.trim())?.remoteId ?? "";
}

function botConnectionScopeLabel(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  return connection.workspaceRoot.trim() ? t("settings.botScopeProject") : t("settings.botScopeGlobal");
}

function botConnectionSecretEnv(connection: BotConnectionView): string {
  return connection.provider === "weixin" ? connection.credential.tokenEnv : connection.credential.appSecretEnv;
}

function botConnectionSecretPatch(connection: BotConnectionView, value: string): Partial<BotConnectionView["credential"]> {
  return connection.provider === "weixin" ? { tokenEnv: value } : { appSecretEnv: value };
}

function botConnectionCredentialSummary(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  if (connection.provider === "weixin") {
    return connection.credential.accountId
      ? t("settings.botCredentialAccount", { value: connection.credential.accountId })
      : t("settings.botCredentialLocalWeixin");
  }
  if (connection.credential.appId) {
    return t("settings.botCredentialApp", { value: connection.credential.appId });
  }
  return t("settings.botCredentialConfigured");
}

function ToggleSegment({
  value,
  disabled,
  onLabel,
  offLabel,
  onChange,
}: {
  value: boolean;
  disabled: boolean;
  onLabel?: string;
  offLabel?: string;
  onChange: (value: boolean) => void;
}) {
  const t = useT();
  return (
    <div className="set-seg">
      <button
        type="button"
        className={`set-seg__btn${value ? " set-seg__btn--on" : ""}`}
        disabled={disabled}
        onClick={() => onChange(true)}
      >
        {onLabel ?? t("settings.toggleOn")}
      </button>
      <button
        type="button"
        className={`set-seg__btn${!value ? " set-seg__btn--on" : ""}`}
        disabled={disabled}
        onClick={() => onChange(false)}
      >
        {offLabel ?? t("settings.toggleOff")}
      </button>
    </div>
  );
}

function sanitizeBotDraft(draft: BotSettingsView): BotSettingsView {
  const bot = normalizeBotSettings(draft);
  return {
    ...bot,
    model: bot.model.trim(),
    toolApprovalMode: normalizeBotToolApprovalMode(bot.toolApprovalMode),
    maxSteps: Math.max(0, Math.floor(bot.maxSteps || 0)),
    debounceMs: Math.max(0, Math.floor(bot.debounceMs || 0)),
    allowlist: {
      ...bot.allowlist,
      qqUsers: uniqueStrings(bot.allowlist.qqUsers.map((v) => v.trim())),
      feishuUsers: uniqueStrings(bot.allowlist.feishuUsers.map((v) => v.trim())),
      weixinUsers: uniqueStrings(bot.allowlist.weixinUsers.map((v) => v.trim())),
      qqGroups: uniqueStrings(bot.allowlist.qqGroups.map((v) => v.trim())),
      feishuGroups: uniqueStrings(bot.allowlist.feishuGroups.map((v) => v.trim())),
      weixinGroups: uniqueStrings(bot.allowlist.weixinGroups.map((v) => v.trim())),
    },
    qq: {
      ...bot.qq,
      appId: bot.qq.appId.trim(),
      appSecretEnv: bot.qq.appSecretEnv.trim(),
    },
    feishu: {
      ...bot.feishu,
      domain: bot.feishu.domain === "lark" ? "lark" : "feishu",
      appId: bot.feishu.appId.trim(),
      appSecretEnv: bot.feishu.appSecretEnv.trim(),
      verificationToken: bot.feishu.verificationToken.trim(),
      mode: bot.feishu.mode === "websocket" ? "websocket" : "webhook",
      webhookPort: Math.max(0, Math.floor(bot.feishu.webhookPort || 0)),
    },
    weixin: {
      ...bot.weixin,
      accountId: bot.weixin.accountId.trim(),
      tokenEnv: bot.weixin.tokenEnv.trim(),
      apiBase: bot.weixin.apiBase.trim().replace(/\/+$/, ""),
    },
    connections: bot.connections.map(normalizeBotConnection).filter((conn) => conn.id && conn.provider),
  };
}

function botDraftWithDerivedGatewayState(draft: BotSettingsView): BotSettingsView {
  const bot = sanitizeBotDraft(draft);
  return {
    ...bot,
    enabled: bot.qq.enabled || bot.connections.some((connection) => connection.enabled),
  };
}

function ModelsSection({ s, busy, apply, backgroundApply }: ModelsSectionProps) {
  const t = useT();
  const [subtab, setSubtab] = useState<"usage" | "access">("usage");
  const autoRefreshKeyRef = useRef("");
  const refs = useMemo(() => allRefs(s), [s.providers]);
  const defaultRef = toRef(s.defaultModel, s);
  const plannerRef = toRef(s.plannerModel, s);
  const subagentRef = toRef(s.subagentModel, s);
  const plannerSelectRef = plannerRef === defaultRef ? "" : plannerRef;
  const [defaultProvider] = defaultRef.split("/");
  const defaultProviderView = s.providers.find((p) => p.name === defaultProvider);
  const modelIssue = !defaultProviderView
    ? t("settings.modelUnavailable", { ref: defaultRef || t("common.none") })
    : !providerIsConfigured(defaultProviderView)
      ? t("settings.modelNeedsKey", { provider: modelProviderLabel(defaultProvider, defaultProviderView, t) })
      : "";
  const agent = s.agent ?? { temperature: 0, maxSteps: 0, plannerMaxSteps: 0, systemPrompt: "", coldResumePrune: true, reasoningLanguage: "auto" };
  const setAgentSteps = (maxSteps: number, plannerMaxSteps: number) => (
    app.SetAgentParams(agent.temperature, maxSteps, plannerMaxSteps, agent.systemPrompt)
  );

  useEffect(() => {
    if (subtab !== "usage") return;
    const groups = providerAccessGroups(s.providers.filter((p) => p.added), t);
    const candidates = groups
      .map((group) => {
        const provider = group.providers.find((p) => providerIsConfigured(p) && p.baseUrl);
        return provider ? { group, provider } : null;
      })
      .filter((item): item is { group: ProviderAccessGroup; provider: ProviderView } => Boolean(item));
    const refreshKey = candidates.map(({ group, provider }) => `${group.id}:${provider.apiKeyEnv || provider.name}:${provider.baseUrl}`).join("|");
    if (!refreshKey || autoRefreshKeyRef.current === refreshKey) return;
    autoRefreshKeyRef.current = refreshKey;

    void backgroundApply(async () => {
      for (const { provider } of candidates) {
        // Background auto-refresh only protects a user-curated model list.
        // If the user hasn't specified any models, don't silently populate
        // the provider with every model from the API.
        if (!provider.models || provider.models.length === 0) continue;
        try {
          const fetched = await app.FetchProviderModels(provider);
          if (fetched.length === 0) continue;
          const models = mergedFetchedProviderModels(provider.models, fetched, { preserveCurated: true });
          const currentDefault = providerDefaultModel(provider.default, models);
          const visionModels = provider.visionModels.filter((model) => models.includes(model));
          if (sameStringList(provider.models, models) && provider.default === currentDefault && sameStringList(provider.visionModels, visionModels)) continue;
          await app.SaveProvider({ ...provider, models, default: currentDefault, visionModels });
        } catch {
          // Background discovery is opportunistic; manual refresh shows errors.
        }
      }
    });
  }, [backgroundApply, s.providers, subtab, t]);

  return (
    <>
      <div className="settings-subtabs">
        <button
          type="button"
          className={`settings-subtab${subtab === "usage" ? " settings-subtab--active" : ""}`}
          aria-selected={subtab === "usage"}
          onClick={() => setSubtab("usage")}
        >
          {t("settings.modelTab.usage")}
        </button>
        <button
          type="button"
          className={`settings-subtab${subtab === "access" ? " settings-subtab--active" : ""}`}
          aria-selected={subtab === "access"}
          onClick={() => setSubtab("access")}
        >
          {t("settings.modelTab.access")}
        </button>
      </div>

      {subtab === "usage" ? (
        <>
          <SettingsSection title={t("settings.modelUsage")}>
            <SettingsField label={t("settings.defaultModel")} hint={t("settings.defaultModelHint")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={toRef(s.defaultModel, s)}
                disabled={busy}
                onPick={(ref) => void apply(() => app.SetDefaultModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.plannerModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={plannerSelectRef}
                disabled={busy}
                includeSameDefault
                onPick={(ref) => void apply(() => app.SetPlannerModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.subagentModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={subagentRef}
                disabled={busy}
                emptyOptionLabel={t("settings.subagentModelDefault")}
                emptyOptionHint={t("common.auto")}
                onPick={(ref) => void apply(() => app.SetSubagentModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.subagentEffort")} hint={t("settings.subagentHint")}>
              <select
                className="mem-select set-grow"
                value={s.subagentEffort || ""}
                disabled={busy}
                onChange={(e) => void apply(() => app.SetSubagentEffort(e.target.value))}
              >
                <option value="">{t("settings.subagentEffortDefault")}</option>
                {EFFORT_PRESETS.map((level) => (
                  <option key={level} value={level}>
                    {level}
                  </option>
                ))}
              </select>
            </SettingsField>

            {modelIssue && <div className="provider-fetch-banner provider-fetch-banner--warn">{modelIssue}</div>}
          </SettingsSection>
          <SettingsSection title={t("settings.agentRuntime")} description={t("settings.agentRuntimeHint")}>
            <SettingsField label={t("settings.executorMaxSteps")} hint={t("settings.executorMaxStepsHint")}>
              <StepLimitControl
                value={agent.maxSteps}
                presets={[10, 25, 50, 0]}
                busy={busy}
                onChange={(next) => void apply(() => setAgentSteps(next, agent.plannerMaxSteps))}
              />
            </SettingsField>
            <SettingsField label={t("settings.plannerMaxSteps")} hint={plannerSelectRef ? t("settings.plannerMaxStepsHint") : t("settings.plannerMaxStepsDisabledHint")}>
              <StepLimitControl
                value={agent.plannerMaxSteps}
                presets={[6, 12, 25, 0]}
                busy={busy}
                onChange={(next) => void apply(() => setAgentSteps(agent.maxSteps, next))}
              />
            </SettingsField>
            <SettingsField label={t("settings.coldResumePrune")} hint={t("settings.coldResumePruneHint")}>
              <div className="set-seg">
                {([true, false] as const).map((on) => (
                  <button
                    key={on ? "on" : "off"}
                    className={`set-seg__btn${agent.coldResumePrune === on ? " set-seg__btn--on" : ""}`}
                    disabled={busy}
                    onClick={() => void apply(() => app.SetColdResumePrune(on))}
                  >
                    {on ? t("settings.coldResumePrune.on") : t("settings.coldResumePrune.off")}
                  </button>
                ))}
              </div>
            </SettingsField>
            <SettingsField label={t("settings.reasoningLanguage")} hint={t("settings.reasoningLanguageHint")}>
              <div className="set-seg">
                {(["auto", "zh", "en"] as const).map((lang) => (
                  <button
                    key={lang}
                    className={`set-seg__btn${agent.reasoningLanguage === lang ? " set-seg__btn--on" : ""}`}
                    disabled={busy}
                    onClick={() => void apply(() => app.SetReasoningLanguage(lang))}
                  >
                    {t(`settings.reasoningLanguage.${lang}`)}
                  </button>
                ))}
              </div>
            </SettingsField>
          </SettingsSection>
        </>
      ) : (
        <ProvidersSection s={s} busy={busy} apply={apply} />
      )}
    </>
  );
}

type ModelPickerOption = {
  ref: string;
  provider: string;
  model: string;
  providerView?: ProviderView;
};

function ModelPicker({
  s,
  refs,
  value,
  disabled,
  includeSameDefault = false,
  emptyOptionLabel,
  emptyOptionHint,
  onPick,
}: {
  s: SettingsView;
  refs: string[];
  value: string;
  disabled: boolean;
  includeSameDefault?: boolean;
  emptyOptionLabel?: string;
  emptyOptionHint?: string;
  onPick: (ref: string) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const triggerRef = useRef<HTMLButtonElement>(null);
  // Debounce search to avoid expensive filtering on every keystroke
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQuery(query), 150);
    return () => clearTimeout(timer);
  }, [query]);
  const q = debouncedQuery.trim().toLowerCase();
  const emptyLabel = includeSameDefault ? t("settings.plannerNone") : emptyOptionLabel;
  const emptyHint = includeSameDefault ? t("settings.plannerNoneHint") : emptyOptionHint;
  const emptyMeta = includeSameDefault ? t("settings.plannerNoneHintShort") : emptyOptionHint;
  const selected = refs.includes(value) ? modelOptionFromRef(value, s) : null;
  const selectedLabel = value === "" && emptyLabel
    ? emptyLabel
    : selected?.model || value || t("common.none");
  const selectedMeta = value === "" && emptyLabel
    ? emptyMeta || ""
    : selected
    ? modelOptionMeta(selected, t)
    : t("settings.noModelsConfigured");
  const emptyOptionVisible = Boolean(emptyLabel) && (!q || `${emptyLabel} ${emptyHint || ""}`.toLowerCase().includes(q));

  const groups = useMemo(() => {
    const providerOrder: string[] = [];
    const providerSeen = new Set<string>();
    for (const p of s.providers) {
      const id = providerGroupID(p);
      if (!providerSeen.has(id)) {
        providerOrder.push(id);
        providerSeen.add(id);
      }
    }
    const options = refs
      .map((ref) => modelOptionFromRef(ref, s))
      .filter((opt): opt is ModelPickerOption => Boolean(opt))
      .filter((opt) => !q || `${opt.ref} ${opt.provider} ${modelProviderLabel(opt.provider, opt.providerView, t)} ${opt.model}`.toLowerCase().includes(q));
    for (const opt of options) {
      const groupID = modelOptionGroupID(opt);
      if (!providerSeen.has(groupID)) {
        providerOrder.push(groupID);
        providerSeen.add(groupID);
      }
    }
    return providerOrder
      .map((groupID) => {
        const providerViews = s.providers.filter((p) => providerGroupID(p) === groupID);
        const firstProvider = providerViews[0];
        return {
          groupID,
          label: firstProvider ? providerGroupLabel(firstProvider, t) : groupID,
          keySet: providerViews.some((p) => p.keySet),
          requiresKey: providerViews.every((p) => providerRequiresKey(p)),
          options: uniqueModelOptions(options.filter((opt) => modelOptionGroupID(opt) === groupID)),
        };
      })
      .filter((group) => group.options.length > 0);
  }, [q, refs, s, t]);

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const pick = (ref: string) => {
    setOpen(false);
    if (ref !== value) onPick(ref);
  };

  return (
    <div className="settings-model-picker">
      <button
        ref={triggerRef}
        type="button"
        className="settings-model-picker__trigger"
        disabled={disabled || (!includeSameDefault && !emptyOptionLabel && refs.length === 0)}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((next) => !next)}
      >
        <span className="settings-model-picker__selected">
          <span>{selectedLabel}</span>
          <small>{selectedMeta}</small>
        </span>
        <ChevronDown size={16} className={`settings-model-picker__chev${open ? " settings-model-picker__chev--open" : ""}`} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="settings-model-picker__menu"
        placement="bottom"
        style={{ width: triggerRef.current?.getBoundingClientRect().width }}
      >
        <div className="settings-model-picker__search">
          <input
            value={query}
            placeholder={t("settings.searchModels")}
            onChange={(e) => setQuery(e.target.value)}
            autoFocus
          />
        </div>
        <div className="settings-model-picker__list" role="listbox">
          {emptyOptionVisible && (
            <button
              type="button"
              role="option"
              aria-selected={value === ""}
              className={`settings-model-picker__option settings-model-picker__option--pinned${value === "" ? " settings-model-picker__option--selected" : ""}`}
              onClick={() => pick("")}
            >
              <span>
                <strong>{emptyLabel}</strong>
                {emptyHint && <small>{emptyHint}</small>}
              </span>
              {value === "" && <Check size={14} />}
            </button>
          )}
          {groups.map((group) => (
            <div className="settings-model-picker__group" key={group.groupID}>
              <div className="settings-model-picker__group-title">
                <span>{group.label}</span>
                <small>{providerKeyStatusLabel(group, t)}</small>
              </div>
              {group.options.map((opt) => (
                <button
                  key={opt.ref}
                  type="button"
                  role="option"
                  aria-selected={opt.ref === value}
                  className={`settings-model-picker__option${opt.ref === value ? " settings-model-picker__option--selected" : ""}`}
                  onClick={() => pick(opt.ref)}
                >
                  <span>
                    <strong>{opt.model}</strong>
                    <small>{modelOptionMeta(opt, t)}</small>
                  </span>
                  {opt.ref === value && <Check size={14} />}
                </button>
              ))}
            </div>
          ))}
          {!emptyOptionVisible && groups.length === 0 && <div className="settings-model-picker__empty">{t("settings.noMatchingModels")}</div>}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function modelOptionFromRef(ref: string, s: SettingsView): ModelPickerOption | null {
  if (!ref) return null;
  const [provider, ...modelParts] = ref.split("/");
  const model = modelParts.join("/") || ref;
  return {
    ref,
    provider,
    model,
    providerView: s.providers.find((p) => p.name === provider),
  };
}

function modelOptionMeta(option: ModelPickerOption, t: ReturnType<typeof useT>): string {
  const key = option.providerView ? providerKeyStatusLabel(option.providerView, t) : t("settings.noKey");
  return `${modelProviderLabel(option.provider, option.providerView, t)} · ${key}`;
}

function providerKeyStatusLabel(provider: { keySet: boolean; requiresKey?: boolean; apiKeyEnv?: string }, t: ReturnType<typeof useT>): string {
  if (!providerRequiresKey(provider)) return t("settings.noKeyRequired");
  return provider.keySet ? t("settings.keySet") : t("settings.noKey");
}

function modelProviderLabel(provider: string, providerView: ProviderView | undefined, t: ReturnType<typeof useT>): string {
  return providerView ? providerGroupLabel(providerView, t) : provider;
}

function modelOptionGroupID(option: ModelPickerOption): string {
  return option.providerView ? providerGroupID(option.providerView) : `custom:${option.provider}`;
}

function uniqueModelOptions(options: ModelPickerOption[]): ModelPickerOption[] {
  const seen = new Set<string>();
  const out: ModelPickerOption[] = [];
  for (const option of options) {
    if (seen.has(option.model)) continue;
    seen.add(option.model);
    out.push(option);
  }
  return out;
}

function sameStringList(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((value, i) => value === b[i]);
}

function proxyModeLabel(mode: ProxyMode, t: ReturnType<typeof useT>): string {
  switch (mode) {
    case "auto":
      return t("settings.proxyMode.auto");
    case "custom":
      return t("settings.proxyMode.custom");
    case "off":
      return t("settings.proxyMode.off");
  }
}

function ProvidersSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const defaultProvider = toRef(s.defaultModel, s).split("/")[0];
  const [editing, setEditing] = useState<string | null>(null);
  const [adding, setAdding] = useState<AddProviderMode>(null);
  const [fetchingProvider, setFetchingProvider] = useState<string | null>(null);
  const [fetchResults, setFetchResults] = useState<Record<string, ProviderFetchResult>>({});
  const [modelDrafts, setModelDrafts] = useState<Record<string, ProviderModelDraft>>({});
  const groups = useMemo(() => providerAccessGroups(s.providers.filter((p) => p.added), t), [s.providers, t]);

  const setGroupFetchResult = (groupID: string, result: ProviderFetchResult | null) => {
    setFetchResults((prev) => {
      const next = { ...prev };
      if (result) next[groupID] = result;
      else delete next[groupID];
      return next;
    });
  };

  const setGroupModelDraft = (groupID: string, draft: ProviderModelDraft | null) => {
    setModelDrafts((prev) => {
      const next = { ...prev };
      if (draft) next[groupID] = draft;
      else delete next[groupID];
      return next;
    });
  };

  const modelDraftForFetch = (p: ProviderView, fetched: string[]): ProviderModelDraft => {
    const candidates = providerModelCandidates(p.models, fetched);
    const selected = mergedFetchedProviderModels(p.models, fetched, { preserveCurated: true });
    const visionSource = p.visionModelsConfigured ? p.visionModels : inferredVisionModels(candidates);
    return {
      providerName: p.name,
      candidates,
      selected: candidates.filter((model) => selected.includes(model)),
      visionModels: candidates.filter((model) => visionSource.includes(model)),
    };
  };

  const updateModelDraftSelection = (groupID: string, nextSelected: (draft: ProviderModelDraft) => string[]) => {
    setModelDrafts((prev) => {
      const draft = prev[groupID];
      if (!draft) return prev;
      const selectedSet = new Set(nextSelected(draft));
      return {
        ...prev,
        [groupID]: {
          ...draft,
          selected: draft.candidates.filter((model) => selectedSet.has(model)),
        },
      };
    });
  };

  const toggleModelDraftVision = (groupID: string, model: string) => {
    setModelDrafts((prev) => {
      const draft = prev[groupID];
      if (!draft) return prev;
      return {
        ...prev,
        [groupID]: {
          ...draft,
          visionModels: draft.visionModels.includes(model)
            ? draft.visionModels.filter((candidate) => candidate !== model)
            : draft.candidates.filter((candidate) => candidate === model || draft.visionModels.includes(candidate)),
        },
      };
    });
  };

  const refreshModels = async (group: ProviderAccessGroup, p: ProviderView) => {
    setFetchingProvider(group.id);
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    try {
      let fetched: string[];
      try {
        fetched = await app.FetchProviderModels(p);
      } catch (e) {
        setGroupFetchResult(group.id, {
          kind: "warn",
          text: t("settings.fetchModelsFailedForProvider", { provider: group.label, err: String((e as Error)?.message ?? e) }),
        });
        return;
      }
      if (fetched.length === 0) {
        setGroupFetchResult(group.id, {
          kind: "warn",
          text: t("settings.fetchModelsEmptyForProvider", { provider: group.label }),
        });
        return;
      }
      const draft = modelDraftForFetch(p, fetched);
      setGroupModelDraft(group.id, draft);
      setGroupFetchResult(group.id, {
        kind: "ok",
        text: t("settings.fetchModelsReadyForProvider", { provider: group.label, n: draft.candidates.length }),
      });
    } finally {
      setFetchingProvider(null);
    }
  };

  const refreshGroup = async (group: ProviderAccessGroup) => {
    const probe = group.providers[0];
    if (!probe) return;
    await refreshModels(group, probe);
  };

  const saveKeyEnvAndAutoRefresh = async (group: ProviderAccessGroup, apiKeyEnv: string, value: string) => {
    const probe = group.providers[0];
    if (!probe || !apiKeyEnv) return;
    setFetchingProvider(group.id);
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    try {
      await apply(async () => {
        await app.SetProviderKey(apiKeyEnv, value);
        try {
          const fetched = await app.FetchProviderModels({ ...probe, apiKeyEnv });
          if (fetched.length > 0) {
            const draft = modelDraftForFetch({ ...probe, apiKeyEnv }, fetched);
            setGroupModelDraft(group.id, draft);
            setGroupFetchResult(group.id, {
              kind: "ok",
              text: t("settings.fetchModelsReadyForProvider", { provider: group.label, n: draft.candidates.length }),
            });
            return;
          }
          setGroupFetchResult(group.id, {
            kind: "warn",
            text: t("settings.fetchModelsEmptyForProvider", { provider: group.label }),
          });
        } catch (e) {
          setGroupFetchResult(group.id, {
            kind: "warn",
            text: t("settings.fetchModelsAfterKeyFailedForProvider", { provider: group.label, err: String((e as Error)?.message ?? e) }),
          });
        }
      });
    } finally {
      setFetchingProvider(null);
    }
  };

  const saveProviderKey = async (group: ProviderAccessGroup, apiKeyEnv: string, value: string) => {
    if (!apiKeyEnv) return;
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    await apply(() => app.SetProviderKey(apiKeyEnv, value));
  };

  const clearProviderKey = async (apiKeyEnv: string) => {
    if (!apiKeyEnv) return;
    await apply(() => app.ClearProviderKey(apiKeyEnv));
  };

  const saveModelDraft = async (group: ProviderAccessGroup) => {
    const draft = modelDrafts[group.id];
    const provider = draft ? group.providers.find((p) => p.name === draft.providerName) : null;
    const models = uniqueStrings(draft?.selected ?? []);
    const visionModels = uniqueStrings(draft?.visionModels ?? []).filter((model) => models.includes(model));
    if (!draft || !provider || models.length === 0) return;
    let saved = false;
    await apply(async () => {
      await app.SaveProvider({
        ...provider,
        models,
        visionModels,
        visionModelsConfigured: true,
        default: providerDefaultModel(provider.default, models),
      });
      saved = true;
    });
    if (!saved) return;
    setGroupModelDraft(group.id, null);
    setGroupFetchResult(group.id, {
      kind: "ok",
      text: t("settings.enabledModelsSavedForProvider", { provider: group.label, n: models.length }),
    });
  };

  return (
    <SettingsSection
      title={t("settings.providerAccess")}
      description={t("settings.providerAccessHint")}
      actions={
        <button className="btn btn--small" disabled={busy || adding !== null} onClick={() => setAdding("official")}>
          {t("settings.addProvider")}
        </button>
      }
    >
      <div className="provider-access-grid">
        {groups.length === 0 && adding === null && (
          <div className="provider-empty">
            <strong>{t("settings.providerAccessEmptyTitle")}</strong>
            <span>{t("settings.providerAccessEmptyHint")}</span>
            <div className="provider-empty__actions">
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding("official")}>
                {t("settings.addProvider.officialChoice")}
              </button>
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding("custom")}>
                {t("settings.addProvider.customChoice")}
              </button>
            </div>
          </div>
        )}
        {adding !== null && (
          <AddProviderPanel
            mode={adding}
            kinds={s.providerKinds}
            busy={busy}
            onMode={setAdding}
            onCancel={() => setAdding(null)}
            onAddOfficial={(kind, key) => apply(() => app.AddOfficialProviderAccess(kind, key)).then(() => setAdding(null))}
            onAddCustom={(pv) => apply(() => app.SaveProvider(pv)).then(() => setAdding(null))}
          />
        )}
        {adding === null && groups.map((group) => (
          <ProviderAccessCard
            key={group.id}
            group={group}
            busy={busy}
            fetching={fetchingProvider === group.id || group.providers.some((p) => fetchingProvider === p.name)}
            fetchResult={fetchResults[group.id]}
            modelDraft={modelDrafts[group.id]}
            defaultProvider={defaultProvider}
            editing={editing}
            kinds={s.providerKinds}
            onEdit={setEditing}
            onCancelEdit={() => setEditing(null)}
            onSave={(pv) => apply(() => app.SaveProvider(pv)).then(() => {
              setEditing(null);
              setGroupModelDraft(group.id, null);
            })}
            onRefresh={() => void refreshGroup(group)}
            onToggleDraftModel={(model) => updateModelDraftSelection(group.id, (draft) => (
              draft.selected.includes(model)
                ? draft.selected.filter((candidate) => candidate !== model)
                : [...draft.selected, model]
            ))}
            onToggleDraftVision={(model) => toggleModelDraftVision(group.id, model)}
            onSelectAllDraftModels={() => updateModelDraftSelection(group.id, (draft) => draft.candidates)}
            onClearDraftModels={() => updateModelDraftSelection(group.id, () => [])}
            onCancelDraftModels={() => setGroupModelDraft(group.id, null)}
            onSaveDraftModels={() => void saveModelDraft(group)}
            onSaveEditorKey={(env, value) => group.builtIn ? saveProviderKey(group, env, value) : saveKeyEnvAndAutoRefresh(group, env, value)}
            onClearEditorKey={clearProviderKey}
            onDelete={(p) => apply(() => app.RemoveProviderAccess(p.name))}
          />
        ))}
      </div>
    </SettingsSection>
  );
}

type ProviderAccessGroup = {
  id: string;
  label: string;
  description: string;
  builtIn: boolean;
  providers: ProviderView[];
  apiKeyEnv: string;
  keySet: boolean;
  requiresKey: boolean;
  configured: boolean;
  keySource?: string;
  keySourcePath?: string;
  baseUrl: string;
  kind: string;
  models: string[];
};

type ProviderFetchResult = {
  kind: "ok" | "warn";
  text: string;
};

type ProviderModelDraft = {
  providerName: string;
  candidates: string[];
  selected: string[];
  visionModels: string[];
};

type AddProviderMode = null | "official" | "custom";
type OfficialProviderKind = "deepseek";

const OFFICIAL_PROVIDER_CHOICES: Array<{ kind: OfficialProviderKind; labelKey: DictKey; descKey: DictKey; keyEnv: string }> = [
  { kind: "deepseek", labelKey: "settings.addProvider.official.deepseek", descKey: "settings.addProvider.official.deepseekDesc", keyEnv: "DEEPSEEK_API_KEY" },
];

function AddProviderPanel({
  mode,
  kinds,
  busy,
  onMode,
  onCancel,
  onAddOfficial,
  onAddCustom,
}: {
  mode: AddProviderMode;
  kinds: string[];
  busy: boolean;
  onMode: (mode: AddProviderMode) => void;
  onCancel: () => void;
  onAddOfficial: (kind: OfficialProviderKind, key: string) => Promise<void>;
  onAddCustom: (p: ProviderView) => void | Promise<void>;
}) {
  const t = useT();
  const [officialKind, setOfficialKind] = useState<OfficialProviderKind>("deepseek");
  const [key, setKey] = useState("");
  const selected = OFFICIAL_PROVIDER_CHOICES.find((choice) => choice.kind === officialKind) ?? OFFICIAL_PROVIDER_CHOICES[0];

  const header = (
    <div className="provider-add-panel__head">
      <div>
        <strong>{t("settings.addProvider.chooseTitle")}</strong>
        <span>{t("settings.addProvider.chooseHint")}</span>
      </div>
      <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
        {t("common.cancel")}
      </button>
    </div>
  );
  const modeSwitch = (
    <div className="provider-add-segmented" role="tablist" aria-label={t("settings.addProvider.chooseTitle")}>
      <button
        type="button"
        role="tab"
        aria-selected={mode === "official"}
        className={mode === "official" ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
        disabled={busy}
        onClick={() => onMode("official")}
      >
        {t("settings.addProvider.officialChoice")}
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={mode === "custom"}
        className={mode === "custom" ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
        disabled={busy}
        onClick={() => onMode("custom")}
      >
        {t("settings.addProvider.customChoice")}
      </button>
    </div>
  );

  if (mode === "official") {
    return (
      <div className="provider-add-panel">
        {header}
        {modeSwitch}
        <div className="provider-add-panel__hint">{t("settings.addProvider.officialHint")}</div>
        <div className="provider-template-grid">
          {OFFICIAL_PROVIDER_CHOICES.map((choice) => (
            <button
              key={choice.kind}
              type="button"
              className={`provider-template-card${officialKind === choice.kind ? " provider-template-card--active" : ""}`}
              disabled={busy}
              onClick={() => setOfficialKind(choice.kind)}
            >
              <strong>{t(choice.labelKey)}</strong>
              <span>{t(choice.descKey)}</span>
            </button>
          ))}
        </div>
        <label className="set-label">{t("settings.providerKeyOptional")}</label>
        <input
          className="mem-input"
          type="password"
          placeholder={t("settings.setKey", { env: selected.keyEnv })}
          value={key}
          disabled={busy}
          onChange={(e) => setKey(e.target.value)}
        />
        <div className="prov-card__actions">
          <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn--primary btn--small"
            disabled={busy}
            onClick={() => void onAddOfficial(officialKind, key.trim())}
          >
            {t("settings.addProvider.confirm")}
          </button>
        </div>
      </div>
    );
  }

  if (mode === "custom") {
    return (
      <div className="provider-add-panel">
        {header}
        {modeSwitch}
        <div className="provider-add-panel__hint">{t("settings.addProvider.customHint")}</div>
        <ProviderEditor
          kinds={kinds}
          busy={busy}
          onCancel={onCancel}
          onSave={onAddCustom}
        />
      </div>
    );
  }
  return null;
}

function ProviderAccessCard({
  group,
  busy,
  fetching,
  fetchResult,
  modelDraft,
  defaultProvider,
  editing,
  kinds,
  onEdit,
  onCancelEdit,
  onSave,
  onRefresh,
  onToggleDraftModel,
  onToggleDraftVision,
  onSelectAllDraftModels,
  onClearDraftModels,
  onCancelDraftModels,
  onSaveDraftModels,
  onSaveEditorKey,
  onClearEditorKey,
  onDelete,
}: {
  group: ProviderAccessGroup;
  busy: boolean;
  fetching: boolean;
  fetchResult?: ProviderFetchResult;
  modelDraft?: ProviderModelDraft;
  defaultProvider: string;
  editing: string | null;
  kinds: string[];
  onEdit: (name: string) => void;
  onCancelEdit: () => void;
  onSave: (p: ProviderView) => void | Promise<void>;
  onRefresh: () => void;
  onToggleDraftModel: (model: string) => void;
  onToggleDraftVision: (model: string) => void;
  onSelectAllDraftModels: () => void;
  onClearDraftModels: () => void;
  onCancelDraftModels: () => void;
  onSaveDraftModels: () => void;
  onSaveEditorKey: (apiKeyEnv: string, value: string) => Promise<void>;
  onClearEditorKey?: (apiKeyEnv: string) => Promise<void>;
  onDelete?: (p: ProviderView) => Promise<void>;
}) {
  const t = useT();
  const editableProvider = group.providers[0];
  const isDefault = group.providers.some((p) => p.name === defaultProvider);
  const editingProvider = group.providers.find((p) => editing === p.name);
  const primaryProviderExpanded = Boolean(editableProvider && editing === editableProvider.name);
  const visibleModels = group.models.slice(0, 6);
  const hiddenModelCount = Math.max(0, group.models.length - visibleModels.length);
  return (
    <article className={`provider-access-card${group.builtIn ? " provider-access-card--builtin" : ""}`}>
      <div className="provider-access-card__head">
        <div className="provider-access-card__identity">
          <div className="provider-access-card__title">
            {group.label}
            <span className={`badge ${group.builtIn ? "badge--project" : "badge--neutral"}`}>
              {group.builtIn ? t("settings.builtinProviderBadge") : t("settings.customProviderBadge")}
            </span>
            <span className={`badge ${group.keySet ? "badge--project" : "badge--feedback"}`}>
              {providerKeyStatusLabel(group, t)}
            </span>
          </div>
          <div className="provider-access-card__desc">{group.description}</div>
        </div>
        <div className="provider-access-card__actions">
          {editableProvider && (
            <button
              className="btn btn--small"
              disabled={busy}
              aria-expanded={primaryProviderExpanded}
              onClick={() => primaryProviderExpanded ? onCancelEdit() : onEdit(editableProvider.name)}
            >
              {primaryProviderExpanded ? t("common.collapse") : t("settings.configureProvider")}
            </button>
          )}
          <button
            className="btn btn--small"
            disabled={busy || fetching || !group.baseUrl || !group.configured}
            onClick={onRefresh}
          >
            {fetching ? t("settings.fetchingModels") : t("settings.fetchModels")}
          </button>
          {editableProvider && onDelete && (
            isDefault && !group.builtIn ? (
              <Tooltip label={t("settings.cantDeleteDefault")}>
                <button className="btn btn--small" disabled>{t("settings.removeProviderAccess")}</button>
              </Tooltip>
            ) : (
              <InlineConfirmButton
                label={t("settings.removeProviderAccess")}
                confirmLabel={group.builtIn ? t("settings.confirmRemoveProviderAccess") : t("settings.confirmDeleteProvider")}
                cancelLabel={t("common.cancel")}
                disabled={busy}
                danger={!group.builtIn}
                onConfirm={() => onDelete(editableProvider)}
              />
            )
          )}
        </div>
      </div>

      <div className="provider-access-meta">
        <span>{group.kind}</span>
        <span>{group.baseUrl}</span>
        <span>{group.apiKeyEnv || t("common.none")}</span>
        {group.keySource && <span title={group.keySourcePath || undefined}>{t("settings.keySource", { source: group.keySource })}</span>}
      </div>

      <div className="provider-card-block">
        <div className="provider-card-block__label">{t(group.configured ? "settings.enabledModels" : "settings.modelList")}</div>
        <div className="provider-model-chips" aria-label={t(group.configured ? "settings.enabledModels" : "settings.modelList")}>
          {visibleModels.length > 0 ? visibleModels.map((model) => (
            <span className="provider-model-chip" key={model}>
              {model}
            </span>
          )) : <span className="provider-model-chip provider-model-chip--empty">{t("settings.noModelsConfigured")}</span>}
          {hiddenModelCount > 0 && (
            <span className="provider-model-chip provider-model-chip--more">
              {t("settings.moreModels", { n: hiddenModelCount })}
            </span>
          )}
        </div>
        {!group.configured && group.requiresKey && (
          <div className="provider-card-status provider-card-status--warn">
            {t("settings.modelsRequireKey")}
          </div>
        )}
        {fetchResult && (
          <div className={`provider-card-status provider-card-status--${fetchResult.kind}`}>
            {fetchResult.text}
          </div>
        )}
      </div>

      {modelDraft && (
        <ProviderModelDraftPicker
          draft={modelDraft}
          busy={busy}
          fetching={fetching}
          onToggle={onToggleDraftModel}
          onToggleVision={onToggleDraftVision}
          onSelectAll={onSelectAllDraftModels}
          onClear={onClearDraftModels}
          onCancel={onCancelDraftModels}
          onSave={onSaveDraftModels}
        />
      )}

      {group.providers.length > 1 && (
        <div className="provider-profiles">
          {group.providers.map((p) => {
            const profileExpanded = editing === p.name;
            return (
              <div className="provider-profile-row" key={p.name}>
                <span>{p.name}</span>
                <span>{p.models.join(", ") || t("common.none")}</span>
                <button
                  className="btn btn--small"
                  disabled={busy}
                  aria-expanded={profileExpanded}
                  onClick={() => profileExpanded ? onCancelEdit() : onEdit(p.name)}
                >
                  {profileExpanded ? t("common.collapse") : t("settings.configureProfile")}
                </button>
              </div>
            );
          })}
        </div>
      )}

      {editingProvider && (
        <ProviderEditor
          initial={editingProvider}
          kinds={kinds}
          busy={busy}
          onCancel={onCancelEdit}
          onSave={onSave}
          onSaveKey={onSaveEditorKey}
          onClearKey={onClearEditorKey}
        />
      )}
    </article>
  );
}

function ProviderModelDraftPicker({
  draft,
  busy,
  fetching,
  onToggle,
  onToggleVision,
  onSelectAll,
  onClear,
  onCancel,
  onSave,
}: {
  draft: ProviderModelDraft;
  busy: boolean;
  fetching: boolean;
  onToggle: (model: string) => void;
  onToggleVision: (model: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  // Debounce search to avoid expensive filtering on every keystroke
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQuery(query), 150);
    return () => clearTimeout(timer);
  }, [query]);
  const selected = new Set(draft.selected);
  const vision = new Set(draft.visionModels);
  const q = debouncedQuery.trim().toLowerCase();
  const visibleCandidates = q
    ? draft.candidates.filter((model) => model.toLowerCase().includes(q))
    : draft.candidates;
  const disabled = busy || fetching;

  return (
    <div className="provider-model-draft">
      <div className="provider-model-draft__head">
        <div>
          <div className="provider-card-block__label">{t("settings.modelCandidates")}</div>
          <span>{t("settings.modelCandidatesSelected", { n: draft.selected.length })}</span>
        </div>
        <div className="provider-model-draft__tools">
          <button type="button" className="btn btn--small" disabled={disabled || draft.selected.length === draft.candidates.length} onClick={onSelectAll}>
            {t("settings.selectAllModels")}
          </button>
          <button type="button" className="btn btn--small" disabled={disabled || draft.selected.length === 0} onClick={onClear}>
            {t("settings.clearModelSelection")}
          </button>
        </div>
      </div>
      <input
        className="mem-input provider-model-draft__search"
        placeholder={t("settings.modelCandidateSearch")}
        value={query}
        disabled={disabled}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div className="provider-model-draft__list" role="list" aria-label={t("settings.modelCandidates")}>
        {visibleCandidates.length > 0 ? visibleCandidates.map((model) => {
          const enabled = selected.has(model);
          return (
            <div className="provider-model-draft__option" key={model}>
              <label className="provider-model-draft__model">
                <input
                  type="checkbox"
                  checked={enabled}
                  disabled={disabled}
                  onChange={() => onToggle(model)}
                />
                <span>{model}</span>
              </label>
              <label className="provider-model-draft__vision">
                <input
                  type="checkbox"
                  checked={enabled && vision.has(model)}
                  disabled={disabled || !enabled}
                  onChange={() => onToggleVision(model)}
                />
                <span>{t("settings.visionModel")}</span>
              </label>
            </div>
          );
        }) : (
          <div className="provider-model-draft__empty">{t("settings.noMatchingCandidateModels")}</div>
        )}
      </div>
      <div className="provider-model-draft__actions">
        <button type="button" className="btn btn--small" disabled={disabled} onClick={onCancel}>
          {t("common.cancel")}
        </button>
        <button type="button" className="btn btn--primary btn--small" disabled={disabled || draft.selected.length === 0} onClick={onSave}>
          {t("settings.saveEnabledModels")}
        </button>
      </div>
    </div>
  );
}

function providerAccessGroups(providers: ProviderView[], t: ReturnType<typeof useT>): ProviderAccessGroup[] {
  const groups = new Map<string, ProviderAccessGroup>();
  for (const p of providers) {
    const id = providerGroupID(p);
    const builtIn = id.startsWith("builtin:");
    const existing = groups.get(id);
    if (existing) {
      existing.providers.push(p);
      existing.keySet = existing.keySet || p.keySet;
      existing.requiresKey = existing.requiresKey && providerRequiresKey(p);
      existing.configured = existing.configured || providerIsConfigured(p);
      if (!existing.keySource && p.keySource) existing.keySource = p.keySource;
      if (!existing.keySourcePath && p.keySourcePath) existing.keySourcePath = p.keySourcePath;
      existing.models = uniqueStrings([...existing.models, ...p.models]);
      continue;
    }
    groups.set(id, {
      id,
      label: providerGroupLabel(p, t),
      description: providerGroupDescription(p, t),
      builtIn,
      providers: [p],
      apiKeyEnv: p.apiKeyEnv,
      keySet: p.keySet,
      requiresKey: providerRequiresKey(p),
      configured: providerIsConfigured(p),
      keySource: p.keySource,
      keySourcePath: p.keySourcePath,
      baseUrl: p.baseUrl,
      kind: p.kind,
      models: uniqueStrings(p.models),
    });
  }
  return Array.from(groups.values());
}

function providerBaseHost(baseUrl: string): string {
  try {
    return new URL(baseUrl).hostname.toLowerCase();
  } catch {
    return "";
  }
}

function canonicalOfficialProviderName(name: string): string {
  switch (name.trim()) {
    case "deepseek-flash":
    case "deepseek-pro":
      return "deepseek";
    default:
      return name.trim();
  }
}

function officialProviderKind(p: ProviderView): string {
  if (!p.builtIn) return "";
  const name = canonicalOfficialProviderName(p.name);
  const host = providerBaseHost(p.baseUrl);
  if (name === "deepseek" && host === "api.deepseek.com") return "deepseek";
  return "";
}

function providerGroupID(p: ProviderView): string {
  const official = officialProviderKind(p);
  if (official) return `builtin:${official}`;
  return `custom:${p.name}`;
}

function providerGroupLabel(p: ProviderView, t?: ReturnType<typeof useT>): string {
  const id = providerGroupID(p);
  if (id === "builtin:deepseek") return t ? t("settings.providerLabel.deepseek") : "DeepSeek";
  return p.name;
}

function providerGroupDescription(p: ProviderView, t: ReturnType<typeof useT>): string {
  const id = providerGroupID(p);
  if (id === "builtin:deepseek") return t("settings.providerDesc.deepseek");
  return p.baseUrl;
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    if (value && !seen.has(value)) {
      seen.add(value);
      out.push(value);
    }
  }
  return out;
}

function parseProviderListInput(value: string): string[] {
  return uniqueStrings(value
    .split(/[,，]/)
    .map((entry) => entry.trim())
    .filter(Boolean));
}

function botAllowlistTextValues(allowlist: BotAllowlistView): Record<BotAllowlistTextKey, string> {
  return {
    qqUsers: allowlist.qqUsers.join("\n"),
    feishuUsers: allowlist.feishuUsers.join("\n"),
    weixinUsers: allowlist.weixinUsers.join("\n"),
    qqGroups: allowlist.qqGroups.join("\n"),
    feishuGroups: allowlist.feishuGroups.join("\n"),
    weixinGroups: allowlist.weixinGroups.join("\n"),
  };
}

function parseBotListInput(value: string): string[] {
  return uniqueStrings(value
    .split(/[\n,，]+/)
    .map((entry) => entry.trim())
    .filter(Boolean));
}

// Memoized model chips for ProviderEditor — prevents re-render when typing
// in name/key/baseUrl fields.
const ModelChips = memo(function ModelChips({ modelNames }: { modelNames: string[] }) {
  const t = useT();
  if (modelNames.length === 0) return null;
  return (
    <div className="provider-model-chips">
      {modelNames.slice(0, 8).map((model) => (
        <span className="provider-model-chip" key={model}>{model}</span>
      ))}
      {modelNames.length > 8 && (
        <span className="provider-model-chip provider-model-chip--more">{t("settings.moreModels", { n: modelNames.length - 8 })}</span>
      )}
    </div>
  );
});

function ProviderEditor({
  initial,
  kinds,
  busy,
  onCancel,
  onSave,
  onSaveKey,
  onClearKey,
}: {
  initial?: ProviderView;
  kinds: string[];
  busy: boolean;
  onCancel: () => void;
  onSave: (p: ProviderView) => void;
  onSaveKey?: (apiKeyEnv: string, value: string) => Promise<void>;
  onClearKey?: (apiKeyEnv: string) => Promise<void>;
}) {
  const t = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [kind, setKind] = useState(initial?.kind ?? kinds[0] ?? "openai");
  const [baseUrl, setBaseUrl] = useState(initial?.baseUrl ?? "");
  const [models, setModels] = useState((initial?.models ?? []).join(", "));
  const [visionModels, setVisionModels] = useState((initial?.visionModels ?? []).join(", "));
  const [visionModelsConfigured, setVisionModelsConfigured] = useState(
    Boolean(initial?.visionModelsConfigured ?? ((initial?.visionModels ?? []).length > 0)),
  );
  const [modelsUrl] = useState(initial?.modelsUrl ?? "");
  const [apiKeyEnv, setApiKeyEnv] = useState(initial?.apiKeyEnv ?? "");
  const [keyDraft, setKeyDraft] = useState("");
  const [balanceUrl, setBalanceUrl] = useState(initial?.balanceUrl ?? "");
  // Empty when unset so the placeholder (and its "0 = default" hint) reads instead
  // of a bare "0"; saved back as 0.
  const [ctx, setCtx] = useState(initial?.contextWindow ? String(initial.contextWindow) : "");
  const [reasoningProtocol, setReasoningProtocol] = useState(normalizeReasoningProtocol(initial?.reasoningProtocol));
  const [supportedEfforts, setSupportedEfforts] = useState<string[]>(initial?.supportedEfforts ?? []);
  const [customEffortDraft, setCustomEffortDraft] = useState("");
  const [defaultEffort, setDefaultEffort] = useState(initial?.defaultEffort ?? "");
  const [fetchingModels, setFetchingModels] = useState(false);
  const [fetchStatus, setFetchStatus] = useState<string | null>(null);
  const [fetchErr, setFetchErr] = useState<string | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const builtIn = initial?.builtIn ?? false;
  const isNewCustomProvider = !initial;

  // Offer the kinds the kernel actually registered; if the stored kind is a
  // legacy/unknown one, keep it as an option so editing doesn't silently change it.
  const kindOptions = kind && !kinds.includes(kind) ? [kind, ...kinds] : kinds;

  // Split supportedEfforts into the 5 canonical presets (for checkbox UI) and
  // any user-added custom names (rendered as removable chips). The preset order
  // is fixed; custom names keep insertion order.
  const presetEfforts = supportedEfforts.filter((e) => EFFORT_PRESETS.includes(e));
  const customEfforts = supportedEfforts.filter((e) => !EFFORT_PRESETS.includes(e));

  const togglePreset = (level: string) => {
    const has = presetEfforts.includes(level);
    const nextPresets = has ? presetEfforts.filter((e) => e !== level) : [...presetEfforts, level];
    setSupportedEfforts([...nextPresets, ...customEfforts]);
    // If the removed preset was the default, fall back to "auto" (empty string).
    if (has && defaultEffort === level) setDefaultEffort("");
  };

  const addCustomEffort = () => {
    const v = customEffortDraft.trim().toLowerCase();
    if (!v || supportedEfforts.includes(v)) {
      setCustomEffortDraft("");
      return;
    }
    setSupportedEfforts([...presetEfforts, ...customEfforts, v]);
    setCustomEffortDraft("");
  };

  const removeCustomEffort = (level: string) => {
    setSupportedEfforts(supportedEfforts.filter((e) => e !== level));
    if (defaultEffort === level) setDefaultEffort("");
  };

  const fetchModels = async () => {
    setFetchingModels(true);
    setFetchStatus(null);
    setFetchErr(null);
    try {
      const effectiveApiKeyEnv = providerApiKeyEnvForSave(name, apiKeyEnv, keyDraft);
      if (!apiKeyEnv.trim()) setApiKeyEnv(effectiveApiKeyEnv);
      if (keyDraft.trim()) await app.SetProviderKey(effectiveApiKeyEnv, keyDraft.trim());
      const fetched = await app.FetchProviderModels({
        name: name.trim() || t("settings.newProviderDraftName"),
        builtIn: initial?.builtIn ?? false,
        added: initial?.added ?? true,
        kind: kind.trim() || kinds[0] || "openai",
        baseUrl: baseUrl.trim(),
        modelsUrl,
        models: [],
        visionModels: [],
        visionModelsConfigured: false,
        default: "",
        apiKeyEnv: effectiveApiKeyEnv,
        keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
        balanceUrl: balanceUrl.trim(),
        contextWindow: Number(ctx) || 0,
        reasoningProtocol,
        supportedEfforts,
        defaultEffort,
      });
      if (fetched.length === 0) throw new Error(t("settings.fetchModelsEmpty"));
      setModels(fetched.join(", "));
      setVisionModels((current) => {
        const existing = parseProviderListInput(current).filter((model) => fetched.includes(model));
        return uniqueStrings([...existing, ...inferredVisionModels(fetched)]).filter((model) => fetched.includes(model)).join(", ");
      });
      setVisionModelsConfigured(true);
      if (keyDraft.trim()) setKeyDraft("");
      setDefaultEffort((v) => v);
      setFetchStatus(t("settings.fetchModelsSuccess", { n: fetched.length }));
    } catch (e) {
      setFetchErr(String((e as Error)?.message ?? e));
    } finally {
      setFetchingModels(false);
    }
  };

  const save = async () => {
    const ms = parseProviderListInput(models);
    const vms = parseProviderListInput(visionModels).filter((model) => ms.includes(model));
    const effectiveApiKeyEnv = providerApiKeyEnvForSave(name, apiKeyEnv, keyDraft);
    if (keyDraft.trim()) await app.SetProviderKey(effectiveApiKeyEnv, keyDraft.trim());
    onSave({
      name: name.trim(),
      builtIn: initial?.builtIn ?? false,
      added: initial?.added ?? true,
      kind: kind.trim() || kinds[0] || "openai",
      baseUrl: baseUrl.trim(),
      models: ms,
      visionModels: vms,
      visionModelsConfigured: visionModelsConfigured || vms.length > 0,
      default: ms[0] ?? "",
      apiKeyEnv: effectiveApiKeyEnv,
      modelsUrl,
      keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
      balanceUrl: balanceUrl.trim(),
      contextWindow: Number(ctx) || 0,
      reasoningProtocol,
      supportedEfforts,
      // Clear the stored default if no levels are selected; the backend's
      // NormalizeEffort would otherwise silently ignore an unsupported value.
      defaultEffort: supportedEfforts.length > 0 ? defaultEffort : "",
    });
  };

  if (builtIn) {
    const keyEnv = initial?.apiKeyEnv.trim() ?? "";
    return (
      <div className="provider-editor provider-editor--builtin provider-editor--key-only">
        {initial && onSaveKey && keyEnv && (
          <>
            <div className="provider-key-status provider-key-status--managed provider-key-status--compact">
              <span title={initial.keySourcePath || undefined}>
                {initial.keySet ? t("settings.configuredKey", { env: keyEnv }) : t("settings.notConfiguredKey", { env: keyEnv })}
                {initial.keySource ? ` · ${t("settings.keySource", { source: initial.keySource })}` : ""}
              </span>
              {initial.keySet && onClearKey && (
                <InlineConfirmButton
                  label={t("settings.clearKey")}
                  confirmLabel={t("settings.confirmClearKey")}
                  cancelLabel={t("common.cancel")}
                  disabled={busy}
                  danger
                  onConfirm={() => onClearKey(keyEnv)}
                />
              )}
            </div>
            <KeyField
              apiKeyEnv={keyEnv}
              busy={busy}
              keySet={initial.keySet}
              onSet={(env, value) => onSaveKey(env, value)}
            />
          </>
        )}
      </div>
    );
  }

  const modelNames = useMemo(
    () => models.split(",").map((m) => m.trim()).filter(Boolean),
    [models],
  );
  const canFetch = Boolean(name.trim() && baseUrl.trim());

  const protocolField = initial ? (
    <select className="mem-select" value={kind} onChange={(e) => setKind(e.target.value)}>
      {kindOptions.map((k) => (
        <option key={k} value={k}>
          {k === "openai" ? t("settings.providerProtocolOpenAI") : k}
        </option>
      ))}
    </select>
  ) : (
    <div className="provider-readonly-field provider-readonly-field--stacked" aria-readonly="true">
      <strong>{t("settings.providerProtocolOpenAI")}</strong>
      <span>{t("settings.providerProtocolOpenAIHint")}</span>
    </div>
  );

  const advancedFields = (
    <details className="provider-editor-advanced" open={advancedOpen} onToggle={(e) => setAdvancedOpen(e.currentTarget.open)}>
      <summary>{t("settings.providerAdvancedSettings")}</summary>
      <div className="provider-editor-advanced__body">
        <label className="set-label">{t("settings.providerApiKeyEnv")}</label>
        <input
          className="mem-input"
          placeholder={apiKeyEnvFromProviderName(name)}
          value={apiKeyEnv}
          onChange={(e) => setApiKeyEnv(e.target.value)}
        />
        <div className="mem-hint">{t("settings.providerApiKeyEnvHint")}</div>
        <label className="set-label">{t("settings.providerBalanceUrl")}</label>
        <input className="mem-input" placeholder={t("settings.balanceUrlPlaceholder")} value={balanceUrl} onChange={(e) => setBalanceUrl(e.target.value)} />
        <div className="mem-hint">{t("settings.balanceUrlHint")}</div>
        <label className="set-label">{t("settings.providerContextWindow")}</label>
        <input className="mem-input" placeholder={t("settings.contextWindowPlaceholder")} value={ctx} onChange={(e) => setCtx(e.target.value)} inputMode="numeric" />
        <div className="mem-hint">{t("settings.contextWindowHint")}</div>
        <label className="set-label">{t("settings.visionModels")}</label>
        <input
          className="mem-input"
          placeholder={t("settings.providerModels")}
          value={visionModels}
          onChange={(e) => {
            setVisionModelsConfigured(true);
            setVisionModels(e.target.value);
          }}
        />
        <div className="mem-hint">{t("settings.visionModelsHint")}</div>
        <label className="set-label">{t("settings.reasoningProtocol")}</label>
        <select className="mem-select" value={reasoningProtocol} onChange={(e) => setReasoningProtocol(e.target.value)}>
          {REASONING_PROTOCOLS.map((protocol) => (
            <option key={protocol || "auto"} value={protocol}>
              {reasoningProtocolLabel(protocol, t)}
            </option>
          ))}
        </select>
        <div className="mem-hint">{t("settings.reasoningProtocolHint")}</div>
        <label className="set-label">{t("settings.supportedEfforts")}</label>
        {EFFORT_PRESETS.map((level) => (
          <label key={level} className="set-check">
            <input
              type="checkbox"
              checked={presetEfforts.includes(level)}
              onChange={() => togglePreset(level)}
            />
            {level}
          </label>
        ))}
        <div className="set-row">
          <input
            className="mem-input set-grow"
            placeholder={t("settings.supportedEffortsCustomPlaceholder")}
            value={customEffortDraft}
            onChange={(e) => setCustomEffortDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addCustomEffort();
              }
            }}
          />
          <button
            type="button"
            className="btn btn--small"
            disabled={
              !customEffortDraft.trim() || supportedEfforts.includes(customEffortDraft.trim().toLowerCase())
            }
            onClick={addCustomEffort}
          >
            {t("common.add")}
          </button>
        </div>
        {customEfforts.length > 0 && (
          <div className="set-rules__chips">
            {customEfforts.map((level) => (
              <span className="set-rule" key={level}>
                {level}
                <Tooltip label={t("common.delete")}>
                  <button
                    type="button"
                    className="set-rule__x"
                    disabled={busy}
                    onClick={() => removeCustomEffort(level)}
                  >
                    ×
                  </button>
                </Tooltip>
              </span>
            ))}
          </div>
        )}
        <div className="mem-hint">{t("settings.supportedEffortsHint")}</div>
        <label className="set-label">{t("settings.defaultEffort")}</label>
        {supportedEfforts.length > 0 ? (
          <select
            className="mem-select"
            value={defaultEffort}
            onChange={(e) => setDefaultEffort(e.target.value)}
          >
            <option value="">{t("settings.defaultEffortAuto")}</option>
            {supportedEfforts.map((level) => (
              <option key={level} value={level}>
                {level}
              </option>
            ))}
          </select>
        ) : (
          <select className="mem-select" value="" disabled>
            <option value="">{t("settings.defaultEffortAuto")}</option>
          </select>
        )}
        <div className="mem-hint">{t("settings.defaultEffortHint")}</div>
      </div>
    </details>
  );

  return (
    <div className={`provider-editor${isNewCustomProvider ? " provider-editor--wizard" : ""}`}>
      <label className="set-label">{t("settings.customProviderName")}</label>
      <input className="mem-input" placeholder={t("settings.customProviderNamePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} disabled={!!initial} />
      <label className="set-label">{t("settings.providerProtocol")}</label>
      {protocolField}
      <label className="set-label">{t("settings.providerBaseUrlLabel")}</label>
      <input className="mem-input" placeholder={t("settings.providerBaseUrl")} value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
      {!initial && (
        <>
          <label className="set-label">{t("settings.providerKey")}</label>
          <input
            className="mem-input"
            type="password"
            placeholder={t("settings.providerKeyPlaceholder")}
            value={keyDraft}
            onChange={(e) => setKeyDraft(e.target.value)}
          />
        </>
      )}
      {initial && onSaveKey && apiKeyEnv.trim() && (
        <>
          <label className="set-label">{t("settings.providerKey")}</label>
          {initial.keySource && (
            <div className="mem-hint" title={initial.keySourcePath || undefined}>
              {t("settings.keySource", { source: initial.keySource })}
            </div>
          )}
          <KeyField
            apiKeyEnv={apiKeyEnv.trim()}
            busy={busy || fetchingModels}
            keySet={initial.keySet}
            onSet={(env, value) => onSaveKey(env, value)}
          />
        </>
      )}
      <div className="provider-model-fetch-row">
        <button
          type="button"
          className="btn btn--small"
          disabled={busy || fetchingModels || !canFetch}
          onClick={() => void fetchModels()}
        >
          {fetchingModels ? t("settings.fetchingModels") : t("settings.testFetchModels")}
        </button>
        <span>{t("settings.testFetchModelsHint")}</span>
      </div>
      {fetchStatus && <div className="provider-fetch-status provider-fetch-status--ok">{fetchStatus}</div>}
      {fetchErr && <div className="provider-fetch-status provider-fetch-status--error">{fetchErr}</div>}
      {modelNames.length > 0 && (
        <div className="provider-card-block">
          <div className="provider-card-block__label">{t("settings.availableModels")}</div>
          <ModelChips modelNames={modelNames} />
        </div>
      )}
      <label className="set-label">{t("settings.manualModels")}</label>
      <input className="mem-input" placeholder={t("settings.providerModels")} value={models} onChange={(e) => setModels(e.target.value)} />
      <div className="mem-hint">{t("settings.manualModelsHint")}</div>
      {advancedFields}
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={() => void save()} disabled={busy || !name.trim() || !baseUrl.trim() || !models.trim()}>
          {t("common.save")}
        </button>
      </div>
    </div>
  );
}

function KeyField({
  apiKeyEnv,
  busy,
  keySet = false,
  onSet,
}: {
  apiKeyEnv: string;
  busy: boolean;
  keySet?: boolean;
  onSet: (apiKeyEnv: string, value: string) => Promise<void>;
}) {
  const t = useT();
  const [val, setVal] = useState("");
  if (!apiKeyEnv) return null;
  return (
    <div className="set-key">
      <input
        className="mem-input"
        type="password"
        placeholder={t(keySet ? "settings.updateKey" : "settings.setKey", { env: apiKeyEnv })}
        value={val}
        onChange={(e) => setVal(e.target.value)}
      />
      <button
        className="btn btn--small"
        disabled={busy || !val.trim()}
        onClick={() => {
          void onSet(apiKeyEnv, val.trim());
          setVal("");
        }}
      >
        {t(keySet ? "settings.updateKeyAction" : "settings.saveKey")}
      </button>
    </div>
  );
}

function PermissionsSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  return (
    <>
    <SettingsSection title={t("settings.permissions")} description={t("settings.permissionsModeHint")}>
      <SettingsField label={t("settings.writerMode")}>
        <select
          className="mem-select set-grow"
          value={s.permissions.mode}
          disabled={busy}
          onChange={(e) => void apply(() => app.SetPermissionMode(e.target.value))}
        >
          <option value="ask">{t("settings.modeAsk")}</option>
          <option value="allow">{t("settings.modeAllow")}</option>
          <option value="deny">{t("settings.modeDeny")}</option>
        </select>
      </SettingsField>
    </SettingsSection>
    <SettingsSection title={t("settings.permissionRules")} description={t("settings.ruleForm")}>
      <div className="set-rules-grid">
        {(["deny", "ask", "allow"] as const).map((list) => (
          <RuleList
            key={list}
            list={list}
            rules={s.permissions[list]}
            busy={busy}
            onAdd={(rule) => apply(() => app.AddPermissionRule(list, rule))}
            onRemove={(rule) => apply(() => app.RemovePermissionRule(list, rule))}
          />
        ))}
      </div>
    </SettingsSection>
    </>
  );
}

function RuleList({
  list,
  rules,
  busy,
  onAdd,
  onRemove,
}: {
  list: string;
  rules: string[];
  busy: boolean;
  onAdd: (rule: string) => Promise<void>;
  onRemove: (rule: string) => Promise<void>;
}) {
  const t = useT();
  const [draft, setDraft] = useState("");
  const add = () => {
    const r = draft.trim();
    if (r) {
      void onAdd(r);
      setDraft("");
    }
  };
  return (
    <div className="set-rules">
      <div className="set-rules__head">
        <div className="set-rules__label">{ruleListLabel(list, t)}</div>
        {ruleListHint(list, t) && <div className="set-rules__hint">{ruleListHint(list, t)}</div>}
      </div>
      <div className="set-rules__chips">
        {rules.length === 0 && <span className="mem-empty">{t("common.none")}</span>}
        {rules.map((r) => (
          <span className="set-rule" key={r}>
            {r}
            <Tooltip label={t("common.delete")}>
              <button className="set-rule__x" disabled={busy} onClick={() => void onRemove(r)}>
                ✕
              </button>
            </Tooltip>
          </span>
        ))}
      </div>
      <div className="set-rules__add">
        <input
          className="mem-input"
          placeholder={t("settings.addRule", { list })}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") add();
          }}
        />
        <button className="btn btn--small" disabled={busy || !draft.trim()} onClick={add}>
          {t("common.add")}
        </button>
      </div>
    </div>
  );
}

function ruleListLabel(list: string, t: ReturnType<typeof useT>): string {
  switch (list) {
    case "deny":
      return t("settings.ruleDeny");
    case "ask":
      return t("settings.ruleAsk");
    case "allow":
      return t("settings.ruleAllow");
    case "allow_write":
      return t("settings.ruleAllowWrite");
    default:
      return list;
  }
}

function ruleListHint(list: string, t: ReturnType<typeof useT>): string {
  switch (list) {
    case "deny":
      return t("settings.ruleDenyHint");
    case "ask":
      return t("settings.ruleAskHint");
    case "allow":
      return t("settings.ruleAllowHint");
    default:
      return "";
  }
}

type HookScope = "global" | "project";

function HooksSection({ onChanged }: { onChanged: (settings?: SettingsView | null) => void }) {
  const t = useT();
  const [scope, setScope] = useState<HookScope>("global");
  const [view, setView] = useState<HooksSettingsView | null>(null);
  const [jsonText, setJsonText] = useState("");
  const [jsonMessage, setJsonMessage] = useState<string | null>(null);
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [pathMessage, setPathMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async (nextScope: HookScope) => {
    setBusy(true);
    setErr(null);
    try {
      const next = normalizeHooksSettingsView(await app.HooksSettings(nextScope), nextScope);
      setView(next);
      setJsonText(formatHooksJSON(next.hooks, next.events));
      setJsonMessage(null);
      setJsonError(null);
      setPathMessage(null);
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
      setView(null);
      setJsonText("");
      setJsonMessage(null);
      setJsonError(null);
      setPathMessage(null);
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load(scope);
  }, [load, scope]);

  const parseHooksEditorJSON = (raw = jsonText): { hooks: HookConfigView[]; text: string } | null => {
    try {
      const hooks = parseHooksJSON(raw, view?.events ?? []);
      const text = formatHooksJSON(hooks, view?.events ?? []);
      setJsonText(text);
      setJsonError(null);
      return { hooks, text };
    } catch (e) {
      setJsonError(t("settings.hooksJsonInvalid", { error: String((e as Error)?.message ?? e) }));
      setJsonMessage(null);
      return null;
    }
  };
  const copyHooksJSON = async () => {
    const parsed = parseHooksEditorJSON();
    if (!parsed) return;
    try {
      await navigator.clipboard?.writeText(parsed.text);
      setJsonMessage(t("settings.hooksJsonCopied"));
    } catch {
      setJsonMessage(t("settings.hooksJsonClipboardUnavailable"));
    }
  };
  const formatHooksEditorJSON = (raw = jsonText) => {
    const parsed = parseHooksEditorJSON(raw);
    if (parsed) setJsonMessage(t("settings.hooksJsonFormatted"));
  };
  const pasteHooksJSON = async () => {
    try {
      const raw = await navigator.clipboard?.readText();
      if (!raw) throw new Error(t("settings.hooksJsonClipboardEmpty"));
      setJsonText(raw);
      formatHooksEditorJSON(raw);
    } catch (e) {
      setJsonError(t("settings.hooksJsonPasteFailed", { error: String((e as Error)?.message ?? e) }));
      setJsonMessage(null);
    }
  };
  const copyHooksPath = async () => {
    const path = view?.path?.trim();
    if (!path) {
      setPathMessage(t("settings.hooksPathUnavailable"));
      return;
    }
    try {
      await navigator.clipboard?.writeText(path);
      setPathMessage(t("settings.hooksPathCopied"));
    } catch {
      setPathMessage(t("settings.hooksJsonClipboardUnavailable"));
    }
  };
  const save = async () => {
    setBusy(true);
    setErr(null);
    try {
      const parsed = parseHooksEditorJSON();
      if (!parsed) return;
      await app.SaveHooksSettingsForRoot(scope, view?.projectRoot?.trim() ?? "", parsed.hooks);
      await load(scope);
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };
  const trustProject = async () => {
    const projectRoot = view?.projectRoot?.trim() ?? "";
    if (!projectRoot) {
      setErr(t("settings.hooksProjectRootUnavailable"));
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      await app.TrustProjectHooksForRoot(projectRoot);
      await load("project");
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      {err && <div className="banner banner--error">{err}</div>}
      <SettingsSection title={t("settings.hooksScopeSection")} description={t("settings.hooksScopeHint")}>
        <SettingsField label={t("settings.hooksScopeField")}>
          <select className="mem-select set-grow" value={scope} disabled={busy} onChange={(e) => setScope(e.target.value === "project" ? "project" : "global")}>
            <option value="global">{t("settings.hooksGlobal")}</option>
            <option value="project">{t("settings.hooksProject")}</option>
          </select>
        </SettingsField>
        <SettingsField label={t("settings.hooksPath")} hint={scope === "project" ? t("settings.hooksPathProjectHint") : t("settings.hooksPathGlobalHint")}>
          <div className="hooks-path-stack">
            <div className={`hooks-path-display${view?.path ? "" : " hooks-path-display--empty"}`}>
              <code className="hooks-path-display__value" title={view?.path || t("settings.hooksPathUnavailable")}>
                {view?.path || t("settings.hooksPathUnavailable")}
              </code>
              <button className="btn btn--small" disabled={busy || !view?.path} onClick={() => void copyHooksPath()}>{t("settings.hooksPathCopy")}</button>
            </div>
            {pathMessage && <div className="hooks-path-display__message">{pathMessage}</div>}
          </div>
        </SettingsField>
        {scope === "project" && (
          <SettingsField label={t("settings.hooksTrust")} hint={t("settings.hooksTrustHint")}>
            <div className="hooks-trust-stack">
              <div className="hooks-trust-row">
                <span className={`set-rule${view?.trusted ? "" : " set-rule--warn"}`}>{view?.trusted ? t("settings.hooksTrusted") : t("settings.hooksUntrusted")}</span>
                <button className="btn btn--small" disabled={busy || view?.trusted || !view?.projectRoot} onClick={() => void trustProject()}>{t("settings.hooksTrustProject")}</button>
              </div>
              <code className={`hooks-trust-root${view?.projectRoot ? "" : " hooks-trust-root--empty"}`} title={view?.projectRoot || t("settings.hooksProjectRootUnavailable")}>
                {view?.projectRoot || t("settings.hooksProjectRootUnavailable")}
              </code>
            </div>
          </SettingsField>
        )}
      </SettingsSection>

      <SettingsSection
        title={t("settings.hooks")}
        description={scope === "project" ? t("settings.hooksProjectHint") : t("settings.hooksGlobalHint")}
        actions={(
          <button className="btn btn--small btn--primary" disabled={busy} onClick={() => void save()}>{t("common.save")}</button>
        )}
      >
        {view && (
          <div className="hooks-json-panel">
            <div className="hooks-json-panel__head">
              <div>
                <div className="set-rules__label">{t("settings.hooksJsonTitle")}</div>
                <div className="set-rules__hint">{t("settings.hooksJsonHint")}</div>
              </div>
              <div className="hooks-json-panel__actions">
                <button className="btn btn--small" disabled={busy} onClick={() => void copyHooksJSON()}>{t("settings.hooksJsonCopy")}</button>
                <button className="btn btn--small" disabled={busy} onClick={() => void pasteHooksJSON()}>{t("settings.hooksJsonPaste")}</button>
                <button className="btn btn--small" disabled={busy || !jsonText.trim()} onClick={() => formatHooksEditorJSON()}>{t("settings.hooksJsonApply")}</button>
              </div>
            </div>
            <textarea
              className="mem-textarea hooks-json-panel__textarea"
              value={jsonText}
              disabled={busy}
              spellCheck={false}
              onChange={(e) => {
                setJsonText(e.target.value);
                setJsonMessage(null);
                setJsonError(null);
              }}
            />
            {jsonError && <div className="hooks-json-panel__message hooks-json-panel__message--error">{jsonError}</div>}
            {jsonMessage && <div className="hooks-json-panel__message">{jsonMessage}</div>}
          </div>
        )}
        {!view && <div className="empty">{t("settings.loading")}</div>}
      </SettingsSection>
    </>
  );
}

function normalizeHooksSettingsView(view: HooksSettingsView, scope: HookScope): HooksSettingsView {
  const events = asArray(view?.events).filter(Boolean);
  return {
    scope: view?.scope === "project" ? "project" : scope,
    path: view?.path ?? "",
    projectRoot: view?.projectRoot ?? "",
    trusted: !!view?.trusted,
    events,
    hooks: asArray(view?.hooks).map(normalizeHookConfig).filter((h) => h.event),
  };
}

function formatHooksJSON(hooks: HookConfigView[], eventOrder: string[]): string {
  const grouped: Record<string, Array<Record<string, string | number>>> = {};
  const events = new Set(eventOrder);
  for (const hook of hooks.map(normalizeHookConfig).filter((h) => h.event)) {
    events.add(hook.event);
    const entry: Record<string, string | number> = { command: hook.command };
    if (hook.match) entry.match = hook.match;
    if (hook.description) entry.description = hook.description;
    if ((hook.timeout ?? 0) > 0) entry.timeout = hook.timeout ?? 0;
    if (hook.cwd) entry.cwd = hook.cwd;
    (grouped[hook.event] ||= []).push(entry);
  }
  const ordered: typeof grouped = {};
  for (const event of [...eventOrder, ...Array.from(events).sort()]) {
    if (grouped[event]?.length && !ordered[event]) ordered[event] = grouped[event];
  }
  return JSON.stringify({ hooks: ordered }, null, 2);
}

function parseHooksJSON(raw: string, validEvents: string[]): HookConfigView[] {
  const trimmed = raw.trim();
  if (!trimmed) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (e) {
    throw new Error(String((e as Error)?.message ?? e));
  }
  if (Array.isArray(parsed)) {
    return parsed.map((item) => normalizeHookConfig(parseHookArrayItem(item, validEvents))).filter((h) => h.event);
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error("expected an object or array");
  }
  const obj = parsed as Record<string, unknown>;
  const hooksValue = obj.hooks && typeof obj.hooks === "object" && !Array.isArray(obj.hooks) ? obj.hooks : obj;
  return flattenHooksMap(hooksValue as Record<string, unknown>, validEvents);
}

function parseHookArrayItem(item: unknown, validEvents: string[]): HookConfigView {
  if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error("hook item must be an object");
  const obj = item as Record<string, unknown>;
  const event = stringField(obj, "event") || "PreToolUse";
  if (validEvents.length > 0 && !validEvents.includes(event)) throw new Error(`unknown hook event ${event}`);
  return {
    event,
    match: stringField(obj, "match"),
    command: stringField(obj, "command"),
    description: stringField(obj, "description"),
    timeout: numberField(obj, "timeout"),
    cwd: stringField(obj, "cwd"),
  };
}

function flattenHooksMap(hooks: Record<string, unknown>, validEvents: string[]): HookConfigView[] {
  const valid = new Set(validEvents);
  const out: HookConfigView[] = [];
  for (const [event, value] of Object.entries(hooks)) {
    if (valid.size > 0 && !valid.has(event)) throw new Error(`unknown hook event ${event}`);
    const items = Array.isArray(value) ? value : [value];
    for (const item of items) {
      if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error(`hook ${event} item must be an object`);
      const obj = item as Record<string, unknown>;
      out.push(normalizeHookConfig({
        event,
        match: stringField(obj, "match"),
        command: stringField(obj, "command"),
        description: stringField(obj, "description"),
        timeout: numberField(obj, "timeout"),
        cwd: stringField(obj, "cwd"),
      }));
    }
  }
  return out.filter((h) => h.event);
}

function stringField(obj: Record<string, unknown>, key: string): string {
  const value = obj[key];
  return typeof value === "string" ? value : "";
}

function numberField(obj: Record<string, unknown>, key: string): number {
  const value = obj[key];
  return typeof value === "number" && Number.isFinite(value) ? Math.floor(value) : 0;
}

function normalizeHookConfig(h: HookConfigView): HookConfigView {
  return {
    event: h.event || "PreToolUse",
    match: h.match ?? "",
    command: h.command ?? "",
    description: h.description ?? "",
    timeout: h.timeout && h.timeout > 0 ? Math.floor(h.timeout) : 0,
    cwd: h.cwd ?? "",
  };
}

function SandboxSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const sb = s.sandbox;
  const [root, setRoot] = useState(sb.workspaceRoot);
  const set = (next: Partial<typeof sb>) =>
    apply(() => app.SetSandbox(next.bash ?? sb.bash, next.network ?? sb.network, next.workspaceRoot ?? sb.workspaceRoot, next.allowWrite ?? sb.allowWrite, next.shell ?? sb.shell));

  return (
    <SettingsSection title={t("settings.sandboxTitle")}>
      <SettingsField label={t("settings.shellInterpreter")}>
        <select className="mem-select set-grow" value={sb.shell || "auto"} disabled={busy} onChange={(e) => void set({ shell: e.target.value })}>
          <option value="auto">{t("settings.shellAuto")}</option>
          <option value="bash">{t("settings.shellBash")}</option>
          <option value="powershell">{t("settings.shellPowershell")}</option>
          <option value="pwsh">{t("settings.shellPwsh")}</option>
        </select>
      </SettingsField>
      <SettingsField label={t("settings.bashSandbox")}>
        <select className="mem-select set-grow" value={sb.bash} disabled={busy} onChange={(e) => void set({ bash: e.target.value })}>
          <option value="enforce">{t("settings.bashEnforce")}</option>
          <option value="off">{t("settings.bashOff")}</option>
        </select>
      </SettingsField>
      <SettingsField label={t("settings.allowNetwork")}>
        <label className="set-check set-check--inline">
          <input type="checkbox" checked={sb.network} disabled={busy} onChange={(e) => void set({ network: e.target.checked })} />
          {t("settings.allowNetwork")}
        </label>
      </SettingsField>
      <SettingsField label={t("settings.workspaceRoot")}>
        <input
          className="mem-input set-grow"
          placeholder={t("settings.workspaceDefault")}
          value={root}
          disabled={busy}
          onChange={(e) => setRoot(e.target.value)}
          onBlur={() => root !== sb.workspaceRoot && void set({ workspaceRoot: root })}
        />
      </SettingsField>
      <RuleList
        list="allow_write"
        rules={sb.allowWrite}
        busy={busy}
        onAdd={(d) => set({ allowWrite: [...sb.allowWrite, d] })}
        onRemove={(d) => set({ allowWrite: sb.allowWrite.filter((x) => x !== d) })}
      />
    </SettingsSection>
  );
}

// Visual-style metadata for the appearance theme cards. The two surface
// swatches + accent are read from CSS variables at render time so they always
// reflect the live token values for the currently-resolved light/dark mode.
const THEME_STYLE_META: Record<ThemeStyle, { name: string; zh: DictKey; note: DictKey; desc: DictKey }> = {
  graphite: { name: "Graphite", zh: "settings.style.graphite.zh", note: "settings.style.graphite.note", desc: "settings.style.graphite.desc" },
  aurora: { name: "Aurora", zh: "settings.style.aurora.zh", note: "settings.style.aurora.note", desc: "settings.style.aurora.desc" },
  slate: { name: "Slate", zh: "settings.style.slate.zh", note: "settings.style.slate.note", desc: "settings.style.slate.desc" },
  carbon: { name: "Carbon", zh: "settings.style.carbon.zh", note: "settings.style.carbon.note", desc: "settings.style.carbon.desc" },
  nocturne: { name: "Nocturne", zh: "settings.style.nocturne.zh", note: "settings.style.nocturne.note", desc: "settings.style.nocturne.desc" },
  amber: { name: "Amber", zh: "settings.style.amber.zh", note: "settings.style.amber.note", desc: "settings.style.amber.desc" },
};

function AppearanceSection({
  theme,
  themeStyle,
  textSize,
  fontFamily,
  monoFontFamily,
  customFontName,
  customMonoFontName,
  onTheme,
  onThemeStyle,
  onTextSize,
  onFontFamily,
  onMonoFontFamily,
  onCustomFontNameChange,
  onCustomMonoFontNameChange,
}: {
  theme: Theme;
  themeStyle: ThemeStyle;
  textSize: TextSize;
  fontFamily: FontFamily;
  monoFontFamily: MonoFontFamily;
  customFontName: string;
  customMonoFontName: string;
  onTheme: (t: Theme) => void;
  onThemeStyle: (style: ThemeStyle) => void;
  onTextSize: (size: TextSize) => void;
  onFontFamily: (font: FontFamily) => void;
  onMonoFontFamily: (font: MonoFontFamily) => void;
  onCustomFontNameChange: (name: string) => void;
  onCustomMonoFontNameChange: (name: string) => void;
}) {
  const t = useT();
  const themeOptions: Theme[] = ["auto", "light", "dark"];
  const availableFontFamilies = useMemo(() => getAvailableFontFamilies(fontFamily), [fontFamily]);
  const availableMonoFontFamilies = useMemo(() => getAvailableMonoFontFamilies(monoFontFamily), [monoFontFamily]);
  return (
    <SettingsSection title={t("settings.appearance")}>
      <SettingsField label={t("settings.theme")}>
        <div className="set-seg">
          {themeOptions.map((opt) => (
            <button
              key={opt}
              className={`set-seg__btn${theme === opt ? " set-seg__btn--on" : ""}`}
              onClick={() => onTheme(opt)}
            >
              {themeName(opt, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.themeStyle")} stacked>
        <div className="theme-card-grid">
          {THEME_STYLES.map((opt) => {
            const meta = THEME_STYLE_META[opt];
            const selected = themeStyle === opt;
            return (
              <button
                key={opt}
                type="button"
                role="radio"
                aria-checked={selected}
                className={`theme-card${selected ? " theme-card--on" : ""}`}
                onClick={() => onThemeStyle(opt)}
              >
                <span className="theme-card__head">
                  <span className="theme-card__name">
                    {meta.name} <span className="theme-card__zh">{t(meta.zh)}</span>
                  </span>
                  <span className="theme-card__tag">{t(meta.note)}</span>
                </span>
                <span className="theme-card__swatches" data-theme-style-card={opt}>
                  <span className="theme-card__swatch theme-card__swatch--bg" />
                  <span className="theme-card__swatch theme-card__swatch--surface" />
                  <span className="theme-card__swatch theme-card__swatch--accent" />
                </span>
                <span className="theme-card__desc">{t(meta.desc)}</span>
                <span className="theme-card__check" aria-hidden="true">
                  <Check size={13} strokeWidth={3} />
                </span>
              </button>
            );
          })}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.textSize")}>
        <div className="set-seg">
          {TEXT_SIZES.map((size) => (
            <button
              key={size}
              className={`set-seg__btn${textSize === size ? " set-seg__btn--on" : ""}`}
              onClick={() => onTextSize(size)}
            >
              {textSizeName(size, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.fontFamily")}>
        <div className="set-seg">
          {availableFontFamilies.map((font) => (
            <button
              key={font}
              className={`set-seg__btn${fontFamily === font ? " set-seg__btn--on" : ""}`}
              onClick={() => onFontFamily(font)}
            >
              {fontFamilyName(font, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      {fontFamily === "custom" && (
        <SettingsField label={t("settings.fontFamilyCustomName")}>
          <textarea
            className="mem-input"
            style={{ width: "100%", resize: "vertical" }}
            rows={2}
            placeholder={t("settings.fontFamilyCustomPlaceholder")}
            value={customFontName}
            onChange={(e) => onCustomFontNameChange(e.target.value)}
          />
        </SettingsField>
      )}
      <SettingsField label={t("settings.monoFontFamily")}>
        <div className="set-seg">
          {availableMonoFontFamilies.map((font) => (
            <button
              key={font}
              className={`set-seg__btn${monoFontFamily === font ? " set-seg__btn--on" : ""}`}
              onClick={() => onMonoFontFamily(font)}
            >
              {monoFontFamilyName(font, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      {monoFontFamily === "custom" && (
        <SettingsField label={t("settings.monoFontFamilyCustomName")}>
          <textarea
            className="mem-input"
            style={{ width: "100%", resize: "vertical" }}
            rows={2}
            placeholder={t("settings.monoFontFamilyCustomPlaceholder")}
            value={customMonoFontName}
            onChange={(e) => onCustomMonoFontNameChange(e.target.value)}
          />
        </SettingsField>
      )}
    </SettingsSection>
  );
}

function themeName(theme: Theme, t: ReturnType<typeof useT>): string {
  switch (theme) {
    case "auto":
      return t("settings.themeAuto");
    case "light":
      return t("settings.themeLight");
    case "dark":
      return t("settings.themeDark");
  }
}

function textSizeName(size: TextSize, t: ReturnType<typeof useT>): string {
  switch (size) {
    case "small":
      return t("settings.textSizeSmall");
    case "default":
      return t("settings.textSizeDefault");
    case "large":
      return t("settings.textSizeLarge");
    case "xlarge":
      return t("settings.textSizeXLarge");
    case "xxlarge":
      return t("settings.textSizeXXLarge");
  }
}

function fontFamilyName(font: FontFamily, t: ReturnType<typeof useT>): string {
  switch (font) {
    case "system":
      return t("settings.fontFamilySystem");
    case "yahei":
      return t("settings.fontFamilyYaHei");
    case "pingfang":
      return t("settings.fontFamilyPingFang");
    case "noto":
      return t("settings.fontFamilyNoto");
    case "custom":
      return t("settings.fontFamilyCustom");
  }
}

function monoFontFamilyName(font: MonoFontFamily, t: ReturnType<typeof useT>): string {
  switch (font) {
    case "system":
      return t("settings.monoFontFamilySystem");
    case "cascadia":
      return t("settings.monoFontFamilyCascadia");
    case "jetbrains":
      return t("settings.monoFontFamilyJetBrains");
    case "sfmono":
      return t("settings.monoFontFamilySFMono");
    case "custom":
      return t("settings.monoFontFamilyCustom");
  }
}

const MB = 1024 * 1024;
const mb = (n: number) => (n / MB).toFixed(1);

// UpdatesSection is the manual side of the auto-updater: it shows the startup
// check preference, running version, and a Check button, then the same state
// machine the top banner uses (useUpdater) — available → download → install, with
// progress and errors inline.
function UpdatesSection({
  configPath,
  checkUpdates,
  telemetry,
  metrics,
  settingsBusy,
  applySettings,
}: {
  configPath: string;
  checkUpdates: boolean;
  telemetry: boolean;
  metrics: boolean;
  settingsBusy: boolean;
  applySettings: (fn: () => Promise<void>) => Promise<void>;
}) {
  const t = useT();
  const { status, check, download: downloadUpdate, install: installUpdate } = useUpdater();
  const [version, setVersion] = useState("");
  useEffect(() => {
    app.Version().then(setVersion).catch(() => {});
  }, []);

  const updaterBusy =
    status.kind === "checking" || status.kind === "downloading" || status.kind === "verifying" || status.kind === "installing";

  return (
    <SettingsSection title={t("updater.title")}>
      <SettingsField
        className="settings-field--wide-copy"
        label={t("updater.autoCheckLabel")}
        hint={t("updater.autoCheckHint")}
      >
        <ToggleSegment
          value={checkUpdates}
          disabled={settingsBusy}
          onChange={(enabled) => void applySettings(() => app.SetDesktopCheckUpdates(enabled))}
        />
      </SettingsField>
      <SettingsField
        className="settings-field--wide-copy"
        label={t("settings.telemetryLabel")}
        hint={t("settings.telemetryHint")}
      >
        <ToggleSegment
          value={telemetry}
          disabled={settingsBusy}
          onChange={(enabled) => void applySettings(() => app.SetDesktopTelemetry(enabled))}
        />
      </SettingsField>
      <SettingsField
        className="settings-field--wide-copy"
        label={t("settings.metricsLabel")}
        hint={t("settings.metricsHint")}
      >
        <ToggleSegment
          value={metrics}
          disabled={settingsBusy}
          onChange={(enabled) => void applySettings(() => app.SetDesktopMetrics(enabled))}
        />
      </SettingsField>
      <SettingsField label={t("updater.currentVersion", { v: version || "…" })}>
        <button className="btn btn--small" disabled={updaterBusy} onClick={() => void check()}>
          {status.kind === "checking" ? t("updater.checking") : t("updater.checkButton")}
        </button>
      </SettingsField>
      {status.kind === "available" && (
        <div className="mem-hint">{t("updater.channelLabel", { channel: status.info.channel || "stable" })}</div>
      )}
      {status.kind === "upToDate" && <div className="mem-hint">{t("updater.upToDate")}</div>}
      {status.kind === "available" && (
        <>
          <SettingsField label={t("updater.available", { v: status.info.latest })}>
            <button className="btn btn--primary btn--small" onClick={() => downloadUpdate(status.info)}>
              {status.info.canSelfUpdate ? t("updater.downloadUpdate") : t("updater.goToDownload")}
            </button>
          </SettingsField>
          {!status.info.canSelfUpdate && <div className="mem-hint">{status.info.manualReason || t("updater.macHint")}</div>}
        </>
      )}
      {status.kind === "downloading" && (
        <div className="mem-hint">
          {t("updater.downloading", {
            done: mb(status.received),
            total: mb(status.total),
            pct: status.total > 0 ? Math.round((status.received / status.total) * 100) : 0,
          })}
        </div>
      )}
      {status.kind === "verifying" && <div className="mem-hint">{t("updater.verifying")}</div>}
      {status.kind === "downloaded" && (
        <SettingsField label={t("updater.downloaded", { v: status.info.latest })}>
          <button className="btn btn--primary btn--small" onClick={installUpdate}>
            {t("updater.restartInstall")}
          </button>
        </SettingsField>
      )}
      {status.kind === "installing" && <div className="mem-hint">{t("updater.installing")}</div>}
      {status.kind === "done" && <div className="mem-hint">{t("updater.done")}</div>}
      {status.kind === "error" && <div className="banner banner--error">{t("updater.failed", { msg: status.message })}</div>}
      {configPath && (
        <Tooltip label={configPath} fill block className="mem-hint settings-config-path">
          {t("settings.config", { path: configPath })}
        </Tooltip>
      )}
    </SettingsSection>
  );
}
