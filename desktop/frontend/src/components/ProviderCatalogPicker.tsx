import { SettingsSelect } from "./SettingsSelect";
import { protocolsForCatalog } from "../lib/providerCatalog";
import { RotateCcw } from "lucide-react";
import { providerProtocolLabel } from "../lib/providerProtocol";
import { providerBrandIcons } from "../lib/providerBrandIcons";
import { useId, useMemo, useState } from "react";
import { useT, type DictKey } from "../lib/i18n";
import type { ProviderCatalog } from "../lib/types";

export interface CatalogChoice {
  id: string;
  label?: string;
  catalog: ProviderCatalog;
  keyEnv: string;
  keySet: boolean;
  models: string[];
  canAdd: boolean;
  status: string;
  statusLabel: string;
  actionLabel: string;
  conflictName?: string;
}

export const apiFormatLabel = providerProtocolLabel;

const regionKeys: Record<string, DictKey> = {
  cn: "settings.catalog.regionChina", global: "settings.catalog.regionGlobal",
  sgp: "settings.catalog.regionSingapore", ams: "settings.catalog.regionEurope",
  local: "settings.catalog.regionLocal",
};
const productKeys: Record<string, DictKey> = {
  api: "settings.catalog.productAPI", coding: "settings.catalog.productCoding", token: "settings.catalog.productToken",
  go: "settings.catalog.productGo", zen: "settings.catalog.productZen", local: "settings.catalog.productLocal",
};

export function ProviderCatalogPicker({ choices, busy, onConnect, onView, onReset }: {
  choices: CatalogChoice[];
  busy: boolean;
  onConnect: (id: string, key: string, baseURL?: string, format?: string) => void;
  onView: (name: string) => void;
  onReset: (id: string) => void;
}) {
  const t = useT();
  const uid = useId();
  const [selectedID, setSelectedID] = useState(choices.find(c => c.canAdd)?.id ?? choices[0]?.id ?? "");
  const [keyDraft, setKeyDraft] = useState({ id: "", keyEnv: "", value: "" });
  const [urlDrafts, setUrlDrafts] = useState<Record<string, string>>({});
  const [formatDrafts, setFormatDrafts] = useState<Record<string, string>>({});
  const [query, setQuery] = useState("");
  const [confirmReset, setConfirmReset] = useState(false);
  const selected = choices.find(c => c.id === selectedID) ?? choices[0];
  const brands = useMemo(() => [...new Map(choices.map(c => [c.catalog.brandId,
    c.catalog.brandId === "token-rhythm" ? t("settings.addProvider.preset.tokenRhythmLabel") : c.catalog.brandLabel])).entries()], [choices, t]);
  if (!selected) return null;
  const genericFormat = (value: string) => value === "dashscope-responses" ? "responses" : value;
  // The official DeepSeek connection starts with Chat Completions regardless
  // of which protocol-specific preset supplied the brand's first catalog row.
  const defaultFormat = selected.catalog.brandId === "deepseek" && selected.catalog.product === "api"
    ? "openai" : genericFormat(selected.catalog.format);
  const format = formatDrafts[selected.id] ?? defaultFormat;
  const protocols = protocolsForCatalog(selected.catalog);
  const defaultURL = protocols[format]?.baseUrl ?? selected.catalog.baseUrl ?? "";
  const baseURL = urlDrafts[selected.id] ?? defaultURL;
  let validURL = !selected.catalog.baseUrl;
  try { const u = new URL(baseURL); validURL = (u.protocol === "https:" || u.protocol === "http:") && Boolean(u.hostname) && !u.username && !u.password; } catch { /* invalid draft */ }
  const key = keyDraft.id === selected.id && keyDraft.keyEnv === selected.keyEnv ? keyDraft.value : "";
  const siblings = choices.filter(c => c.catalog.brandId === selected.catalog.brandId);
  const regionChoices = siblings.filter(c => c.catalog.region === selected.catalog.region);
  const formatChoices = regionChoices.filter(c => c.catalog.product === selected.catalog.product);
  const pick = (choice: CatalogChoice | undefined) => {
    if (!choice) return;
    setSelectedID(choice.id);
    setKeyDraft({id: "", keyEnv: "", value: ""});
    setConfirmReset(false);
  };
  const chooseDimension = (dimension: "region" | "product" | "format", value: string) => {
    const candidates = (dimension === "region" ? siblings : dimension === "product" ? regionChoices : formatChoices)
      .filter(c => c.catalog[dimension] === value);
    pick(candidates.find(c => c.catalog.product === selected.catalog.product && c.catalog.format === selected.catalog.format)
      ?? candidates.find(c => c.catalog.format === selected.catalog.format) ?? candidates[0]);
  };
  const field = (dimension: "region" | "product" | "format", label: DictKey, candidates: CatalogChoice[]) => {
    const values = [...new Set(candidates.map(c => c.catalog[dimension]))];
    return <div className="provider-catalog__field">
      <label className="set-label" htmlFor={`${uid}-${dimension}`}>{t(label)}</label>
      <SettingsSelect id={`${uid}-${dimension}`} className="mem-select" disabled={busy || values.length < 2}
        value={selected.catalog[dimension]} onValueChange={value => chooseDimension(dimension, value)}>
        {values.map(value => <option key={value} value={value}>{dimension === "format"
          ? value === "bundle" ? t("settings.catalog.formatBundle") : apiFormatLabel(value)
          : (dimension === "region" ? regionKeys[value] : productKeys[value])
            ? t((dimension === "region" ? regionKeys[value] : productKeys[value])) : value}</option>)}
      </SettingsSelect>
    </div>;
  };
  return <div className="provider-catalog">
    <aside className="provider-catalog__sidebar">
      <input className="mem-input" aria-label={t("settings.catalog.search")} placeholder={t("settings.catalog.search")}
        value={query} onChange={e => setQuery(e.target.value)} />
      <div className="provider-catalog__brands" aria-label={t("settings.catalog.providers")}>
        {brands.filter(([id, label]) => `${id} ${label}`.toLowerCase().includes(query.trim().toLowerCase())).map(([id, label]) =>
          <button key={id} type="button" className="provider-catalog__brand" aria-pressed={selected.catalog.brandId === id}
            disabled={busy} onClick={() => pick(choices.find(c => c.catalog.brandId === id && c.canAdd) ?? choices.find(c => c.catalog.brandId === id))}>
            {providerBrandIcons.has(id)
              ? <span className="provider-catalog__icon" aria-hidden="true" style={{maskImage: `url(/provider-icons/${id}.svg)`, WebkitMaskImage: `url(/provider-icons/${id}.svg)`}} />
              : <span className="provider-catalog__monogram" aria-hidden="true">{label.slice(0, 1)}</span>}<span>{label}</span>
          </button>)}
      </div>
    </aside>
    <section className="provider-catalog__detail" aria-label={selected.catalog.brandLabel}>
      <strong className="provider-catalog__title">{selected.catalog.brandLabel}</strong>
      {(new Set(siblings.map(c => c.catalog.region)).size > 1 || new Set(regionChoices.map(c => c.catalog.product)).size > 1) && <div className="provider-catalog__options">
        {new Set(siblings.map(c => c.catalog.region)).size > 1 && field("region", "settings.catalog.region", siblings)}
        {new Set(regionChoices.map(c => c.catalog.product)).size > 1 && field("product", "settings.catalog.product", regionChoices)}
      </div>}
      <div className="provider-catalog__field">
        <label className="set-label" htmlFor={`${uid}-format`}>{t("settings.providerProtocol")}</label>
        <SettingsSelect id={`${uid}-format`} className="mem-select" disabled={busy} value={format}
          onValueChange={value => {
            const next = value;
            const match = formatChoices.find(c => genericFormat(c.catalog.format) === next);
            if (match) {
              if (Object.prototype.hasOwnProperty.call(urlDrafts, selected.id)) setUrlDrafts(prev => ({...prev, [match.id]: baseURL}));
              else setUrlDrafts(prev => { const copy = {...prev}; delete copy[match.id]; return copy; });
              setKeyDraft({id: match.id, keyEnv: match.keyEnv, value: key});
              setSelectedID(match.id);
              setFormatDrafts(prev => ({...prev, [match.id]: next}));
            } else setFormatDrafts(prev => ({...prev, [selected.id]: next}));
          }}>
          {["openai", "responses", "anthropic"].map(value => <option key={value} value={value}>{apiFormatLabel(value)}</option>)}
          {selected.catalog.format === "bundle" && <option value="bundle">{t("settings.catalog.formatBundle")}</option>}
        </SettingsSelect>
        {!protocols[format] && !formatChoices.some(c => genericFormat(c.catalog.format) === format) && <div className="mem-hint">{t("settings.catalog.verifyFormat")}</div>}
      </div>
      {formatChoices.filter(c => c.catalog.format === selected.catalog.format).length > 1 && <div className="provider-catalog__field">
        <label className="set-label" htmlFor={`${uid}-variant`}>{t("settings.catalog.variant")}</label>
        <SettingsSelect id={`${uid}-variant`} className="mem-select" value={selected.id} disabled={busy} onValueChange={value => pick(choices.find(c => c.id === value))}>
          {formatChoices.filter(c => c.catalog.format === selected.catalog.format).map(c => <option key={c.id} value={c.id}>{c.label || c.id}</option>)}
        </SettingsSelect>
      </div>}
      {selected.catalog.brandId === "anthropic" && <div className="mem-hint">{t("settings.catalog.anthropicHint")}</div>}
      {selected.catalog.region === "local" && <div className="mem-hint">{t("settings.catalog.localHint")}</div>}
      {selected.catalog.baseUrl && <div className="provider-catalog__field">
        <label className="set-label" htmlFor={`${uid}-url`}>Base URL</label>
        <div className="provider-catalog__url-control">
        <input id={`${uid}-url`} className="mem-input" value={baseURL} disabled={busy} aria-invalid={!validURL} onChange={e => setUrlDrafts(prev => ({...prev, [selected.id]:e.target.value}))} />
        {baseURL !== defaultURL && <button type="button" className="provider-catalog__restore" disabled={busy} aria-label={t("settings.catalog.restoreURL")} title={t("settings.catalog.restoreURL")} onClick={() => setUrlDrafts(prev => { const next = {...prev}; delete next[selected.id]; return next; })}><RotateCcw size={16}/></button>}
        </div>
        <div className="mem-hint">{t("settings.catalog.endpointHint")}</div>
      </div>}
      <label className="set-label" htmlFor={`${uid}-key`}>API Key</label>
      <input id={`${uid}-key`} className="mem-input" type="password" autoComplete="off"
        placeholder={t("settings.setKey", {env: selected.keyEnv})} value={key}
        disabled={busy || !selected.canAdd} onChange={e => setKeyDraft({id: selected.id, keyEnv: selected.keyEnv, value: e.target.value})} />
      {selected.keySet && <div className="mem-hint">{t("settings.catalog.savedKey")}</div>}
      <div className="provider-catalog__models"><span className="set-label">{t("settings.catalog.models")}</span>
        <span>{selected.models.join(", ") || t("settings.catalog.modelsAfterConnect")}</span>
      </div>
      {selected.statusLabel && <div role="status" className="mem-hint">{selected.statusLabel}</div>}
      <div className="prov-card__actions">
        {selected.conflictName && <button type="button" className="btn btn--small" disabled={busy} onClick={() => onView(selected.conflictName!)}>{t("settings.addProvider.viewPresetProvider")}</button>}
        {(selected.status === "name_conflict" || selected.status === "installed_modified") && <button type="button" className="btn btn--small" disabled={busy}
          onClick={() => { if (confirmReset) { onReset(selected.id); setConfirmReset(false); } else setConfirmReset(true); }}>
          {t(confirmReset ? "settings.addProvider.confirmResetPreset" : "settings.addProvider.resetPreset")}
        </button>}
        <button type="button" className="btn btn--small btn--primary" disabled={busy || !selected.canAdd || !validURL}
          onClick={() => onConnect(selected.id, key.trim(), baseURL.trim() || undefined, format === genericFormat(selected.catalog.format) ? undefined : format)}>{selected.actionLabel}</button>
      </div>
    </section>
  </div>;
}
