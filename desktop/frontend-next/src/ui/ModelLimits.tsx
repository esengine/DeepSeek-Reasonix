import { t } from "../i18n";
import type { ModelLimit } from "../port/port";

export interface LimitText {
  win: string;
  out: string;
}

// Long ids often differ only at the end, so the middle is what gives way.
export const shortId = (id: string) => (id.length > 16 ? `${id.slice(0, 8)}…${id.slice(-7)}` : id);

export const digits = (s: string) => s.replace(/\D/g, "");

export function limitTextOf(own: ModelLimit | undefined): LimitText {
  return { win: own?.contextWindow ? String(own.contextWindow) : "", out: own?.maxOutputTokens ? String(own.maxOutputTokens) : "" };
}

// A blank field inherits and is never sent as a value. A negative output cap
// stored by hand survives an untouched field, because digits() cannot type one.
export function limitsToSend(picked: string[], text: Record<string, LimitText>, stored: Record<string, ModelLimit>): Record<string, ModelLimit> {
  const out: Record<string, ModelLimit> = {};
  for (const model of picked) {
    const cur = text[model];
    if (!cur) continue;
    const was = stored[model];
    const outText = cur.out === String(was?.maxOutputTokens) ? was.maxOutputTokens : Number(cur.out) || 0;
    const winN = Number(cur.win) || 0;
    if (winN || outText) out[model] = { ...(winN ? { contextWindow: winN } : {}), ...(outText ? { maxOutputTokens: outText } : {}) };
  }
  return out;
}

export function ModelLimits({
  models, value, onChange, inherited, labels,
}: {
  models: string[];
  // How a model reads when its shared prefix is left out; absent draws it as it is.
  labels?: Record<string, string>;
  value: Record<string, LimitText>;
  onChange: (next: Record<string, LimitText>) => void;
  inherited: (model: string) => ModelLimit | null | undefined;
}) {
  const set = (model: string, patch: Partial<LimitText>) =>
    onChange({ ...value, [model]: { ...(value[model] ?? { win: "", out: "" }), ...patch } });
  return (
    <div className="ml-group" role="group" aria-label={t("models.limits_per_model")}>
      <div className="ml-head">
        <strong>{t("models.limits_per_model")}</strong>
        <span>{t("models.a_blank_field_follows_the")}</span>
      </div>
      <ul className="me-list ml-list">
        {models.map((model) => {
          const cur = value[model] ?? { win: "", out: "" };
          const from = inherited(model);
          const own = cur.win !== "" || cur.out !== "";
          return (
            <li key={model} className="ml-row" data-model={model} data-own={own || undefined}>
              <span className="me-name" title={model}>{labels?.[model] ?? shortId(model)}</span>
              <span className="unit-field">
                <input
                  data-action="provider.draft" data-value="model-window"
                  aria-label={t("models.context_window_for_model", { model })} inputMode="numeric" value={cur.win}
                  placeholder={from?.contextWindow ? t("models.inherits_n", { n: from.contextWindow.toLocaleString("en-US") }) : t("models.inherits_not_declared")}
                  onChange={(e) => set(model, { win: digits(e.target.value) })}
                /><i>{t("common.window")}</i>
              </span>
              <span className="unit-field">
                <input
                  data-action="provider.draft" data-value="model-max-output"
                  aria-label={t("models.max_output_for_model", { model })} inputMode="numeric" value={cur.out}
                  placeholder={from?.maxOutputTokens && from.maxOutputTokens > 0 ? t("models.inherits_n", { n: from.maxOutputTokens.toLocaleString("en-US") }) : t("models.inherits_automatic")}
                  onChange={(e) => set(model, { out: digits(e.target.value) })}
                /><i>{t("common.output")}</i>
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
