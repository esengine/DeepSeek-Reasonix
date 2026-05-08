import { useCallback, useEffect, useState } from "preact/hooks";
import { api } from "../lib/api.js";
import {
  type BudgetState,
  QUICK_CAPS_USD,
  budgetTone,
  bumpSuggestions,
  deriveBudgetState,
} from "../lib/budget.js";
import { html } from "../lib/html.js";
import { type DashboardLang, getLang, setLang, t, useLang } from "../i18n/index.js";

interface SettingsData {
  apiKey?: string | null;
  baseUrl?: string;
  preset?: string;
  reasoningEffort?: string;
  search?: boolean;
  model?: string;
  editMode?: string;
  proNext?: boolean;
  budgetUsd?: number | null;
  /** Cumulative session spend (USD); null when no session is attached. */
  sessionSpendUsd?: number | null;
}

function fmtUsd2(n: number): string {
  return `$${n.toFixed(n < 1 ? 4 : 2)}`;
}

function BudgetGauge({ state }: { state: BudgetState }) {
  if (state.kind === "off") return null;
  const tone = budgetTone(state);
  const fill = Math.min(100, state.pct);
  const valueColor =
    tone === "err"
      ? "color:var(--c-err)"
      : tone === "warn"
        ? "color:var(--c-warn)"
        : "color:var(--fg-1)";
  return html`
    <div style="display:flex;flex-direction:column;gap:6px">
      <div style="display:flex;justify-content:space-between;align-items:baseline;font-size:13px">
        <span style=${valueColor}>
          <strong style="font-family:var(--font-mono)">${fmtUsd2(state.spent)}</strong>
          <span style="color:var(--fg-3)"> ${t("settings.budgetOf")} </span>
          <strong style="font-family:var(--font-mono)">${fmtUsd2(state.cap)}</strong>
        </span>
        <span style=${`font-family:var(--font-mono);font-size:11px;${valueColor}`}>${state.pct.toFixed(1)}%</span>
      </div>
      <div class=${`progress ${tone}`}><div class="progress-fill" style=${`width:${fill}%`}></div></div>
      <span style="color:var(--fg-3);font-size:11px">
        ${
          state.kind === "exhausted"
            ? t("settings.budgetRefusing")
            : state.kind === "warn"
              ? t("settings.budgetWarnLine")
              : t("settings.budgetIdleLine")
        }
      </span>
    </div>
  `;
}

interface BudgetSectionProps {
  state: BudgetState;
  saving: boolean;
  onSetCap: (usd: number) => void;
  onClear: () => void;
}

function BudgetSection({ state, saving, onSetCap, onClear }: BudgetSectionProps) {
  const [custom, setCustom] = useState("");
  const submitCustom = () => {
    const n = Number.parseFloat(custom);
    if (Number.isFinite(n) && n > 0) {
      onSetCap(n);
      setCustom("");
    }
  };

  const quickButtons = (caps: ReadonlyArray<number>) =>
    caps.map(
      (c) => html`
        <button
          key=${c}
          class="btn"
          style="font-family:var(--font-mono)"
          disabled=${saving}
          onClick=${() => onSetCap(c)}
        >$${c}</button>
      `,
    );

  const customField = html`
    <span style="display:inline-flex;align-items:center;gap:4px;margin-left:auto">
      <span style="color:var(--fg-3);font-size:11px">${t("settings.budgetCustom")}</span>
      <input
        type="number"
        min="0.01"
        step="0.01"
        value=${custom}
        placeholder="0.00"
        onInput=${(e: Event) => setCustom((e.target as HTMLInputElement).value)}
        onKeyDown=${(e: KeyboardEvent) => {
          if (e.key === "Enter") submitCustom();
        }}
        style="width:72px;font-family:var(--font-mono)"
        disabled=${saving}
      />
      <button
        class="btn primary"
        disabled=${saving || !(Number.parseFloat(custom) > 0)}
        onClick=${submitCustom}
      >→</button>
    </span>
  `;

  return html`
    <div class="card" style="display:flex;flex-direction:column;gap:12px">
      <${BudgetGauge} state=${state} />

      ${
        state.kind === "off"
          ? html`
              <div>
                <div style="color:var(--fg-3);font-size:11px;margin-bottom:6px">${t("settings.budgetSetCap")}</div>
                <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">
                  ${quickButtons(QUICK_CAPS_USD)}
                  ${customField}
                </div>
              </div>
            `
          : state.kind === "warn" || state.kind === "exhausted"
            ? html`
                <div>
                  <div style="color:var(--fg-3);font-size:11px;margin-bottom:6px">${t("settings.budgetBumpHint")}</div>
                  <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">
                    ${bumpSuggestions(state.cap).map(
                      (next) => html`
                        <button
                          key=${next}
                          class="btn primary"
                          style="font-family:var(--font-mono)"
                          disabled=${saving}
                          onClick=${() => onSetCap(next)}
                        >→ $${next % 1 === 0 ? next : next.toFixed(2)}</button>
                      `,
                    )}
                    ${customField}
                  </div>
                  <div style="margin-top:8px">
                    <button class="btn" disabled=${saving} onClick=${onClear}>${t("settings.budgetClear")}</button>
                  </div>
                </div>
              `
            : html`
                <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">
                  ${bumpSuggestions(state.cap).map(
                    (next) => html`
                      <button
                        key=${next}
                        class="btn"
                        style="font-family:var(--font-mono)"
                        disabled=${saving}
                        onClick=${() => onSetCap(next)}
                      >→ $${next % 1 === 0 ? next : next.toFixed(2)}</button>
                    `,
                  )}
                  ${customField}
                  <button
                    class="btn"
                    style="margin-left:8px"
                    disabled=${saving}
                    onClick=${onClear}
                  >${t("settings.budgetClear")}</button>
                </div>
              `
      }
    </div>
  `;
}

export function SettingsPanel() {
  useLang();
  const [data, setData] = useState<SettingsData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);
  const [draft, setDraft] = useState<Partial<SettingsData>>({});

  const load = useCallback(async () => {
    try {
      const r = await api<SettingsData>("/settings");
      setData(r);
      setDraft({});
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);
  useEffect(() => {
    load();
  }, [load]);

  const save = useCallback(
    async (fields: Partial<SettingsData>) => {
      setSaving(true);
      setError(null);
      try {
        await api("/settings", { method: "POST", body: fields });
        await load();
        setSaved(t("settings.saved", { fields: Object.keys(fields).join(", ") }));
        setTimeout(() => setSaved(null), 3000);
      } catch (err) {
        setError((err as Error).message);
      } finally {
        setSaving(false);
      }
    },
    [load],
  );

  if (!data && !error)
    return html`<div class="card" style="color:var(--fg-3)">${t("settings.loading")}</div>`;
  if (error && !data) return html`<div class="card accent-err">${error}</div>`;
  if (!data) return null;
  const v = data;

  const sectionH3 = (text: string) => html`
    <h3 style="margin:18px 0 8px;font-family:var(--font-mono);font-size:11px;color:var(--fg-3);text-transform:uppercase;letter-spacing:.1em">${text}</h3>
  `;
  const fieldRow = (
    label: string,
    control: unknown,
    note?: string,
  ) => html`
    <div style="display:flex;align-items:center;gap:10px;padding:6px 0">
      <span style="flex:0 0 110px;font-family:var(--font-mono);font-size:11.5px;color:var(--fg-3)">${label}</span>
      <div style="flex:1;display:flex;align-items:center;gap:8px">${control}</div>
      ${note ? html`<span style="color:var(--fg-3);font-size:11px">${note}</span>` : null}
    </div>
  `;

  const currentLang = getLang();

  return html`
    <div style="max-width:760px;display:flex;flex-direction:column;gap:6px">
      ${
        saved ? html`<div><span class="pill ok">${saved}</span></div>` : null
      }
      ${
        error ? html`<div class="card accent-err">${error}</div>` : null
      }

      ${sectionH3(t("settings.sectionLanguage"))}
      <div class="card">
        ${fieldRow(
          t("settings.language"),
          html`
            <select
              value=${currentLang}
              onChange=${(e: Event) => {
                const lang = (e.target as HTMLSelectElement).value as DashboardLang;
                setLang(lang);
              }}
            >
              <option value="en">${t("settings.langEn")}</option>
              <option value="zh-CN">${t("settings.langZhCn")}</option>
            </select>
          `,
        )}
      </div>

      ${sectionH3(t("settings.sectionApi"))}
      <div class="card">
        ${fieldRow(
          t("settings.apiKey"),
          html`<code class="mono" style="color:var(--fg-2);font-size:11.5px">${v.apiKey ?? t("settings.notSet")}</code>`,
        )}
        ${fieldRow(
          t("settings.replace"),
          html`
            <input
              type="password"
              placeholder=${t("settings.pasteKey")}
              value=${draft.apiKey ?? ""}
              onInput=${(e: Event) => setDraft({ ...draft, apiKey: (e.target as HTMLInputElement).value })}
              style="flex:1"
            />
            <button
              class="btn primary"
              disabled=${saving || !(draft.apiKey ?? "").trim()}
              onClick=${() => save({ apiKey: draft.apiKey })}
            >${t("settings.saveKey")}</button>
          `,
        )}
        ${fieldRow(
          t("settings.baseUrl"),
          html`
            <input
              type="text"
              value=${draft.baseUrl ?? v.baseUrl ?? ""}
              placeholder=${t("settings.baseUrlPlaceholder")}
              onInput=${(e: Event) => setDraft({ ...draft, baseUrl: (e.target as HTMLInputElement).value })}
              style="flex:1"
            />
            <button
              class="btn"
              disabled=${saving || (draft.baseUrl ?? v.baseUrl ?? "") === (v.baseUrl ?? "")}
              onClick=${() => save({ baseUrl: draft.baseUrl })}
            >${t("common.save")}</button>
          `,
        )}
      </div>

      ${sectionH3(t("settings.sectionDefaults"))}
      <div class="card">
        ${fieldRow(
          t("settings.preset"),
          html`
            <select
              value=${["auto", "flash", "pro"].includes(v.preset ?? "") ? v.preset : "auto"}
              onChange=${(e: Event) => save({ preset: (e.target as HTMLSelectElement).value })}
              disabled=${saving}
            >
              <option value="auto">${t("settings.presetAuto")}</option>
              <option value="flash">${t("settings.presetFlash")}</option>
              <option value="pro">${t("settings.presetPro")}</option>
            </select>
          `,
          t("settings.appliesNextTurn"),
        )}
        ${fieldRow(
          t("settings.effort"),
          html`
            <select
              value=${v.reasoningEffort}
              onChange=${(e: Event) => save({ reasoningEffort: (e.target as HTMLSelectElement).value })}
              disabled=${saving}
            >
              <option value="max">${t("settings.effortMax")}</option>
              <option value="high">${t("settings.effortHigh")}</option>
            </select>
          `,
          t("settings.appliesNextTurn"),
        )}
        ${fieldRow(
          t("settings.webSearch"),
          html`
            <button
              class=${`btn ${v.search ? "primary" : ""}`}
              onClick=${() => save({ search: !v.search })}
              disabled=${saving}
            >${v.search ? t("common.on") : t("common.off")}</button>
          `,
          t("settings.webSearchNote"),
        )}
      </div>

      ${sectionH3(t("settings.sectionCompute"))}
      <div class="card">
        ${fieldRow(
          t("settings.proNext"),
          html`
            <button
              class=${`btn ${v.proNext ? "primary" : ""}`}
              onClick=${() => save({ proNext: !v.proNext })}
              disabled=${saving}
            >${v.proNext ? t("settings.proArmed") : t("settings.proArm")}</button>
          `,
          t("settings.proNextNote"),
        )}
      </div>

      ${sectionH3(t("settings.sectionBudget"))}
      <${BudgetSection}
        state=${deriveBudgetState(v.budgetUsd, v.sessionSpendUsd)}
        saving=${saving}
        onSetCap=${(usd: number) => save({ budgetUsd: usd })}
        onClear=${() => save({ budgetUsd: null })}
      />

      ${sectionH3(t("settings.sectionRuntime"))}
      <div class="card">
        ${fieldRow(
          t("settings.activeModel"),
          html`<code class="mono">${v.model ?? "—"}</code>`,
        )}
        ${fieldRow(
          t("settings.editMode"),
          html`<code class="mono">${v.editMode}</code>`,
          t("settings.editModeNote"),
        )}
      </div>
    </div>
  `;
}
