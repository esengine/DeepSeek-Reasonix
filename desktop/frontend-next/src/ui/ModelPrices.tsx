import { t } from "../i18n";
import type { ModelRate } from "../port/port";
import { shortId } from "./ModelLimits";

export interface PriceText {
  inp: string;
  out: string;
  hit: string;
  cur: string;
  src: string;
  chk: string;
}

const CURRENCIES = ["CNY", "USD"] as const;
const blank = (cur = "CNY"): PriceText => ({ inp: "", out: "", hit: "", cur, src: "", chk: "" });
const typed = (n: number) => String(n);

export function priceTextOf(own: ModelRate | undefined): PriceText {
  return own ? { inp: typed(own.input), out: typed(own.output), hit: own.cacheHit === own.input ? "" : typed(own.cacheHit), cur: own.currency || "CNY", src: own.sourceUrl ?? "", chk: own.checked ?? "" } : blank();
}

// Digits and at most one point: a price is not an integer, and a sign or an
// exponent is how a typo becomes a rate nobody meant.
export const decimal = (s: string) => {
  const cleaned = s.replace(/[^\d.]/g, "");
  const at = cleaned.indexOf(".");
  return at < 0 ? cleaned : cleaned.slice(0, at + 1) + cleaned.slice(at + 1).replace(/\./g, "");
};

const parsed = (s: string) => (s === "" || s === "." ? null : Number(s));

// A row is a price once input and output are both typed; one of them alone is a
// half-finished row, which is neither a price nor an empty one.
export const incomplete = (row: PriceText | undefined) => !!row && (row.inp === "") !== (row.out === "");

const MAX_SOURCE = 500;
const utcToday = () => new Date().toISOString().slice(0, 10);

// A source is a link to read: http or https, a host, no credentials, no blanks,
// control or format characters. Nothing here opens or fetches it.
export function sourceProblem(src: string): boolean {
  if (src === "") return false;
  if (src.length > MAX_SOURCE || /[\s\p{Cc}\p{Cf}]/u.test(src)) return true;
  try {
    const u = new URL(src);
    return !((u.protocol === "http:" || u.protocol === "https:") && u.hostname !== "" && u.username === "" && u.password === "");
  } catch {
    return true;
  }
}

// A check date is a calendar date, no later than today in UTC.
export function checkedProblem(chk: string): boolean {
  if (chk === "") return false;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(chk)) return true;
  const d = new Date(`${chk}T00:00:00Z`);
  return Number.isNaN(d.getTime()) || d.toISOString().slice(0, 10) !== chk || chk > utcToday();
}

export const sourceBad = (row: PriceText | undefined) => !!row && (sourceProblem(row.src) || checkedProblem(row.chk));

// What a row sends: the source and date ride with the price they describe.
const provenance = (row: PriceText): Pick<ModelRate, "sourceUrl" | "checked"> => ({
  ...(row.src !== "" ? { sourceUrl: row.src } : {}),
  ...(row.chk !== "" ? { checked: row.chk } : {}),
});

const hostOf = (src: string) => { try { return new URL(src).host; } catch { return ""; } };

// An empty row clears the model's own price; an incomplete one changes nothing.
// A blank cached-input price is the input price: no discount is assumed.
export function pricesToSend(picked: string[], text: Record<string, PriceText>, stored: Record<string, ModelRate>): Record<string, ModelRate | null> {
  const out: Record<string, ModelRate | null> = {};
  for (const model of picked) {
    const row = text[model] ?? blank();
    if (incomplete(row)) {
      if (stored[model]) out[model] = stored[model];
      continue;
    }
    const input = parsed(row.inp);
    const output = parsed(row.out);
    if (input === null || output === null) {
      if (stored[model]) out[model] = null;
      continue;
    }
    out[model] = { input, output, cacheHit: parsed(row.hit) ?? input, currency: row.cur, ...provenance(row) };
  }
  return out;
}

export function ModelPrices({
  models, value, onChange, official = false, stored = {}, onOpenSource, labels,
}: {
  models: string[];
  labels?: Record<string, string>;
  value: Record<string, PriceText>;
  onChange: (next: Record<string, PriceText>) => void;
  official?: boolean;
  stored?: Record<string, ModelRate>;
  onOpenSource?: (url: string) => void;
}) {
  const set = (model: string, patch: Partial<PriceText>) =>
    onChange({ ...value, [model]: { ...(value[model] ?? blank()), ...patch } });
  return (
    <div className="ml-group mp-group" role="group" aria-label={t("按模型设置价格")}>
      <div className="ml-head">
        <strong>{t("按模型设置价格")}</strong>
        <span>{t("每百万 token 的单价。留空表示未知，用量页会如实显示「未知」，而不是 0；填 0 表示免费。缓存命中价留空时按输入价计算。")}</span>
      </div>
      <ul className="me-list ml-list">
        {models.map((model) => {
          const cur = value[model] ?? blank();
          const own = cur.inp !== "" || cur.out !== "";
          const foreign = official && own && stored[model] !== undefined && stored[model]!.official === false;
          const freeCached = foreign && cur.hit !== "" && Number(cur.hit) === 0 && Number(cur.inp) > 0;
          return (
            <li key={model} className="ml-row mp-row" data-model={model} data-own={own || undefined}>
              <span className="me-name" title={model}>{labels?.[model] ?? shortId(model)}</span>
              <span className="unit-field">
                <input data-action="provider.draft" data-value="model-price-input" aria-label={t("{model} 的输入单价", { model })}
                  aria-invalid={incomplete(cur) && cur.inp === "" ? true : undefined} inputMode="decimal" value={cur.inp} placeholder={t("未知")}
                  onChange={(e) => set(model, { inp: decimal(e.target.value) })} /><i>{t("输入")}</i>
              </span>
              <span className="unit-field">
                <input data-action="provider.draft" data-value="model-price-output" aria-label={t("{model} 的输出单价", { model })}
                  aria-invalid={incomplete(cur) && cur.out === "" ? true : undefined} inputMode="decimal" value={cur.out} placeholder={t("未知")}
                  onChange={(e) => set(model, { out: decimal(e.target.value) })} /><i>{t("输出")}</i>
              </span>
              <span className="unit-field">
                <input data-action="provider.draft" data-value="model-price-cache" aria-label={t("{model} 的缓存命中单价", { model })}
                  inputMode="decimal" value={cur.hit} placeholder={t("同输入价")}
                  onChange={(e) => set(model, { hit: decimal(e.target.value) })} /><i>{t("缓存命中")}</i>
              </span>
              <select data-action="provider.draft" data-value="model-price-currency" aria-label={t("{model} 的货币", { model })} value={cur.cur}
                onChange={(e) => set(model, { cur: e.target.value })}>
                {CURRENCIES.map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
              {own && (
                <details className="mp-source" data-model={model}>
                  <summary>{t("来源与核对日期")}</summary>
                  <span className="mp-source-fields">
                    <span className="unit-field mp-src">
                      <input type="text" data-action="provider.draft" data-value="model-price-source" aria-label={t("{model} 的价格来源", { model })}
                        aria-invalid={sourceProblem(cur.src) ? true : undefined} inputMode="url" autoComplete="off" spellCheck={false} value={cur.src}
                        placeholder="https://" onChange={(e) => set(model, { src: e.target.value.trim() })} /><i>{t("来源")}</i>
                    </span>
                    <span className="unit-field mp-chk">
                      <input type="text" data-action="provider.draft" data-value="model-price-checked" aria-label={t("{model} 的核对日期", { model })}
                        aria-invalid={checkedProblem(cur.chk) ? true : undefined} inputMode="numeric" autoComplete="off" value={cur.chk}
                        placeholder="YYYY-MM-DD" onChange={(e) => set(model, { chk: e.target.value.trim() })} /><i>{t("核对日期")}</i>
                    </span>
                  </span>
                  {sourceProblem(cur.src) && <span className="why">{t("来源必须是 http 或 https 链接，不能带用户名密码。")}</span>}
                  {checkedProblem(cur.chk) && <span className="why">{t("核对日期不能晚于今天（按 UTC）。")}</span>}
                </details>
              )}
              {own && cur.src !== "" && !sourceProblem(cur.src) && (
                <span className="mp-source-line">
                  <span className="mp-host" title={cur.src}>{hostOf(cur.src)}</span>
                  {cur.chk !== "" && !checkedProblem(cur.chk) && <span className="mp-checked">{t("核对于 {date}（UTC）", { date: cur.chk })}</span>}
                  {onOpenSource && (
                    <button type="button" className="act" data-action="provider.price-source" data-target={model} aria-label={t("{model} 打开价格来源", { model })}
                      onClick={() => onOpenSource(cur.src)}>{t("打开")}</button>
                  )}
                </span>
              )}
              {incomplete(cur) && <span className="why">{t("输入价和输出价需要都填写，才会保存这一行。")}</span>}
              {foreign && (
                <span className="why mp-foreign">
                  <span>{t("这个价格不在官方价目表里，选了人民币或美元显示时无法换算。")}</span>
                  {freeCached && <span>{t("缓存命中价是 0：命中的 token 会按免费计，费用会偏低。")}</span>}
                  <button type="button" className="act" data-action="provider.draft" data-value="model-price-restore" aria-label={t("{model} 恢复官方价", { model })}
                    onClick={() => set(model, { inp: "", out: "", hit: "" })}>{t("恢复官方价")}</button>
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
