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
    <div className="ml-group mp-group" role="group" aria-label={t("usage.price_by_model")}>
      <div className="ml-head">
        <strong>{t("usage.price_by_model")}</strong>
        <span>{t("usage.price_per_million_tokens_left")}</span>
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
                <input data-action="provider.draft" data-value="model-price-input" aria-label={t("usage.model_input_price", { model })}
                  aria-invalid={incomplete(cur) && cur.inp === "" ? true : undefined} inputMode="decimal" value={cur.inp} placeholder={t("usage.unknown")}
                  onChange={(e) => set(model, { inp: decimal(e.target.value) })} /><i>{t("common.input")}</i>
              </span>
              <span className="unit-field">
                <input data-action="provider.draft" data-value="model-price-output" aria-label={t("usage.model_output_price", { model })}
                  aria-invalid={incomplete(cur) && cur.out === "" ? true : undefined} inputMode="decimal" value={cur.out} placeholder={t("usage.unknown")}
                  onChange={(e) => set(model, { out: decimal(e.target.value) })} /><i>{t("common.output")}</i>
              </span>
              <span className="unit-field">
                <input data-action="provider.draft" data-value="model-price-cache" aria-label={t("usage.model_cache_hit_price", { model })}
                  inputMode="decimal" value={cur.hit} placeholder={t("usage.same_as_input")}
                  onChange={(e) => set(model, { hit: decimal(e.target.value) })} /><i>{t("common.cache_hit")}</i>
              </span>
              <select data-action="provider.draft" data-value="model-price-currency" aria-label={t("usage.model_currency", { model })} value={cur.cur}
                onChange={(e) => set(model, { cur: e.target.value })}>
                {CURRENCIES.map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
              {own && (
                <details className="mp-source" data-model={model}>
                  <summary>{t("usage.source_and_check_date")}</summary>
                  <span className="mp-source-fields">
                    <span className="unit-field mp-src">
                      <input type="text" data-action="provider.draft" data-value="model-price-source" aria-label={t("usage.source_of_the_price_for", { model })}
                        aria-invalid={sourceProblem(cur.src) ? true : undefined} inputMode="url" autoComplete="off" spellCheck={false} value={cur.src}
                        placeholder="https://" onChange={(e) => set(model, { src: e.target.value.trim() })} /><i>{t("common.source")}</i>
                    </span>
                    <span className="unit-field mp-chk">
                      <input type="text" data-action="provider.draft" data-value="model-price-checked" aria-label={t("usage.check_date_of_the_price", { model })}
                        aria-invalid={checkedProblem(cur.chk) ? true : undefined} inputMode="numeric" autoComplete="off" value={cur.chk}
                        placeholder="YYYY-MM-DD" onChange={(e) => set(model, { chk: e.target.value.trim() })} /><i>{t("usage.checked_on")}</i>
                    </span>
                  </span>
                  {sourceProblem(cur.src) && <span className="why">{t("usage.the_source_must_be_an")}</span>}
                  {checkedProblem(cur.chk) && <span className="why">{t("usage.the_check_date_cannot_be")}</span>}
                </details>
              )}
              {own && cur.src !== "" && !sourceProblem(cur.src) && (
                <span className="mp-source-line">
                  <span className="mp-host" title={cur.src}>{hostOf(cur.src)}</span>
                  {cur.chk !== "" && !checkedProblem(cur.chk) && <span className="mp-checked">{t("usage.checked_on_date_utc", { date: cur.chk })}</span>}
                  {onOpenSource && (
                    <button type="button" className="act" data-action="provider.price-source" data-target={model} aria-label={t("usage.open_the_price_source_of", { model })}
                      onClick={() => onOpenSource(cur.src)}>{t("common.open")}</button>
                  )}
                </span>
              )}
              {incomplete(cur) && <span className="why">{t("usage.fill_in_both_input_and")}</span>}
              {foreign && (
                <span className="why mp-foreign">
                  <span>{t("usage.this_price_is_not_in")}</span>
                  {freeCached && <span>{t("usage.the_cache_hit_price_is")}</span>}
                  <button type="button" className="act" data-action="provider.draft" data-value="model-price-restore" aria-label={t("usage.model_restore_official_price", { model })}
                    onClick={() => set(model, { inp: "", out: "", hit: "" })}>{t("usage.restore_official_price")}</button>
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
