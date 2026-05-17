import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { type DashboardLang, getLang, setLang, t, useLang } from "../i18n/index.js";
import { api } from "../lib/api.js";
import {
  type BudgetState,
  QUICK_CAPS_USD,
  budgetTone,
  bumpSuggestions,
  deriveBudgetState,
} from "../lib/budget.js";
import { html } from "../lib/html.js";
import {
  INTERVAL_PRESETS_MS,
  type IntervalUnit,
  type LoopRunStatus,
  formatRemaining,
  parseCustomInterval,
} from "../lib/loop-control.js";

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
  skillPaths?: string[];
}

function fmtUsd2(n: number): string {
  return `$${n.toFixed(n < 1 ? 4 : 2)}`;
}

interface ModelPriceEntry {
  inputCacheHit: number;
  inputCacheMiss: number;
  output: number;
}

interface ModelCatalog {
  models: string[] | null;
  current: string | null;
  pricing: Record<string, ModelPriceEntry>;
}

function formatPricing(p: ModelPriceEntry | undefined): string | null {
  if (!p) return null;
  return t("settings.modelPricingLine", {
    hit: p.inputCacheHit.toFixed(3),
    miss: p.inputCacheMiss.toFixed(3),
    out: p.output.toFixed(3),
  });
}

function ModelRow({
  current,
  catalog,
  saving,
  onPick,
}: {
  current: string;
  catalog: ModelCatalog | null;
  saving: boolean;
  onPick: (model: string) => void;
}) {
  const list = catalog?.models ?? null;
  const ready = list && list.length > 0;
  if (!ready) {
    // Fallback: catalog hasn't loaded (or API failed). Read-only — same as before D-4.
    return html`<code class="mono">${current ?? "—"}</code>`;
  }
  // Ensure the live model is selectable even if the catalog hasn't reported it
  // yet (preset overrides, custom IDs).
  const options = list.includes(current) ? list : [current, ...list];
  const price = catalog?.pricing[current];
  return html`
    <span style="display:inline-flex;flex-direction:column;gap:4px">
      <select
        value=${current}
        onChange=${(e: Event) => {
          const next = (e.target as HTMLSelectElement).value;
          if (next && next !== current) onPick(next);
        }}
        disabled=${saving}
        style="font-family:var(--font-mono);min-width:200px"
      >
        ${options.map((m) => html`<option key=${m} value=${m}>${m}</option>`)}
      </select>
      ${
        price
          ? html`<span style="color:var(--fg-3);font-size:11px;font-family:var(--font-mono)">${formatPricing(price)}</span>`
          : null
      }
    </span>
  `;
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

interface LoopSectionProps {
  status: LoopRunStatus | null;
  /** ms remaining until next fire — ticks down client-side between status polls. */
  remainingMs: number;
  /** Last-turn cost in USD; used as a hint for "each iteration costs ~". */
  avgIterCostUsd: number | null;
  busy: boolean;
  onStart: (intervalMs: number, prompt: string) => void;
  onStop: () => void;
}

function LoopSection({
  status,
  remainingMs,
  avgIterCostUsd,
  busy,
  onStart,
  onStop,
}: LoopSectionProps) {
  const [intervalMs, setIntervalMs] = useState<number>(INTERVAL_PRESETS_MS[1]!.ms);
  const [prompt, setPrompt] = useState("");
  const [customValue, setCustomValue] = useState("");
  const [customUnit, setCustomUnit] = useState<IntervalUnit>("m");

  if (status) {
    return html`
      <div class="card" style="display:flex;flex-direction:column;gap:10px">
        <div style="display:flex;justify-content:space-between;align-items:baseline">
          <span style="color:var(--c-warn);font-family:var(--font-mono);font-size:11px">⟳ ${t("settings.loopRunning")}</span>
          <span style="color:var(--fg-3);font-size:11px">
            ${t("settings.loopIter", { iter: status.iter })} · ${t("settings.loopFiresIn", { remaining: formatRemaining(remainingMs) })}
          </span>
        </div>
        <div style="background:var(--bg-elev-2);border:1px solid var(--bd);border-radius:var(--r);padding:8px 10px;font-family:var(--font-mono);font-size:12px;color:var(--fg-1);white-space:pre-wrap;max-height:120px;overflow-y:auto">${status.prompt}</div>
        <div>
          <button class="btn danger" disabled=${busy} onClick=${onStop}>${t("settings.loopStop")}</button>
        </div>
      </div>
    `;
  }

  const customMs = parseCustomInterval(customValue, customUnit);
  const canStart = !busy && intervalMs > 0 && prompt.trim().length > 0;

  return html`
    <div class="card" style="display:flex;flex-direction:column;gap:10px">
      <div style="color:var(--fg-3);font-size:11px">
        ${t("settings.loopIdleHint")}
        ${
          typeof avgIterCostUsd === "number" && avgIterCostUsd > 0
            ? html` ${t("settings.loopCostHint", { cost: `$${avgIterCostUsd.toFixed(4)}` })}`
            : null
        }
      </div>
      <div style="display:flex;flex-direction:column;gap:6px">
        <span style="color:var(--fg-3);font-size:11px">${t("settings.loopInterval")}</span>
        <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">
          ${INTERVAL_PRESETS_MS.map(
            (p) => html`
              <button
                key=${p.ms}
                class=${`btn ${intervalMs === p.ms && customValue === "" ? "primary" : ""}`}
                style="font-family:var(--font-mono)"
                disabled=${busy}
                onClick=${() => {
                  setIntervalMs(p.ms);
                  setCustomValue("");
                }}
              >${p.label}</button>
            `,
          )}
          <span style="display:inline-flex;align-items:center;gap:4px;margin-left:auto">
            <span style="color:var(--fg-3);font-size:11px">${t("settings.loopCustom")}</span>
            <input
              type="number"
              min="1"
              step="1"
              value=${customValue}
              onInput=${(e: Event) => {
                const raw = (e.target as HTMLInputElement).value;
                setCustomValue(raw);
                const ms = parseCustomInterval(raw, customUnit);
                if (ms !== null) setIntervalMs(ms);
              }}
              style="width:64px;font-family:var(--font-mono)"
              disabled=${busy}
            />
            <select
              value=${customUnit}
              onChange=${(e: Event) => {
                const next = (e.target as HTMLSelectElement).value as IntervalUnit;
                setCustomUnit(next);
                if (customValue) {
                  const ms = parseCustomInterval(customValue, next);
                  if (ms !== null) setIntervalMs(ms);
                }
              }}
              disabled=${busy}
            >
              <option value="s">s</option>
              <option value="m">m</option>
              <option value="h">h</option>
            </select>
          </span>
        </div>
        ${
          customValue && customMs === null
            ? html`<span style="color:var(--c-err);font-size:11px">${t("settings.loopRangeError")}</span>`
            : null
        }
      </div>
      <div style="display:flex;flex-direction:column;gap:6px">
        <span style="color:var(--fg-3);font-size:11px">${t("settings.loopPrompt")}</span>
        <textarea
          rows="3"
          placeholder=${t("settings.loopPromptPlaceholder")}
          value=${prompt}
          onInput=${(e: Event) => setPrompt((e.target as HTMLTextAreaElement).value)}
          style="width:100%;font-family:var(--font-mono);resize:vertical"
          disabled=${busy}
        ></textarea>
      </div>
      <div>
        <button
          class="btn primary"
          disabled=${!canStart}
          onClick=${() => onStart(intervalMs, prompt.trim())}
        >${t("settings.loopStart")}</button>
      </div>
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
  const [catalog, setCatalog] = useState<ModelCatalog | null>(null);
  const [loopStatus, setLoopStatus] = useState<LoopRunStatus | null>(null);
  const [loopAvgCost, setLoopAvgCost] = useState<number | null>(null);
  const [loopBusy, setLoopBusy] = useState(false);
  /** Wall-clock time of the last status sync — used to interpolate the countdown. */
  const lastStatusSyncRef = useRef<number>(0);
  const [now, setNow] = useState<number>(() => Date.now());

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
  useEffect(() => {
    api<ModelCatalog>("/models")
      .then(setCatalog)
      .catch(() => undefined);
  }, []);

  const refreshLoop = useCallback(async () => {
    try {
      const r = await api<{ status: LoopRunStatus | null }>("/loop/status");
      setLoopStatus(r.status);
      lastStatusSyncRef.current = Date.now();
    } catch {
      /* ignore — status is best-effort */
    }
    try {
      const r = await api<{ stats?: { lastTurnCostUsd?: number } }>("/overview");
      setLoopAvgCost(r.stats?.lastTurnCostUsd ?? null);
    } catch {
      /* ignore */
    }
  }, []);
  useEffect(() => {
    let cancelled = false;
    refreshLoop();
    const id = setInterval(() => {
      if (!cancelled) refreshLoop();
    }, 5_000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [refreshLoop]);
  useEffect(() => {
    if (!loopStatus) return;
    const id = setInterval(() => setNow(Date.now()), 1_000);
    return () => clearInterval(id);
  }, [loopStatus]);

  const remainingMs = loopStatus
    ? Math.max(0, loopStatus.nextFireMs - (now - lastStatusSyncRef.current))
    : 0;

  const startLoop = useCallback(
    async (intervalMs: number, prompt: string) => {
      setLoopBusy(true);
      try {
        await api("/loop/start", { method: "POST", body: { intervalMs, prompt } });
        await refreshLoop();
      } catch (err) {
        setError((err as Error).message);
      } finally {
        setLoopBusy(false);
      }
    },
    [refreshLoop],
  );
  const stopLoop = useCallback(async () => {
    setLoopBusy(true);
    try {
      await api("/loop/stop", { method: "POST" });
      await refreshLoop();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoopBusy(false);
    }
  }, [refreshLoop]);

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

  const skillPathsDraft = draft.skillPaths ?? data?.skillPaths ?? [];
  const skillPathsText = Array.isArray(skillPathsDraft)
    ? skillPathsDraft.join(", ")
    : String(skillPathsDraft ?? "");

  if (!data && !error)
    return html`<div class="card" style="color:var(--fg-3)">${t("settings.loading")}</div>`;
  if (error && !data) return html`<div class="card accent-err">${error}</div>`;
  if (!data) return null;
  const v = data;

  const sectionH3 = (text: string) => html`
    <h3 style="margin:18px 0 8px;font-family:var(--font-mono);font-size:11px;color:var(--fg-3);text-transform:uppercase;letter-spacing:.1em">${text}</h3>
  `;
  const fieldRow = (label: string, control: unknown, note?: string) => html`
    <div style="display:flex;align-items:center;gap:10px;padding:6px 0">
      <span style="flex:0 0 110px;font-family:var(--font-mono);font-size:11.5px;color:var(--fg-3)">${label}</span>
      <div style="flex:1;display:flex;align-items:center;gap:8px">${control}</div>
      ${note ? html`<span style="color:var(--fg-3);font-size:11px">${note}</span>` : null}
    </div>
  `;

  const currentLang = getLang();

  return html`
    <div style="max-width:760px;display:flex;flex-direction:column;gap:6px">
      ${saved ? html`<div><span class="pill ok">${saved}</span></div>` : null}
      ${error ? html`<div class="card accent-err">${error}</div>` : null}

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

      ${sectionH3(t("settings.sectionSkills"))}
      <div class="card">
        ${fieldRow(
          t("settings.skillPaths"),
          html`
            <input
              type="text"
              value=${skillPathsText}
              placeholder=${t("settings.skillPathsPlaceholder")}
              onInput=${(e: Event) =>
                setDraft({
                  ...draft,
                  skillPaths: (e.target as HTMLInputElement).value
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean),
                })}
              style="flex:1;font-family:var(--font-mono)"
            />
            <button
              class="btn"
              disabled=${saving || skillPathsText === (v.skillPaths ?? []).join(", ")}
              onClick=${() => save({ skillPaths: draft.skillPaths ?? [] })}
            >${t("common.save")}</button>
          `,
          t("settings.skillPathsNote"),
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

      ${sectionH3(t("settings.sectionLoop"))}
      <${LoopSection}
        status=${loopStatus}
        remainingMs=${remainingMs}
        avgIterCostUsd=${loopAvgCost}
        busy=${loopBusy}
        onStart=${startLoop}
        onStop=${stopLoop}
      />

      ${sectionH3(t("settings.sectionRuntime"))}
      <div class="card">
        ${fieldRow(
          t("settings.activeModel"),
          html`<${ModelRow}
            current=${v.model ?? "—"}
            catalog=${catalog}
            saving=${saving}
            onPick=${(m: string) => save({ model: m })}
          />`,
          t("settings.appliesNextTurn"),
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
