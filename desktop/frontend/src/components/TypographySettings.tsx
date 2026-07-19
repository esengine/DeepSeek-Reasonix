import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, ChevronDown, ChevronRight, Minus, Plus, RotateCcw, Sparkles, Undo2, UserRound } from "lucide-react";
import { useT } from "../lib/i18n";
import {
  TYPOGRAPHY_REGIONS,
  TYPOGRAPHY_REGION_META,
  applyTypographyPreferences,
  createDefaultTypographyPreferences,
  getTypographyPreferences,
  isSafeCustomFontNameInput,
  sanitizeCustomFontName,
  type RegionFontFamily,
  type TypographyPreferences,
  type TypographyRegion,
} from "../lib/typographyPreferences";

const REGION_KEYS: Record<TypographyRegion, Parameters<ReturnType<typeof useT>>[0]> = {
  interface: "settings.typography.region.interface",
  conversation: "settings.typography.region.conversation",
  composer: "settings.typography.region.composer",
  code: "settings.typography.region.code",
  metadata: "settings.typography.region.metadata",
};

const FONT_OPTIONS: Array<{ value: RegionFontFamily; key: Parameters<ReturnType<typeof useT>>[0] }> = [
  { value: "inherit", key: "settings.typography.font.inherit" },
  { value: "system", key: "settings.fontFamilySystem" },
  { value: "yahei", key: "settings.fontFamilyYaHei" },
  { value: "pingfang", key: "settings.fontFamilyPingFang" },
  { value: "noto", key: "settings.fontFamilyNoto" },
  { value: "cascadia", key: "settings.monoFontFamilyCascadia" },
  { value: "jetbrains", key: "settings.monoFontFamilyJetBrains" },
  { value: "sfmono", key: "settings.monoFontFamilySFMono" },
  { value: "custom", key: "settings.fontFamilyCustom" },
];

type PreviewTab = "body" | "reasoning" | "tools";

export function TypographySettings({ onBack }: { onBack: () => void }) {
  const t = useT();
  const [selected, setSelected] = useState<TypographyRegion>("conversation");
  const [preferences, setPreferences] = useState<TypographyPreferences>(() => getTypographyPreferences());
  const [previewTab, setPreviewTab] = useState<PreviewTab>("body");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const previous = useRef<TypographyPreferences | null>(null);
  const preference = preferences[selected];
  const meta = TYPOGRAPHY_REGION_META[selected];

  useLayoutEffect(() => {
    document.querySelector<HTMLElement>(".settings-center__content")?.scrollTo({ top: 0 });
  }, []);
  const presets = useMemo(
    () => Array.from(new Set([meta.baseSize, meta.baseSize + 2, meta.baseSize + 4, meta.baseSize + 6])).filter((n) => n <= meta.max),
    [meta],
  );

  const commit = (next: TypographyPreferences) => {
    previous.current = preferences;
    setPreferences(next);
    applyTypographyPreferences(next);
  };
  const updateSelected = (patch: Partial<typeof preference>) => {
    commit({ ...preferences, [selected]: { ...preference, ...patch } });
  };
  const setSize = (size: number) => updateSelected({
    followGlobal: false,
    fontSize: Math.min(meta.max, Math.max(meta.min, Math.round(size))),
  });
  const resetSelected = () => {
    const defaults = createDefaultTypographyPreferences();
    commit({ ...preferences, [selected]: defaults[selected] });
  };
  const resetAll = () => commit(createDefaultTypographyPreferences());
  const undo = () => {
    if (!previous.current) return;
    const next = previous.current;
    previous.current = preferences;
    setPreferences(next);
    applyTypographyPreferences(next);
  };

  return (
    <div className="typography-settings">
      <header className="typography-settings__header">
        <div>
          <div className="typography-settings__heading-row">
            <button type="button" className="typography-settings__back" aria-label={t("settings.typography.back")} title={t("settings.typography.back")} onClick={onBack}>
              <ArrowLeft size={17} aria-hidden="true" />
            </button>
            <h2>{t("settings.typography.title")}</h2>
          </div>
          <p>{t("settings.typography.subtitle")}</p>
        </div>
        <button type="button" className="btn btn--small" onClick={resetAll}>
          <RotateCcw size={13} aria-hidden="true" /> {t("settings.typography.resetAll")}
        </button>
      </header>

      <div className="typography-settings__workspace">
        <nav className="typography-settings__regions" aria-label={t("settings.typography.regions")}>
          <div className="typography-settings__regions-label">{t("settings.typography.regions")}</div>
          <button type="button" className="typography-settings__region" onClick={onBack}>
            <span>{t("settings.typography.globalDefault")}</span>
            <span />
            <ChevronRight size={14} aria-hidden="true" />
          </button>
          {TYPOGRAPHY_REGIONS.map((region) => (
            <button
              key={region}
              type="button"
              className={region === selected ? "typography-settings__region typography-settings__region--active" : "typography-settings__region"}
              aria-current={region === selected ? "page" : undefined}
              onClick={() => setSelected(region)}
            >
              <span>{t(REGION_KEYS[region])}</span>
              {!preferences[region].followGlobal ? <span className="typography-settings__dot" aria-label={t("settings.typography.customized")} /> : null}
              <ChevronRight size={14} aria-hidden="true" />
            </button>
          ))}
        </nav>

        <main className="typography-settings__detail">
          <div className="typography-settings__detail-head">
            <div>
              <div className="typography-settings__title-row">
                <h3>{t(REGION_KEYS[selected])}</h3>
                {!preference.followGlobal ? <span className="typography-settings__badge">{t("settings.typography.customized")}</span> : null}
              </div>
              <p>{t(`settings.typography.desc.${selected}` as Parameters<typeof t>[0])}</p>
            </div>
            <label className="typography-settings__follow">
              <span>{t("settings.typography.followGlobal")}</span>
              <input
                type="checkbox"
                checked={preference.followGlobal}
                onChange={(event) => updateSelected({ followGlobal: event.target.checked })}
              />
              <span className="typography-settings__switch" aria-hidden="true" />
            </label>
          </div>

          <div className={preference.followGlobal ? "typography-settings__controls typography-settings__controls--disabled" : "typography-settings__controls"}>
            <label className="typography-settings__field">
              <span>{t("settings.typography.font")}</span>
              <select
                value={preference.fontFamily}
                disabled={preference.followGlobal}
                onChange={(event) => updateSelected({ fontFamily: event.target.value as RegionFontFamily })}
              >
                {FONT_OPTIONS.map((option) => <option key={option.value} value={option.value}>{t(option.key)}</option>)}
              </select>
            </label>
            {preference.fontFamily === "custom" && !preference.followGlobal ? (
              <label className="typography-settings__field">
                <span>{t("settings.typography.customFont")}</span>
                <input
                  type="text"
                  value={preference.customFontName}
                  placeholder={t("settings.fontFamilyCustomPlaceholder")}
                  onChange={(event) => {
                    const value = event.target.value.slice(0, 200);
                    if (isSafeCustomFontNameInput(value)) updateSelected({ customFontName: value });
                  }}
                  onBlur={() => {
                    const normalized = sanitizeCustomFontName(preference.customFontName);
                    if (normalized !== preference.customFontName) updateSelected({ customFontName: normalized });
                  }}
                />
              </label>
            ) : null}

            <div className="typography-settings__field">
              <span>{t("settings.typography.size")}</span>
              <div className="typography-settings__size-row">
                <div className="typography-settings__stepper">
                  <button type="button" disabled={preference.followGlobal || preference.fontSize <= meta.min} onClick={() => setSize(preference.fontSize - 1)} aria-label={t("settings.typography.decrease")}><Minus size={14} /></button>
                  <span>{preference.fontSize}px</span>
                  <button type="button" disabled={preference.followGlobal || preference.fontSize >= meta.max} onClick={() => setSize(preference.fontSize + 1)} aria-label={t("settings.typography.increase")}><Plus size={14} /></button>
                </div>
                <div className="typography-settings__presets">
                  {presets.map((size) => (
                    <button key={size} type="button" disabled={preference.followGlobal} className={preference.fontSize === size ? "is-active" : ""} onClick={() => setSize(size)}>{size}</button>
                  ))}
                </div>
              </div>
            </div>

            <button type="button" className="typography-settings__advanced" onClick={() => setAdvancedOpen((open) => !open)} aria-expanded={advancedOpen}>
              {advancedOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              <span>{t("settings.typography.advanced")}</span>
              <small>{t("settings.typography.advancedSummary")}</small>
            </button>
            {advancedOpen ? <p className="typography-settings__advanced-note">{t("settings.typography.advancedNote")}</p> : null}
          </div>

          <div className="typography-settings__actions">
            <button type="button" className="btn btn--small" onClick={resetSelected}>{t("settings.typography.resetRegion")}</button>
            <span>{t("settings.typography.applied")}</span>
            <button type="button" className="typography-settings__undo" disabled={!previous.current} onClick={undo}><Undo2 size={13} /> {t("settings.typography.undo")}</button>
          </div>

          <section className={`typography-settings__preview typography-settings__preview--${selected}`}>
            <div className="typography-settings__preview-head">
              <span>{t("settings.typography.preview")}</span>
              <div role="tablist" aria-label={t("settings.typography.preview")}>
                {(["body", "reasoning", "tools"] as PreviewTab[]).map((tab) => (
                  <button key={tab} type="button" role="tab" aria-selected={previewTab === tab} className={previewTab === tab ? "is-active" : ""} onClick={() => setPreviewTab(tab)}>{t(`settings.typography.preview.${tab}` as Parameters<typeof t>[0])}</button>
                ))}
              </div>
            </div>
            <div className="typography-settings__preview-body">
              {previewTab === "body" ? (
                <div className="typography-settings__preview-chat">
                  <div className="typography-settings__preview-message">
                    <span className="typography-settings__preview-avatar"><UserRound size={14} aria-hidden="true" /></span>
                    <p>{t("settings.typography.previewQuestion")}</p>
                  </div>
                  <div className="typography-settings__preview-message typography-settings__preview-message--assistant">
                    <span className="typography-settings__preview-avatar"><Sparkles size={14} aria-hidden="true" /></span>
                    <div>
                      <p>{t("settings.typography.previewBody")}</p>
                      <code>E = hνF｜k｜</code>
                      <small>{t("settings.typography.previewSources")}</small>
                    </div>
                  </div>
                </div>
              ) : null}
              {previewTab === "reasoning" ? <p className="typography-settings__preview-reasoning">{t("settings.typography.previewReasoning")}</p> : null}
              {previewTab === "tools" ? <pre>{t("settings.typography.previewTool")}</pre> : null}
            </div>
          </section>
        </main>
      </div>
    </div>
  );
}
