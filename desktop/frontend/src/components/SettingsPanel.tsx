import { useEffect, useState } from "react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { normalizeLangPref, useI18n, useT, type LangPref } from "../lib/i18n";
import { useUpdater } from "../lib/useUpdater";
import {
  THEME_STYLES,
  applyTheme,
  defaultStyleForTheme,
  getResolvedTheme,
  getTheme,
  getThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  themeForStyle,
  type Theme,
  type ThemeStyle,
} from "../lib/theme";
import { TEXT_SIZES, applyTextSize, getTextSize, type TextSize } from "../lib/textSize";
import { FONT_FAMILIES, applyFontFamily, getFontFamily, type FontFamily } from "../lib/fontFamily";
import type { NetworkView, ProviderView, SettingsView } from "../lib/types";
import { MCPServersSettingsPage, SkillsSettingsPage } from "./CapabilitiesPanel";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { MemorySettingsPage } from "./MemoryPanel";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";

type SettingsTab = "general" | "models" | "mcp" | "skills" | "memory" | "permissions" | "sandbox" | "network" | "appearance" | "updates";

const SETTINGS_TABS: SettingsTab[] = ["general", "models", "mcp", "skills", "memory", "permissions", "sandbox", "network", "appearance", "updates"];

// SettingsPanel is the desktop settings surface, aligning with Claude Code's
// settings: model & providers (incl. API keys), permissions, sandbox, and
// appearance. Every change writes reasonix.toml (or .env for keys)
// through the kernel's config edit API and rebuilds the controller live.
export function SettingsPanel({
  onClose,
  onChanged,
}: {
  onClose: () => void;
  onChanged: () => void;
}) {
  const t = useT();
  const [s, setS] = useState<SettingsView | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [theme, setThemeState] = useState<Theme>(getTheme());
  const [themeStyle, setThemeStyleState] = useState<ThemeStyle>(() => getThemeStyle(getTheme()));
  const [textSize, setTextSizeState] = useState<TextSize>(getTextSize());
  const [fontFamily, setFontFamilyState] = useState<FontFamily>(getFontFamily());
  const [tab, setTab] = useState<SettingsTab>("general");

  const reload = async () => setS(normalizeSettingsView(await app.Settings().catch(() => null)));
  useEffect(() => {
    void reload();
  }, []);
  useEffect(() => {
    if (!s) return;
    const nextTheme = normalizeThemePreference(s.desktopTheme);
    const nextStyle = normalizeThemeStyleForTheme(s.desktopThemeStyle, nextTheme);
    setThemeState(nextTheme);
    setThemeStyleState(nextStyle);
  }, [s?.desktopTheme, s?.desktopThemeStyle]);

  // apply runs a mutation, re-reads settings, and refreshes the topbar/model. A
  // rejected binding (validation / rebuild failure) surfaces as an inline banner.
  const apply = async (fn: () => Promise<void>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      await reload();
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <ResizableDrawer onClose={onClose} wide>
        <header className="drawer__head">
          <div className="drawer__title">{t("settings.title")}</div>
          <Tooltip label={t("common.close")}>
            <button className="chip" onClick={onClose}>
              ✕
            </button>
          </Tooltip>
        </header>

        {!s ? (
          <div className="empty">{t("settings.loading")}</div>
        ) : (
          <div className="drawer__body drawer__body--settings">
            <div className="settings-shell">
              <nav className="settings-nav" aria-label={t("settings.title")}>
                {SETTINGS_TABS.map((id) => (
                  <button
                    key={id}
                    className={`settings-nav__item${tab === id ? " settings-nav__item--active" : ""}`}
                    onClick={() => setTab(id)}
                  >
                    <span>{settingsTabLabel(id, t)}</span>
                    <small>{settingsTabMeta(id, s, t)}</small>
                  </button>
                ))}
              </nav>
              <main className="settings-content">
                {err && <div className="banner banner--error">{err}</div>}
                {tab === "general" && <GeneralSection s={s} busy={busy} apply={apply} />}
                {tab === "models" && <ModelsSection s={s} busy={busy} apply={apply} />}
                {tab === "mcp" && <MCPServersSettingsPage />}
                {tab === "skills" && <SkillsSettingsPage />}
                {tab === "memory" && <MemorySettingsPage />}
                {tab === "permissions" && <PermissionsSection s={s} busy={busy} apply={apply} />}
                {tab === "sandbox" && <SandboxSection s={s} busy={busy} apply={apply} />}
                {tab === "network" && <NetworkSection s={s} busy={busy} apply={apply} />}
                {tab === "appearance" && (
                  <AppearanceSection
                    theme={theme}
                    themeStyle={themeStyle}
                    textSize={textSize}
                    fontFamily={fontFamily}
                    onTheme={(t) => {
                      const nextStyle = themeForStyle(themeStyle) === getResolvedTheme(t) ? themeStyle : defaultStyleForTheme(t);
                      applyTheme(t, nextStyle, { persist: false });
                      setThemeState(t);
                      setThemeStyleState(nextStyle);
                      void apply(() => app.SetDesktopAppearance(t, nextStyle));
                    }}
                    onThemeStyle={(style) => {
                      const nextTheme = themeForStyle(style);
                      applyTheme(nextTheme, style, { persist: false });
                      setThemeState(nextTheme);
                      setThemeStyleState(style);
                      void apply(() => app.SetDesktopAppearance(nextTheme, style));
                    }}
                    onTextSize={(size) => {
                      applyTextSize(size);
                      setTextSizeState(size);
                    }}
                    onFontFamily={(font) => {
                      applyFontFamily(font);
                      setFontFamilyState(font);
                    }}
                  />
                )}
                {tab === "updates" && <UpdatesSection configPath={s.configPath} />}
              </main>
            </div>
          </div>
        )}
    </ResizableDrawer>
  );
}

type SectionProps = {
  s: SettingsView;
  busy: boolean;
  apply: (fn: () => Promise<void>) => Promise<void>;
};

function settingsTabLabel(id: SettingsTab, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "models":
      return t("settings.tab.models");
    case "general":
      return t("settings.tab.general");
    case "mcp":
      return t("settings.tab.mcp");
    case "skills":
      return t("settings.tab.skills");
    case "memory":
      return t("settings.tab.memory");
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

function settingsTabMeta(id: SettingsTab, _s: SettingsView, t: ReturnType<typeof useT>): string {
  return t(`settings.tabSub.${id}`);
}

// allRefs flattens providers into "provider/model" refs for the model selectors.
function allRefs(s: SettingsView): string[] {
  const out: string[] = [];
  for (const p of s.providers) for (const m of p.models) out.push(`${p.name}/${m}`);
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
const PROXY_TYPES = ["http", "https", "socks5", "socks5h"] as const;
const LANGUAGE_PREFS: LangPref[] = ["", "zh", "en"];
const AUTO_PLAN_MODES = ["off", "on"] as const;

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

function normalizeProviderView(p: ProviderView): ProviderView {
  return {
    ...p,
    builtIn: Boolean(p.builtIn),
    added: Boolean(p.added),
    models: asArray(p.models),
    modelsUrl: p.modelsUrl ?? "",
    reasoningProtocol: p.reasoningProtocol ?? "",
    supportedEfforts: asArray(p.supportedEfforts),
    defaultEffort: p.defaultEffort ?? "",
  };
}

function normalizeSettingsView(view: SettingsView | null | undefined): SettingsView | null {
  if (!view) return null;
  const permissions = view.permissions ?? { mode: "ask", allow: [], ask: [], deny: [] };
  const sandbox = view.sandbox ?? { bash: "enforce", network: false, workspaceRoot: "", allowWrite: [] };
  const network = view.network ?? {
    proxyMode: "auto",
    proxyUrl: "",
    noProxy: "",
    proxy: { type: "socks5", server: "", port: 0, username: "", password: "" },
  };
  const agent = view.agent ?? { temperature: 0, maxSteps: 0, systemPrompt: "" };
  return {
    ...view,
    subagentModel: view.subagentModel ?? "",
    subagentEffort: view.subagentEffort ?? "",
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
    autoPlan: normalizeAutoPlan(view.autoPlan),
    desktopLanguage: normalizeLangPref(view.desktopLanguage),
    desktopTheme: normalizeThemePreference(view.desktopTheme),
    desktopThemeStyle: normalizeThemeStyleForTheme(view.desktopThemeStyle, normalizeThemePreference(view.desktopTheme)),
    closeBehavior: normalizeCloseBehavior(view.closeBehavior),
  };
}

type CloseBehavior = "background" | "quit";

function normalizeCloseBehavior(mode: string | undefined): CloseBehavior {
  return mode === "quit" ? "quit" : "background";
}

function closeBehaviorLabel(mode: CloseBehavior, t: ReturnType<typeof useT>): string {
  return mode === "quit" ? t("settings.closeBehavior.quit") : t("settings.closeBehavior.background");
}

function GeneralSection({ s, busy, apply }: SectionProps) {
  const { t, setPref } = useI18n();
  const closeBehavior = normalizeCloseBehavior(s.closeBehavior);
  const autoPlan = normalizeAutoPlan(s.autoPlan);
  const languagePref = normalizeLangPref(s.desktopLanguage);
  const setLanguage = (next: LangPref) => {
    setPref(next);
    void apply(() => app.SetDesktopLanguage(next));
  };
  return (
    <section className="mem-section">
      <div className="mem-section__title">{t("settings.tab.general")}</div>
      <div className="set-row">
        <label className="set-label">{t("settings.language")}</label>
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
      </div>
      <div className="set-row">
        <label className="set-label">{t("settings.closeBehavior")}</label>
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
      </div>
      <div className="set-row">
        <label className="set-label">{t("settings.autoPlan")}</label>
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
      </div>
    </section>
  );
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
    <section className="mem-section">
      <div className="mem-section__title">{t("settings.tab.network")}</div>
      <div className="set-row">
        <label className="set-label">{t("settings.proxyMode")}</label>
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
      </div>

      {draft.proxyMode === "custom" && (
        <>
          <div className="set-row">
            <label className="set-label">{t("settings.proxyType")}</label>
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
          </div>
          <div className="set-row">
            <label className="set-label">{t("settings.proxyServer")}</label>
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
          <div className="set-row">
            <label className="set-label">{t("settings.proxyUsername")}</label>
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
          <div className="set-field">
            <div className="set-row">
              <label className="set-label">{t("settings.proxyUrl")}</label>
              <input
                className="mem-input set-grow"
                placeholder="socks5://127.0.0.1:7890"
                value={draft.proxyUrl}
                disabled={busy}
                onChange={(e) => setDraft({ ...draft, proxyUrl: e.target.value })}
              />
            </div>
            <div className="mem-hint set-hint">{t("settings.proxyUrlHint")}</div>
          </div>
          <div className="set-row">
            <label className="set-label">{t("settings.noProxy")}</label>
            <input
              className="mem-input set-grow"
              placeholder="localhost,127.0.0.1,.local"
              value={draft.noProxy}
              disabled={busy}
              onChange={(e) => setDraft({ ...draft, noProxy: e.target.value })}
            />
          </div>
        </>
      )}

      <div className="prov-card__actions">
        <button
          className="btn btn--primary btn--small"
          disabled={busy || !dirty}
          onClick={() => void apply(() => app.SetNetwork(draft))}
        >
          {t("settings.saveNetwork")}
        </button>
      </div>
    </section>
  );
}

function ModelsSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const [subtab, setSubtab] = useState<"usage" | "access">("usage");
  const refs = allRefs(s);
  const defaultRef = toRef(s.defaultModel, s);
  const plannerRef = toRef(s.plannerModel, s);
  const subagentRef = toRef(s.subagentModel, s);
  const [defaultProvider, defaultModel] = defaultRef.split("/");
  const plannerModeDetail = plannerRef
    ? t("settings.plannerDualDetail", { planner: plannerRef, executor: defaultRef || t("common.none") })
    : t("settings.plannerSingleDetail", { model: defaultRef || t("common.none") });

  return (
    <>
      <div className="set-seg settings-model-subtabs" role="tablist" aria-label={t("settings.tab.models")}>
        <button
          className={`set-seg__btn${subtab === "usage" ? " set-seg__btn--on" : ""}`}
          role="tab"
          aria-selected={subtab === "usage"}
          onClick={() => setSubtab("usage")}
        >
          {t("settings.modelTab.usage")}
        </button>
        <button
          className={`set-seg__btn${subtab === "access" ? " set-seg__btn--on" : ""}`}
          role="tab"
          aria-selected={subtab === "access"}
          onClick={() => setSubtab("access")}
        >
          {t("settings.modelTab.access")}
        </button>
      </div>

      {subtab === "usage" ? (
        <section className="mem-section">
          <div className="mem-section__title">{t("settings.tab.models")}</div>

          <div className="set-row">
            <label className="set-label">{t("settings.defaultModel")}</label>
            <select
              className="mem-select set-grow"
              value={toRef(s.defaultModel, s)}
              disabled={busy}
              onChange={(e) => void apply(() => app.SetDefaultModel(e.target.value))}
            >
              {refs.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>

          <div className="set-row">
            <label className="set-label">{t("settings.plannerModel")}</label>
            <select
              className="mem-select set-grow"
              value={toRef(s.plannerModel, s)}
              disabled={busy}
              onChange={(e) => void apply(() => app.SetPlannerModel(e.target.value))}
            >
              <option value="">{t("settings.plannerNone")}</option>
              {refs.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>

          <div className="set-row">
            <label className="set-label">{t("settings.subagentModel")}</label>
            <select
              className="mem-select set-grow"
              value={subagentRef}
              disabled={busy}
              onChange={(e) => void apply(() => app.SetSubagentModel(e.target.value))}
            >
              <option value="">{t("settings.subagentModelDefault")}</option>
              {refs.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>

          <div className="set-row">
            <label className="set-label">{t("settings.subagentEffort")}</label>
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
          </div>
          <div className="mem-hint">{t("settings.subagentHint")}</div>

          <div className="settings-model-card">
            <div>
              <span>{t("settings.activeProvider")}</span>
              <strong>{defaultProvider || t("common.none")}</strong>
              <small>{defaultModel || defaultRef || t("common.none")}</small>
            </div>
            <div>
              <span>{t("settings.plannerStatus")}</span>
              <strong>{plannerRef ? t("settings.plannerDual") : t("settings.plannerSingle")}</strong>
              <small>{plannerModeDetail}</small>
            </div>
            <div>
              <span>{t("settings.subagentDefaults")}</span>
              <strong>{subagentRef || t("common.auto")}</strong>
              <small>{s.subagentEffort ? `effort ${s.subagentEffort}` : t("common.auto")}</small>
            </div>
          </div>
        </section>
      ) : (
        <ProvidersSection s={s} busy={busy} apply={apply} />
      )}
    </>
  );
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
  const [editing, setEditing] = useState<string | null>(null); // provider name, or "__new__"
  const accessProviders = s.providers.filter((p) => p.added || !p.builtIn);
  const availableOfficial = availableOfficialProviders(s);

  const refreshModels = (p: ProviderView) =>
    apply(async () => {
      const models = await app.FetchProviderModels(p);
      const nextDefault = models.includes(p.default) ? p.default : models[0] || p.default;
      await app.SaveProvider({ ...p, added: true, models, default: nextDefault });
    });

  return (
    <section className="mem-section">
      <div className="mem-section__head">
        <div>
          <div className="mem-section__title">{t("settings.providerAccess")}</div>
          <div className="settings-summary">{t("settings.providerAccessHint")}</div>
        </div>
        {editing !== "__new__" && (
          <button className="btn btn--small" disabled={busy} onClick={() => setEditing("__new__")}>
            {t("settings.addProvider")}
          </button>
        )}
      </div>

      <div className="provider-list">
        {accessProviders.length === 0 && editing !== "__new__" && (
          <div className="mem-empty">{t("settings.providerAccessEmptyTitle")}</div>
        )}
        {accessProviders.map((p) =>
          editing === p.name ? (
            <ProviderEditor
              key={p.name}
              initial={p}
              kinds={s.providerKinds}
              busy={busy}
              onCancel={() => setEditing(null)}
              onSave={(pv) => apply(() => app.SaveProvider(pv)).then(() => setEditing(null))}
            />
          ) : (
            <div className="prov-card" key={p.name}>
              <div className="prov-card__head">
                <span className="prov-card__name">{providerLabel(p, t)}</span>
                <span className={`badge ${p.keySet ? "badge--project" : "badge--feedback"}`}>
                  {p.keySet ? t("settings.keySet") : t("settings.noKey")}
                </span>
                {p.builtIn && <span className="badge">{t("settings.builtinProvider")}</span>}
                <span className="prov-card__spacer" />
                <button className="btn btn--small" disabled={busy} onClick={() => void refreshModels(p)}>
                  {t("settings.fetchModels")}
                </button>
                <button className="btn btn--small" disabled={busy} onClick={() => setEditing(p.name)}>
                  {t("common.edit")}
                </button>
                {defaultProvider === p.name ? (
                  <Tooltip label={t("settings.cantRemoveDefault")}>
                    <button className="btn btn--small" disabled>
                      {t("settings.removeProviderAccess")}
                    </button>
                  </Tooltip>
                ) : (
                  <InlineConfirmButton
                    label={t("settings.removeProviderAccess")}
                    confirmLabel={p.builtIn ? t("settings.confirmRemoveProviderAccess") : t("settings.confirmDeleteProvider")}
                    cancelLabel={t("common.cancel")}
                    disabled={busy}
                    danger
                    onConfirm={() => apply(() => app.RemoveProviderAccess(p.name))}
                  />
                )}
              </div>
              <div className="prov-card__meta">
                <span>{p.kind}</span>
                <span>{p.baseUrl}</span>
                <span>{p.models.join(", ")}</span>
                {p.reasoningProtocol && <span>{t("settings.providerReasoningProtocol")}: {p.reasoningProtocol}</span>}
              </div>
              <KeyField
                apiKeyEnv={p.apiKeyEnv}
                busy={busy}
                canClear={p.keySet}
                onSet={(v) => apply(() => app.SetProviderKey(p.apiKeyEnv, v))}
                onClear={() => apply(() => app.ClearProviderKey(p.apiKeyEnv))}
              />
            </div>
          ),
        )}
      </div>

      {availableOfficial.length > 0 && (
        <div className="provider-list provider-list--compact">
          <div className="mem-section__title">{t("settings.builtinProviders")}</div>
          {availableOfficial.map((p) => (
            <div className="prov-card" key={p.name}>
              <div className="prov-card__head">
                <span className="prov-card__name">{providerLabel(p, t)}</span>
                <span className="badge">{p.models.join(", ")}</span>
                <span className="prov-card__spacer" />
                <button
                  className="btn btn--small"
                  disabled={busy}
                  onClick={() => void apply(() => app.AddOfficialProviderAccess(providerAccessKind(p), ""))}
                >
                  {t("settings.addProviderAccess")}
                </button>
              </div>
              <div className="prov-card__meta">
                <span>{p.baseUrl}</span>
                <span>{p.apiKeyEnv}</span>
              </div>
            </div>
          ))}
        </div>
      )}

      {editing === "__new__" && (
        <ProviderEditor
          kinds={s.providerKinds}
          busy={busy}
          onCancel={() => setEditing(null)}
          onSave={(pv) => apply(() => app.SaveProvider(pv)).then(() => setEditing(null))}
        />
      )}
    </section>
  );
}

function providerAccessKind(p: ProviderView): string {
  if (p.name === "mimo-api") return "mimo-api";
  if (p.name === "mimo-pro" || p.name === "mimo-flash") return "mimo-token-plan";
  return "deepseek";
}

function providerAccessOrder(kind: string): number {
  switch (kind) {
    case "deepseek":
      return 0;
    case "mimo-api":
      return 1;
    case "mimo-token-plan":
      return 2;
    default:
      return 99;
  }
}

function availableOfficialProviders(s: SettingsView): ProviderView[] {
  const addedKinds = new Set(s.providers.filter((p) => p.builtIn && p.added).map(providerAccessKind));
  const seenKinds = new Set<string>();
  return (s.officialProviders.length > 0 ? s.officialProviders : s.providers)
    .filter((p) => p.builtIn && !p.added)
    .sort((a, b) => providerAccessOrder(providerAccessKind(a)) - providerAccessOrder(providerAccessKind(b)))
    .filter((p) => {
      const kind = providerAccessKind(p);
      if (addedKinds.has(kind) || seenKinds.has(kind)) return false;
      seenKinds.add(kind);
      return true;
    });
}

function providerLabel(p: ProviderView, t: ReturnType<typeof useT>): string {
  switch (p.name) {
    case "deepseek-flash":
    case "deepseek-pro":
      return t("settings.providerLabel.deepseek");
    case "mimo-api":
      return t("settings.providerLabel.mimoApi");
    case "mimo-pro":
    case "mimo-flash":
      return t("settings.providerLabel.mimoTokenPlan");
    default:
      return p.name;
  }
}

function ProviderEditor({
  initial,
  kinds,
  busy,
  onCancel,
  onSave,
}: {
  initial?: ProviderView;
  kinds: string[];
  busy: boolean;
  onCancel: () => void;
  onSave: (p: ProviderView) => void;
}) {
  const t = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [kind, setKind] = useState(initial?.kind ?? kinds[0] ?? "openai");
  const [baseUrl, setBaseUrl] = useState(initial?.baseUrl ?? "");
  const [modelsUrl, setModelsUrl] = useState(initial?.modelsUrl ?? "");
  const [models, setModels] = useState((initial?.models ?? []).join(", "));
  const [apiKeyEnv, setApiKeyEnv] = useState(initial?.apiKeyEnv ?? "");
  const [balanceUrl, setBalanceUrl] = useState(initial?.balanceUrl ?? "");
  // Empty when unset so the placeholder (and its "0 = default" hint) reads instead
  // of a bare "0"; saved back as 0.
  const [ctx, setCtx] = useState(initial?.contextWindow ? String(initial.contextWindow) : "");
  const [supportedEfforts, setSupportedEfforts] = useState<string[]>(initial?.supportedEfforts ?? []);
  const [customEffortDraft, setCustomEffortDraft] = useState("");
  const [defaultEffort, setDefaultEffort] = useState(initial?.defaultEffort ?? "");
  const [reasoningProtocol, setReasoningProtocol] = useState(initial?.reasoningProtocol ?? "");

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

  const save = () => {
    const ms = models
      .split(",")
      .map((m) => m.trim())
      .filter(Boolean);
    onSave({
      name: name.trim(),
      builtIn: initial?.builtIn ?? false,
      added: true,
      kind: kind.trim() || kinds[0] || "openai",
      baseUrl: baseUrl.trim(),
      models: ms,
      modelsUrl: modelsUrl.trim(),
      default: ms[0] ?? "",
      apiKeyEnv: apiKeyEnv.trim(),
      keySet: initial?.keySet ?? false,
      balanceUrl: balanceUrl.trim(),
      contextWindow: Number(ctx) || 0,
      reasoningProtocol,
      supportedEfforts,
      // Clear the stored default if no levels are selected; the backend's
      // NormalizeEffort would otherwise silently ignore an unsupported value.
      defaultEffort: supportedEfforts.length > 0 ? defaultEffort : "",
    });
  };

  return (
    <div className="prov-card prov-card--edit">
      <input className="mem-input" placeholder={t("settings.providerName")} value={name} onChange={(e) => setName(e.target.value)} disabled={!!initial} />
      <label className="set-label">{t("settings.providerKind")}</label>
      <select className="mem-select" value={kind} onChange={(e) => setKind(e.target.value)}>
        {kindOptions.map((k) => (
          <option key={k} value={k}>
            {k}
          </option>
        ))}
      </select>
      <input className="mem-input" placeholder={t("settings.providerBaseUrl")} value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
      <input className="mem-input" placeholder={t("settings.providerModelsUrl")} value={modelsUrl} onChange={(e) => setModelsUrl(e.target.value)} />
      <input className="mem-input" placeholder={t("settings.providerModels")} value={models} onChange={(e) => setModels(e.target.value)} />
      <input className="mem-input" placeholder={t("settings.providerApiKeyEnv")} value={apiKeyEnv} onChange={(e) => setApiKeyEnv(e.target.value)} />
      <label className="set-label">{t("settings.providerBalanceUrl")}</label>
      <input className="mem-input" placeholder={t("settings.balanceUrlPlaceholder")} value={balanceUrl} onChange={(e) => setBalanceUrl(e.target.value)} />
      <div className="mem-hint">{t("settings.balanceUrlHint")}</div>
      <label className="set-label">{t("settings.providerContextWindow")}</label>
      <input className="mem-input" placeholder={t("settings.contextWindowPlaceholder")} value={ctx} onChange={(e) => setCtx(e.target.value)} inputMode="numeric" />
      <div className="mem-hint">{t("settings.contextWindowHint")}</div>
      <label className="set-label">{t("settings.providerReasoningProtocol")}</label>
      <select className="mem-select" value={reasoningProtocol} onChange={(e) => setReasoningProtocol(e.target.value)}>
        <option value="">{t("settings.providerReasoningProtocolAuto")}</option>
        <option value="deepseek">deepseek</option>
        <option value="openai">openai</option>
        <option value="none">none</option>
      </select>
      <div className="mem-hint">{t("settings.providerReasoningProtocolHint")}</div>
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
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={save} disabled={busy || !name.trim() || !baseUrl.trim()}>
          {t("common.save")}
        </button>
      </div>
    </div>
  );
}

function KeyField({
  apiKeyEnv,
  busy,
  canClear,
  onSet,
  onClear,
}: {
  apiKeyEnv: string;
  busy: boolean;
  canClear: boolean;
  onSet: (v: string) => Promise<void>;
  onClear: () => Promise<void>;
}) {
  const t = useT();
  const [val, setVal] = useState("");
  if (!apiKeyEnv) return null;
  return (
    <div className="set-key">
      <input
        className="mem-input"
        type="password"
        placeholder={t("settings.setKey", { env: apiKeyEnv })}
        value={val}
        onChange={(e) => setVal(e.target.value)}
      />
      <button
        className="btn btn--small"
        disabled={busy || !val.trim()}
        onClick={() => {
          void onSet(val.trim());
          setVal("");
        }}
      >
        {t("settings.saveKey")}
      </button>
      {canClear && (
        <InlineConfirmButton
          label={t("settings.clearKey")}
          confirmLabel={t("settings.confirmClearKey")}
          cancelLabel={t("common.cancel")}
          disabled={busy}
          danger
          onConfirm={onClear}
        />
      )}
    </div>
  );
}

function PermissionsSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  return (
    <section className="mem-section">
      <div className="mem-section__title">{t("settings.permissions")}</div>
      <div className="set-row">
        <label className="set-label">{t("settings.writerMode")}</label>
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
      </div>
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
      <div className="mem-hint">{t("settings.ruleForm")}</div>
    </section>
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
      <div className="set-rules__label">{list}</div>
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

function SandboxSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const sb = s.sandbox;
  const [root, setRoot] = useState(sb.workspaceRoot);
  const set = (next: Partial<typeof sb>) =>
    apply(() => app.SetSandbox(next.bash ?? sb.bash, next.network ?? sb.network, next.workspaceRoot ?? sb.workspaceRoot, next.allowWrite ?? sb.allowWrite));

  return (
    <section className="mem-section">
      <div className="mem-section__title">{t("settings.sandboxTitle")}</div>
      <div className="set-row">
        <label className="set-label">{t("settings.bashSandbox")}</label>
        <select className="mem-select set-grow" value={sb.bash} disabled={busy} onChange={(e) => void set({ bash: e.target.value })}>
          <option value="enforce">{t("settings.bashEnforce")}</option>
          <option value="off">{t("settings.bashOff")}</option>
        </select>
      </div>
      <label className="set-check">
        <input type="checkbox" checked={sb.network} disabled={busy} onChange={(e) => void set({ network: e.target.checked })} />
        {t("settings.allowNetwork")}
      </label>
      <div className="set-row">
        <label className="set-label">{t("settings.workspaceRoot")}</label>
        <input
          className="mem-input set-grow"
          placeholder={t("settings.workspaceDefault")}
          value={root}
          disabled={busy}
          onChange={(e) => setRoot(e.target.value)}
          onBlur={() => root !== sb.workspaceRoot && void set({ workspaceRoot: root })}
        />
      </div>
      <RuleList
        list="allow_write"
        rules={sb.allowWrite}
        busy={busy}
        onAdd={(d) => set({ allowWrite: [...sb.allowWrite, d] })}
        onRemove={(d) => set({ allowWrite: sb.allowWrite.filter((x) => x !== d) })}
      />
    </section>
  );
}

function AppearanceSection({
  theme,
  themeStyle,
  textSize,
  fontFamily,
  onTheme,
  onThemeStyle,
  onTextSize,
  onFontFamily,
}: {
  theme: Theme;
  themeStyle: ThemeStyle;
  textSize: TextSize;
  fontFamily: FontFamily;
  onTheme: (t: Theme) => void;
  onThemeStyle: (style: ThemeStyle) => void;
  onTextSize: (size: TextSize) => void;
  onFontFamily: (font: FontFamily) => void;
}) {
  const t = useT();
  const themeOptions: Theme[] = ["auto", "light", "dark"];
  return (
    <section className="mem-section">
      <div className="mem-section__title">{t("settings.appearance")}</div>
      <div className="set-row">
        <label className="set-label">{t("settings.theme")}</label>
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
      </div>
      <div className="set-row set-row--stack">
        <label className="set-label">{t("settings.themeStyle")}</label>
        <div className="theme-style-grid">
          {THEME_STYLES.map((opt) => (
            <button
              key={opt}
              className={`theme-style-btn${themeStyle === opt ? " theme-style-btn--on" : ""}`}
              onClick={() => onThemeStyle(opt)}
            >
              <span className="theme-style-swatch" data-theme-style-swatch={opt} />
              <span>{opt}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="set-row">
        <label className="set-label">{t("settings.textSize")}</label>
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
      </div>
      <div className="set-row">
        <label className="set-label">{t("settings.fontFamily")}</label>
        <div className="set-seg">
          {FONT_FAMILIES.map((font) => (
            <button
              key={font}
              className={`set-seg__btn${fontFamily === font ? " set-seg__btn--on" : ""}`}
              onClick={() => onFontFamily(font)}
            >
              {fontFamilyName(font, t)}
            </button>
          ))}
        </div>
      </div>
    </section>
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
  }
}

const MB = 1024 * 1024;
const mb = (n: number) => (n / MB).toFixed(1);

// UpdatesSection is the manual side of the auto-updater: it shows the running
// version and a Check button, then the same state machine the top banner uses
// (useUpdater) — available → install/download, with progress and errors inline.
function UpdatesSection({ configPath }: { configPath: string }) {
  const t = useT();
  const { status, check, apply } = useUpdater();
  const [version, setVersion] = useState("");
  useEffect(() => {
    app.Version().then(setVersion).catch(() => {});
  }, []);

  const busy =
    status.kind === "checking" || status.kind === "downloading" || status.kind === "verifying" || status.kind === "applying";

  return (
    <section className="mem-section">
      <div className="mem-section__title">{t("updater.title")}</div>
      <div className="set-row">
        <label className="set-label">{t("updater.currentVersion", { v: version || "…" })}</label>
        <span className="prov-card__spacer" />
        <button className="btn btn--small" disabled={busy} onClick={() => void check()}>
          {status.kind === "checking" ? t("updater.checking") : t("updater.checkButton")}
        </button>
      </div>
      {status.kind === "upToDate" && <div className="mem-hint">{t("updater.upToDate")}</div>}
      {status.kind === "available" && (
        <>
          <div className="set-row">
            <span className="set-label">{t("updater.available", { v: status.info.latest })}</span>
            <span className="prov-card__spacer" />
            <button className="btn btn--primary btn--small" onClick={() => apply(status.info)}>
              {status.info.canSelfUpdate ? t("updater.installNow") : t("updater.goToDownload")}
            </button>
          </div>
          {!status.info.canSelfUpdate && <div className="mem-hint">{t("updater.macHint")}</div>}
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
      {status.kind === "applying" && <div className="mem-hint">{t("updater.applying")}</div>}
      {status.kind === "done" && <div className="mem-hint">{t("updater.done")}</div>}
      {status.kind === "error" && <div className="banner banner--error">{t("updater.failed", { msg: status.message })}</div>}
      {configPath && (
        <Tooltip label={configPath} fill block className="mem-hint settings-config-path">
          {t("settings.config", { path: configPath })}
        </Tooltip>
      )}
    </section>
  );
}
