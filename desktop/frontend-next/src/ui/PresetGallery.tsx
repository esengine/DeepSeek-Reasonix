import { useCallback, useEffect, useMemo, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { ProviderPreset } from "../port/port";
import { KIND_LABEL, hostOf } from "./vendors";
import { VendorMark, hasVendorMark, vendorName } from "./vendormarks";

const COMMON_VENDORS = 8;

export function PresetGallery({ load, onPick, disabled }: {
  load: () => Promise<ProviderPreset[]>;
  onPick: (preset: ProviderPreset) => void;
  disabled: boolean;
}) {
  const [presets, setPresets] = useState<ProviderPreset[] | null>(null);
  const [failed, setFailed] = useState("");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [vendor, setVendor] = useState("");

  const read = useCallback(() => {
    setFailed("");
    Promise.resolve().then(() => load()).then(setPresets).catch((e) => {
      setPresets(null);
      setFailed(reason(e));
    });
  }, [load]);
  useEffect(read, [read]);

  const vendors = useMemo(() => {
    const seen = [...new Set((presets ?? []).map((p) => p.vendor).filter((v): v is string => !!v))];
    const label = (v: string) => vendorName(v);
    return [...seen.slice(0, COMMON_VENDORS), ...seen.slice(COMMON_VENDORS).sort((a, b) => label(a).localeCompare(label(b)))];
  }, [presets]);

  const groups = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const shown = (presets ?? []).filter((p) => (!vendor || p.vendor === vendor) && (!needle || `${p.label} ${p.id} ${hostOf(p.baseUrl)}`.toLowerCase().includes(needle)));
    const byHost = new Map<string, ProviderPreset[]>();
    for (const p of shown) byHost.set(hostOf(p.baseUrl), [...(byHost.get(hostOf(p.baseUrl)) ?? []), p]);
    return [...byHost];
  }, [presets, query, vendor]);

  if (failed) {
    return (
      <p className="addp-presets-note" role="alert">
        {t("常用服务列表没能读到：{detail}", { detail: failed })}
        <button type="button" className="act" data-action="provider.preset-retry" onClick={read}>{t("重试")}</button>
      </p>
    );
  }
  if (presets === null) return null;
  if (presets.length === 0) return <p className="addp-presets-note">{t("内核没有提供常用服务预设，可以在下面手动填写。")}</p>;
  return (
    <div className="addp-presets">
      <button type="button" className="addp-presets-toggle" data-action="provider.preset-toggle" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <strong>{t("从常用服务开始")}</strong>
        <span>{presets.length}</span>
      </button>
      {open && (
        <>
          <div className="fields">
            <label className="grow full">
              <input type="search" data-action="provider.draft" data-value="preset-query" aria-label={t("搜索常用服务")} placeholder={t("搜索常用服务")} value={query} onChange={(e) => setQuery(e.target.value)} spellCheck={false} />
            </label>
          </div>
          {vendors.length > 0 && (
            <div className="addp-vendors" role="group" aria-label={t("按厂商筛选")}>
              <button type="button" data-action="provider.preset-vendor" data-target="" aria-pressed={vendor === ""} onClick={() => setVendor("")}>{t("全部")}</button>
              {vendors.map((v) => (
                <button key={v} type="button" data-action="provider.preset-vendor" data-target={v} aria-pressed={vendor === v} onClick={() => setVendor(v)}>
                  {hasVendorMark(v) && <VendorMark slug={v} size={14} />}
                  {vendorName(v)}
                </button>
              ))}
            </div>
          )}
          {groups.length === 0 && <p className="addp-presets-note">{t("没有匹配的服务")}</p>}
          {groups.map(([host, items]) => (
            <section key={host} className="addp-presets-group" aria-label={host}>
              <h4>{host}</h4>
              <div className="addp-presets-grid">
                {items.map((preset) => (
                  <button key={preset.id} type="button" className="addp-preset" data-action="provider.preset" data-target={preset.id} disabled={disabled} onClick={() => onPick(preset)}>
                    <span>{hasVendorMark(preset.vendor) && <VendorMark slug={preset.vendor} />}{preset.label}</span>
                    <small>{t(KIND_LABEL[preset.kind] ?? preset.kind)}</small>
                  </button>
                ))}
              </div>
            </section>
          ))}
        </>
      )}
    </div>
  );
}
