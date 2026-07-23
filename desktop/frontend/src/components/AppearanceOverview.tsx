import { useCallback, useEffect, useMemo, useState } from "react";
import { Check, Copy, Images, LockKeyhole, Minus, Plus, RotateCcw } from "lucide-react";
import { app } from "../lib/bridge";
import { useT, type DictKey } from "../lib/i18n";
import { THEME_STYLES, type Theme, type ThemeStyle, isThemeStyle } from "../lib/theme";
import type { ConversationWidth } from "../lib/conversationWidth";
import { TEXT_SIZES, type TextSize } from "../lib/textSize";
import { type FontFamily, type MonoFontFamily } from "../lib/fontFamily";
import { DEFAULT_ZOOM, MIN_ZOOM, MAX_ZOOM, ZOOM_STEP, zoomToPercent, type ZoomLevel } from "../lib/dpiScale";
import { getAvailableFontFamilies, getAvailableMonoFontFamilies } from "../lib/fontAvailability";
import {
  type ThemeExperienceView,
  activateBaseStyle,
  applyExperienceToDOM,
  cancelGlobalPreview,
  configuredBaseStyleForSync,
  disableThemePack,
  loadThemeExperience,
  restoreGraphiteAppearance,
  setThemeMode,
} from "../lib/themeExperience";
import { useToast } from "../lib/toast";
import { ThemeGallery } from "./ThemeGallery";
import { TypographySettings } from "./TypographySettings";

const STYLE_NAME_KEY: Record<ThemeStyle, DictKey> = {
  graphite: "settings.style.graphite.zh",
  aurora: "settings.style.aurora.zh",
  slate: "settings.style.slate.zh",
  carbon: "settings.style.carbon.zh",
  nocturne: "settings.style.nocturne.zh",
  amber: "settings.style.amber.zh",
};

function textSizeLabel(size: TextSize, t: (key: never) => string): string {
  switch (size) {
    case "small":
      return t("settings.textSizeSmall" as never);
    case "default":
      return t("settings.textSizeDefault" as never);
    case "large":
      return t("settings.textSizeLarge" as never);
    case "xlarge":
      return t("settings.textSizeXLarge" as never);
    case "xxlarge":
      return t("settings.textSizeXXLarge" as never);
    default:
      return size;
  }
}

function fontFamilyLabel(font: FontFamily, t: ReturnType<typeof useT>): string {
  switch (font) {
    case "system":
      return t("settings.fontFamilySystem");
    case "yahei":
      return t("settings.fontFamilyYaHei");
    case "pingfang":
      return t("settings.fontFamilyPingFang");
    case "noto":
      return t("settings.fontFamilyNoto");
    case "custom":
      return t("settings.fontFamilyCustom");
  }
}

function monoFontFamilyLabel(font: MonoFontFamily, t: ReturnType<typeof useT>): string {
  switch (font) {
    case "system":
      return t("settings.monoFontFamilySystem");
    case "cascadia":
      return t("settings.monoFontFamilyCascadia");
    case "jetbrains":
      return t("settings.monoFontFamilyJetBrains");
    case "sfmono":
      return t("settings.monoFontFamilySFMono");
    case "custom":
      return t("settings.monoFontFamilyCustom");
  }
}

// Re-export field shells used by SettingsPanel (defined there as local helpers).
// AppearanceOverview is rendered inside SettingsPageShell and uses the same CSS.

export function AppearanceOverview({
  theme,
  themeStyle,
  conversationWidth,
  textSize,
  showDisplayZoom,
  zoomPct,
  fontFamily,
  monoFontFamily,
  customFontName,
  customMonoFontName,
  onTheme,
  onThemeStyle,
  onConversationWidth,
  onTextSize,
  onRestartZoom,
  onFontFamily,
  onMonoFontFamily,
  onCustomFontNameChange,
  onCustomMonoFontNameChange,
}: {
  theme: Theme;
  themeStyle: ThemeStyle;
  conversationWidth: ConversationWidth;
  textSize: TextSize;
  showDisplayZoom: boolean;
  zoomPct: number;
  fontFamily: FontFamily;
  monoFontFamily: MonoFontFamily;
  customFontName: string;
  customMonoFontName: string;
  onTheme: (t: Theme) => void;
  onThemeStyle: (style: ThemeStyle) => void;
  onConversationWidth: (width: ConversationWidth) => void;
  onTextSize: (size: TextSize) => void;
  onRestartZoom: (zoom: ZoomLevel) => Promise<void>;
  onFontFamily: (font: FontFamily) => void;
  onMonoFontFamily: (font: MonoFontFamily) => void;
  onCustomFontNameChange: (name: string) => void;
  onCustomMonoFontNameChange: (name: string) => void;
}) {
  const t = useT();
  const { showToast } = useToast();
  const [view, setView] = useState<"overview" | "gallery" | "typography">("overview");
  const [galleryIntent, setGalleryIntent] = useState<"browse" | "copy-base">("browse");
  const [experience, setExperience] = useState<ThemeExperienceView | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const exp = await loadThemeExperience();
      setExperience(exp);
      applyExperienceToDOM(exp);
    } catch (err) {
      console.warn("theme experience load failed", err);
    }
  }, []);

  useEffect(() => {
    void refresh();
    return () => {
      cancelGlobalPreview();
    };
  }, [refresh]);

  // Keep experience in sync when parent theme/style props change from outside.
  useEffect(() => {
    setExperience((prev) =>
      prev
        ? {
            ...prev,
            themeMode: theme,
            baseStyle: themeStyle,
            effectiveStyle: prev.activePack?.baseStyle || themeStyle,
          }
        : prev,
    );
  }, [theme, themeStyle]);

  const availableFontFamilies = useMemo(() => getAvailableFontFamilies(fontFamily), [fontFamily]);
  const availableMonoFontFamilies = useMemo(() => getAvailableMonoFontFamilies(monoFontFamily), [monoFontFamily]);
  const zoomMinPct = zoomToPercent(MIN_ZOOM);
  const zoomMaxPct = zoomToPercent(MAX_ZOOM);
  const zoomStepPct = Math.round(ZOOM_STEP * 100);
  const zoomProgressPct = Math.min(100, Math.max(0, ((zoomPct - zoomMinPct) / (zoomMaxPct - zoomMinPct)) * 100));
  const canDecreaseZoom = zoomPct > zoomMinPct;
  const canIncreaseZoom = zoomPct < zoomMaxPct;
  const setZoomPercent = (pct: number) => {
    void onRestartZoom(pct / 100);
  };

  const pack = experience?.activePack ?? null;
  const baseStyle = (isThemeStyle(experience?.baseStyle) ? experience!.baseStyle : themeStyle) as ThemeStyle;
  const styleNameKey = STYLE_NAME_KEY[baseStyle] || STYLE_NAME_KEY.graphite;

  const currentTitle = pack
    ? pack.nameKey
      ? t(pack.nameKey as never)
      : pack.name
    : t(styleNameKey);
  const kindLabel = pack
    ? pack.kind === "user" || (!pack.builtin && pack.kind !== "official")
      ? t("settings.themeGallery.kindUser")
      : pack.kind === "official" || (pack.builtin && pack.id.startsWith("official-"))
        ? t("settings.themeGallery.kindOfficial")
        : t("settings.themeGallery.kindBase")
    : t("settings.themeGallery.kindBase");

  const swatches = pack
    ? [pack.tokens?.light?.bg || "#fff", pack.tokens?.light?.accent || "#ccc", pack.tokens?.dark?.accent || "#888"]
    : null;

  const thumbUrl = pack?.previewUrl || pack?.backgroundUrl || "";

  const handleBrowse = () => {
    setGalleryIntent("browse");
    setView("gallery");
  };

  const handleCopy = async () => {
    if (!pack) {
      setGalleryIntent("copy-base");
      setView("gallery");
      return;
    }
    setBusy(true);
    try {
      const newId = `${pack.id}-copy`.toLowerCase().replace(/[^a-z0-9-]/g, "-").slice(0, 48);
      const created = await app.CopyThemePack(pack.id, newId, `${pack.name} Copy`);
      showToast(t("settings.themeLibrary.copied", { name: created.name }), "info");
      setView("gallery");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const handleDisable = async () => {
    setBusy(true);
    try {
      const exp = await disableThemePack();
      setExperience(exp);
      onThemeStyle(isThemeStyle(exp.baseStyle) ? exp.baseStyle : "graphite");
      showToast(t("settings.themeGallery.disabled"), "info");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const handleBaseChange = async (style: ThemeStyle) => {
    setBusy(true);
    try {
      const exp = await activateBaseStyle(style);
      setExperience(exp);
      onThemeStyle(style);
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const handleThemeMode = async (mode: Theme) => {
    onTheme(mode);
    try {
      const exp = await setThemeMode(mode);
      setExperience(exp);
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  if (view === "gallery" && experience) {
    return (
      <ThemeGallery
        experience={experience}
        initialCreateBaseStyle={galleryIntent === "copy-base" ? baseStyle : undefined}
        onExperienceChange={(exp) => {
          setExperience(exp);
          // Keep the configured base style separate from an active pack's live
          // effective style. applyExperienceToDOM already owns the pack override;
          // mirroring it through onThemeStyle would corrupt the restore snapshot.
          const configuredStyle = configuredBaseStyleForSync(exp);
          if (configuredStyle) onThemeStyle(configuredStyle);
        }}
        onBack={() => {
          cancelGlobalPreview();
          setGalleryIntent("browse");
          setView("overview");
          void refresh();
        }}
      />
    );
  }

  if (view === "typography") {
    return <TypographySettings onBack={() => setView("overview")} />;
  }

  return (
    <div className="appearance-overview">
      <header className="appearance-overview__header">
        <h2 className="appearance-overview__title">{t("settings.appearance")}</h2>
        <p className="appearance-overview__sub">{t("settings.appearanceMeta")}</p>
      </header>

      <section className="appearance-overview__current" aria-labelledby="appearance-current-label">
        <h3 id="appearance-current-label" className="appearance-overview__section-label">
          {t("settings.themeGallery.currentAppearance")}
        </h3>
        <div className="appearance-overview__hero">
          <div className="appearance-overview__thumb">
            {thumbUrl ? (
              <img src={thumbUrl} alt="" loading="lazy" />
            ) : (
              <div className="appearance-overview__thumb-base theme-card__swatches" data-theme-style-card={baseStyle}>
                <span className="theme-card__swatch theme-card__swatch--bg" />
                <span className="theme-card__swatch theme-card__swatch--surface" />
                <span className="theme-card__swatch theme-card__swatch--accent" />
              </div>
            )}
          </div>
          <div className="appearance-overview__hero-meta">
            <div className="appearance-overview__hero-title-row">
              <h4 className="appearance-overview__hero-name">{currentTitle}</h4>
              <span className="theme-gallery__badge">{kindLabel}</span>
            </div>
            {swatches ? (
              <div className="appearance-overview__swatches" aria-hidden="true">
                {swatches.map((c, i) => (
                  <span key={i} style={{ background: c }} />
                ))}
              </div>
            ) : null}
            <div className="appearance-overview__in-use">
              <Check size={14} /> {t("settings.themeLibrary.active")}
            </div>
            <div className="appearance-overview__hero-actions">
              <button type="button" className="btn btn--primary" disabled={busy} onClick={handleBrowse}>
                <Images size={14} /> {t("settings.themeGallery.browse")}
              </button>
              <button type="button" className="btn" disabled={busy} onClick={() => void handleCopy()}>
                <Copy size={14} /> {t("settings.themeGallery.createCopy")}
              </button>
              {pack ? (
                <button type="button" className="btn" disabled={busy} onClick={() => void handleDisable()}>
                  {t("settings.themeGallery.disable")}
                </button>
              ) : null}
            </div>
          </div>
        </div>
      </section>

      <div className="appearance-overview__rows">
        <div className="appearance-overview__row">
          <div id="appearance-theme-mode-label" className="appearance-overview__row-label">{t("settings.theme")}</div>
          <div
            className="set-seg appearance-overview__segmented appearance-overview__segmented--theme"
            role="radiogroup"
            aria-labelledby="appearance-theme-mode-label"
          >
            {(["auto", "light", "dark"] as Theme[]).map((opt) => (
              <button
                key={opt}
                type="button"
                role="radio"
                aria-checked={theme === opt}
                className={`set-seg__btn${theme === opt ? " set-seg__btn--on" : ""}`}
                onClick={() => void handleThemeMode(opt)}
              >
                {opt === "auto" ? t("settings.themeAuto") : opt === "light" ? t("settings.themeLight") : t("settings.themeDark")}
              </button>
            ))}
          </div>
        </div>

        <div className="appearance-overview__row">
          <div id="appearance-base-style-label" className="appearance-overview__row-label">{t("settings.themeGallery.baseStyle")}</div>
          <div className="appearance-overview__control-stack">
            <select
              className="appearance-overview__select"
              value={baseStyle}
              disabled={busy || !!pack}
              aria-labelledby="appearance-base-style-label"
              aria-describedby={pack ? "appearance-base-style-help" : undefined}
              onChange={(e) => void handleBaseChange(e.target.value as ThemeStyle)}
            >
              {THEME_STYLES.map((s) => (
                <option key={s} value={s}>
                  {t(STYLE_NAME_KEY[s])}
                </option>
              ))}
            </select>
            {pack ? (
              <span id="appearance-base-style-help" className="appearance-overview__lock-note">
                <LockKeyhole size={12} aria-hidden="true" />
                {t("settings.themeGallery.baseLockedByPack")}
              </span>
            ) : null}
          </div>
        </div>

        <div className="appearance-overview__row">
          <div id="appearance-conversation-width-label" className="appearance-overview__row-label">
            {t("settings.conversationWidth")}
          </div>
          <div
            className="set-seg appearance-overview__segmented"
            role="radiogroup"
            aria-labelledby="appearance-conversation-width-label"
          >
            <button
              type="button"
              role="radio"
              aria-checked={conversationWidth === "standard"}
              className={`set-seg__btn${conversationWidth === "standard" ? " set-seg__btn--on" : ""}`}
              onClick={() => onConversationWidth("standard")}
            >
              {t("settings.conversationWidthStandard")} (960px)
            </button>
            <button
              type="button"
              role="radio"
              aria-checked={conversationWidth === "full"}
              className={`set-seg__btn${conversationWidth === "full" ? " set-seg__btn--on" : ""}`}
              onClick={() => onConversationWidth("full")}
            >
              {t("settings.conversationWidthFull")} (90%)
            </button>
          </div>
        </div>

        <div className="appearance-overview__row">
          <div id="appearance-text-size-label" className="appearance-overview__row-label">{t("settings.textSize")}</div>
          <div
            className="set-seg appearance-overview__segmented appearance-overview__segmented--text-size"
            role="radiogroup"
            aria-labelledby="appearance-text-size-label"
          >
            {TEXT_SIZES.map((size) => (
              <button
                key={size}
                type="button"
                role="radio"
                aria-checked={textSize === size}
                className={`set-seg__btn${textSize === size ? " set-seg__btn--on" : ""}`}
                onClick={() => onTextSize(size)}
              >
                {textSizeLabel(size, t)}
              </button>
            ))}
          </div>
        </div>

        <div className="appearance-overview__row">
          <div id="appearance-font-family-label" className="appearance-overview__row-label">{t("settings.fontFamily")}</div>
          <select
            className="appearance-overview__select"
            value={fontFamily}
            aria-labelledby="appearance-font-family-label"
            onChange={(e) => onFontFamily(e.target.value as FontFamily)}
          >
            {availableFontFamilies.map((f) => (
              <option key={f} value={f}>
                {fontFamilyLabel(f, t)}
              </option>
            ))}
          </select>
        </div>

        {fontFamily === "custom" ? (
          <div className="appearance-overview__row">
            <div id="appearance-custom-font-name-label" className="appearance-overview__row-label">
              {t("settings.fontFamilyCustomName")}
            </div>
            <textarea
              className="mem-input appearance-overview__font-input"
              rows={2}
              aria-labelledby="appearance-custom-font-name-label"
              placeholder={t("settings.fontFamilyCustomPlaceholder")}
              value={customFontName}
              onChange={(e) => onCustomFontNameChange(e.target.value)}
            />
          </div>
        ) : null}

        <div className="appearance-overview__row">
          <div id="appearance-mono-font-family-label" className="appearance-overview__row-label">{t("settings.monoFontFamily")}</div>
          <select
            className="appearance-overview__select"
            value={monoFontFamily}
            aria-labelledby="appearance-mono-font-family-label"
            onChange={(e) => onMonoFontFamily(e.target.value as MonoFontFamily)}
          >
            {availableMonoFontFamilies.map((f) => (
              <option key={f} value={f}>
                {monoFontFamilyLabel(f, t)}
              </option>
            ))}
          </select>
        </div>

        {monoFontFamily === "custom" ? (
          <div className="appearance-overview__row">
            <div id="appearance-custom-mono-font-name-label" className="appearance-overview__row-label">
              {t("settings.monoFontFamilyCustomName")}
            </div>
            <textarea
              className="mem-input appearance-overview__font-input"
              rows={2}
              aria-labelledby="appearance-custom-mono-font-name-label"
              placeholder={t("settings.monoFontFamilyCustomPlaceholder")}
              value={customMonoFontName}
              onChange={(e) => onCustomMonoFontNameChange(e.target.value)}
            />
          </div>
        ) : null}

        <div className="appearance-overview__row appearance-overview__row--typography">
          <div>
            <div className="appearance-overview__row-label">{t("settings.typography.title")}</div>
            <div className="appearance-overview__row-note">{t("settings.typography.entrySummary")}</div>
          </div>
          <button type="button" className="btn btn--small" onClick={() => setView("typography")}>
            {t("settings.typography.open")}
          </button>
        </div>

        {showDisplayZoom ? (
          <div className="appearance-overview__row">
            <div className="appearance-overview__row-label">{t("settings.displayZoom")}</div>
            <div className="zoom-slider-wrap">
              <div className="zoom-slider__head">
                <div className="zoom-slider__value">{zoomPct}%</div>
                <div className="zoom-stepper">
                  <button
                    type="button"
                    className="zoom-stepper__btn"
                    aria-label={t("settings.displayZoomDecrease")}
                    title={t("settings.displayZoomDecrease")}
                    disabled={!canDecreaseZoom}
                    onClick={() => setZoomPercent(zoomPct - zoomStepPct)}
                  >
                    <Minus size={13} aria-hidden="true" />
                  </button>
                  <button
                    type="button"
                    className="zoom-stepper__reset"
                    aria-label={t("settings.displayZoomReset")}
                    title={t("settings.displayZoomReset")}
                    disabled={zoomPct === zoomToPercent(DEFAULT_ZOOM)}
                    onClick={() => {
                      void onRestartZoom(DEFAULT_ZOOM);
                    }}
                  >
                    <RotateCcw size={12} aria-hidden="true" />
                    <span>100%</span>
                  </button>
                  <button
                    type="button"
                    className="zoom-stepper__btn"
                    aria-label={t("settings.displayZoomIncrease")}
                    title={t("settings.displayZoomIncrease")}
                    disabled={!canIncreaseZoom}
                    onClick={() => setZoomPercent(zoomPct + zoomStepPct)}
                  >
                    <Plus size={13} aria-hidden="true" />
                  </button>
                </div>
              </div>
              <div className="zoom-slider-row">
                <span className="zoom-slider__label">{zoomMinPct}%</span>
                <div className="slider-track">
                  <div className="slider-track__bg" />
                  <div className="slider-track__fill" style={{ width: `calc(${zoomProgressPct}% + 15px)` }} />
                  <div className="slider-thumb" style={{ left: `${zoomProgressPct}%` }} />
                  <input
                    aria-label={t("settings.displayZoom")}
                    type="range"
                    min={zoomMinPct}
                    max={zoomMaxPct}
                    step={zoomStepPct}
                    value={zoomPct}
                    onChange={(e) => setZoomPercent(Number(e.target.value))}
                  />
                </div>
                <span className="zoom-slider__label">{zoomMaxPct}%</span>
              </div>
            </div>
          </div>
        ) : null}

        <div className="appearance-overview__row appearance-overview__row--footer">
          <span id="appearance-restore-help" className="appearance-overview__reset-hint">
            {t("settings.themeGallery.restoreGraphiteHint")}
          </span>
          <button
            type="button"
            className="btn btn--small"
            aria-describedby="appearance-restore-help"
            disabled={busy}
            onClick={() => {
              void (async () => {
                setBusy(true);
                try {
                  const exp = await restoreGraphiteAppearance();
                  setExperience(exp);
                  onThemeStyle("graphite");
                  showToast(t("settings.themeGallery.restoredGraphite"), "info");
                } catch (err) {
                  showToast(err instanceof Error ? err.message : String(err), "error");
                } finally {
                  setBusy(false);
                }
              })();
            }}
          >
            {t("settings.themeGallery.restoreGraphite")}
          </button>
        </div>
      </div>
    </div>
  );
}
