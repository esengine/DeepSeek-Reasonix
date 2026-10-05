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
  models, value, onChange, inherited,
}: {
  models: string[];
  value: Record<string, LimitText>;
  onChange: (next: Record<string, LimitText>) => void;
  inherited: (model: string) => ModelLimit | null | undefined;
}) {
  const set = (model: string, patch: Partial<LimitText>) =>
    onChange({ ...value, [model]: { ...(value[model] ?? { win: "", out: "" }), ...patch } });
  return (
    <div className="ml-group" role="group" aria-label={t("按模型设置限制")}>
      <div className="ml-head">
        <strong>{t("按模型设置限制")}</strong>
        <span>{t("留空的模型沿用上面的值，灰字是它当前继承到的值；填写后只对该模型生效。")}</span>
      </div>
      <ul className="me-list ml-list">
        {models.map((model) => {
          const cur = value[model] ?? { win: "", out: "" };
          const from = inherited(model);
          const own = cur.win !== "" || cur.out !== "";
          return (
            <li key={model} className="ml-row" data-model={model} data-own={own || undefined}>
              <span className="me-name" title={model}>{shortId(model)}</span>
              <span className="unit-field">
                <input
                  data-action="provider.draft" data-value="model-window"
                  aria-label={t("{model} 的上下文窗口", { model })} inputMode="numeric" value={cur.win}
                  placeholder={from?.contextWindow ? t("继承 {n}", { n: from.contextWindow.toLocaleString("en-US") }) : t("继承：未声明")}
                  onChange={(e) => set(model, { win: digits(e.target.value) })}
                /><i>{t("窗口")}</i>
              </span>
              <span className="unit-field">
                <input
                  data-action="provider.draft" data-value="model-max-output"
                  aria-label={t("{model} 的最大输出", { model })} inputMode="numeric" value={cur.out}
                  placeholder={from?.maxOutputTokens && from.maxOutputTokens > 0 ? t("继承 {n}", { n: from.maxOutputTokens.toLocaleString("en-US") }) : t("继承：自动")}
                  onChange={(e) => set(model, { out: digits(e.target.value) })}
                /><i>{t("输出")}</i>
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
