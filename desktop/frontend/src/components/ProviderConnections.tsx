import { useEffect, useState, type ReactNode } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import type { ProviderAccessGroup } from "./SettingsPanel";
import type { ProviderPresetView } from "../lib/types";
import { catalogForPreset } from "../lib/providerCatalog";
import { providerBrandIcons } from "../lib/providerBrandIcons";
import { useT } from "../lib/i18n";
import { apiFormatLabel } from "./ProviderCatalogPicker";
import { moveProvider, orderByProvider, useProviderOrder, writeProviderOrder } from "../lib/providerOrder";

export function connectionBrand(group: ProviderAccessGroup, presets: ProviderPresetView[]) {
  const preset = presets.find((p) => p.providerNames.some((name) => group.providers.some((v) => v.name === name)))
    ?? presets.find((p) => {
      const base = catalogForPreset(p).baseUrl;
      try { return Boolean(base) && new URL(base).origin === new URL(group.baseUrl).origin; } catch { return false; }
    });
  if (preset) { const c = catalogForPreset(preset); return { id: c.brandId, label: c.brandLabel }; }
  return { id: group.id, label: group.label };
}

export function ProviderConnections({ groups, presets, revealedProvider, hidden, busy, onAdd, renderDetail }: {
  groups: ProviderAccessGroup[]; presets: ProviderPresetView[]; revealedProvider: string | null;
  hidden: boolean; busy: boolean; onAdd: () => void; renderDetail: (group: ProviderAccessGroup) => ReactNode;
}) {
  const t = useT();
  const [selected, setSelected] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [mobileDetail, setMobileDetail] = useState(false);
  const order = useProviderOrder();
  const orderedGroups = orderByProvider(groups, (group) => group.providers[0]?.name ?? group.id, order);
  const active = orderedGroups.find((g) => g.id === selected)?.id ?? orderedGroups[0]?.id;
  useEffect(() => {
    const target = groups.find((g) => g.providers.some((p) => p.name === revealedProvider));
    if (target) { setSelected(target.id); setQuery(""); setMobileDetail(true); }
  }, [revealedProvider, groups]);
  const connections = orderedGroups.filter(g => `${connectionBrand(g,presets).label} ${g.label} ${g.models.join(" ")} ${g.baseUrl}`.toLowerCase().includes(query.trim().toLowerCase()));
  const reorder = (group: ProviderAccessGroup, direction: -1 | 1) => {
    const ids = orderedGroups.map((item) => item.providers[0]?.name ?? item.id);
    const next = moveProvider(order, ids, group.providers[0]?.name ?? group.id, direction);
    if (next) {
      // Keep the detail panel on the same connection when the initial first
      // connection moves; otherwise the fallback selection would change.
      if (selected === null) setSelected(active);
      writeProviderOrder(next);
    }
  };
  if (!groups.length) return null;
  return <div hidden={hidden} className={`provider-connections${mobileDetail ? " provider-connections--detail" : ""}`}>
    <nav className="provider-connections__nav" aria-label={t("settings.providerAccess")}>
      <input className="mem-input" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("settings.connections.search")} aria-label={t("settings.connections.search")} />
      <div className="provider-connections__list">
        {connections.map((g, index) => { const brand=connectionBrand(g,presets); return <div className="provider-connections__row" key={g.id}>
          <button type="button" className="provider-connections__item provider-connections__item--flat" title={`${g.label} · ${apiFormatLabel(g.kind)}`} aria-current={active===g.id ? "true" : undefined} onClick={()=>{setSelected(g.id);setMobileDetail(true);}}>
            {providerBrandIcons.has(brand.id) ? <span className="provider-catalog__icon" style={{maskImage:`url(/provider-icons/${brand.id}.svg)`}} aria-hidden="true"/> : <span className="provider-catalog__monogram" aria-hidden="true">{brand.label.slice(0,1)}</span>}
            <strong>{g.label}</strong><span className={`provider-connections__status${g.configured ? " provider-connections__status--ready" : ""}`} aria-label={t(g.configured ? "settings.connections.configured" : "settings.modelsRequireKey")}/>
          </button>
          {!query.trim() && connections.length > 1 && <span className="provider-connections__order-actions">
            <button type="button" aria-label={t("settings.connections.moveUp", { name: g.label })} title={t("settings.connections.moveUp", { name: g.label })} disabled={index === 0} onClick={() => reorder(g, -1)}><ChevronUp size={15} /></button>
            <button type="button" aria-label={t("settings.connections.moveDown", { name: g.label })} title={t("settings.connections.moveDown", { name: g.label })} disabled={index === connections.length - 1} onClick={() => reorder(g, 1)}><ChevronDown size={15} /></button>
          </span>}
        </div>;})}
        {!connections.length && <p className="muted">{t("settings.connections.noResults")}</p>}
      </div>
      <button className="btn" disabled={busy} onClick={onAdd}>{t("settings.addProvider")}</button>
    </nav>
    <div className="provider-connections__detail">
      <button className="btn btn--small provider-connections__back" onClick={() => setMobileDetail(false)}>{t("settings.connections.back")}</button>
      {/* Retain editors and fetched-model drafts across navigation and add flow. */}
      {groups.map((g) => <div key={g.id} hidden={g.id !== active}>{renderDetail(g)}</div>)}
    </div>
  </div>;
}
