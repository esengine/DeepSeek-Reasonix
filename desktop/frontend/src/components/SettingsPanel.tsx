import { useEffect, useState } from "react";
import { app } from "../lib/bridge";
import { useI18n, useT } from "../lib/i18n";
import { useUpdater } from "../lib/useUpdater";
import { applyTheme, getTheme, type Theme } from "../lib/theme";
import type { ProviderView, SettingsView } from "../lib/types";
import { ResizableDrawer } from "./ResizableDrawer";

type SettingsTab = "models" | "providers" | "permissions" | "sandbox" | "agent" | "appearance" | "updates";

const SETTINGS_TABS: SettingsTab[] = ["models", "providers", "permissions", "sandbox", "agent", "appearance", "updates"];

// SettingsPanel is the desktop settings surface, aligning with Claude Code's
// settings: model & providers (incl. API keys), permissions, sandbox, agent
// params, and appearance. Every change writes reasonix.toml (or .env for keys)
// through the kernel's config edit API and rebuilds the controller live.
export function SettingsPanel({ onClose, onChanged }: { onClose: () => void; onChanged: () => void }) {
  const t = useT();
  const [s, setS] = useState<SettingsView | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [theme, setThemeState] = useState<Theme>(getTheme());
  const [tab, setTab] = useState<SettingsTab>("models");

  const reload = async () => setS(await app.Settings().catch(() => null));
  useEffect(() => {
    void reload();
  }, []);

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
          <button className="chip" onClick={onClose} title={t("common.close")}>
            ✕
          </button>
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
                {tab === "models" && <ModelsSection s={s} busy={busy} apply={apply} onManageProviders={() => setTab("providers")} />}
                {tab === "providers" && <ProvidersSection s={s} busy={busy} apply={apply} />}
                {tab === "permissions" && <PermissionsSection s={s} busy={busy} apply={apply} />}
                {tab === "sandbox" && <SandboxSection s={s} busy={busy} apply={apply} />}
                {tab === "agent" && <AgentSection s={s} busy={busy} apply={apply} />}
                {tab === "appearance" && (
                  <AppearanceSection
                    theme={theme}
                    onTheme={(t) => {
                      applyTheme(t);
                      setThemeState(t);
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
    case "providers":
      return t("settings.tab.providers");
    case "permissions":
      return t("settings.tab.permissions");
    case "sandbox":
      return t("settings.tab.sandbox");
    case "agent":
      return t("settings.tab.agent");
    case "appearance":
      return t("settings.tab.appearance");
    case "updates":
      return t("settings.tab.updates");
  }
}

function settingsTabMeta(id: SettingsTab, s: SettingsView, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "models":
      return toRef(s.defaultModel, s) || t("common.none");
    case "providers":
      return t("settings.providerCount", { n: s.providers.length });
    case "permissions":
      return s.permissions.mode;
    case "sandbox":
      return s.sandbox.bash;
    case "agent":
      return t("settings.agentMeta", { temp: s.agent.temperature, steps: s.agent.maxSteps || "∞" });
    case "appearance":
      return t("settings.appearanceMeta");
    case "updates":
      return t("settings.updatesMeta");
  }
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

function ModelsSection({ s, busy, apply, onManageProviders }: SectionProps & { onManageProviders: () => void }) {
  const t = useT();
  const refs = allRefs(s);
  const defaultRef = toRef(s.defaultModel, s);
  const plannerRef = toRef(s.plannerModel, s);
  const [defaultProvider, defaultModel] = defaultRef.split("/");

  return (
    <section className="mem-section">
      <div className="mem-section__head">
        <div className="mem-section__title">{t("settings.tab.models")}</div>
        <button className="btn btn--small" onClick={onManageProviders}>
          {t("settings.manageProviders")}
        </button>
      </div>

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

      <div className="settings-model-card">
        <div>
          <span>{t("settings.activeProvider")}</span>
          <strong>{defaultProvider || t("common.none")}</strong>
          <small>{defaultModel || defaultRef || t("common.none")}</small>
        </div>
        <div>
          <span>{t("settings.plannerStatus")}</span>
          <strong>{plannerRef ? t("settings.plannerEnabled") : t("settings.plannerDisabled")}</strong>
          <small>{plannerRef || t("settings.plannerNone")}</small>
        </div>
      </div>

      <div className="settings-summary-grid">
        <div className="settings-summary">
          <span>{t("settings.providers")}</span>
          <strong>{s.providers.length}</strong>
        </div>
        <div className="settings-summary">
          <span>{t("settings.availableModels")}</span>
          <strong>{refs.length}</strong>
        </div>
      </div>
    </section>
  );
}

function ProvidersSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  // The provider backing the default model — can't be deleted (would dangle the
  // default). default_model may be a provider name or a "provider/model" ref.
  const defaultProvider = toRef(s.defaultModel, s).split("/")[0];
  const [editing, setEditing] = useState<string | null>(null); // provider name, or "__new__"

  return (
    <section className="mem-section">
      <div className="mem-section__head">
        <div className="mem-section__title">{t("settings.tab.providers")}</div>
        {editing !== "__new__" && (
          <button className="btn btn--small" disabled={busy} onClick={() => setEditing("__new__")}>
            {t("settings.addProvider")}
          </button>
        )}
      </div>

      <div className="provider-list">
        {s.providers.map((p) =>
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
                <span className="prov-card__name">{p.name}</span>
                <span className={`badge ${p.keySet ? "badge--project" : "badge--feedback"}`}>
                  {p.keySet ? t("settings.keySet") : t("settings.noKey")}
                </span>
                <span className="prov-card__spacer" />
                <button className="btn btn--small" disabled={busy} onClick={() => setEditing(p.name)}>
                  {t("common.edit")}
                </button>
                <button
                  className="btn btn--small"
                  disabled={busy || defaultProvider === p.name}
                  title={defaultProvider === p.name ? t("settings.cantDeleteDefault") : t("settings.deleteProvider")}
                  onClick={() => void apply(() => app.DeleteProvider(p.name))}
                >
                  {t("common.delete")}
                </button>
              </div>
              <div className="prov-card__meta">
                <span>{p.kind}</span>
                <span>{p.baseUrl}</span>
                <span>{p.models.join(", ")}</span>
              </div>
              <KeyField apiKeyEnv={p.apiKeyEnv} busy={busy} onSet={(v) => apply(() => app.SetProviderKey(p.apiKeyEnv, v))} />
            </div>
          ),
        )}
      </div>

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
  const [models, setModels] = useState((initial?.models ?? []).join(", "));
  const [apiKeyEnv, setApiKeyEnv] = useState(initial?.apiKeyEnv ?? "");
  const [balanceUrl, setBalanceUrl] = useState(initial?.balanceUrl ?? "");
  // Empty when unset so the placeholder (and its "0 = default" hint) reads instead
  // of a bare "0"; saved back as 0.
  const [ctx, setCtx] = useState(initial?.contextWindow ? String(initial.contextWindow) : "");

  // Offer the kinds the kernel actually registered; if the stored kind is a
  // legacy/unknown one, keep it as an option so editing doesn't silently change it.
  const kindOptions = kind && !kinds.includes(kind) ? [kind, ...kinds] : kinds;

  const save = () => {
    const ms = models
      .split(",")
      .map((m) => m.trim())
      .filter(Boolean);
    onSave({
      name: name.trim(),
      kind: kind.trim() || kinds[0] || "openai",
      baseUrl: baseUrl.trim(),
      models: ms,
      default: ms[0] ?? "",
      apiKeyEnv: apiKeyEnv.trim(),
      keySet: initial?.keySet ?? false,
      balanceUrl: balanceUrl.trim(),
      contextWindow: Number(ctx) || 0,
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
      <input className="mem-input" placeholder={t("settings.providerModels")} value={models} onChange={(e) => setModels(e.target.value)} />
      <input className="mem-input" placeholder={t("settings.providerApiKeyEnv")} value={apiKeyEnv} onChange={(e) => setApiKeyEnv(e.target.value)} />
      <label className="set-label">{t("settings.providerBalanceUrl")}</label>
      <input className="mem-input" placeholder={t("settings.balanceUrlPlaceholder")} value={balanceUrl} onChange={(e) => setBalanceUrl(e.target.value)} />
      <div className="mem-hint">{t("settings.balanceUrlHint")}</div>
      <label className="set-label">{t("settings.providerContextWindow")}</label>
      <input className="mem-input" placeholder={t("settings.contextWindowPlaceholder")} value={ctx} onChange={(e) => setCtx(e.target.value)} inputMode="numeric" />
      <div className="mem-hint">{t("settings.contextWindowHint")}</div>
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

function KeyField({ apiKeyEnv, busy, onSet }: { apiKeyEnv: string; busy: boolean; onSet: (v: string) => Promise<void> }) {
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
            <button className="set-rule__x" disabled={busy} onClick={() => void onRemove(r)} title={t("common.delete")}>
              ✕
            </button>
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

function AgentSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const [temp, setTemp] = useState(String(s.agent.temperature));
  const [steps, setSteps] = useState(String(s.agent.maxSteps));
  const [prompt, setPrompt] = useState(s.agent.systemPrompt);
  const dirty = temp !== String(s.agent.temperature) || steps !== String(s.agent.maxSteps) || prompt !== s.agent.systemPrompt;

  return (
    <section className="mem-section">
      <div className="mem-section__title">{t("settings.agent")}</div>
      <div className="set-row">
        <label className="set-label">{t("settings.temperature")}</label>
        <input className="mem-input set-narrow" value={temp} onChange={(e) => setTemp(e.target.value)} disabled={busy} inputMode="decimal" />
        <label className="set-label">{t("settings.maxSteps")}</label>
        <input className="mem-input set-narrow" value={steps} onChange={(e) => setSteps(e.target.value)} disabled={busy} inputMode="numeric" />
        <span className="mem-hint">{t("settings.unlimited")}</span>
      </div>
      <div className="set-rules__label">{t("settings.systemPrompt")}</div>
      <textarea className="mem-textarea" value={prompt} onChange={(e) => setPrompt(e.target.value)} disabled={busy} spellCheck={false} />
      <div className="prov-card__actions">
        <button
          className="btn btn--primary btn--small"
          disabled={busy || !dirty}
          onClick={() => void apply(() => app.SetAgentParams(Number(temp) || 0, Number(steps) || 0, prompt))}
        >
          {t("settings.saveAgent")}
        </button>
      </div>
    </section>
  );
}

function AppearanceSection({ theme, onTheme }: { theme: Theme; onTheme: (t: Theme) => void }) {
  const { t, pref, setPref } = useI18n();
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
      <div className="set-row">
        <label className="set-label">{t("settings.language")}</label>
        <select className="mem-select set-grow" value={pref} onChange={(e) => setPref(e.target.value as "" | "en" | "zh")}>
          <option value="">{t("settings.langAuto")}</option>
          <option value="zh">中文</option>
          <option value="en">English</option>
        </select>
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
        <div className="mem-hint settings-config-path" title={configPath}>
          {t("settings.config", { path: configPath })}
        </div>
      )}
    </section>
  );
}
