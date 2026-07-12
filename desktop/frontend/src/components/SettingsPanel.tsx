import { lazy, memo, Suspense, useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent, type ReactNode } from "react";
import { Bot as BotIcon, Check, CheckCircle2, ChevronDown, ChevronUp, Clipboard, GripVertical, KeyRound, Loader2, MessageCircle, Minus, Play, Plus, QrCode, RefreshCw, RotateCcw, Send } from "lucide-react";
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
import { DEFAULT_ZOOM, MAX_ZOOM, MIN_ZOOM, ZOOM_STEP, snapZoom, zoomToPercent, saveRestartZoom, getRestartZoom, type ZoomLevel } from "../lib/dpiScale";
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
import type { BotAccessView, BotAllowlistView, BotConnectionDiagnostic, BotConnectionView, BotInstallStartResult, BotRouteView, BotSettingsView, HookConfigView, HooksSettingsView, NetworkView, ProviderPresetView, ProviderView, SettingsTab, SettingsView } from "../lib/types";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";
import { getGenerativePreset, setGenerativePreset, generativeMusic, type GenerativePreset } from "../lib/generative-music";
import { SoundSelect } from "./SoundSelect";
import { getSuccessPreference, setSuccessPreference, getAttentionPreference, setAttentionPreference, playSuccessChime, playAttentionChime, type SoundWavPref } from "../lib/sound";
import { ModalCloseButton } from "./ModalCloseButton";
import { ShortcutComboDisplay } from "./ShortcutComboDisplay";

const SETTINGS_TABS: SettingsTab[] = ["general", "models", "bots", "mcp", "skills", "subagents", "plugins", "memory", "hooks", "diagnostics", "shortcuts", "permissions", "sandbox", "network", "appearance", "updates"];
export type SettingsInitialFocus = { target: "bot-allowlist"; connectionId?: string };
type DesktopPlatform = "darwin" | "windows" | "linux";

const MCPServersSettingsPage = lazy(() => import("./CapabilitiesPanel").then((module) => ({ default: module.MCPServersSettingsPage })));
const SkillsSettingsPage = lazy(() => import("./CapabilitiesPanel").then((module) => ({ default: module.SkillsSettingsPage })));
const PluginsSettingsPage = lazy(() => import("./CapabilitiesPanel").then((module) => ({ default: module.PluginsSettingsPage })));
const MemorySettingsPage = lazy(() => import("./MemoryPanel").then((module) => ({ default: module.MemorySettingsPage })));
const SubagentsSettingsPage = lazy(() => import("./SubagentsPanel").then((module) => ({ default: module.SubagentsSettingsPage })));
const DiagnosticsSettingsPage = lazy(() => import("./DiagnosticsSettingsPage").then((module) => ({ default: module.DiagnosticsSettingsPage })));
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
  desktopPlatform,
  onUseSubagent,
}: {
  onClose: () => void;
  onChanged: (settings?: SettingsView | null) => void;
  initialTab?: SettingsTab;
  initialFocus?: SettingsInitialFocus;
  agentRunning?: boolean;
  desktopPlatform: DesktopPlatform;
  onUseSubagent: (command: string) => void;
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
  const [zoomPct, setZoomPct] = useState<number>(zoomToPercent(getRestartZoom()));
  const [fontFamily, setFontFamilyState] = useState<FontFamily>(getFontFamily());
  const [monoFontFamily, setMonoFontFamilyState] = useState<MonoFontFamily>(getMonoFontFamily());
  const [customFontName, setCustomFontNameState] = useState<string>(getCustomFontName());
  const [customMonoFontName, setCustomMonoFontNameState] = useState<string>(getCustomMonoFontName());
  const [tab, setTab] = useState<SettingsTab>(initialTab === "providers" ? "models" : initialTab ?? "general");
  const pendingSubagentCommandRef = useRef<string | null>(null);
  // Play the modal exit animation, then let the parent unmount us and focus
  // the composer with the selected slash command.
  const { status, requestClose } = useDeferredClose(() => {
    const command = pendingSubagentCommandRef.current;
    pendingSubagentCommandRef.current = null;
    onClose();
    if (command) onUseSubagent(command);
  }, 240);
  const zoomSaveSeq = useRef(0);

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
  useEffect(() => {
    if (desktopPlatform !== "windows") return;
    let cancelled = false;
    void (async () => {
      try {
        const persisted = await app.GetDesktopZoomFactor();
        if (cancelled || typeof persisted !== "number" || !Number.isFinite(persisted)) return;
        const snapped = snapZoom(persisted);
        saveRestartZoom(snapped);
        setZoomPct(zoomToPercent(snapped));
      } catch {
        // Older mocks or startup races can lack the binding; keep the local fallback.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [desktopPlatform]);

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
      setErr(formatSettingsError(e, t));
    } finally {
      setBusy(false);
    }
  }, [reload, onChanged, t]);
  const backgroundApply = useCallback(async (fn: () => Promise<void>) => {
    setErr(null);
    setWarning(null);
    try {
      await fn();
      const next = await reload();
      onChanged(next);
    } catch (e) {
      setErr(formatSettingsError(e, t));
    }
  }, [reload, onChanged, t]);
  const setRestartZoom = useCallback(async (zoom: ZoomLevel) => {
    const snapped = snapZoom(zoom);
    const seq = ++zoomSaveSeq.current;
    setErr(null);
    setWarning(null);
    setZoomPct(zoomToPercent(snapped));
    try {
      await app.SetDesktopZoomFactor(snapped);
      if (seq === zoomSaveSeq.current) saveRestartZoom(snapped);
    } catch (e) {
      if (seq !== zoomSaveSeq.current) return;
      setErr(formatSettingsError(e, t));
      setZoomPct(zoomToPercent(getRestartZoom()));
    }
  }, [t]);

  // Close on Esc
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("[data-anchored-popover='active']")) requestClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [requestClose]);

  // The settings-reliant pages (general, models, network, permissions,
  // sandbox, appearance, updates) need SettingsView loaded. MCP, Skills, Plugins,
  // and Memory
  // load their own data and render regardless.
  const needsSettings = tab === "general" || tab === "models" || tab === "bots" || tab === "subagents" || tab === "network" || tab === "permissions" || tab === "sandbox" || tab === "appearance" || tab === "updates";
  const lazySettingsPageFallback = <div className="empty">{t("settings.loading")}</div>;

  return (
    <div className="management-modal-backdrop settings-modal-backdrop" data-state={status} onMouseDown={(e) => { if (e.target === e.currentTarget) requestClose(); }}>
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
                {tab === "subagents" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><Suspense fallback={lazySettingsPageFallback}><SubagentsSettingsPage s={s} onUseInChat={(command) => {
                  pendingSubagentCommandRef.current = command;
                  requestClose();
                }} /></Suspense></SettingsPageShell>}
                {tab === "plugins" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><Suspense fallback={lazySettingsPageFallback}><PluginsSettingsPage /></Suspense></SettingsPageShell>}
                {tab === "memory" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><Suspense fallback={lazySettingsPageFallback}><MemorySettingsPage /></Suspense></SettingsPageShell>}
                {tab === "hooks" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><HooksSection onChanged={onChanged} /></SettingsPageShell>}
                {tab === "diagnostics" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><Suspense fallback={lazySettingsPageFallback}><DiagnosticsSettingsPage onNavigate={setTab} /></Suspense></SettingsPageShell>}
                {tab === "shortcuts" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><ShortcutsSection /></SettingsPageShell>}
                {tab === "permissions" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><PermissionsSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "sandbox" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><SandboxSection s={s} busy={busy} apply={apply} windows={desktopPlatform === "windows"} /></SettingsPageShell>}
                {tab === "network" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><NetworkSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "appearance" && s && (
                  <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}>
                    <AppearanceSection
                      theme={theme}
                      themeStyle={themeStyle}
                      textSize={textSize}
                      showDisplayZoom={desktopPlatform === "windows"}
                      zoomPct={zoomPct}
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
                      onRestartZoom={setRestartZoom}
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
    case "subagents":
    case "plugins":
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
    case "plugins": return t("settings.tab.plugins");
    case "memory": return t("settings.tab.memory");
    case "diagnostics": return t("settings.tab.diagnostics");
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
    case "subagents":
      return t("settings.tab.subagents");
    case "plugins":
      return t("settings.tab.plugins");
    case "memory":
      return t("settings.tab.memory");
    case "hooks":
      return t("settings.tab.hooks");
    case "diagnostics":
      return t("settings.tab.diagnostics");
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
    case "subagents":
      return t("subagents.tabHint");
    case "plugins":
      return t("settings.tabSub.plugins");
    case "memory":
      return t("settings.tabSub.memory");
    case "hooks":
      return t("settings.tabSub.hooks");
    case "diagnostics":
      return t("settings.tabSub.diagnostics");
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
export function allRefs(s: SettingsView): string[] {
  const out: string[] = [];
  for (const p of s.providers) {
    if (!p.added || !providerIsConfigured(p)) continue;
    for (const m of p.models) out.push(`${p.name}/${m}`);
  }
  return out;
}

// toRef normalises a stored model id (a provider name, a bare model, or a ref) to
// a "provider/model" ref so a <select> of refs can show it selected.
export function toRef(model: string, s: SettingsView): string {
  if (!model) return "";
  if (model.includes("/")) return model;
  const byName = s.providers.find((p) => p.name === model);
  if (byName) return `${byName.name}/${byName.default || byName.models[0] || ""}`;
  const byModel = s.providers.find((p) => p.models.includes(model));
  if (byModel) return `${byModel.name}/${model}`;
  return model;
}

const PROXY_MODES = ["auto", "custom", "off"] as const;

// EFFORT_PRESETS is the canonical union of /effort levels the kernel recognises.
// The settings UI uses it for subagent defaults; provider-specific levels are
// inferred by the backend or edited in TOML for rare gateways.
export const EFFORT_PRESETS: readonly string[] = ["low", "medium", "high", "xhigh", "max"];
const REASONING_PROTOCOLS: readonly string[] = ["", "deepseek", "openai", "none"];
const THINKING_MODES: readonly string[] = ["", "enabled", "disabled", "adaptive"];
const PROXY_TYPES = ["http", "https", "socks5", "socks5h"] as const;
const LANGUAGE_PREFS: LangPref[] = ["", "zh", "en"];
const AUTO_PLAN_MODES = ["off", "on"] as const;
const TOOL_APPROVAL_MODES = ["ask", "auto", "yolo"] as const;
const BOT_TOOL_APPROVAL_MODES = ["", "ask", "auto", "yolo"] as const;
const BOT_QUEUE_MODES = ["steer", "followup", "collect", "interrupt"] as const;
const BOT_QUEUE_DROPS = ["summarize", "old", "new"] as const;
const BOT_ROUTE_CHAT_TYPES = ["", "dm", "group", "guild", "direct", "thread"] as const;

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

function normalizeThinkingMode(thinking: string | undefined): string {
  const v = String(thinking ?? "").trim().toLowerCase();
  return THINKING_MODES.includes(v) ? v : "";
}

export function providerEditorEffectiveKind(isNewCustomProvider: boolean, kind: string, kinds: string[]): string {
  void isNewCustomProvider;
  const selected = kind.trim();
  return selected || kinds[0] || "openai";
}

function trimmedURL(value: string): string {
  return value.trim().replace(/\/+$/, "");
}

export function providerChatURLPreview(baseUrl: string, chatUrl: string, fullURL: boolean): string {
  if (fullURL) return trimmedURL(chatUrl);
  const base = trimmedURL(baseUrl);
  return base ? `${base}/chat/completions` : "";
}

export function providerBaseURLFromChatURL(chatUrl: string): string {
  const full = trimmedURL(chatUrl);
  for (const suffix of ["/chat/completions", "/responses", "/response"]) {
    if (full.endsWith(suffix)) return trimmedURL(full.slice(0, -suffix.length));
  }
  return full;
}

function formatProviderHeaders(headers: Record<string, string> | null | undefined): string {
  const entries = Object.entries(headers ?? {})
    .map(([key, value]) => [key.trim(), String(value ?? "").trim()] as const)
    .filter(([key, value]) => key && value)
    .sort(([a], [b]) => a.localeCompare(b));
  return entries.map(([key, value]) => `${key}: ${value}`).join("\n");
}

function parseProviderHeaders(raw: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const colon = trimmed.indexOf(":");
    const eq = trimmed.indexOf("=");
    const cut = colon >= 0 && (eq < 0 || colon < eq) ? colon : eq;
    if (cut <= 0) continue;
    const key = trimmed.slice(0, cut).trim();
    const value = trimmed.slice(cut + 1).trim();
    if (key && value) out[key] = value;
  }
  return out;
}

function sortedJSONValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortedJSONValue);
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(value as Record<string, unknown>).sort((a, b) => a.localeCompare(b))) {
      out[key] = sortedJSONValue((value as Record<string, unknown>)[key]);
    }
    return out;
  }
  return value;
}

function formatSettingsError(error: unknown, t: ReturnType<typeof useT>): string {
  const msg = String((error as Error)?.message ?? error ?? "").trim();
  const unknownModel = /^unknown model (.+)$/i.exec(msg);
  if (unknownModel) return t("settings.errorUnknownModel", { model: unknownModel[1] });
  const providerNotAdded = /^model (.+) is not available because provider (.+) is not added$/i.exec(msg);
  if (providerNotAdded) return t("settings.errorModelProviderMissing", { model: providerNotAdded[1], provider: providerNotAdded[2] });
  const providerNoKey = /^model (.+) is not available because provider (.+) has no key$/i.exec(msg);
  if (providerNoKey) return t("settings.errorModelProviderNoKey", { model: providerNoKey[1], provider: providerNoKey[2] });
  const removeAccessBusy = /^finish or cancel active work using (.+) before removing the provider access$/i.exec(msg);
  if (removeAccessBusy) return t("settings.errorRemoveAccessBusy", { provider: removeAccessBusy[1] });
  const deleteProviderBusy = /^finish or cancel active work using (.+) before deleting the provider$/i.exec(msg);
  if (deleteProviderBusy) return t("settings.errorDeleteProviderBusy", { provider: deleteProviderBusy[1] });
  const saveBeforeRemoveAccess = /^save current session before removing provider access: (.+)$/is.exec(msg);
  if (saveBeforeRemoveAccess) return t("settings.errorSaveBeforeRemoveAccess", { err: saveBeforeRemoveAccess[1] });
  const saveBeforeDeleteProvider = /^save current session before deleting provider: (.+)$/is.exec(msg);
  if (saveBeforeDeleteProvider) return t("settings.errorSaveBeforeDeleteProvider", { err: saveBeforeDeleteProvider[1] });
  const removeProviderUsed = /^remove provider: (.+) is used by open tabs and no other configured provider exists$/i.exec(msg);
  if (removeProviderUsed) return t("settings.errorRemoveProviderNoFallback", { provider: removeProviderUsed[1] });
  return msg || t("settings.errorUnknown");
}

function validateProviderExtraBodyValue(value: unknown, path = "extra_body", t?: ReturnType<typeof useT>): void {
  if (value === null) {
    throw new Error(t ? t("settings.providerExtraBodyNull", { path }) : `${path} cannot contain null`);
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) => validateProviderExtraBodyValue(item, `${path}[${index}]`, t));
    return;
  }
  if (typeof value === "object") {
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      validateProviderExtraBodyValue(child, `${path}.${key}`, t);
    }
  }
}

export function formatProviderExtraBody(extraBody: Record<string, unknown> | null | undefined): string {
  const cleaned: Record<string, unknown> = {};
  for (const [rawKey, value] of Object.entries(extraBody ?? {})) {
    const key = rawKey.trim();
    if (!key || value === undefined) continue;
    cleaned[key] = value;
  }
  if (Object.keys(cleaned).length === 0) return "";
  return JSON.stringify(sortedJSONValue(cleaned), null, 2);
}

export function parseProviderExtraBody(raw: string, t?: ReturnType<typeof useT>): Record<string, unknown> {
  const trimmed = raw.trim();
  if (!trimmed) return {};
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(t ? t("settings.providerExtraBodyObjectRequired") : "extra body must be a JSON object");
  }
  validateProviderExtraBodyValue(parsed, "extra_body", t);
  const out: Record<string, unknown> = {};
  for (const [rawKey, value] of Object.entries(parsed as Record<string, unknown>)) {
    const key = rawKey.trim();
    if (key) out[key] = value;
  }
  return out;
}

export function providerExtraBodyParseError(error: unknown, t: ReturnType<typeof useT>): string {
  if (error instanceof SyntaxError) return t("settings.providerExtraBodyError");
  const message = String((error as Error)?.message ?? error ?? "").trim();
  return message || t("settings.providerExtraBodyError");
}

function providerModelFetchFallbackMessage(error: unknown, t: ReturnType<typeof useT>): string {
  const message = String((error as Error)?.message ?? error);
  if (/\bstatus\s+(401|403)\b/i.test(message)) {
    return t("settings.fetchModelsManualFallbackAuth");
  }
  if (/\bstatus\s+(404|405)\b/i.test(message)) {
    return t("settings.fetchModelsManualFallbackUnsupported");
  }
  if (/\b(status\s+5\d\d|request failed|network|timeout|timed out|connection|deadline|fetch failed)\b/i.test(message)) {
    return t("settings.fetchModelsManualFallbackNetwork");
  }
  if (/\b(decode response|invalid character|unexpected end|unexpected format)\b/i.test(message)) {
    return t("settings.fetchModelsManualFallbackDecode");
  }
  return t("settings.fetchModelsManualFallbackGeneric", { err: message });
}

function normalizeReasoningLanguage(lang: string | undefined): string {
  const v = String(lang ?? "").trim().toLowerCase();
  return v === "zh" || v === "en" ? v : "auto";
}

function normalizeBotQueueMode(mode: unknown): string {
  const raw = String(mode ?? "").trim().toLowerCase();
  return BOT_QUEUE_MODES.includes(raw as any) ? raw : "steer";
}

function normalizeBotQueueDrop(mode: unknown): string {
  const raw = String(mode ?? "").trim().toLowerCase();
  return BOT_QUEUE_DROPS.includes(raw as any) ? raw : "summarize";
}

function normalizeBotRouteChatType(value: unknown): string {
  const raw = String(value ?? "").trim().toLowerCase();
  return BOT_ROUTE_CHAT_TYPES.includes(raw as any) ? raw : "";
}

function normalizeBotRoute(raw: any): BotRouteView {
  return {
    connectionId: String(raw?.connectionId ?? "").trim(),
    platform: String(raw?.platform ?? "").trim().toLowerCase(),
    chatType: normalizeBotRouteChatType(raw?.chatType),
    chatId: String(raw?.chatId ?? "").trim(),
    userId: String(raw?.userId ?? "").trim(),
    threadId: String(raw?.threadId ?? "").trim(),
    model: String(raw?.model ?? "").trim(),
    toolApprovalMode: normalizeBotToolApprovalMode(raw?.toolApprovalMode),
    workspaceRoot: String(raw?.workspaceRoot ?? "").trim(),
  };
}

function emptyBotRoute(): BotRouteView {
  return {
    connectionId: "",
    platform: "",
    chatType: "",
    chatId: "",
    userId: "",
    threadId: "",
    model: "",
    toolApprovalMode: "",
    workspaceRoot: "",
  };
}

function botRouteHasValue(route: BotRouteView): boolean {
  return Boolean(
    route.connectionId ||
    route.platform ||
    route.chatType ||
    route.chatId ||
    route.userId ||
    route.threadId ||
    route.model ||
    route.toolApprovalMode ||
    route.workspaceRoot
  );
}

function defaultBotSettings(): BotSettingsView {
  return {
    enabled: false,
    model: "",
    toolApprovalMode: "ask",
    maxSteps: 0,
    debounceMs: 1500,
    queueMode: "steer",
    queueCap: 20,
    queueDrop: "summarize",
    ignoreSelfMessages: true,
    selfUserIds: {
      qq: [],
      feishu: [],
      weixin: [],
    },
    control: {
      enabled: false,
      addr: "127.0.0.1:37913",
      tokenEnv: "REASONIX_BOT_CONTROL_TOKEN",
    },
    pairing: {
      enabled: true,
      requestTtlMinutes: 60,
      maxPendingPerPlatform: 3,
    },
    routes: [],
    allowlist: {
      enabled: true,
      allowAll: false,
      qqUsers: [],
      feishuUsers: [],
      weixinUsers: [],
      qqApprovers: [],
      feishuApprovers: [],
      weixinApprovers: [],
      qqAdmins: [],
      feishuAdmins: [],
      weixinAdmins: [],
      qqGroups: [],
      feishuGroups: [],
      weixinGroups: [],
    },
    qq: { enabled: false, appId: "", appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: false, sandbox: false, model: "", toolApprovalMode: "ask", workspaceRoot: "", access: defaultBotAccess() },
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

function defaultBotAccess(): BotAccessView {
  return {
    enabled: true,
    allowAll: false,
    pairingEnabled: true,
    users: [],
    groups: [],
    approvers: [],
    admins: [],
  };
}

function normalizeBotAccess(raw: any, fallback: BotAccessView = defaultBotAccess()): BotAccessView {
  const access = raw ?? fallback;
  return {
    enabled: access.enabled !== false,
    allowAll: Boolean(access.allowAll),
    pairingEnabled: access.pairingEnabled !== false,
    users: asArray(access.users),
    groups: asArray(access.groups),
    approvers: asArray(access.approvers),
    admins: asArray(access.admins),
  };
}

function normalizeBotSettings(bot: BotSettingsView | null | undefined): BotSettingsView {
  const fallback = defaultBotSettings();
  const allowlist = bot?.allowlist ?? fallback.allowlist;
  const selfUserIds = bot?.selfUserIds ?? fallback.selfUserIds;
  const control = bot?.control ?? fallback.control;
  const pairing = bot?.pairing ?? fallback.pairing;
  const mode = bot?.feishu?.mode === "websocket" ? "websocket" : "webhook";
  return {
    ...fallback,
    ...bot,
    toolApprovalMode: normalizeBotToolApprovalMode(bot?.toolApprovalMode),
    maxSteps: Math.max(0, Number(bot?.maxSteps ?? fallback.maxSteps) || 0),
    debounceMs: Number(bot?.debounceMs) || fallback.debounceMs,
    queueMode: normalizeBotQueueMode(bot?.queueMode),
    queueCap: Math.max(0, Math.floor(Number(bot?.queueCap ?? fallback.queueCap) || 0)),
    queueDrop: normalizeBotQueueDrop(bot?.queueDrop),
    ignoreSelfMessages: bot?.ignoreSelfMessages !== false,
    selfUserIds: {
      qq: asArray(selfUserIds.qq),
      feishu: asArray(selfUserIds.feishu),
      weixin: asArray(selfUserIds.weixin),
    },
    control: {
      enabled: Boolean(control.enabled),
      addr: String(control.addr ?? fallback.control.addr),
      tokenEnv: String(control.tokenEnv ?? fallback.control.tokenEnv),
    },
    pairing: {
      enabled: pairing.enabled !== false,
      requestTtlMinutes: Math.max(0, Math.floor(Number(pairing.requestTtlMinutes ?? fallback.pairing.requestTtlMinutes) || 0)),
      maxPendingPerPlatform: Math.max(0, Math.floor(Number(pairing.maxPendingPerPlatform ?? fallback.pairing.maxPendingPerPlatform) || 0)),
    },
    routes: asArray(bot?.routes).map(normalizeBotRoute).filter(botRouteHasValue),
    allowlist: {
      ...fallback.allowlist,
      ...allowlist,
      qqUsers: asArray(allowlist.qqUsers),
      feishuUsers: asArray(allowlist.feishuUsers),
      weixinUsers: asArray(allowlist.weixinUsers),
      qqApprovers: asArray(allowlist.qqApprovers),
      feishuApprovers: asArray(allowlist.feishuApprovers),
      weixinApprovers: asArray(allowlist.weixinApprovers),
      qqAdmins: asArray(allowlist.qqAdmins),
      feishuAdmins: asArray(allowlist.feishuAdmins),
      weixinAdmins: asArray(allowlist.weixinAdmins),
      qqGroups: asArray(allowlist.qqGroups),
      feishuGroups: asArray(allowlist.feishuGroups),
      weixinGroups: asArray(allowlist.weixinGroups),
    },
    qq: {
      ...fallback.qq,
      ...bot?.qq,
      model: String(bot?.qq?.model ?? fallback.qq.model).trim(),
      toolApprovalMode: normalizeBotToolApprovalMode(bot?.qq?.toolApprovalMode),
      workspaceRoot: String(bot?.qq?.workspaceRoot ?? fallback.qq.workspaceRoot).trim(),
      access: normalizeBotAccess(bot?.qq?.access, fallback.qq.access),
    },
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
    access: normalizeBotAccess(raw?.access),
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

function normalizeStringMap(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const out: Record<string, string> = {};
  for (const [rawKey, rawValue] of Object.entries(value as Record<string, unknown>)) {
    const key = rawKey.trim();
    const val = String(rawValue ?? "").trim();
    if (key && val) out[key] = val;
  }
  return out;
}

function normalizeExtraBodyMap(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const out: Record<string, unknown> = {};
  for (const [rawKey, rawValue] of Object.entries(value as Record<string, unknown>)) {
    const key = rawKey.trim();
    if (key && rawValue !== undefined) out[key] = rawValue;
  }
  return out;
}

function normalizeProviderView(p: ProviderView): ProviderView {
  const visionModels = asArray(p.visionModels);
  const requiresKey = providerRequiresKey(p);
  return {
    ...p,
    builtIn: Boolean(p.builtIn),
    added: Boolean(p.added),
    chatUrl: p.chatUrl ?? "",
    models: asArray(p.models),
    visionModels,
    visionModelsConfigured: Boolean(p.visionModelsConfigured ?? visionModels.length > 0),
    modelsUrl: p.modelsUrl ?? "",
    headers: normalizeStringMap(p.headers),
    extraBody: normalizeExtraBodyMap(p.extraBody),
    authHeader: Boolean(p.authHeader),
    reasoningProtocol: normalizeReasoningProtocol(p.reasoningProtocol),
    thinking: normalizeThinkingMode(p.thinking),
    supportedEfforts: asArray(p.supportedEfforts),
    modelOverrides: asArray(p.modelOverrides),
    requiresKey,
    configured: providerIsConfigured({ ...p, requiresKey }),
    keySource: p.keySource ?? "",
    keySourcePath: p.keySourcePath ?? "",
  };
}

type ProviderPresetStatus = NonNullable<ProviderPresetView["status"]>;

function normalizeProviderPresetStatus(status: ProviderPresetView["status"] | undefined, added: boolean): ProviderPresetStatus {
  if (status === "installed" || status === "installed_modified" || status === "name_conflict" || status === "similar_existing") return status;
  return added ? "installed" : "available";
}

function normalizeProviderPresetView(p: ProviderPresetView): ProviderPresetView {
  const requiresKey = Boolean(p.requiresKey ?? p.keyEnv);
  const configured = Boolean(p.configured ?? (!requiresKey || p.keySet));
  const status = normalizeProviderPresetStatus(p.status, Boolean(p.added));
  return {
    ...p,
    id: String(p.id ?? "").trim(),
    label: String(p.label ?? "").trim(),
    description: String(p.description ?? "").trim(),
    keyEnv: String(p.keyEnv ?? "").trim(),
    providerNames: asArray(p.providerNames),
    models: asArray(p.models),
    added: Boolean(p.added || status === "installed" || status === "installed_modified" || status === "name_conflict"),
    status,
    statusProviderNames: asArray(p.statusProviderNames),
    keySet: Boolean(p.keySet),
    requiresKey,
    configured,
    keySource: p.keySource ?? "",
    keySourcePath: p.keySourcePath ?? "",
  };
}

function normalizeSettingsView(view: SettingsView | null | undefined): SettingsView | null {
  if (!view) return null;
  const permissions = view.permissions ?? { mode: "ask", allow: [], ask: [], deny: [] };
  const sandbox = view.sandbox ?? { bash: "enforce", network: false, workspaceRoot: "", allowWrite: [], effectiveWorkspaceRoot: "", effectiveWriteRoots: [], shell: "auto", effectiveShell: "" };
  const network = view.network ?? {
    proxyMode: "auto",
    proxyUrl: "",
    noProxy: "",
    proxy: { type: "socks5", server: "", port: 0, username: "", password: "" },
  };
  const agent = view.agent ?? { temperature: 0, maxSteps: 0, plannerMaxSteps: 0, maxSubagentDepth: 2, systemPrompt: "", coldResumePrune: true, reasoningLanguage: "auto" };
  agent.plannerMaxSteps = Number.isFinite(agent.plannerMaxSteps) ? Math.max(0, Math.trunc(agent.plannerMaxSteps)) : 0;
  agent.maxSteps = Number.isFinite(agent.maxSteps) ? Math.max(0, Math.trunc(agent.maxSteps)) : 0;
  agent.maxSubagentDepth = Number.isFinite(agent.maxSubagentDepth) && agent.maxSubagentDepth <= 1 ? 1 : 2;
  agent.reasoningLanguage = normalizeReasoningLanguage(agent.reasoningLanguage);
  return {
    ...view,
    providers: asArray(view.providers).map(normalizeProviderView),
    officialProviders: asArray(view.officialProviders).map(normalizeProviderView),
    providerPresets: asArray(view.providerPresets).map(normalizeProviderPresetView).filter((p) => p.id),
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
      effectiveWorkspaceRoot: String(sandbox.effectiveWorkspaceRoot ?? ""),
      effectiveWriteRoots: asArray(sandbox.effectiveWriteRoots),
      effectiveShell: String(sandbox.effectiveShell ?? sandbox.shell ?? ""),
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

function providerKindLabel(kind: string, t: ReturnType<typeof useT>): string {
  switch (kind) {
    case "anthropic":
      return t("settings.providerProtocolAnthropic");
    case "openai":
      return t("settings.providerProtocolOpenAI");
    default:
      return kind;
  }
}

function providerKindHint(kind: string, t: ReturnType<typeof useT>): string {
  return kind === "anthropic" ? t("settings.providerProtocolAnthropicHint") : t("settings.providerProtocolOpenAIHint");
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

function thinkingModeLabel(mode: string, t: ReturnType<typeof useT>): string {
  switch (mode) {
    case "enabled":
      return t("settings.thinkingMode.enabled");
    case "disabled":
      return t("settings.thinkingMode.disabled");
    case "adaptive":
      return t("settings.thinkingMode.adaptive");
    default:
      return t("settings.thinkingMode.auto");
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
const BOT_ALLOWLIST_TEXT_KEYS = [
  "qqUsers",
  "feishuUsers",
  "weixinUsers",
  "qqApprovers",
  "feishuApprovers",
  "weixinApprovers",
  "qqAdmins",
  "feishuAdmins",
  "weixinAdmins",
  "qqGroups",
  "feishuGroups",
  "weixinGroups",
] as const;
type BotAllowlistTextKey = typeof BOT_ALLOWLIST_TEXT_KEYS[number];
type BotSelfUserTextKey = keyof BotSettingsView["selfUserIds"];
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
const BOT_PLATFORM_KEYS = ["qq", "feishu", "weixin"] as const;
type BotPlatformKey = typeof BOT_PLATFORM_KEYS[number];
const BOT_ALLOWLIST_ROLES = ["Users", "Groups", "Approvers", "Admins"] as const;
type BotAllowlistRole = typeof BOT_ALLOWLIST_ROLES[number];
type BotAccessListField = "users" | "groups" | "approvers" | "admins";

function botAllowlistKey(platform: BotPlatformKey, role: BotAllowlistRole): BotAllowlistTextKey {
  return `${platform}${role}`;
}

function botConnectionPlatform(connection: BotConnectionView): BotPlatformKey {
  if (connection.provider === "weixin") return "weixin";
  if (connection.provider === "qq") return "qq";
  return "feishu";
}

function botPlatformLabel(platform: BotPlatformKey, t: ReturnType<typeof useT>): string {
  if (platform === "qq") return "QQ";
  if (platform === "weixin") return t("settings.botWeixin");
  return t("settings.botPlatformFeishuLark");
}

type BotConnectionListItem =
  | { kind: "qq" }
  | { kind: "connection"; connection: BotConnectionView };

type BotsSectionProps = SectionProps & { initialFocus?: SettingsInitialFocus };

function BotsSection({ s, busy, apply, initialFocus }: BotsSectionProps) {
  const t = useT();
  const savedBot = normalizeBotSettings(s.bot);
  const [draft, setDraft] = useState<BotSettingsView>(savedBot);
  const [allowlistText, setAllowlistText] = useState<Record<BotAllowlistTextKey, string>>(() => botAllowlistTextValues(savedBot.allowlist));
  const [selfUserText, setSelfUserText] = useState<Record<BotSelfUserTextKey, string>>(() => botSelfUserTextValues(savedBot.selfUserIds));
  const [showAllPlatforms, setShowAllPlatforms] = useState(false);
  const [installTarget, setInstallTarget] = useState<BotInstallTarget>("qq");
  const [install, setInstall] = useState<BotInstallState>({ target: "qq", result: null, status: "idle", timeLeft: 0, message: "" });
  const [diagnostics, setDiagnostics] = useState<Record<string, BotConnectionDiagnostic | string>>({});
  const [testTargets, setTestTargets] = useState<Record<string, string>>({});
  const [connectionSecrets, setConnectionSecrets] = useState<Record<string, string>>({});
  const [accessText, setAccessText] = useState<Record<string, string>>({});
  const [qqSecretValue, setQQSecretValue] = useState("");
  const [expandedConnectionId, setExpandedConnectionId] = useState("");
  const [advancedMode, setAdvancedMode] = useState(false);
  const installRef = useRef(install);
  const installPollTimerRef = useRef<number | null>(null);
  const installCountdownTimerRef = useRef<number | null>(null);
  const installRequestInFlightRef = useRef(false);
  const installAttemptRef = useRef(0);
  const stepConnectRef = useRef<HTMLElement | null>(null);
  const initialFocusHandledRef = useRef("");
  const refs = allRefs(s);

  useEffect(() => {
    const nextBot = normalizeBotSettings(s.bot);
    setDraft(nextBot);
    setAllowlistText(botAllowlistTextValues(nextBot.allowlist));
    setSelfUserText(botSelfUserTextValues(nextBot.selfUserIds));
    setConnectionSecrets({});
    setAccessText({});
    setQQSecretValue("");
    setTestTargets({});
  }, [s.bot]);
  const focusAccessStep = () => {
    if (!expandedConnectionId && connectionItems.length > 0) {
      const first = connectionItems[0];
      if (first.kind === "qq") {
        setInstallTarget("qq");
        setExpandedConnectionId(QQ_CONNECTION_ID);
      } else {
        const nextTarget = botInstallTargetForConnection(first.connection);
        setInstallTarget(nextTarget);
        setExpandedConnectionId(first.connection.id);
      }
    }
    window.setTimeout(() => stepConnectRef.current?.scrollIntoView({ block: "start", behavior: "smooth" }), 60);
  };
  useEffect(() => {
    if (initialFocus?.target !== "bot-allowlist") return;
    const focusKey = `${initialFocus.target}:${initialFocus.connectionId ?? ""}`;
    if (initialFocusHandledRef.current === focusKey) return;
    initialFocusHandledRef.current = focusKey;
    focusAccessStep();
  }, [initialFocus]);
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
  const updateBotSettings = (patch: Partial<BotSettingsView>) =>
    setDraft((prev) => ({ ...prev, ...patch }));
  const persistBotSettings = (patch: Partial<BotSettingsView>) =>
    persistBotDraft({ ...draft, ...patch });
  const updateSelfUserText = (key: BotSelfUserTextKey, value: string) =>
    setSelfUserText((prev) => ({ ...prev, [key]: value }));
  const persistSelfUserText = (key: BotSelfUserTextKey, value: string) => {
    const entries = parseBotListInput(value);
    const nextSelfUserIds = { ...draft.selfUserIds, [key]: entries };
    setSelfUserText((prev) => ({ ...prev, [key]: entries.join("\n") }));
    void persistBotSettings({ selfUserIds: nextSelfUserIds });
  };
  const updateRoute = (index: number, patch: Partial<BotRouteView>) =>
    setDraft((prev) => ({
      ...prev,
      routes: prev.routes.map((route, routeIndex) => routeIndex === index ? normalizeBotRoute({ ...route, ...patch }) : route),
    }));
  const persistRoute = (index: number, patch: Partial<BotRouteView>) =>
    persistBotDraft({
      ...draft,
      routes: draft.routes.map((route, routeIndex) => routeIndex === index ? normalizeBotRoute({ ...route, ...patch }) : route),
    });
  const addRoute = () =>
    setDraft((prev) => ({ ...prev, routes: [...prev.routes, emptyBotRoute()] }));
  const removeRoute = (index: number) =>
    void persistBotDraft({ ...draft, routes: draft.routes.filter((_, routeIndex) => routeIndex !== index) });
  const updateQQ = (patch: Partial<BotSettingsView["qq"]>) =>
    setDraft((prev) => ({ ...prev, qq: { ...prev.qq, ...patch } }));
  const persistQQ = (patch: Partial<BotSettingsView["qq"]>) =>
    persistBotDraft({ ...draft, qq: { ...draft.qq, ...patch } });
  const updateQQAccess = (patch: Partial<BotAccessView>) =>
    updateQQ({ access: normalizeBotAccess({ ...draft.qq.access, ...patch }) });
  const persistQQAccess = (patch: Partial<BotAccessView>) =>
    persistQQ({ access: normalizeBotAccess({ ...draft.qq.access, ...patch }) });
  const updateConnectionAccess = (id: string, patch: Partial<BotAccessView>) =>
    setConnections((items) => items.map((item) => item.id === id ? { ...item, access: normalizeBotAccess({ ...item.access, ...patch }) } : item));
  const persistConnectionAccess = (connection: BotConnectionView, patch: Partial<BotAccessView>) =>
    persistConnection(connection.id, { access: normalizeBotAccess({ ...connection.access, ...patch }) });
  const accessTextKey = (id: string, field: BotAccessListField) => `${id}:${field}`;
  const accessListText = (id: string, access: BotAccessView, field: BotAccessListField) =>
    accessText[accessTextKey(id, field)] ?? access[field].join("\n");
  const setAccessListText = (id: string, field: BotAccessListField, value: string) =>
    setAccessText((prev) => ({ ...prev, [accessTextKey(id, field)]: value }));
  const persistAccessListText = (
    id: string,
    access: BotAccessView,
    field: BotAccessListField,
    value: string,
    persistAccess: (patch: Partial<BotAccessView>) => void,
  ) => {
    const entries = parseBotListInput(value);
    setAccessText((prev) => ({ ...prev, [accessTextKey(id, field)]: entries.join("\n") }));
    persistAccess({ ...access, [field]: entries } as Partial<BotAccessView>);
  };
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
  const selectedInstallLabel = botTargetLabel(installTarget, t);
  const installUserCode = install.result?.userCode && installTarget !== "weixin" ? formatInstallUserCode(install.result.userCode) : "";
  const qqSecretEnv = draft.qq.appSecretEnv.trim() || DEFAULT_QQ_SECRET_ENV;
  const qqConfigured = draft.qq.enabled && draft.qq.appId.trim() && qqSecretEnv && draft.qq.secretSet;
  const qqCanEnableAccess = botAccessReady(draft.qq.access);
  const qqCanSaveAndEnable = Boolean(draft.qq.appId.trim() && qqSecretEnv && (draft.qq.secretSet || qqSecretValue.trim()) && qqCanEnableAccess);
  const qqAdded = qqBotAdded(draft.qq);
  const nativeRuntimeAvailable = typeof window !== "undefined" && Boolean(window.runtime);
  const browserPreviewBotConfigured = !nativeRuntimeAvailable && (qqAdded || draft.connections.length > 0);
  const qqOnline = qqConfigured && nativeRuntimeAvailable;
  const connectionItems: BotConnectionListItem[] = [
    ...(qqAdded ? [{ kind: "qq" as const }] : []),
    ...draft.connections.map((connection) => ({ kind: "connection" as const, connection })),
  ];
  const selectedInstallConnection = isQQInstallTarget ? undefined : draft.connections.find((connection) => botInstallTargetMatchesConnection(installTarget, connection));
  const selectedChannelConfigured = isQQInstallTarget ? qqAdded : Boolean(selectedInstallConnection);
  const routeConnectionOptions = [
    ...(qqAdded ? [{ id: "qq", label: "QQ" }] : []),
    ...draft.connections.map((connection) => ({
      id: connection.id || [connection.provider, connection.domain].filter(Boolean).join("-"),
      label: connection.label || botConnectionLabel(connection, t),
    })).filter((item) => item.id),
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
    setDiagnostics((prev) => ({ ...prev, [QQ_CONNECTION_ID]: t("settings.botQQAccessRequired") }));
    setExpandedConnectionId(QQ_CONNECTION_ID);
    window.setTimeout(() => stepConnectRef.current?.scrollIntoView({ block: "start", behavior: "smooth" }), 60);
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
      qq: { enabled: false, appId: "", appSecretEnv: DEFAULT_QQ_SECRET_ENV, secretSet: false, sandbox: false, model: "", toolApprovalMode: "ask", workspaceRoot: "", access: defaultBotAccess() },
    });
    await apply(async () => {
      await app.SetBotSettings(nextDraft);
      if (draft.qq.secretSet) await app.ClearBotSecret(env);
    });
    setDraft(nextDraft);
    setQQSecretValue("");
    setExpandedConnectionId("");
  };
  const selectedQQ = isQQInstallTarget && qqAdded;
  const selectedConnection = isQQInstallTarget ? null : selectedInstallConnection ?? null;
  const selectedDiagnostic = selectedConnection ? diagnostics[selectedConnection.id] : undefined;
  const selectedDiagnosticDetail = diagnosticReportDetail(selectedDiagnostic);
  const selectedConnectionRemote = selectedConnection ? firstConnectionRemote(selectedConnection) : "";
  const selectedConnectionToolApprovalMode = selectedConnection ? normalizeBotToolApprovalMode(selectedConnection.toolApprovalMode) : "ask";
  const simpleAccessMode = draft.allowlist.allowAll ? "everyone" : "trusted";
  const connectedPlatforms = new Set<BotPlatformKey>();
  if (qqAdded) connectedPlatforms.add("qq");
  for (const connection of draft.connections) connectedPlatforms.add(botConnectionPlatform(connection));
  const platformHasAllowlistText = (platform: BotPlatformKey) =>
    BOT_ALLOWLIST_ROLES.some((role) => allowlistText[botAllowlistKey(platform, role)].trim());
  const visibleAccessPlatforms = BOT_PLATFORM_KEYS.filter((platform) =>
    showAllPlatforms || connectedPlatforms.size === 0 || connectedPlatforms.has(platform) || platformHasAllowlistText(platform));
  const platformFilterAvailable = connectedPlatforms.size > 0 &&
    BOT_PLATFORM_KEYS.some((platform) => !connectedPlatforms.has(platform) && !platformHasAllowlistText(platform));
  const botChannelConnectionForTarget = (target: BotInstallTarget) =>
    target === "qq" ? null : draft.connections.find((connection) => botInstallTargetMatchesConnection(target, connection));
  const botChannelIsConfigured = (target: BotInstallTarget) =>
    target === "qq" ? qqAdded : Boolean(botChannelConnectionForTarget(target));
  const openBotChannel = (target: BotInstallTarget) => {
    setInstallTarget(target);
    const connection = botChannelConnectionForTarget(target);
    setExpandedConnectionId(target === "qq" && qqAdded ? QQ_CONNECTION_ID : connection?.id || "");
  };
  const setSimpleAccessMode = (mode: "trusted" | "everyone") => {
    const patch = mode === "everyone"
      ? { enabled: false, allowAll: true }
      : { enabled: true, allowAll: false };
    updateAllowlist(patch);
    void persistAllowlist(patch);
  };
  const renderBotAccessSection = (
    id: string,
    access: BotAccessView,
    updateAccess: (patch: Partial<BotAccessView>) => void,
    persistAccess: (patch: Partial<BotAccessView>) => void,
  ) => {
    const mode = access.allowAll ? "everyone" : "trusted";
    const setMode = (nextMode: "trusted" | "everyone") => {
      const patch = nextMode === "everyone"
        ? { enabled: false, allowAll: true }
        : { enabled: true, allowAll: false };
      updateAccess(patch);
      persistAccess(patch);
    };
    return (
      <section className="bot-detail-section bot-detail-section--access">
        <div>
          <div className="bot-detail-section__head">{t("settings.botAccessControl")}</div>
          <p>{access.allowAll ? t("settings.botSimpleAccessEveryoneSummary") : t("settings.botSimpleAccessTrustedSummary", { count: botAccessEntryCount(access) })}</p>
        </div>
        <div className="bot-choice-grid bot-choice-grid--access">
          <button
            type="button"
            className={`bot-choice-card${mode === "trusted" ? " bot-choice-card--active" : ""}`}
            disabled={busy}
            onClick={() => setMode("trusted")}
          >
            <strong>{t("settings.botAccessTrusted")}</strong>
            <span>{t("settings.botAccessTrustedHint")}</span>
          </button>
          <button
            type="button"
            className={`bot-choice-card${mode === "everyone" ? " bot-choice-card--active" : ""}`}
            disabled={busy}
            onClick={() => setMode("everyone")}
          >
            <strong>{t("settings.botAccessEveryone")}</strong>
            <span>{t("settings.botAccessEveryoneHint")}</span>
          </button>
        </div>
        <div className="bot-pairing-row">
          <div>
            <strong>{t("settings.botAccessPairing")}</strong>
            <span>{t("settings.botAccessPairingHint")}</span>
          </div>
          <ToggleSegment
            value={access.pairingEnabled}
            disabled={busy}
            onChange={(pairingEnabled) => {
              updateAccess({ pairingEnabled });
              persistAccess({ pairingEnabled });
            }}
          />
        </div>
        {access.allowAll ? (
          <div className="bot-access-panel__warning">{t("settings.botAllowAllWarn")}</div>
        ) : (
          <div className="bot-access-platforms bot-access-platforms--single">
            <div className="bot-access-platform">
              <BotListInput
                label={t("settings.botListUsers")}
                value={accessListText(id, access, "users")}
                disabled={busy}
                placeholder={t("settings.botListPlaceholder")}
                onChange={(value) => setAccessListText(id, "users", value)}
                onBlur={(value) => persistAccessListText(id, access, "users", value, persistAccess)}
              />
              <BotListInput
                label={t("settings.botListGroups")}
                value={accessListText(id, access, "groups")}
                disabled={busy}
                placeholder={t("settings.botListPlaceholder")}
                onChange={(value) => setAccessListText(id, "groups", value)}
                onBlur={(value) => persistAccessListText(id, access, "groups", value, persistAccess)}
              />
            </div>
          </div>
        )}
        <details className="bot-access-panel bot-simple-roles">
          <summary className="bot-access-panel__summary">
            <span>
              <strong>{t("settings.botRoleAccess")}</strong>
              <small>{t("settings.botRoleAccessHint")}</small>
            </span>
            <ChevronDown className="bot-access-panel__chevron" size={16} aria-hidden="true" />
          </summary>
          <div className="bot-access-panel__body">
            <div className="bot-access-platforms bot-access-platforms--single">
              <div className="bot-access-platform">
                <BotListInput
                  label={t("settings.botListApprovers")}
                  value={accessListText(id, access, "approvers")}
                  disabled={busy || access.allowAll}
                  placeholder={t("settings.botListPlaceholder")}
                  onChange={(value) => setAccessListText(id, "approvers", value)}
                  onBlur={(value) => persistAccessListText(id, access, "approvers", value, persistAccess)}
                />
                <BotListInput
                  label={t("settings.botListAdmins")}
                  value={accessListText(id, access, "admins")}
                  disabled={busy || access.allowAll}
                  placeholder={t("settings.botListPlaceholder")}
                  onChange={(value) => setAccessListText(id, "admins", value)}
                  onBlur={(value) => persistAccessListText(id, access, "admins", value, persistAccess)}
                />
              </div>
            </div>
          </div>
        </details>
      </section>
    );
  };
  const qqDetailCard = (
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

      <section className="bot-detail-section bot-detail-section--runtime-primary">
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
        <SettingsField label={t("settings.botToolApprovalMode")} hint={t("settings.botToolApprovalModeHint")}>
          <div className="provider-add-segmented" role="group" aria-label={t("settings.botToolApprovalMode")}>
            {TOOL_APPROVAL_MODES.map((mode) => (
              <button
                key={mode}
                type="button"
                className={normalizeBotToolApprovalMode(draft.qq.toolApprovalMode) === mode ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
                disabled={busy}
                onClick={() => void persistQQ({ toolApprovalMode: mode })}
              >
                {t(`settings.botToolApprovalMode.${mode}` as DictKey)}
              </button>
            ))}
          </div>
        </SettingsField>
        <SettingsField label={t("settings.botChannelModel")} hint={t("settings.botChannelModelHint")}>
          <ModelPicker
            s={s}
            refs={refs}
            value={toRef(draft.qq.model, s)}
            disabled={busy}
            emptyOptionLabel={t("settings.botChannelModelAuto")}
            emptyOptionHint={settingsModelMeta(s, t)}
            onPick={(model) => void persistQQ({ model })}
          />
        </SettingsField>
      </section>

      {renderBotAccessSection(QQ_CONNECTION_ID, draft.qq.access, updateQQAccess, (patch) => void persistQQAccess(patch))}

      <section className="bot-detail-section">
        <div className="bot-detail-section__head">{t("settings.botRuntimeSettings")}</div>
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
        <SettingsField label={t("settings.botWorkspaceRoot")} hint={t("settings.botWorkspaceRootHint")}>
          <input
            className="mem-input"
            value={draft.qq.workspaceRoot}
            disabled={busy}
            placeholder={t("settings.botWorkspaceRootPlaceholder")}
            spellCheck={false}
            onChange={(event) => updateQQ({ workspaceRoot: event.target.value })}
            onBlur={(event) => void persistQQ({ workspaceRoot: event.currentTarget.value })}
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
  );

  const connectionDetailCard = selectedConnection ? (
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

      <section className="bot-detail-section bot-detail-section--runtime-primary">
        <SettingsField label={t("settings.botEnableBot")} hint={t("settings.botGatewayEnabled")}>
          <ToggleSegment
            value={selectedConnection.enabled}
            disabled={busy}
            onChange={(enabled) => void persistConnection(selectedConnection.id, { enabled })}
          />
        </SettingsField>
        <SettingsField label={t("settings.botToolApprovalMode")} hint={t("settings.botToolApprovalModeHint")}>
          <div className="provider-add-segmented" role="group" aria-label={t("settings.botToolApprovalMode")}>
            {TOOL_APPROVAL_MODES.map((mode) => (
              <button
                key={mode}
                type="button"
                className={selectedConnectionToolApprovalMode === mode ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
                disabled={busy}
                onClick={() => persistConnectionToolApprovalMode(selectedConnection.id, mode)}
              >
                {t(`settings.botToolApprovalMode.${mode}` as DictKey)}
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
      </section>

      {renderBotAccessSection(
        selectedConnection.id,
        selectedConnection.access,
        (patch) => updateConnectionAccess(selectedConnection.id, patch),
        (patch) => void persistConnectionAccess(selectedConnection, patch),
      )}

      <section className="bot-detail-section">
        <div className="bot-detail-section__head">{t("settings.botRuntimeSettings")}</div>
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
  ) : null;

  const installPanelContent = (
    <>
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
    </>
  );

  const botManager = (
    <section ref={stepConnectRef} id="bot-step-connect" className="bot-channel-manager-card">
      <div className="bot-channel-manager-card__head">
        <div>
          <strong>{t("settings.botManageBots")}</strong>
          <span>{t("settings.botManageBotsHint")}</span>
        </div>
      </div>
      {browserPreviewBotConfigured ? (
        <div className="bot-connection-warning">{t("settings.botBrowserPreviewWarning")}</div>
      ) : null}
      <div className="bot-channel-manager">
        <div className="bot-channel-tabs" role="tablist" aria-label={t("settings.botChannelTabsLabel")}>
          {BOT_INSTALL_TARGETS.map((target) => {
            const configured = botChannelIsConfigured(target);
            const connected = target === "qq" ? qqOnline : botChannelConnectionForTarget(target)?.status === "connected";
            return (
              <button
                key={target}
                type="button"
                role="tab"
                aria-selected={installTarget === target}
                className={`bot-channel-tab${installTarget === target ? " bot-channel-tab--active" : ""}`}
                disabled={busy || install.status === "starting"}
                onClick={() => openBotChannel(target)}
              >
                <span className="bot-channel-tab__icon" aria-hidden="true">
                  {target === "qq" || target === "weixin" ? <MessageCircle size={24} /> : <BotIcon size={24} />}
                </span>
                <span className="bot-channel-tab__text">
                  <strong>{botTargetLabel(target, t)}</strong>
                  <small>{botTargetHint(target, t)}</small>
                </span>
                <span className={`bot-channel-tab__dot${connected ? " bot-channel-tab__dot--online" : configured ? " bot-channel-tab__dot--configured" : ""}`} />
              </button>
            );
          })}
        </div>
        <div className="bot-channel-manager__detail" role="tabpanel" aria-label={selectedInstallLabel}>
          {!selectedChannelConfigured ? (
            <article className="bot-channel-setup-card">
              <div className="bot-channel-setup-card__head">
                <div>
                  <strong>{t("settings.botChannelSetupTitle", { provider: selectedInstallLabel })}</strong>
                  <span>{t("settings.botChannelSetupHint")}</span>
                </div>
                <span className="badge badge--neutral">{t("settings.botChannelNeedsSetup")}</span>
              </div>
              {installPanelContent}
            </article>
          ) : selectedQQ ? (
            qqDetailCard
          ) : selectedConnection ? (
            connectionDetailCard
          ) : (
            <div className="bot-manager__empty">{t("settings.botSelectBotHint")}</div>
          )}
        </div>
      </div>
    </section>
  );

  return (
    <div className="bot-phone-connect">
      {botManager}

	      <details
        id="bot-advanced-settings"
        className="bot-simple-advanced"
        open={advancedMode}
        onToggle={(event) => {
          const nextOpen = event.currentTarget.open;
          setAdvancedMode((current) => current === nextOpen ? current : nextOpen);
        }}
      >
        <summary className="bot-simple-advanced__summary">
          <span>
            <strong>{t("settings.botShowAdvancedSettings")}</strong>
            <small>{t("settings.botAdvancedSettingsHint")}</small>
          </span>
          <span className="bot-simple-advanced__toggle">
            {advancedMode ? t("common.collapse") : t("common.expand")}
            <ChevronDown aria-hidden="true" size={16} />
          </span>
	        </summary>
	        <div className="bot-simple-advanced__body">
	          <details className="bot-access-panel bot-global-access-panel">
	            <summary className="bot-access-panel__summary">
	              <span>
	                <strong>{t("settings.botGlobalAllowlist")}</strong>
	                <small>{t("settings.botGlobalAllowlistHint")}</small>
	              </span>
	              <ChevronDown className="bot-access-panel__chevron" size={16} aria-hidden="true" />
	            </summary>
	            <div className="bot-access-panel__body">
	              <div className="bot-choice-grid bot-choice-grid--access">
	                <button
	                  type="button"
	                  className={`bot-choice-card${simpleAccessMode === "trusted" ? " bot-choice-card--active" : ""}`}
	                  disabled={busy}
	                  onClick={() => setSimpleAccessMode("trusted")}
	                >
	                  <strong>{t("settings.botAccessTrusted")}</strong>
	                  <span>{t("settings.botAccessTrustedHint")}</span>
	                </button>
	                <button
	                  type="button"
	                  className={`bot-choice-card${simpleAccessMode === "everyone" ? " bot-choice-card--active" : ""}`}
	                  disabled={busy}
	                  onClick={() => setSimpleAccessMode("everyone")}
	                >
	                  <strong>{t("settings.botAccessEveryone")}</strong>
	                  <span>{t("settings.botAccessEveryoneHint")}</span>
	                </button>
	              </div>
	              <div className="bot-pairing-row">
	                <div>
	                  <strong>{t("settings.botAccessPairing")}</strong>
	                  <span>{t("settings.botAccessPairingHint")}</span>
	                </div>
	                <ToggleSegment
	                  value={draft.pairing.enabled}
	                  disabled={busy}
	                  onChange={(enabled) => void persistBotSettings({ pairing: { ...draft.pairing, enabled } })}
	                />
	              </div>
	              {draft.allowlist.allowAll ? (
	                <div className="bot-access-panel__warning">{t("settings.botAllowAllWarn")}</div>
	              ) : (
	                <>
	                  <div className="bot-access-platforms">
	                    {visibleAccessPlatforms.map((platform) => (
	                      <div className="bot-access-platform" key={platform}>
	                        <div className="bot-access-platform__name">{botPlatformLabel(platform, t)}</div>
	                        <BotListInput
	                          label={t("settings.botListUsers")}
	                          value={allowlistText[botAllowlistKey(platform, "Users")]}
	                          disabled={busy}
	                          placeholder={t("settings.botListPlaceholder")}
	                          onChange={(value) => setAllowlistText((prev) => ({ ...prev, [botAllowlistKey(platform, "Users")]: value }))}
	                          onBlur={(value) => persistAllowlistText(botAllowlistKey(platform, "Users"), value)}
	                        />
	                        <BotListInput
	                          label={t("settings.botListGroups")}
	                          value={allowlistText[botAllowlistKey(platform, "Groups")]}
	                          disabled={busy}
	                          placeholder={t("settings.botListPlaceholder")}
	                          onChange={(value) => setAllowlistText((prev) => ({ ...prev, [botAllowlistKey(platform, "Groups")]: value }))}
	                          onBlur={(value) => persistAllowlistText(botAllowlistKey(platform, "Groups"), value)}
	                        />
	                      </div>
	                    ))}
	                  </div>
	                  {platformFilterAvailable ? (
	                    <button type="button" className="bot-access-platforms__toggle" onClick={() => setShowAllPlatforms((value) => !value)}>
	                      {showAllPlatforms ? t("settings.botAccessShowConnectedOnly") : t("settings.botAccessShowAllPlatforms")}
	                    </button>
	                  ) : null}
	                </>
	              )}
	              <details className="bot-access-panel bot-simple-roles">
	                <summary className="bot-access-panel__summary">
	                  <span>
	                    <strong>{t("settings.botRoleAccess")}</strong>
	                    <small>{t("settings.botRoleAccessHint")}</small>
	                  </span>
	                  <ChevronDown className="bot-access-panel__chevron" size={16} aria-hidden="true" />
	                </summary>
	                <div className="bot-access-panel__body">
	                  <div className="bot-access-platforms">
	                    {visibleAccessPlatforms.map((platform) => (
	                      <div className="bot-access-platform" key={platform}>
	                        <div className="bot-access-platform__name">{botPlatformLabel(platform, t)}</div>
	                        <BotListInput
	                          label={t("settings.botListApprovers")}
	                          value={allowlistText[botAllowlistKey(platform, "Approvers")]}
	                          disabled={busy || draft.allowlist.allowAll}
	                          placeholder={t("settings.botListPlaceholder")}
	                          onChange={(value) => setAllowlistText((prev) => ({ ...prev, [botAllowlistKey(platform, "Approvers")]: value }))}
	                          onBlur={(value) => persistAllowlistText(botAllowlistKey(platform, "Approvers"), value)}
	                        />
	                        <BotListInput
	                          label={t("settings.botListAdmins")}
	                          value={allowlistText[botAllowlistKey(platform, "Admins")]}
	                          disabled={busy || draft.allowlist.allowAll}
	                          placeholder={t("settings.botListPlaceholder")}
	                          onChange={(value) => setAllowlistText((prev) => ({ ...prev, [botAllowlistKey(platform, "Admins")]: value }))}
	                          onBlur={(value) => persistAllowlistText(botAllowlistKey(platform, "Admins"), value)}
	                        />
	                      </div>
	                    ))}
	                  </div>
	                </div>
	              </details>
	            </div>
	          </details>
	          <details className="bot-access-panel bot-gateway-panel">
            <summary className="bot-access-panel__summary">
              <span>
                <strong>{t("settings.botGatewayDefaults")}</strong>
                <small>{t("settings.botGatewayDefaultsHint")}</small>
              </span>
              <ChevronDown className="bot-access-panel__chevron" size={16} aria-hidden="true" />
            </summary>
            <div className="bot-access-panel__body">
              <SettingsField label={t("settings.botRuntime")} hint={t("settings.botRuntimeHint")}>
                <div className="bot-inline-grid bot-inline-grid--runtime">
                  <label>
                    <span>{t("settings.botMaxSteps")}</span>
                    <input
                      className="mem-input"
                      type="number"
                      min={0}
                      value={draft.maxSteps}
                      disabled={busy}
                      onChange={(event) => updateBotSettings({ maxSteps: Number(event.target.value) || 0 })}
                      onBlur={(event) => void persistBotSettings({ maxSteps: Number(event.currentTarget.value) || 0 })}
                    />
                  </label>
                  <label>
                    <span>{t("settings.botDebounceMs")}</span>
                    <input
                      className="mem-input"
                      type="number"
                      min={0}
                      value={draft.debounceMs}
                      disabled={busy}
                      onChange={(event) => updateBotSettings({ debounceMs: Number(event.target.value) || 0 })}
                      onBlur={(event) => void persistBotSettings({ debounceMs: Number(event.currentTarget.value) || 0 })}
                    />
                  </label>
                  <label>
                    <span>{t("settings.botQueueCap")}</span>
                    <input
                      className="mem-input"
                      type="number"
                      min={0}
                      value={draft.queueCap}
                      disabled={busy}
                      onChange={(event) => updateBotSettings({ queueCap: Number(event.target.value) || 0 })}
                      onBlur={(event) => void persistBotSettings({ queueCap: Number(event.currentTarget.value) || 0 })}
                    />
                  </label>
                </div>
              </SettingsField>
              <SettingsField label={t("settings.botQueueModeSimple")} hint={t("settings.botQueueModeSimpleHint")}>
                <select
                  className="mem-select"
                  value={normalizeBotQueueMode(draft.queueMode)}
                  disabled={busy}
                  onChange={(event) => void persistBotSettings({ queueMode: event.target.value })}
                >
                  {BOT_QUEUE_MODES.map((mode) => (
                    <option key={mode} value={mode}>{t(`settings.botQueueMode.${mode}` as DictKey)}</option>
                  ))}
                </select>
              </SettingsField>
              <SettingsField label={t("settings.botQueueDropLabel")} hint={t("settings.botQueueDropHint")}>
                <select
                  className="mem-select"
                  value={normalizeBotQueueDrop(draft.queueDrop)}
                  disabled={busy}
                  onChange={(event) => void persistBotSettings({ queueDrop: event.target.value })}
                >
                  {BOT_QUEUE_DROPS.map((mode) => (
                    <option key={mode} value={mode}>{t(`settings.botQueueDrop.${mode}` as DictKey)}</option>
                  ))}
                </select>
              </SettingsField>
              <SettingsField label={t("settings.botIgnoreSelfMessages")} hint={t("settings.botIgnoreSelfMessagesHint")}>
                <ToggleSegment
                  value={draft.ignoreSelfMessages}
                  disabled={busy}
                  onChange={(ignoreSelfMessages) => void persistBotSettings({ ignoreSelfMessages })}
                />
              </SettingsField>
              <SettingsField label={t("settings.botSelfUserIds")} hint={t("settings.botSelfUserIdsHint")}>
                <div className="bot-list-grid">
                  <BotListInput
                    label={t("settings.botQQUsers")}
                    value={selfUserText.qq}
                    disabled={busy}
                    placeholder={t("settings.botListPlaceholder")}
                    onChange={(value) => updateSelfUserText("qq", value)}
                    onBlur={(value) => persistSelfUserText("qq", value)}
                  />
                  <BotListInput
                    label={t("settings.botFeishuLarkUsers")}
                    value={selfUserText.feishu}
                    disabled={busy}
                    placeholder={t("settings.botListPlaceholder")}
                    onChange={(value) => updateSelfUserText("feishu", value)}
                    onBlur={(value) => persistSelfUserText("feishu", value)}
                  />
                  <BotListInput
                    label={t("settings.botWeixinUsers")}
                    value={selfUserText.weixin}
                    disabled={busy}
                    placeholder={t("settings.botListPlaceholder")}
                    onChange={(value) => updateSelfUserText("weixin", value)}
                    onBlur={(value) => persistSelfUserText("weixin", value)}
                  />
                </div>
              </SettingsField>
              <SettingsField label={t("settings.botPairing")} hint={t("settings.botPairingDetailHint")}>
                <div className="bot-inline-grid bot-inline-grid--runtime">
                  <label>
                    <span>{t("settings.botPairingTTL")}</span>
                    <input
                      className="mem-input"
                      type="number"
                      min={0}
                      value={draft.pairing.requestTtlMinutes}
                      disabled={busy}
                      onChange={(event) => updateBotSettings({ pairing: { ...draft.pairing, requestTtlMinutes: Number(event.target.value) || 0 } })}
                      onBlur={(event) => void persistBotSettings({ pairing: { ...draft.pairing, requestTtlMinutes: Number(event.currentTarget.value) || 0 } })}
                    />
                  </label>
                  <label>
                    <span>{t("settings.botPairingMaxPending")}</span>
                    <input
                      className="mem-input"
                      type="number"
                      min={0}
                      value={draft.pairing.maxPendingPerPlatform}
                      disabled={busy}
                      onChange={(event) => updateBotSettings({ pairing: { ...draft.pairing, maxPendingPerPlatform: Number(event.target.value) || 0 } })}
                      onBlur={(event) => void persistBotSettings({ pairing: { ...draft.pairing, maxPendingPerPlatform: Number(event.currentTarget.value) || 0 } })}
                    />
                  </label>
                </div>
              </SettingsField>
            </div>
          </details>

          <details className="bot-access-panel bot-routes-panel">
            <summary className="bot-access-panel__summary">
              <span>
                <strong>{t("settings.botRoutes")}</strong>
                <small>{t("settings.botRoutesHint")}</small>
              </span>
              <ChevronDown className="bot-access-panel__chevron" size={16} aria-hidden="true" />
            </summary>
            <div className="bot-access-panel__body">
              {draft.routes.length === 0 ? (
                <div className="bot-route-empty">{t("settings.botRoutesEmpty")}</div>
              ) : (
                <div className="bot-route-list">
                  {draft.routes.map((route, index) => (
                    <div className="bot-route-row" key={index}>
                      <div className="bot-route-row__head">
                        <strong>{t("settings.botRouteTitle", { n: index + 1 })}</strong>
                        <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => removeRoute(index)}>
                          {t("common.delete")}
                        </button>
                      </div>
                      <div className="bot-route-grid">
                        <label>
                          <span>{t("settings.botRouteConnection")}</span>
                          <select
                            className="mem-select"
                            value={route.connectionId}
                            disabled={busy}
                            onChange={(event) => {
                              updateRoute(index, { connectionId: event.target.value });
                              void persistRoute(index, { connectionId: event.target.value });
                            }}
                          >
                            <option value="">{t("settings.botRouteAny")}</option>
                            {routeConnectionOptions.map((option) => (
                              <option key={option.id} value={option.id}>{option.label} · {option.id}</option>
                            ))}
                          </select>
                        </label>
                        <label>
                          <span>{t("settings.botRoutePlatform")}</span>
                          <select
                            className="mem-select"
                            value={route.platform}
                            disabled={busy}
                            onChange={(event) => {
                              updateRoute(index, { platform: event.target.value });
                              void persistRoute(index, { platform: event.target.value });
                            }}
                          >
                            <option value="">{t("settings.botRouteAny")}</option>
                            <option value="qq">QQ</option>
                            <option value="feishu">{t("settings.botFeishu")}</option>
                            <option value="weixin">{t("settings.botWeixin")}</option>
                          </select>
                        </label>
                        <label>
                          <span>{t("settings.botRouteChatType")}</span>
                          <select
                            className="mem-select"
                            value={route.chatType}
                            disabled={busy}
                            onChange={(event) => {
                              updateRoute(index, { chatType: event.target.value });
                              void persistRoute(index, { chatType: event.target.value });
                            }}
                          >
                            {BOT_ROUTE_CHAT_TYPES.map((chatType) => (
                              <option key={chatType || "any"} value={chatType}>{t(`settings.botRouteChatType.${chatType || "any"}` as DictKey)}</option>
                            ))}
                          </select>
                        </label>
                        <label>
                          <span>{t("settings.botRouteChatId")}</span>
                          <input
                            className="mem-input"
                            value={route.chatId}
                            disabled={busy}
                            spellCheck={false}
                            onChange={(event) => updateRoute(index, { chatId: event.target.value })}
                            onBlur={(event) => void persistRoute(index, { chatId: event.currentTarget.value })}
                          />
                        </label>
                        <label>
                          <span>{t("settings.botRouteUserId")}</span>
                          <input
                            className="mem-input"
                            value={route.userId}
                            disabled={busy}
                            spellCheck={false}
                            onChange={(event) => updateRoute(index, { userId: event.target.value })}
                            onBlur={(event) => void persistRoute(index, { userId: event.currentTarget.value })}
                          />
                        </label>
                        <label>
                          <span>{t("settings.botRouteThreadId")}</span>
                          <input
                            className="mem-input"
                            value={route.threadId}
                            disabled={busy}
                            spellCheck={false}
                            onChange={(event) => updateRoute(index, { threadId: event.target.value })}
                            onBlur={(event) => void persistRoute(index, { threadId: event.currentTarget.value })}
                          />
                        </label>
                      </div>
                      <div className="bot-route-grid bot-route-grid--outputs">
                        <label>
                          <span>{t("settings.botWorkspaceRoot")}</span>
                          <input
                            className="mem-input"
                            value={route.workspaceRoot}
                            disabled={busy}
                            placeholder={t("settings.botWorkspaceRootPlaceholder")}
                            spellCheck={false}
                            onChange={(event) => updateRoute(index, { workspaceRoot: event.target.value })}
                            onBlur={(event) => void persistRoute(index, { workspaceRoot: event.currentTarget.value })}
                          />
                        </label>
                        <label>
                          <span>{t("settings.botChannelModel")}</span>
                          <ModelPicker
                            s={s}
                            refs={refs}
                            value={toRef(route.model, s)}
                            disabled={busy}
                            emptyOptionLabel={t("settings.botChannelModelAuto")}
                            emptyOptionHint={settingsModelMeta(s, t)}
                            onPick={(model) => void persistRoute(index, { model })}
                          />
                        </label>
                        <label>
                          <span>{t("settings.botToolApprovalMode")}</span>
                          <select
                            className="mem-select"
                            value={route.toolApprovalMode}
                            disabled={busy}
                            onChange={(event) => {
                              updateRoute(index, { toolApprovalMode: event.target.value });
                              void persistRoute(index, { toolApprovalMode: event.target.value });
                            }}
                          >
                            {BOT_TOOL_APPROVAL_MODES.map((mode) => (
                              <option key={mode || "inherit"} value={mode}>{t(`settings.botToolApprovalMode.${mode || "inherit"}` as DictKey)}</option>
                            ))}
                          </select>
                        </label>
                      </div>
                    </div>
                  ))}
                </div>
              )}
              <button type="button" className="btn btn--secondary btn--small bot-route-add" disabled={busy} onClick={addRoute}>
                {t("settings.botAddRoute")}
              </button>
            </div>
          </details>
        </div>
      </details>
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

function botAccessEntryCount(access: BotAccessView): number {
  return [
    ...asArray(access.users),
    ...asArray(access.groups),
    ...asArray(access.approvers),
    ...asArray(access.admins),
  ].filter((value) => value.trim()).length;
}

function botAccessReady(access: BotAccessView): boolean {
  if (access.allowAll || access.pairingEnabled) return true;
  if (!access.enabled) return false;
  return botAccessEntryCount(access) > 0;
}

function botInstallTargetMatchesConnection(target: BotOfficialInstallTarget, connection: BotConnectionView): boolean {
  if (target === "weixin") return connection.provider === "weixin";
  if (target === "lark") return connection.provider === "feishu" && connection.domain === "lark";
  return connection.provider === "feishu" && connection.domain !== "lark";
}

function botInstallTargetForConnection(connection: BotConnectionView): BotInstallTarget {
  if (connection.provider === "weixin") return "weixin";
  if (connection.provider === "feishu" && connection.domain === "lark") return "lark";
  if (connection.provider === "qq") return "qq";
  return "feishu";
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

function BotListInput({
  label,
  value,
  disabled,
  placeholder,
  onChange,
  onBlur,
}: {
  label: ReactNode;
  value: string;
  disabled: boolean;
  placeholder: string;
  onChange: (value: string) => void;
  onBlur: (value: string) => void;
}) {
  return (
    <label className="bot-list-input">
      <span>{label}</span>
      <textarea
        className="mem-input bot-list-input__textarea"
        value={value}
        disabled={disabled}
        placeholder={placeholder}
        spellCheck={false}
        onChange={(event) => onChange(event.target.value)}
        onBlur={(event) => onBlur(event.currentTarget.value)}
      />
    </label>
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
    queueMode: normalizeBotQueueMode(bot.queueMode),
    queueCap: Math.max(0, Math.floor(bot.queueCap || 0)),
    queueDrop: normalizeBotQueueDrop(bot.queueDrop),
    selfUserIds: {
      qq: uniqueStrings(bot.selfUserIds.qq.map((v) => v.trim())),
      feishu: uniqueStrings(bot.selfUserIds.feishu.map((v) => v.trim())),
      weixin: uniqueStrings(bot.selfUserIds.weixin.map((v) => v.trim())),
    },
    control: {
      enabled: bot.control.enabled,
      addr: bot.control.addr.trim(),
      tokenEnv: bot.control.tokenEnv.trim(),
    },
    pairing: {
      enabled: bot.pairing.enabled,
      requestTtlMinutes: Math.max(0, Math.floor(bot.pairing.requestTtlMinutes || 0)),
      maxPendingPerPlatform: Math.max(0, Math.floor(bot.pairing.maxPendingPerPlatform || 0)),
    },
    routes: bot.routes.map(normalizeBotRoute).filter(botRouteHasValue),
    allowlist: {
      ...bot.allowlist,
      qqUsers: uniqueStrings(bot.allowlist.qqUsers.map((v) => v.trim())),
      feishuUsers: uniqueStrings(bot.allowlist.feishuUsers.map((v) => v.trim())),
      weixinUsers: uniqueStrings(bot.allowlist.weixinUsers.map((v) => v.trim())),
      qqApprovers: uniqueStrings(bot.allowlist.qqApprovers.map((v) => v.trim())),
      feishuApprovers: uniqueStrings(bot.allowlist.feishuApprovers.map((v) => v.trim())),
      weixinApprovers: uniqueStrings(bot.allowlist.weixinApprovers.map((v) => v.trim())),
      qqAdmins: uniqueStrings(bot.allowlist.qqAdmins.map((v) => v.trim())),
      feishuAdmins: uniqueStrings(bot.allowlist.feishuAdmins.map((v) => v.trim())),
      weixinAdmins: uniqueStrings(bot.allowlist.weixinAdmins.map((v) => v.trim())),
      qqGroups: uniqueStrings(bot.allowlist.qqGroups.map((v) => v.trim())),
      feishuGroups: uniqueStrings(bot.allowlist.feishuGroups.map((v) => v.trim())),
      weixinGroups: uniqueStrings(bot.allowlist.weixinGroups.map((v) => v.trim())),
    },
    qq: {
      ...bot.qq,
      appId: bot.qq.appId.trim(),
      appSecretEnv: bot.qq.appSecretEnv.trim(),
      model: bot.qq.model.trim(),
      toolApprovalMode: normalizeBotToolApprovalMode(bot.qq.toolApprovalMode),
      workspaceRoot: bot.qq.workspaceRoot.trim(),
      access: sanitizeBotAccess(bot.qq.access),
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
    connections: bot.connections.map((conn) => ({ ...normalizeBotConnection(conn), access: sanitizeBotAccess(conn.access) })).filter((conn) => conn.id && conn.provider),
  };
}

function sanitizeBotAccess(access: BotAccessView): BotAccessView {
  const normalized = normalizeBotAccess(access);
  return {
    ...normalized,
    users: uniqueStrings(normalized.users.map((v) => v.trim()).filter(Boolean)),
    groups: uniqueStrings(normalized.groups.map((v) => v.trim()).filter(Boolean)),
    approvers: uniqueStrings(normalized.approvers.map((v) => v.trim()).filter(Boolean)),
    admins: uniqueStrings(normalized.admins.map((v) => v.trim()).filter(Boolean)),
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
  const agent = s.agent ?? { temperature: 0, maxSteps: 0, plannerMaxSteps: 0, maxSubagentDepth: 2, systemPrompt: "", coldResumePrune: true, reasoningLanguage: "auto" };
  const subagentDepth = Number.isFinite(agent.maxSubagentDepth) && agent.maxSubagentDepth <= 1 ? 1 : 2;
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

            <SettingsField label={t("settings.subagentDepth")} hint={t("settings.subagentDepthHint")}>
              <div className="provider-add-segmented" role="group" aria-label={t("settings.subagentDepth")}>
                {[1, 2].map((depth) => (
                  <button
                    key={depth}
                    type="button"
                    className={subagentDepth === depth ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
                    disabled={busy}
                    aria-pressed={subagentDepth === depth}
                    onClick={() => void apply(() => app.SetMaxSubagentDepth(depth))}
                  >
                    {depth === 1 ? t("settings.subagentDepthOne") : t("settings.subagentDepthTwo")}
                  </button>
                ))}
              </div>
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

export function ModelPicker({
  s,
  refs,
  value,
  disabled,
  includeSameDefault = false,
  ariaLabel,
  emptyOptionLabel,
  emptyOptionHint,
  onPick,
}: {
  s: SettingsView;
  refs: string[];
  value: string;
  disabled: boolean;
  includeSameDefault?: boolean;
  ariaLabel?: string;
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
        aria-label={ariaLabel}
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
  const [revealedProvider, setRevealedProvider] = useState<string | null>(null);
  const [fetchingProvider, setFetchingProvider] = useState<string | null>(null);
  const [fetchResults, setFetchResults] = useState<Record<string, ProviderFetchResult>>({});
  const [modelDrafts, setModelDrafts] = useState<Record<string, ProviderModelDraft>>({});
  const visibleProviders = useMemo(() => s.providers.filter((p) => p.added || p.name === revealedProvider), [s.providers, revealedProvider]);
  const groups = useMemo(() => providerAccessGroups(visibleProviders, t), [visibleProviders, t]);

  useEffect(() => {
    if (revealedProvider && !s.providers.some((p) => p.name === revealedProvider)) {
      setRevealedProvider(null);
      if (editing === revealedProvider) setEditing(null);
    }
  }, [editing, revealedProvider, s.providers]);

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
        await app.SaveProviderKey(apiKeyEnv, value);
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
            providerPresets={s.providerPresets}
            busy={busy}
            onMode={setAdding}
            onCancel={() => setAdding(null)}
            onAddOfficial={(kind, key) => apply(() => app.AddOfficialProviderAccess(kind, key)).then(() => setAdding(null))}
            onAddPreset={(id, key) => apply(() => app.AddProviderPresetAccess(id, key)).then(() => setAdding(null))}
            onViewPresetConflict={(providerName) => {
              setRevealedProvider(providerName);
              setEditing(providerName);
              setAdding(null);
            }}
            onResetPreset={(id) => apply(() => app.ResetProviderPresetAccess(id)).then(() => setAdding(null))}
            onAddCustom={(pv, key) => apply(() => key ? app.SaveProviderWithKey(pv, key) : app.SaveProvider(pv)).then(() => setAdding(null))}
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
            onSave={(pv, key) => apply(() => key ? app.SaveProviderWithKey(pv, key) : app.SaveProvider(pv)).then(() => {
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
            onDelete={(p) => apply(() => app.RemoveProviderAccess(p.name)).then(() => {
              if (revealedProvider === p.name) {
                setRevealedProvider(null);
                setEditing(null);
              }
            })}
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

type ProviderTemplateChoice =
  | { id: string; source: "official"; kind: OfficialProviderKind; label: string; description: string; keyEnv: string; added: boolean; keySet: boolean }
  | { id: string; source: "preset"; presetID: string; label: string; description: string; keyEnv: string; added: boolean; status: ProviderPresetStatus; statusProviderNames: string[]; keySet: boolean };

function providerTemplateCanAdd(choice: ProviderTemplateChoice | undefined): boolean {
  if (!choice) return false;
  if (choice.source === "official") return !choice.added;
  return choice.status !== "installed" && choice.status !== "installed_modified" && choice.status !== "name_conflict";
}

function providerTemplateStatusBadge(choice: ProviderTemplateChoice, t: ReturnType<typeof useT>): string {
  if (choice.source === "official") return choice.added ? t("settings.addProvider.addedBadge") : "";
  if (choice.status === "installed") return t("settings.addProvider.addedBadge");
  if (choice.status === "installed_modified") return t("settings.addProvider.modifiedBadge");
  if (choice.status === "name_conflict") return t("settings.addProvider.nameConflictBadge");
  if (choice.status === "similar_existing") return t("settings.addProvider.similarExistingBadge");
  return "";
}

function providerTemplateActionLabel(choice: ProviderTemplateChoice | undefined, t: ReturnType<typeof useT>): string {
  if (!choice) return t("settings.addProvider.confirm");
  if (choice.source === "preset" && choice.status === "name_conflict") return t("settings.addProvider.nameConflictAction");
  if (!providerTemplateCanAdd(choice)) return t("settings.addProvider.alreadyAddedAction");
  return t("settings.addProvider.confirm");
}

function providerTemplateStatusClass(choice: ProviderTemplateChoice): string {
  if (choice.source !== "preset" || choice.status === "available") return "";
  return ` provider-template-card--${choice.status.split("_").join("-")}`;
}

function providerTemplateConflictProviderName(choice: ProviderTemplateChoice): string {
  if (choice.source !== "preset" || (choice.status !== "name_conflict" && choice.status !== "installed_modified")) return "";
  return choice.statusProviderNames[0] ?? "";
}

function providerPresetDescription(preset: ProviderPresetView, t: ReturnType<typeof useT>): string {
  switch (preset.id) {
    case "longcat-openai":
      return t("settings.addProvider.preset.longcatOpenAIDesc");
    case "longcat-anthropic":
      return t("settings.addProvider.preset.longcatAnthropicDesc");
    case "kimi-cn":
      return t("settings.addProvider.preset.kimiCnDesc");
    case "kimi-global":
      return t("settings.addProvider.preset.kimiGlobalDesc");
    case "kimi-coding-plan":
      return t("settings.addProvider.preset.kimiCodingPlanDesc");
    case "mimo-api":
      return t("settings.addProvider.preset.mimoApiDesc");
    case "mimo-anthropic":
      return t("settings.addProvider.preset.mimoAnthropicDesc");
    case "mimo-token-plan-cn":
      return t("settings.addProvider.preset.mimoTokenPlanCnDesc");
    case "mimo-token-plan-cn-anthropic":
      return t("settings.addProvider.preset.mimoTokenPlanCnAnthropicDesc");
    case "mimo-token-plan-sgp":
      return t("settings.addProvider.preset.mimoTokenPlanSgpDesc");
    case "mimo-token-plan-sgp-anthropic":
      return t("settings.addProvider.preset.mimoTokenPlanSgpAnthropicDesc");
    case "mimo-token-plan-ams":
      return t("settings.addProvider.preset.mimoTokenPlanAmsDesc");
    case "mimo-token-plan-ams-anthropic":
      return t("settings.addProvider.preset.mimoTokenPlanAmsAnthropicDesc");
    case "minimax-cn-api":
      return t("settings.addProvider.preset.minimaxCnApiDesc");
    case "minimax-global-api":
      return t("settings.addProvider.preset.minimaxGlobalApiDesc");
    case "minimax-cn-anthropic":
      return t("settings.addProvider.preset.minimaxCnAnthropicDesc");
    case "minimax-global-anthropic":
      return t("settings.addProvider.preset.minimaxGlobalAnthropicDesc");
    case "glm-cn":
      return t("settings.addProvider.preset.glmCnDesc");
    case "zai-global":
      return t("settings.addProvider.preset.zaiGlobalDesc");
    case "glm-coding-plan-cn":
      return t("settings.addProvider.preset.glmCodingPlanCnDesc");
    case "glm-coding-plan-cn-anthropic":
      return t("settings.addProvider.preset.glmCodingPlanCnAnthropicDesc");
    case "zai-coding-plan-global":
      return t("settings.addProvider.preset.zaiCodingPlanGlobalDesc");
    case "zai-coding-plan-global-anthropic":
      return t("settings.addProvider.preset.zaiCodingPlanGlobalAnthropicDesc");
    case "opencode-go":
      return t("settings.addProvider.preset.opencodeGoDesc");
    case "opencode-go-anthropic":
      return t("settings.addProvider.preset.opencodeGoAnthropicDesc");
    case "opencode-zen-anthropic":
      return t("settings.addProvider.preset.opencodeZenAnthropicDesc");
    case "qwen-cn":
      return t("settings.addProvider.preset.qwenCnDesc");
    case "qwen-global":
      return t("settings.addProvider.preset.qwenGlobalDesc");
    case "qwen-coding-plan-cn":
      return t("settings.addProvider.preset.qwenCodingPlanCnDesc");
    case "qwen-coding-plan-cn-anthropic":
      return t("settings.addProvider.preset.qwenCodingPlanCnAnthropicDesc");
    case "qwen-coding-plan-global":
      return t("settings.addProvider.preset.qwenCodingPlanGlobalDesc");
    case "qwen-coding-plan-global-anthropic":
      return t("settings.addProvider.preset.qwenCodingPlanGlobalAnthropicDesc");
    case "stepfun":
      return t("settings.addProvider.preset.stepfunDesc");
    case "stepfun-anthropic":
      return t("settings.addProvider.preset.stepfunAnthropicDesc");
    case "novita":
      return t("settings.addProvider.preset.novitaDesc");
    case "gmi":
      return t("settings.addProvider.preset.gmiDesc");
    case "vercel-ai-gateway":
      return t("settings.addProvider.preset.vercelAiGatewayDesc");
    case "huggingface":
      return t("settings.addProvider.preset.huggingfaceDesc");
    case "nvidia":
      return t("settings.addProvider.preset.nvidiaDesc");
    case "kilocode":
      return t("settings.addProvider.preset.kilocodeDesc");
    case "ollama-cloud":
      return t("settings.addProvider.preset.ollamaCloudDesc");
    default:
      return preset.description;
  }
}

function AddProviderPanel({
  mode,
  kinds,
  providerPresets,
  busy,
  onMode,
  onCancel,
  onAddOfficial,
  onAddPreset,
  onViewPresetConflict,
  onResetPreset,
  onAddCustom,
}: {
  mode: AddProviderMode;
  kinds: string[];
  providerPresets: ProviderPresetView[];
  busy: boolean;
  onMode: (mode: AddProviderMode) => void;
  onCancel: () => void;
  onAddOfficial: (kind: OfficialProviderKind, key: string) => Promise<void>;
  onAddPreset: (id: string, key: string) => Promise<void>;
  onViewPresetConflict: (providerName: string) => void;
  onResetPreset: (id: string) => Promise<void>;
  onAddCustom: (p: ProviderView, key?: string) => void | Promise<void>;
}) {
  const t = useT();
  const templateChoices = useMemo<ProviderTemplateChoice[]>(() => [
    ...OFFICIAL_PROVIDER_CHOICES.map((choice) => ({
      id: `official:${choice.kind}`,
      source: "official" as const,
      kind: choice.kind,
      label: t(choice.labelKey),
      description: t(choice.descKey),
      keyEnv: choice.keyEnv,
      added: false,
      keySet: false,
    })),
    ...providerPresets.map((preset) => ({
      id: `preset:${preset.id}`,
      source: "preset" as const,
      presetID: preset.id,
      label: preset.label,
      description: providerPresetDescription(preset, t),
      keyEnv: preset.keyEnv,
      added: preset.added,
      status: normalizeProviderPresetStatus(preset.status, preset.added),
      statusProviderNames: asArray(preset.statusProviderNames),
      keySet: preset.keySet,
    })),
  ], [providerPresets, t]);
  const [templateID, setTemplateID] = useState("official:deepseek");
  const [key, setKey] = useState("");
  const firstAvailableTemplateID = templateChoices.find(providerTemplateCanAdd)?.id ?? templateChoices[0]?.id ?? "";
  const selected = templateChoices.find((choice) => choice.id === templateID) ?? templateChoices.find((choice) => choice.id === firstAvailableTemplateID) ?? templateChoices[0];
  useEffect(() => {
    const current = templateChoices.find((choice) => choice.id === templateID);
    if (firstAvailableTemplateID && (!current || (!providerTemplateCanAdd(current) && firstAvailableTemplateID !== templateID))) {
      setTemplateID(firstAvailableTemplateID);
    }
  }, [firstAvailableTemplateID, templateChoices, templateID]);

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
          {templateChoices.map((choice) => {
            const canAdd = providerTemplateCanAdd(choice);
            const badge = providerTemplateStatusBadge(choice, t);
            const conflictProviderName = providerTemplateConflictProviderName(choice);
            if (choice.source === "preset" && (choice.status === "name_conflict" || choice.status === "installed_modified")) {
              return (
                <div
                  key={choice.id}
                  className={`provider-template-card${providerTemplateStatusClass(choice)}`}
                >
                  <strong>
                    {choice.label}
                    {badge ? ` · ${badge}` : ""}
                  </strong>
                  <span>{choice.description}</span>
                  <div className="provider-template-card__actions">
                    <button
                      type="button"
                      className="btn btn--small"
                      disabled={busy || !conflictProviderName}
                      onClick={() => onViewPresetConflict(conflictProviderName)}
                    >
                      {choice.status === "installed_modified" ? t("settings.addProvider.viewPresetProvider") : t("settings.addProvider.viewConflictProvider")}
                    </button>
                    <InlineConfirmButton
                      label={t("settings.addProvider.resetPreset")}
                      confirmLabel={t("settings.addProvider.confirmResetPreset")}
                      cancelLabel={t("common.cancel")}
                      disabled={busy}
                      danger
                      onConfirm={() => onResetPreset(choice.presetID)}
                    />
                  </div>
                </div>
              );
            }
            return (
              <button
                key={choice.id}
                type="button"
                className={`provider-template-card${selected?.id === choice.id ? " provider-template-card--active" : ""}${providerTemplateStatusClass(choice)}`}
                disabled={busy || !canAdd}
                onClick={() => setTemplateID(choice.id)}
              >
                <strong>
                  {choice.label}
                  {badge ? ` · ${badge}` : ""}
                </strong>
                <span>{choice.description}</span>
              </button>
            );
          })}
        </div>
        <label className="set-label">{t("settings.providerKeyOptional")}</label>
        <input
          className="mem-input"
          type="password"
          placeholder={selected ? t("settings.setKey", { env: selected.keyEnv }) : ""}
          value={key}
          disabled={busy || !providerTemplateCanAdd(selected)}
          onChange={(e) => setKey(e.target.value)}
        />
        <div className="prov-card__actions">
          <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn--primary btn--small"
            disabled={busy || !providerTemplateCanAdd(selected)}
            onClick={() => {
              if (!providerTemplateCanAdd(selected)) return;
              if (selected.source === "official") void onAddOfficial(selected.kind, key.trim());
              else void onAddPreset(selected.presetID, key.trim());
            }}
          >
            {providerTemplateActionLabel(selected, t)}
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
  onSave: (p: ProviderView, key?: string) => void | Promise<void>;
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
    qqApprovers: allowlist.qqApprovers.join("\n"),
    feishuApprovers: allowlist.feishuApprovers.join("\n"),
    weixinApprovers: allowlist.weixinApprovers.join("\n"),
    qqAdmins: allowlist.qqAdmins.join("\n"),
    feishuAdmins: allowlist.feishuAdmins.join("\n"),
    weixinAdmins: allowlist.weixinAdmins.join("\n"),
    qqGroups: allowlist.qqGroups.join("\n"),
    feishuGroups: allowlist.feishuGroups.join("\n"),
    weixinGroups: allowlist.weixinGroups.join("\n"),
  };
}

function botSelfUserTextValues(selfUserIds: BotSettingsView["selfUserIds"]): Record<BotSelfUserTextKey, string> {
  return {
    qq: selfUserIds.qq.join("\n"),
    feishu: selfUserIds.feishu.join("\n"),
    weixin: selfUserIds.weixin.join("\n"),
  };
}

function parseBotListInput(value: string): string[] {
  return uniqueStrings(value
    .split(/[\n,，]+/)
    .map((entry) => entry.trim())
    .filter(Boolean));
}

const ProviderEditorModelPicker = memo(function ProviderEditorModelPicker({
  candidates,
  selectedModels,
  visionModels,
  disabled,
  onToggleModel,
  onToggleVision,
  onSelectAll,
  onClear,
}: {
  candidates: string[];
  selectedModels: string[];
  visionModels: string[];
  disabled: boolean;
  onToggleModel: (model: string) => void;
  onToggleVision: (model: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQuery(query), 150);
    return () => clearTimeout(timer);
  }, [query]);
  if (candidates.length === 0) return null;
  const selected = new Set(selectedModels);
  const vision = new Set(visionModels);
  const q = debouncedQuery.trim().toLowerCase();
  const visibleCandidates = q
    ? candidates.filter((model) => model.toLowerCase().includes(q))
    : candidates;
  return (
    <div className="provider-model-draft provider-model-draft--inline">
      <div className="provider-model-draft__head">
        <div>
          <div className="provider-card-block__label">{t("settings.modelCandidates")}</div>
          <span>{t("settings.modelCandidatesSelected", { n: selectedModels.length })}</span>
        </div>
        <div className="provider-model-draft__tools">
          <button type="button" className="btn btn--small" disabled={disabled || selectedModels.length === candidates.length} onClick={onSelectAll}>
            {t("settings.selectAllModels")}
          </button>
          <button type="button" className="btn btn--small" disabled={disabled || selectedModels.length === 0} onClick={onClear}>
            {t("settings.clearModelSelection")}
          </button>
        </div>
      </div>
      {candidates.length > 8 && (
        <input
          className="mem-input provider-model-draft__search"
          placeholder={t("settings.modelCandidateSearch")}
          value={query}
          disabled={disabled}
          onChange={(e) => setQuery(e.target.value)}
        />
      )}
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
                  onChange={() => onToggleModel(model)}
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
  onSave: (p: ProviderView, key?: string) => void | Promise<void>;
  onSaveKey?: (apiKeyEnv: string, value: string) => Promise<void>;
  onClearKey?: (apiKeyEnv: string) => Promise<void>;
}) {
  const t = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [kind, setKind] = useState(initial?.kind ?? "openai");
  const [baseUrl, setBaseUrl] = useState(initial?.baseUrl ?? "");
  const [chatUrl, setChatUrl] = useState(initial?.chatUrl ?? "");
  const [fullChatUrl, setFullChatUrl] = useState(Boolean((initial?.chatUrl ?? "").trim()));
  const [models, setModels] = useState((initial?.models ?? []).join(", "));
  const [modelCandidates, setModelCandidates] = useState<string[]>(initial?.models ?? []);
  const [visionModels, setVisionModels] = useState((initial?.visionModels ?? []).join(", "));
  const [visionModelsConfigured, setVisionModelsConfigured] = useState(
    Boolean(initial?.visionModelsConfigured ?? ((initial?.visionModels ?? []).length > 0)),
  );
  const [modelsUrl, setModelsUrl] = useState(initial?.modelsUrl ?? "");
  const [apiKeyEnv, setApiKeyEnv] = useState(initial?.apiKeyEnv ?? "");
  const [headersDraft, setHeadersDraft] = useState(formatProviderHeaders(initial?.headers));
  const [extraBodyDraft, setExtraBodyDraft] = useState(formatProviderExtraBody(initial?.extraBody));
  const [authHeader, setAuthHeader] = useState(Boolean(initial?.authHeader));
  const [keyDraft, setKeyDraft] = useState("");
  const [balanceUrl, setBalanceUrl] = useState(initial?.balanceUrl ?? "");
  // Empty when unset so the placeholder (and its "0 = default" hint) reads instead
  // of a bare "0"; saved back as 0.
  const [ctx, setCtx] = useState(initial?.contextWindow ? String(initial.contextWindow) : "");
  const [reasoningProtocol, setReasoningProtocol] = useState(normalizeReasoningProtocol(initial?.reasoningProtocol));
  const [thinking, setThinking] = useState(normalizeThinkingMode(initial?.thinking));
  const [supportedEfforts] = useState<string[]>(initial?.supportedEfforts ?? []);
  const [defaultEffort] = useState(initial?.defaultEffort ?? "");
  const [fetchingModels, setFetchingModels] = useState(false);
  const [fetchStatus, setFetchStatus] = useState<string | null>(null);
  const [fetchFallback, setFetchFallback] = useState<string | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const builtIn = initial?.builtIn ?? false;
  const isNewCustomProvider = !initial;
  const providerKindChoices = useMemo(() => {
    const choices = uniqueStrings([kind, ...kinds].map((candidate) => candidate.trim()).filter(Boolean));
    return choices.length > 0 ? choices : ["openai"];
  }, [kind, kinds]);
  const effectiveKind = providerEditorEffectiveKind(isNewCustomProvider, kind, providerKindChoices);
  const effectiveBaseUrl = fullChatUrl ? providerBaseURLFromChatURL(chatUrl) : baseUrl.trim();
  const effectiveChatUrl = fullChatUrl ? trimmedURL(chatUrl) : "";
  const effectiveModelsUrl = modelsUrl.trim();
  const effectiveHeaders = parseProviderHeaders(headersDraft);
  const extraBodyParse = useMemo(() => {
    try {
      return { value: parseProviderExtraBody(extraBodyDraft, t), error: "" };
    } catch (e) {
      return { value: {}, error: providerExtraBodyParseError(e, t) };
    }
  }, [extraBodyDraft, t]);
  const effectiveExtraBody = extraBodyParse.value;
  const extraBodyInvalid = Boolean(extraBodyDraft.trim() && extraBodyParse.error);
  const previewChatUrl = providerChatURLPreview(baseUrl, chatUrl, fullChatUrl);

  // Empty supportedEfforts means "use protocol defaults". The simplified
  // provider flow no longer edits these levels directly, but it preserves
  // existing advanced TOML unless the user explicitly disables reasoning.
  const cleanedSupportedEfforts = reasoningProtocol !== "none"
    ? uniqueStrings(
        supportedEfforts
          .map((level) => level.toLowerCase().trim())
          .filter((level) => level && level !== "auto")
      )
    : [];
  const normalizedDefaultEffort = defaultEffort.toLowerCase().trim();
  const cleanDefaultEffort = cleanedSupportedEfforts.includes(normalizedDefaultEffort) ? normalizedDefaultEffort : "";

  const fetchModels = async () => {
    if (extraBodyInvalid) return;
    setFetchingModels(true);
    setFetchStatus(null);
    setFetchFallback(null);
    try {
      const effectiveApiKeyEnv = providerApiKeyEnvForSave(name, apiKeyEnv, keyDraft);
      if (!apiKeyEnv.trim()) setApiKeyEnv(effectiveApiKeyEnv);
      if (keyDraft.trim()) await app.SaveProviderKey(effectiveApiKeyEnv, keyDraft.trim());
      const fetched = await app.FetchProviderModels({
        name: name.trim() || t("settings.newProviderDraftName"),
        builtIn: initial?.builtIn ?? false,
        added: initial?.added ?? true,
        kind: effectiveKind,
        baseUrl: effectiveBaseUrl,
        chatUrl: effectiveChatUrl,
        modelsUrl: effectiveModelsUrl,
        models: [],
        visionModels: [],
        visionModelsConfigured: false,
        default: "",
        apiKeyEnv: effectiveApiKeyEnv,
        headers: effectiveHeaders,
        extraBody: effectiveExtraBody,
        authHeader,
        keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
        balanceUrl: balanceUrl.trim(),
        contextWindow: Number(ctx) || 0,
        reasoningProtocol,
        thinking,
        supportedEfforts: cleanedSupportedEfforts,
        defaultEffort: cleanDefaultEffort,
        modelOverrides: initial?.modelOverrides ?? [],
      });
      if (fetched.length === 0) {
        setFetchFallback(t("settings.fetchModelsManualFallbackEmpty"));
        return;
      }
      setModelCandidates(fetched);
      setModels(fetched.join(", "));
      setVisionModels((current) => {
        const existing = parseProviderListInput(current).filter((model) => fetched.includes(model));
        return uniqueStrings([...existing, ...inferredVisionModels(fetched)]).filter((model) => fetched.includes(model)).join(", ");
      });
      setVisionModelsConfigured(true);
      if (keyDraft.trim()) setKeyDraft("");
      setFetchStatus(t("settings.fetchModelsSuccess", { n: fetched.length }));
    } catch (e) {
      setFetchFallback(providerModelFetchFallbackMessage(e, t));
    } finally {
      setFetchingModels(false);
    }
  };

  const save = async () => {
    if (extraBodyInvalid) return;
    setFetchStatus(null);
    setFetchFallback(null);
    const ms = parseProviderListInput(models);
    const vms = parseProviderListInput(visionModels).filter((model) => ms.includes(model));
    const effectiveApiKeyEnv = providerApiKeyEnvForSave(name, apiKeyEnv, keyDraft);
    const provider: ProviderView = {
      name: name.trim(),
      builtIn: initial?.builtIn ?? false,
      added: initial?.added ?? true,
      kind: effectiveKind,
      baseUrl: effectiveBaseUrl,
      chatUrl: effectiveChatUrl,
      models: ms,
      visionModels: vms,
      visionModelsConfigured: visionModelsConfigured || vms.length > 0,
      default: ms[0] ?? "",
      apiKeyEnv: effectiveApiKeyEnv,
      headers: effectiveHeaders,
      extraBody: effectiveExtraBody,
      authHeader,
      modelsUrl: effectiveModelsUrl,
      keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
      balanceUrl: balanceUrl.trim(),
      contextWindow: Number(ctx) || 0,
      reasoningProtocol,
      thinking,
      supportedEfforts: cleanedSupportedEfforts,
      // Clear the stored default if no levels are selected; the backend's
      // NormalizeEffort would otherwise silently ignore an unsupported value.
      defaultEffort: cleanedSupportedEfforts.length > 0 ? cleanDefaultEffort : "",
      modelOverrides: initial?.modelOverrides ?? [],
    };
    try {
      await onSave(provider, keyDraft.trim() || undefined);
    } catch (e) {
      setFetchFallback(String((e as Error)?.message ?? e));
    }
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
    () => parseProviderListInput(models),
    [models],
  );
  const modelCandidateNames = useMemo(
    () => uniqueStrings([...modelCandidates, ...modelNames]),
    [modelCandidates, modelNames],
  );
  const visionModelNames = useMemo(
    () => parseProviderListInput(visionModels).filter((model) => modelNames.includes(model)),
    [modelNames, visionModels],
  );
  const canFetch = Boolean(name.trim() && effectiveBaseUrl);

  const setModelsFromList = (nextModels: string[]) => {
    setModels(uniqueStrings(nextModels).join(", "));
  };

  const updateManualModels = (value: string) => {
    setModels(value);
    const typedModels = parseProviderListInput(value);
    if (typedModels.length > 0) {
      setModelCandidates((current) => uniqueStrings([...current, ...typedModels]));
    }
  };

  const toggleEditorModel = (model: string) => {
    const selected = new Set(modelNames);
    if (selected.has(model)) {
      selected.delete(model);
      setVisionModels(visionModelNames.filter((candidate) => candidate !== model).join(", "));
    } else {
      selected.add(model);
    }
    setModelsFromList(modelCandidateNames.filter((candidate) => selected.has(candidate)));
    setVisionModelsConfigured(true);
  };

  const toggleEditorVisionModel = (model: string) => {
    if (!modelNames.includes(model)) return;
    const vision = new Set(visionModelNames);
    if (vision.has(model)) vision.delete(model);
    else vision.add(model);
    setVisionModels(modelCandidateNames.filter((candidate) => vision.has(candidate)).join(", "));
    setVisionModelsConfigured(true);
  };

  const selectAllEditorModels = () => {
    setModelsFromList(modelCandidateNames);
    setVisionModels(visionModelNames.filter((model) => modelCandidateNames.includes(model)).join(", "));
    setVisionModelsConfigured(true);
  };

  const clearEditorModels = () => {
    setModels("");
    setVisionModels("");
    setVisionModelsConfigured(true);
  };

  const advancedFields = (
    <details className="provider-editor-advanced" open={advancedOpen} onToggle={(e) => setAdvancedOpen(e.currentTarget.open)}>
      <summary>
        <span className="provider-editor-advanced__title">
          <ChevronDown className="provider-editor-advanced__icon" size={16} aria-hidden="true" />
          {t("settings.providerAdvancedSettings")}
        </span>
        <span className="provider-editor-advanced__hint">
          {advancedOpen ? t("settings.providerAdvancedCollapseHint") : t("settings.providerAdvancedExpandHint")}
        </span>
      </summary>
      <div className="provider-editor-advanced__body">
        <label className="set-label">{t("settings.providerApiKeyEnv")}</label>
        <input
          className="mem-input"
          placeholder={apiKeyEnvFromProviderName(name)}
          value={apiKeyEnv}
          onChange={(e) => setApiKeyEnv(e.target.value)}
        />
        <div className="mem-hint">{t("settings.providerApiKeyEnvHint")}</div>
        <label className="set-label">{t("settings.providerModelsUrl")}</label>
        <input
          className="mem-input"
          placeholder={t("settings.providerModelsUrlPlaceholder")}
          value={modelsUrl}
          onChange={(e) => setModelsUrl(e.target.value)}
        />
        <div className="mem-hint">{t("settings.providerModelsUrlHint")}</div>
        <label className="set-label">{t("settings.providerHeaders")}</label>
        <textarea
          className="mem-textarea provider-headers-textarea"
          placeholder={t("settings.providerHeadersPlaceholder")}
          value={headersDraft}
          onChange={(e) => setHeadersDraft(e.target.value)}
          rows={3}
        />
        <div className="mem-hint">{t("settings.providerHeadersHint")}</div>
        <label className="set-label">{t("settings.providerExtraBody")}</label>
        <textarea
          className="mem-textarea provider-headers-textarea"
          placeholder={t("settings.providerExtraBodyPlaceholder")}
          value={extraBodyDraft}
          onChange={(e) => setExtraBodyDraft(e.target.value)}
          rows={4}
        />
        <div className={`mem-hint${extraBodyInvalid ? " mem-hint--error" : ""}`}>
          {extraBodyInvalid ? extraBodyParse.error : t("settings.providerExtraBodyHint")}
        </div>
        <label className="set-check">
          <input
            type="checkbox"
            checked={authHeader}
            onChange={(e) => setAuthHeader(e.target.checked)}
          />
          {t("settings.providerAuthHeader")}
        </label>
        <div className="mem-hint">{t("settings.providerAuthHeaderHint")}</div>
        <label className="set-label">{t("settings.reasoningProtocol")}</label>
        <select className="mem-select" value={reasoningProtocol} onChange={(e) => setReasoningProtocol(e.target.value)}>
          {REASONING_PROTOCOLS.map((protocol) => (
            <option key={protocol || "auto"} value={protocol}>
              {reasoningProtocolLabel(protocol, t)}
            </option>
          ))}
        </select>
        <div className="mem-hint">{t("settings.reasoningProtocolHint")}</div>
        <label className="set-label">{t("settings.thinkingMode")}</label>
        <select className="mem-select" value={thinking} onChange={(e) => setThinking(normalizeThinkingMode(e.target.value))}>
          {THINKING_MODES.map((mode) => (
            <option key={mode || "auto"} value={mode}>
              {thinkingModeLabel(mode, t)}
            </option>
          ))}
        </select>
        <div className="mem-hint">{t("settings.thinkingModeHint")}</div>
        <label className="set-label">{t("settings.providerBalanceUrl")}</label>
        <input
          className="mem-input"
          placeholder={t("settings.balanceUrlPlaceholder")}
          value={balanceUrl}
          onChange={(e) => setBalanceUrl(e.target.value)}
        />
        <div className="mem-hint">{t("settings.balanceUrlHint")}</div>
        <label className="set-label">{t("settings.providerContextWindow")}</label>
        <input
          className="mem-input"
          inputMode="numeric"
          min={0}
          placeholder={t("settings.contextWindowPlaceholder")}
          type="number"
          value={ctx}
          onChange={(e) => setCtx(e.target.value)}
        />
        <div className="mem-hint">{t("settings.contextWindowHint")}</div>
      </div>
    </details>
  );

  return (
    <div className={`provider-editor${isNewCustomProvider ? " provider-editor--wizard" : ""}`}>
      <label className="set-label">{t("settings.customProviderName")}</label>
      <input className="mem-input" placeholder={t("settings.customProviderNamePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} disabled={!!initial} />
      <label className="set-label">{t("settings.providerProtocol")}</label>
      <select className="mem-select" value={kind} onChange={(e) => setKind(e.target.value)}>
        {providerKindChoices.map((choice) => (
          <option key={choice} value={choice}>
            {providerKindLabel(choice, t)}
          </option>
        ))}
      </select>
      <div className="mem-hint">{providerKindHint(effectiveKind, t)}</div>
      <div className="set-row">
        <label className="set-label set-grow">
          {t(fullChatUrl ? "settings.providerChatUrlLabel" : "settings.providerBaseUrlLabel")}
        </label>
        <label className="set-check">
          <input
            type="checkbox"
            checked={fullChatUrl}
            onChange={(e) => {
              const checked = e.target.checked;
              setFullChatUrl(checked);
              if (checked && !chatUrl.trim()) {
                setChatUrl(providerChatURLPreview(baseUrl, "", false));
              } else if (!checked && !baseUrl.trim()) {
                setBaseUrl(providerBaseURLFromChatURL(chatUrl));
              }
            }}
          />
          {t("settings.providerUseFullChatUrl")}
        </label>
      </div>
      <input
        className="mem-input"
        placeholder={t(fullChatUrl ? "settings.providerChatUrlPlaceholder" : "settings.providerBaseUrl")}
        value={fullChatUrl ? chatUrl : baseUrl}
        onChange={(e) => {
          const value = e.target.value;
          if (fullChatUrl) {
            setChatUrl(value);
            setBaseUrl(providerBaseURLFromChatURL(value));
          } else {
            setBaseUrl(value);
          }
        }}
      />
      <div className="mem-hint">
        {previewChatUrl ? t("settings.providerRequestPreview", { url: previewChatUrl }) : t("settings.providerRequestPreviewEmpty")}
      </div>
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
          disabled={busy || fetchingModels || !canFetch || extraBodyInvalid}
          onClick={() => void fetchModels()}
        >
          {fetchingModels ? t("settings.fetchingModels") : t("settings.testFetchModels")}
        </button>
        <span>{t("settings.testFetchModelsHint")}</span>
      </div>
      {fetchStatus && <div className="provider-fetch-status provider-fetch-status--ok">{fetchStatus}</div>}
      {fetchFallback && <div className="provider-fetch-status provider-fetch-status--warn">{fetchFallback}</div>}
      <label className="set-label">{t("settings.manualModels")}</label>
      <input className="mem-input" placeholder={t("settings.providerModels")} value={models} onChange={(e) => updateManualModels(e.target.value)} />
      <div className="mem-hint">{t("settings.manualModelsHint")}</div>
      <ProviderEditorModelPicker
        candidates={modelCandidateNames}
        selectedModels={modelNames}
        visionModels={visionModelNames}
        disabled={busy || fetchingModels}
        onToggleModel={toggleEditorModel}
        onToggleVision={toggleEditorVisionModel}
        onSelectAll={selectAllEditorModels}
        onClear={clearEditorModels}
      />
      {advancedFields}
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={() => void save()} disabled={busy || !name.trim() || !effectiveBaseUrl || !models.trim() || extraBodyInvalid}>
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
      const hooks = parseHooksJSON(raw, view?.events ?? [], t);
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

function parseHooksJSON(raw: string, validEvents: string[], t: ReturnType<typeof useT>): HookConfigView[] {
  const trimmed = raw.trim();
  if (!trimmed) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (e) {
    throw new Error(String((e as Error)?.message ?? e));
  }
  if (Array.isArray(parsed)) {
    return parsed.map((item) => normalizeHookConfig(parseHookArrayItem(item, validEvents, t))).filter((h) => h.event);
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error(t("settings.hooksJsonExpectedObjectArray"));
  }
  const obj = parsed as Record<string, unknown>;
  const hooksValue = obj.hooks && typeof obj.hooks === "object" && !Array.isArray(obj.hooks) ? obj.hooks : obj;
  return flattenHooksMap(hooksValue as Record<string, unknown>, validEvents, t);
}

function parseHookArrayItem(item: unknown, validEvents: string[], t: ReturnType<typeof useT>): HookConfigView {
  if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error(t("settings.hooksJsonItemObject"));
  const obj = item as Record<string, unknown>;
  const event = stringField(obj, "event") || "PreToolUse";
  if (validEvents.length > 0 && !validEvents.includes(event)) throw new Error(t("settings.hooksJsonUnknownEvent", { event }));
  return {
    event,
    match: stringField(obj, "match"),
    command: stringField(obj, "command"),
    description: stringField(obj, "description"),
    timeout: numberField(obj, "timeout"),
    cwd: stringField(obj, "cwd"),
  };
}

function flattenHooksMap(hooks: Record<string, unknown>, validEvents: string[], t: ReturnType<typeof useT>): HookConfigView[] {
  const valid = new Set(validEvents);
  const out: HookConfigView[] = [];
  for (const [event, value] of Object.entries(hooks)) {
    if (valid.size > 0 && !valid.has(event)) throw new Error(t("settings.hooksJsonUnknownEvent", { event }));
    const items = Array.isArray(value) ? value : [value];
    for (const item of items) {
      if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error(t("settings.hooksJsonEventItemObject", { event }));
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

function effectiveShellLabel(value: string, t: ReturnType<typeof useT>): string {
  switch (value) {
    case "git-bash": return t("settings.effectiveShellGitBash");
    case "pwsh": return t("settings.effectiveShellPwsh");
    case "powershell": return t("settings.effectiveShellPowershell");
    case "bash": return t("settings.effectiveShellBash");
    case "auto": return t("common.auto");
    default: return value.trim() || t("common.none");
  }
}

function SandboxSection({ s, busy, apply, windows }: SectionProps & { windows: boolean }) {
  const t = useT();
  const sb = s.sandbox;
  const [root, setRoot] = useState(sb.workspaceRoot);
  const effectiveWriteRoots = asArray(sb.effectiveWriteRoots).filter((path) => String(path).trim());
  const effectiveShell = effectiveShellLabel(String(sb.effectiveShell || sb.shell || ""), t);
  const set = (next: Partial<typeof sb>) =>
    apply(() => app.SetSandbox(next.bash ?? sb.bash, next.network ?? sb.network, next.workspaceRoot ?? sb.workspaceRoot, next.allowWrite ?? sb.allowWrite, next.shell ?? sb.shell));
  const reload = () => apply(() => app.ReloadSettings());

  return (
    <SettingsSection
      title={t("settings.sandboxTitle")}
      description={t("settings.sandboxBoundaryHint")}
      actions={
        <Tooltip label={t("settings.reloadSessionConfigHint")}>
          <button className="btn btn--small" disabled={busy} title={t("settings.reloadSessionConfigHint")} onClick={() => void reload()}>
            <RefreshCw size={14} aria-hidden="true" />
            <span>{t("settings.reloadSessionConfig")}</span>
          </button>
        </Tooltip>
      }
    >
      <SettingsField label={t("settings.shellInterpreter")}>
        <select className="mem-select set-grow" value={sb.shell || "auto"} disabled={busy} onChange={(e) => void set({ shell: e.target.value })}>
          <option value="auto">{windows ? t("settings.shellAutoWindows") : t("settings.shellAuto")}</option>
          <option value="bash">{t("settings.shellBash")}</option>
          <option value="powershell">{t("settings.shellPowershell")}</option>
          <option value="pwsh">{t("settings.shellPwsh")}</option>
        </select>
      </SettingsField>
      <SettingsField label={t("settings.effectiveShell")}>
        <div className="settings-readonly-field">{effectiveShell}</div>
      </SettingsField>
      <SettingsField label={t("settings.bashSandbox")}>
        {/* Windows force-resolves bash to "off" (see config.BashModeForGOOS), so
            offering enforce there would silently snap back on save. */}
        <select className="mem-select set-grow" value={sb.bash} disabled={busy || windows} onChange={(e) => void set({ bash: e.target.value })}>
          <option value="enforce" disabled={windows}>{t("settings.bashEnforce")}</option>
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
      <SettingsField label={t("settings.effectiveWriteRoots")} hint={t("settings.effectiveWriteRootsHint")} stacked>
        <div className="set-rules set-rules--readonly">
          <div className="set-rules__chips">
            {effectiveWriteRoots.length === 0 && <span className="mem-empty">{t("settings.noEffectiveWriteRoots")}</span>}
            {effectiveWriteRoots.map((path, index) => (
              <span className="set-rule set-rule--path" key={`${path}-${index}`}>
                {path}
              </span>
            ))}
          </div>
        </div>
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
  showDisplayZoom,
  zoomPct,
  fontFamily,
  monoFontFamily,
  customFontName,
  customMonoFontName,
  onTheme,
  onThemeStyle,
  onTextSize,
  onRestartZoom,
  onFontFamily,
  onMonoFontFamily,
  onCustomFontNameChange,
  onCustomMonoFontNameChange,
}: {
  theme: Theme;
  themeStyle: ThemeStyle;
  textSize: TextSize;
  showDisplayZoom: boolean;
  zoomPct: number;
  fontFamily: FontFamily;
  monoFontFamily: MonoFontFamily;
  customFontName: string;
  customMonoFontName: string;
  onTheme: (t: Theme) => void;
  onThemeStyle: (style: ThemeStyle) => void;
  onTextSize: (size: TextSize) => void;
  onRestartZoom: (zoom: ZoomLevel) => Promise<void>;
  onFontFamily: (font: FontFamily) => void;
  onMonoFontFamily: (font: MonoFontFamily) => void;
  onCustomFontNameChange: (name: string) => void;
  onCustomMonoFontNameChange: (name: string) => void;
}) {
  const t = useT();
  const themeOptions: Theme[] = ["auto", "light", "dark"];
  const availableFontFamilies = useMemo(() => getAvailableFontFamilies(fontFamily), [fontFamily]);
  const availableMonoFontFamilies = useMemo(() => getAvailableMonoFontFamilies(monoFontFamily), [monoFontFamily]);
  const zoomMinPct = zoomToPercent(MIN_ZOOM);
  const zoomMaxPct = zoomToPercent(MAX_ZOOM);
  const zoomStepPct = Math.round(ZOOM_STEP * 100);
  const zoomProgressPct = Math.min(100, Math.max(0, ((zoomPct - zoomMinPct) / (zoomMaxPct - zoomMinPct)) * 100));
  const canDecreaseZoom = zoomPct > zoomMinPct;
  const canIncreaseZoom = zoomPct < zoomMaxPct;
  const setZoomPercent = (pct: number) => {
    void onRestartZoom(pct / 100);
  };
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
      {showDisplayZoom && (
        <SettingsField label={t("settings.displayZoom")}>
          <div className="zoom-slider-wrap">
            <div className="zoom-slider__head">
              <div className="zoom-slider__value">{zoomPct}%</div>
              <div className="zoom-stepper">
                <button
                  type="button"
                  className="zoom-stepper__btn"
                  aria-label={t("settings.displayZoomDecrease")}
                  title={t("settings.displayZoomDecrease")}
                  disabled={!canDecreaseZoom}
                  onClick={() => setZoomPercent(zoomPct - zoomStepPct)}
                >
                  <Minus size={13} aria-hidden="true" />
                </button>
                <button
                  type="button"
                  className="zoom-stepper__reset"
                  aria-label={t("settings.displayZoomReset")}
                  title={t("settings.displayZoomReset")}
                  disabled={zoomPct === zoomToPercent(DEFAULT_ZOOM)}
                  onClick={() => { void onRestartZoom(DEFAULT_ZOOM); }}
                >
                  <RotateCcw size={12} aria-hidden="true" />
                  <span>100%</span>
                </button>
                <button
                  type="button"
                  className="zoom-stepper__btn"
                  aria-label={t("settings.displayZoomIncrease")}
                  title={t("settings.displayZoomIncrease")}
                  disabled={!canIncreaseZoom}
                  onClick={() => setZoomPercent(zoomPct + zoomStepPct)}
                >
                  <Plus size={13} aria-hidden="true" />
                </button>
              </div>
            </div>
            <div className="zoom-slider-row">
              <span className="zoom-slider__label">{zoomMinPct}%</span>
              <div className="slider-track">
                <div className="slider-track__bg" />
                <div
                  className="slider-track__fill"
                  style={{ width: `calc(${zoomProgressPct}% + 15px)` }}
                />
                <div className="slider-thumb" style={{ left: `${zoomProgressPct}%` }} />
                <input
                  aria-label={t("settings.displayZoom")}
                  type="range"
                  min={zoomMinPct}
                  max={zoomMaxPct}
                  step={zoomStepPct}
                  value={zoomPct}
                  onChange={(e) => setZoomPercent(Number(e.target.value))}
                />
              </div>
              <span className="zoom-slider__label">{zoomMaxPct}%</span>
            </div>
          </div>
        </SettingsField>
      )}
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
