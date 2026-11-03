import { ApplyNote } from "./Group";
import { useCallback, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties } from "react";
import { effectsMode, onEffectsChange, setEffectsMode, type EffectsMode } from "../state/prefs";
import type { AgentPort, Appearance as Look, ThemePack } from "../port/port";
import { ZOOM_PRESETS } from "./zoom";
import { chord } from "./keys";
import { host } from "../port/host";
import { MONO_FAMILIES, TEXT_STEPS, UI_FAMILIES, installed, readSizeOf, readSteps } from "./look";
import { STORAGE as LANG_KEY, t } from "../i18n";
import { pct } from "../i18n/format";
import { reason } from "../i18n/kernel";
import { useCrop } from "./useCrop";
import { Notifications } from "./Notifications";
import { Folding } from "./Folding";
import { WindowSection } from "./WindowSection";
import { LiveGraceSection } from "./LiveGraceSection";
import { LegacySessionsSection } from "./LegacySessionsSection";
import { OpenSessionLimitSection } from "./OpenSessionLimitSection";
import { LiveStreamBudgetSection } from "./LiveStreamBudgetSection";
import { NewAnswerSection } from "./NewAnswerSection";
import { PlanProgressSection } from "./PlanProgressSection";
import { ComposerHintSection } from "./ComposerHintSection";
import { OfficePreviewSection } from "./OfficePreviewSection";
import { SessionQuickSection } from "./SessionQuickSection";
import { NavEntriesSection } from "./NavEntriesSection";
import { NavRailSection } from "./NavRailSection";
import { GraphicsSection } from "./GraphicsSection";
import { GlobalShortcutSection } from "./GlobalShortcutSection";
import { SendShortcut } from "./SendShortcut";
import { RunningMessageSetting } from "./RunningMessageSetting";
import { KeysSheet } from "./KeysSheet";
import { SessionTabsSetting } from "./SessionTabsSetting";
import { EscStopSetting } from "./EscStopSetting";
import { PageSleepSetting } from "./PageSleepSetting";
import { ThemeImport } from "./ThemeImport";
import { ThemeRemove } from "./ThemeRemove";
import { WallpaperSceneImport } from "./WallpaperSceneImport";
import { WallpaperVolumeSlider } from "./WallpaperVolumeSlider";
import { useThemeInventory } from "./useThemeInventory";
import { ThemeScheduleFields } from "./ThemeScheduleFields";

// "" follows the machine; the rest are explicit, the same shape the light/dark
// control uses.
const LANGS: [string, string][] = [
  ["", "common.followTheSystem"],
  ["zh", "common.text"],
  ["en", "appearance.english"],
];

// Exported because the nav rail names the current one next to the tab.
export const SCHEMES: [string, string][] = [
  ["auto", "common.followTheSystem"],
  ["light", "common.light"],
  ["dark", "common.dark"],
  ["scheduled", "common.scheduled"],
];

interface Props {
  port: AgentPort;
  theme: string;
  contrast: string;
  weight: string;
  onWeight: (v: string) => void;
  onContrast: (c: string) => void;
  onTheme: (t: string) => void;
  reloadThemes: () => void;
  look: Look;
  onLook: (look: Look) => void;
}

// Body size in the transcript alone, so the frame stays where the layout put it.
// The steps live in look.ts next to the default they have to agree with.

// "" follows the system's own accessibility setting; the rest are explicit.
// 笔画粗细是另一条轴：颜色调得再亮，400 的汉字在 11px 上仍然是细的。默认跟着
// 语言走 —— 汉字笔画密，同字号下比拉丁更早糊。
const WEIGHTS: [string, string, string][] = [
  ["", "appearance.matchTheLanguage", "appearance.mediumForChineseRegularFor"],
  ["light", "appearance.regular", "appearance.thinnerStrokesLooserTexture"],
  ["heavy", "common.bolder", "appearance.firmerAtSmallSizesAnd"],
];

const MEASURES: [string, string, string][] = [
  ["standard", "appearance.reading", "appearance.keepsALineShortEnough"],
  ["full", "common.fullWidth", "appearance.proseFollowsTheWindowWith"],
  ["full-all", "appearance.fullWidthComposerToo", "appearance.proseAndTheComposerBoth"],
  ["custom", "appearance.customPercentage", "appearance.setTheConversationAndComposer"],
];

const EFFECTS: [EffectsMode, string, string][] = [
  ["full", "common.full", "appearance.backgroundBlurTheAnimatedSky"],
  ["reduced", "appearance.reduced", "appearance.turnsOffBackgroundBlurThe"],
];

const CONTRASTS: [string, string, string][] = [
  ["", "common.followTheSystem", "appearance.usesTheStrongestStepWhen"],
  ["soft", "common.soft", "appearance.softerBodyTextEasierOver"],
  ["normal", "common.standard", "appearance.betweenTheTwo"],
  ["strong", "appearance.stronger", "appearance.brightSurroundingsOrWhenEdges"],
];

// WebKit 的滑块轨道不知道当前值，填充比例得由调用方喂进来。
const at = (v: number, min: number, max: number) =>
  ({ "--at": `${Math.round(((v - min) / (max - min)) * 100)}%` }) as React.CSSProperties;

/** What the picker offers: the kernel's list of wallpaper types. */
export const WALLPAPER_ACCEPT = "image/png,image/jpeg,image/webp,image/avif,image/gif,video/mp4,video/webm";

export function Appearance({ port, theme, onTheme, contrast, onContrast, weight, onWeight, reloadThemes, look, onLook }: Props) {
  const { packs, unread, load } = useThemeInventory(port);
  const effects = useSyncExternalStore(onEffectsChange, effectsMode);

  // Activating repaints through App's own theme effect, so this only refreshes
  // the list and asks App to re-read which pack is active.
  const pick = useCallback(
    (id: string) => {
      setFailed("");
      port
        .activateTheme(id)
        .then(() => {
          load();
          reloadThemes();
        })
        .catch((e) => setFailed(reason(e)));
    },
    [port, load, reloadThemes],
  );

  const remove = useCallback(
    (id: string) => {
      setFailed("");
      return port
        .removeTheme(id)
        .then(() => {
          load();
          reloadThemes();
        })
        .catch((e) => setFailed(reason(e)));
    },
    [port, load, reloadThemes],
  );
  const cell = (p: ThemePack) => (
    <div className="pal-cell" key={p.id}>
      <Swatch pack={p} theme={theme} name={t(p.name)} on={!!p.active} onPick={() => pick(p.id)} />
      {p.removable && <ThemeRemove name={p.name} onRemove={() => remove(p.id)} />}
    </div>
  );

  const custom = packs.some((p) => p.active);
  // Calm, workbench-like themes stay in the first shelf. Illustrated packs
  // are still first-class installed themes, but they no longer dominate the
  // page a user opens merely to switch between light and dark. If one of them
  // is active it remains visible beside the foundations, so the current state
  // never disappears into a closed disclosure.
  const foundations = packs.filter((p) => !p.background?.image && !p.sky);
  const activeIllustrated = packs.find((p) => p.active && (p.background?.image || p.sky));
  const featured = activeIllustrated ? [...foundations, activeIllustrated] : foundations;
  const illustrated = packs.filter((p) => (p.background?.image || p.sky) && p.id !== activeIllustrated?.id);

  // A change lands on screen through App's own effect; this only sends it on
  // so the next launch opens the same way.
  const set = useCallback((patch: Partial<Look>) => onLook({ ...look, ...patch }), [look, onLook]);
  // Until the kernel has announced its range there is nothing to draw the
  // slider from; it is held inert rather than given a range of its own.
  const zoomRange = look.zoomRange ?? { min: 0, max: 2, step: 1 };
  const widthRange = look.widthRange ?? { min: 0, max: 2, step: 1 };
  const setPaper = useCallback(
    (patch: Partial<NonNullable<Look["wallpaper"]>>) =>
      look.wallpaper && onLook({ ...look, wallpaper: { ...look.wallpaper, ...patch } }),
    [look, onLook],
  );

  const file = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState("");

  const pickPaper = useCallback(
    async (chosen: File | undefined) => {
      if (!chosen) return;
      setBusy(true);
      setFailed("");
      try {
        onLook(await port.uploadWallpaper(chosen));
      } catch (e) {
        setFailed(reason(e));
      } finally {
        setBusy(false);
      }
    },
    [port, onLook],
  );

  const dropPaper = useCallback(() => {
    setFailed("");
    void port
      .clearWallpaper()
      .then(() => onLook({ ...look, wallpaper: undefined }))
      .catch((e) => setFailed(reason(e)));
  }, [port, look, onLook]);

  const [view, setView] = useState<HTMLDivElement | null>(null);
  const crop = useCrop(look.wallpaper?.url, look.wallpaper?.kind, view);

  // Only families this machine can really draw with, asked of the font system
  // rather than assumed per platform.
  const uiFonts = useMemo(() => installed(UI_FAMILIES), []);
  const monoFonts = useMemo(() => installed(MONO_FAMILIES), []);

  // Applying a language under a running tree would mean every memoised
  // component subscribing to one, for a setting that changes once in a window's
  // life. Storing it and reloading is the honest trade.
  const setLang = useCallback(
    (next: string) => {
      localStorage.setItem(LANG_KEY, next);
      onLook({ ...look, language: next });
      setTimeout(() => location.reload(), 120);
    },
    [look, onLook],
  );
  const langNow = localStorage.getItem(LANG_KEY) ?? look.language ?? "";
  // Read here rather than inside the summary: these are the same three values
  // the folded blocks render, and a second reading of them is a second answer.
  const changed = [
    look.fontUi ? t("common.typeface") : "",
    weight ? t("common.textWeight") : "",
    contrast ? t("common.textContrast") : "",
    effects === "reduced" ? t("common.visualEffects") : "",
  ].filter(Boolean);

  return (
    <>
      {(failed || unread) && (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{failed || unread}</span>
        </div>
      )}
      <section className="grp" id="set-mode" data-setting="mode">
        <div className="grp-hd">
          <h3>{t("common.lightAndDark")}</h3>
        </div>
        <p className="hint">{t("appearance.followTheSystemSwitchesWith")}</p>
        <div className="grp-items">
          <div className="seg" data-text role="group" aria-label={t("common.lightAndDark")}>
            {SCHEMES.map(([id, name]) => (
              <button key={id} data-action="appearance.scheme" data-value={id} aria-pressed={theme === id} onClick={() => onTheme(id)}>
                {t(name)}
              </button>
            ))}
          </div>
          {theme === "scheduled" && <ThemeScheduleFields />}
        </div>
        <ApplyNote id="mode" />
      </section>

      <section className="grp" id="set-scheme" data-setting="scheme">
        <div className="grp-hd">
          <h3>{t("appearance.interfaceTheme")}</h3>
          <span className="now">{t("common.inEffectNow")}</span>
        </div>
        <p className="hint">{t("appearance.chooseTheWorkbenchSurfacesAnd")}</p>
        <div className="grp-items">
          <div className="palettes" role="group" aria-label={t("appearance.interfaceTheme")}>
            <Swatch name={t("common.default")} on={!custom} onPick={() => pick("")} />
            {featured.map(cell)}
          </div>
          {illustrated.length > 0 && (
            <details className="theme-more">
              <summary>
                <span>
                  <b>{t("appearance.moreInstalledThemes")}</b>
                  <em>{t("appearance.illustratedAndBackgroundThemesYou")}</em>
                </span>
                <span className="now">{t("common.text2", { n: illustrated.length })}</span>
              </summary>
              <div className="palettes" role="group" aria-label={t("appearance.moreInstalledThemes")}>
                {illustrated.map(cell)}
              </div>
            </details>
          )}
          {/* A pack loads with its good tokens and says which ones it lost. The
              author is the only one who can fix that, and they will not read a
              log — so it is here, next to the thing that looks wrong. */}
          {packs.filter((p) => p.warnings?.length).map((p) => (
            <div className="find" data-lvl="warn" key={p.id}>
              <span className="t">{t("appearance.aFewOfSEntries", { name: p.name })}</span>
              {p.warnings?.map((w) => (
                <span className="why" key={w}>
                  {w}
                </span>
              ))}
            </div>
          ))}
          <ThemeImport port={port} empty={packs.length === 0} onImported={load} onUse={pick} />
        </div>
        <ApplyNote id="scheme" />
      </section>

      <section className="grp" id="set-code-appearance" data-setting="font">
        <div className="grp-hd">
          <h3>{t("appearance.codeAppearance")}</h3>
        </div>
        <p className="hint">{t("appearance.codeBlocksCommandsDiffsAnd")}</p>
        <div className="grp-items">
          <FontPick
            slot="mono"
            label={t("appearance.monospaceFont")}
            value={look.fontMono ?? ""}
            options={monoFonts}
            sample="func main() { fmt.Println(0O1lI) }"
            onPick={(v) => set({ fontMono: v })}
          />
        </div>
        <ApplyNote id="font" />
      </section>

      <section className="grp" id="set-size" data-setting="size">
        <div className="grp-hd">
          <h3>{t("common.size")}</h3>
        </div>
        <p className="hint">{t("appearance.interfaceScalesSpacingAndControls")}</p>
        {host().inShell() && <p className="hint">{t("appearance.keyboardLargerSmallerBackTo", { up: chord("+"), down: chord("-"), reset: chord("0") })}</p>}
        <div className="grp-items">
          <div className="prow">
            <span className="tx">{t("appearance.interface")}</span>
            <div className="seg" data-text role="group" aria-label={t("appearance.interfaceSize")}>
              {ZOOM_PRESETS.map(([v, name]) => (
                <button key={v} data-action="appearance.zoom" aria-pressed={(look.zoom || 1) === v} onClick={() => set({ zoom: v })}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <div className="prow">
            <span className="tx">{t("appearance.fineTune")}</span>
            <input
              className="slider"
              style={at(look.zoom || 1, zoomRange.min, zoomRange.max)}
              type="range"
              min={zoomRange.min}
              max={zoomRange.max}
              step={zoomRange.step}
              disabled={!look.zoomRange}
              value={look.zoom || 1}
              aria-label={t("appearance.fineTuneInterfaceSize")}
              data-action="appearance.zoom"
              onChange={(e) => set({ zoom: Number(e.target.value) })}
            />
            <span className="now">{pct(look.zoom || 1)}</span>
          </div>
          <div className="prow">
            <span className="tx">{t("appearance.interfaceText")}</span>
            <div className="seg" data-text role="group" aria-label={t("appearance.interfaceTextSize")}>
              {TEXT_STEPS.map(([v, name]) => (
                <button key={v} data-action="appearance.text-size" aria-pressed={(look.textSize || 1) === v} onClick={() => set({ textSize: v === 1 ? 0 : v })}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <div className="prow">
            <span className="tx">{t("common.bodyText")}</span>
            <div className="seg" data-text role="group" aria-label={t("appearance.bodyTextSize")}>
              {readSteps().map(([v, name]) => (
                <button key={v} data-action="appearance.reading-size" aria-pressed={readSizeOf(look.readSize) === v} onClick={() => set({ readSize: v })}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <div className="prow">
            <span className="tx">{t("common.lineWidth")}</span>
            <div className="seg" data-text role="group" aria-label={t("appearance.bodyLineWidth")}>
              {MEASURES.map(([id, name, why]) => (
                <button key={id} data-action="appearance.measure" data-value={id}
                  aria-pressed={(look.width || "standard") === id} title={t(why)}
                  disabled={id === "custom" && !look.widthRange} onClick={() => set({ width: id })}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          {look.width === "custom" && (
            <div className="prow">
              <span className="tx">{t("common.conversationWidth")}</span>
              <input className="slider" type="range"
                style={at(look.widthPercent ?? 1, widthRange.min, widthRange.max)}
                min={widthRange.min} max={widthRange.max} step={widthRange.step}
                disabled={!look.widthRange} value={look.widthPercent ?? 1}
                aria-label={t("common.conversationWidth")} aria-valuetext={pct((look.widthPercent ?? 1) / 100)}
                data-action="appearance.measure" data-value="custom"
                onChange={(e) => set({ widthPercent: Number(e.target.value) })} />
              <span className="now">{pct((look.widthPercent ?? 1) / 100)}</span>
            </div>
          )}
        </div>
        <p className="hint">{t(look.width === "custom"
          ? "appearance.customPercentageResizesBothThe"
          : "appearance.lineWidthAppliesToProse")}</p>
        <ApplyNote id="size" />
      </section>

      <section className="grp" id="set-wallpaper" data-setting="wallpaper">
        <div className="grp-hd">
          <h3>{t("common.wallpaper")}</h3>
          {look.wallpaper && (
              <button className="now nowbtn" data-action="wallpaper.remove" onClick={dropPaper}>
              {t("common.remove")}
            </button>
          )}
        </div>
        <p className="hint">{t("appearance.theImageFillsOnlyThe")}</p>
        <div className="grp-items">
          <input
            ref={file}
              data-action="wallpaper.change"
            type="file"
            accept={WALLPAPER_ACCEPT}
            hidden
            onChange={(e) => {
              void pickPaper(e.target.files?.[0]);
              e.target.value = "";
            }}
          />
          <button className="paperpick" data-busy={busy ? "" : undefined} onClick={() => file.current?.click()}>
            <span className="plus" aria-hidden="true">
              <svg viewBox="0 0 16 16">
                <path d="M8 3.7v8.6M3.7 8h8.6" />
              </svg>
            </span>
            {t(look.wallpaper ? "appearance.chooseAnother" : "appearance.chooseAnImageOrVideo")}
          </button>
          <WallpaperSceneImport port={port} look={look} onLook={onLook} setPaper={setPaper} onError={setFailed} />
          {/* Composited the way the window is: the picture, then page colour
              over it. The scrim fades out with the strength, or a picture
              turned down to nothing would still be sitting under a dark wash. */}
          {look.wallpaper && (
            <div
              className="paperview"
              role="img"
              aria-label={t("common.wallpaper")}
              ref={setView}
              style={{
                backgroundImage: look.wallpaper.kind === "video" ? undefined : `url("${look.wallpaper.url}")`,
                backgroundPosition: `${Math.round(look.wallpaper.focusX * 100)}% ${Math.round(look.wallpaper.focusY * 100)}%`,
              }}
            >
              {look.wallpaper.kind === "video" && (
                <video
                  src={look.wallpaper.url}
                  muted
                  playsInline
                  preload="metadata"
                  aria-hidden="true"
                  style={{ position: "absolute", inset: 0, width: "100%", height: "100%", objectFit: "cover", pointerEvents: "none", objectPosition: `${Math.round(look.wallpaper.focusX * 100)}% ${Math.round(look.wallpaper.focusY * 100)}%` }}
                />
              )}
              <i style={{ opacity: 1 - look.wallpaper.opacity }} />
              <b style={{ opacity: look.wallpaper.dim * Math.min(look.wallpaper.opacity * 4, 1) }} />
            </div>
          )}
          {look.wallpaper && (
            <>
              <label className="prow">
                <span className="tx">{t("appearance.strength")}</span>
                <input
                  className="slider"
                  style={at(look.wallpaper.opacity, 0.05, 1)}
                  type="range"
                  min={0.05}
                  max={1}
                  step={0.05}
                  value={look.wallpaper.opacity}
                  data-action="appearance.background"
                  data-value="opacity"
                  onChange={(e) => setPaper({ opacity: Number(e.target.value) })}
                />
                <span className="now">{pct(look.wallpaper.opacity)}</span>
              </label>
              <label className="prow">
                <span className="tx">{t("appearance.dim")}</span>
                <input
                  className="slider"
                  style={at(look.wallpaper.dim, 0, 1)}
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={look.wallpaper.dim}
                  data-action="appearance.background"
                  data-value="dim"
                  onChange={(e) => setPaper({ dim: Number(e.target.value) })}
                />
                <span className="now">{pct(look.wallpaper.dim)}</span>
              </label>
              <label className="prow" data-idle={crop.x ? undefined : ""}>
                <span className="tx">{t("appearance.horizontalFocus")}</span>
                <input
                  className="slider"
                  style={at(look.wallpaper.focusX, 0, 1)}
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={look.wallpaper.focusX}
                  disabled={!crop.x}
                  data-action="appearance.background"
                  data-value="focus-x"
                  onChange={(e) => setPaper({ focusX: Number(e.target.value) })}
                />
                <span className="now">{pct(look.wallpaper.focusX)}</span>
              </label>
              <label className="prow" data-idle={crop.y ? undefined : ""}>
                <span className="tx">{t("appearance.verticalFocus")}</span>
                <input
                  className="slider"
                  style={at(look.wallpaper.focusY, 0, 1)}
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={look.wallpaper.focusY}
                  disabled={!crop.y}
                  data-action="appearance.background"
                  data-value="focus-y"
                  onChange={(e) => setPaper({ focusY: Number(e.target.value) })}
                />
                <span className="now">{pct(look.wallpaper.focusY)}</span>
              </label>
              {(look.wallpaper.audioTracks?.length || look.wallpaper.audios?.length || look.wallpaper.audio) && (
                <WallpaperVolumeSlider volume={look.wallpaper.volume} onChange={setPaper} />
              )}
              {crop.known && !(crop.x && crop.y) && (
                <p className="note">
                  {crop.x
                    ? t("appearance.atTheCurrentWindowSize")
                    : crop.y
                      ? t("appearance.atTheCurrentWindowSize2")
                      : t("appearance.thisWallpaperMatchesTheWindow")}
                </p>
              )}
            </>
          )}
        </div>
        <ApplyNote id="wallpaper" />
      </section>

      <section className="grp" id="set-language" data-setting="language">
        <div className="grp-hd">
          <h3>{t("common.language")}</h3>
        </div>
        <p className="hint">
          {t("appearance.theLanguageOfTheInterface")}
        </p>
        <div className="grp-items">
          <div className="seg" data-text role="group" aria-label={t("common.language")}>
            {LANGS.map(([id, name]) => (
              <button key={id} data-action="appearance.language" data-value={id} aria-pressed={langNow === id} onClick={() => setLang(id)}>
                {t(name)}
              </button>
            ))}
          </div>
          <p className="note">{t("appearance.changingTheLanguageReloadsThe")}</p>
        </div>
        <ApplyNote id="language" />
      </section>


      <WindowSection port={port} />
      <NavRailSection />
      <NavEntriesSection />
      <LiveGraceSection />
      <LegacySessionsSection />
      <OpenSessionLimitSection />
      <LiveStreamBudgetSection />
      <SessionQuickSection />
      <NewAnswerSection />
      <GraphicsSection />
      <SendShortcut />
      <ComposerHintSection />
      <OfficePreviewSection />
      <RunningMessageSetting />
      <EscStopSetting />
      <GlobalShortcutSection />
      <KeysSheet />
      <PageSleepSetting />
      <SessionTabsSetting />
      <Folding />
      <PlanProgressSection />
      <Notifications port={port} />

      {/* What makes this page read as a console is that all of it arrives at
          once, not that any one control is obscure. The summary says what is
          folded and whether it has been changed — a disclosure hiding a value
          somebody set is a screen disagreeing with the window itself. */}
      <details className="advset appearance-advanced">
        <summary className="grp" id="set-advanced-appearance" data-setting="advanced-appearance">
          <h3 className="grp-hd"><span>{t("common.advancedAppearance")}</span></h3>
          <span className="hint">{changed.length ? t("appearance.changed", { list: changed.join(" · ") }) : t("appearance.fontsTextWeightTextContrast")}</span>
        </summary>
        <section className="grp" id="set-font" data-setting="font">
          <div className="grp-hd">
            <h3>{t("common.typeface")}</h3>
          </div>
          <p className="hint">{t("appearance.enterATypefaceNameOr")}</p>
          <div className="grp-items">
            <FontPick
              slot="ui"
              label={t("appearance.interface")}
              value={look.fontUi ?? ""}
              options={uiFonts}
              sample={t("appearance.typefacePreviewAaBb0123")}
              onPick={(v) => set({ fontUi: v })}
            />
          </div>
          <ApplyNote id="font" />
        </section>

        <section className="grp" id="set-weight" data-setting="weight">
          <div className="grp-hd">
            <h3>{t("common.textWeight")}</h3>
          </div>
          <p className="hint">{t("appearance.adjustsStrokeWeightDenseChinese")}</p>
          <div className="grp-items">
            <div className="seg" data-text role="group" aria-label={t("common.textWeight")}>
              {WEIGHTS.map(([id, name, why]) => (
                <button key={id || "auto"} data-action="appearance.weight" data-value={id || "auto"} aria-pressed={weight === id} title={t(why)} onClick={() => onWeight(id)}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <ApplyNote id="weight" />
        </section>

        <section className="grp" id="set-effects" data-setting="effects">
          <div className="grp-hd">
            <h3>{t("common.visualEffects")}</h3>
          </div>
          <p className="hint">{t("appearance.ifTheWindowKeepsYour")}</p>
          <div className="grp-items">
            <div className="seg" data-text role="group" aria-label={t("common.visualEffects")}>
              {EFFECTS.map(([id, name, why]) => (
                <button key={id} data-action="appearance.effects" data-value={id} aria-pressed={effects === id} title={t(why)} onClick={() => setEffectsMode(id)}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <ApplyNote id="effects" />
        </section>

        <section className="grp" id="set-contrast" data-setting="contrast">
          <div className="grp-hd">
            <h3>{t("common.textContrast")}</h3>
          </div>
          <p className="hint">{t("appearance.adjustsTheContrastOfBody")}</p>
          <div className="grp-items">
            <div className="seg" data-text role="group" aria-label={t("common.textContrast")}>
              {CONTRASTS.map(([id, name, why]) => (
                <button key={id || "auto"} data-action="appearance.contrast" data-value={id || "auto"} aria-pressed={contrast === id} title={t(why)} onClick={() => onContrast(id)}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <ApplyNote id="contrast" />
        </section>
      </details>
    </>
  );
}

// One control, not two. A dropdown beside a text field looked like two settings
// and read as a question — which of them wins? — when they were only ever two
// views of one value. A field with suggestions is the same affordance without
// the question: pick one of the installed families, or type any other name.
//
// The sample underneath is drawn in the family itself, which is the only way to
// tell whether the name actually resolved to anything on this machine.
function FontPick({
  slot, label, value, options, sample, onPick,
}: {
  slot: string;
  label: string;
  value: string;
  options: string[];
  sample: string;
  onPick: (v: string) => void;
}) {
  const list = `fonts-${slot}`;
  return (
    <div className="fontrow">
      <div className="prow">
        <span className="tx">{label}</span>
        <input
          className="fontown"
          list={list}
          value={value}
          placeholder={options.length ? t("appearance.defaultAvailableOnThisMachine", { n: options.length }) : t("common.default")}
          spellCheck={false}
          autoComplete="off"
          data-action="appearance.font"
        data-target={slot}
        onChange={(e) => onPick(e.target.value)}
        />
        <datalist id={list}>
          {options.map((f) => (
            <option key={f} value={f} />
          ))}
        </datalist>
        {value && (
          <button className="nowbtn" data-action="appearance.font" data-target={slot} data-value="default" onClick={() => onPick("")} title={t("appearance.backToTheDefaultTypeface")}>
            {t("common.clear")}
          </button>
        )}
      </div>
      <div
        className="fontsample"
        data-slot={slot}
        style={value ? { fontFamily: `"${value}", ${slot === "mono" ? "monospace" : "sans-serif"}` } : undefined}
      >
        {sample}
      </div>
    </div>
  );
}

// The card paints itself from the pack's own tokens, so a palette is picked by
// looking at it rather than by reading its name — and a pack that ships no
// picture, or whose picture fails to load, still previews correctly.
function Swatch({
  pack, theme, name, on, onPick,
}: {
  pack?: ThemePack;
  theme?: string;
  name: string;
  on: boolean;
  onPick: () => void;
}) {
  const [failedPreview, setFailedPreview] = useState<ThemePack>();
  const tokens = pack && (pack.tokens[activeScheme(theme)] ?? pack.tokens.light ?? pack.tokens.dark);
  const style = tokens
    ? ({
        "--pal-page": tokens.bg,
        "--pal-surface": tokens.bgSoft ?? tokens.bg,
        "--pal-panel": tokens.panel ?? tokens.bgSoft ?? tokens.bg,
        "--pal-border": tokens.border,
        "--pal-text": tokens.fg,
        "--pal-dim": tokens.fgDim ?? tokens.fg,
        "--pal-accent": tokens.accent,
      } as CSSProperties)
    : undefined;

  return (
    <button className="pal" type="button" data-action="theme.activate" data-value={pack?.id ?? ""} title={pack?.description ? t(pack.description) : name} data-on={on ? "" : undefined} aria-pressed={on} onClick={onPick}>
      <span className="pal-art" style={style}>
        <span className="pal-rail" />
        <span className="pal-body">
          <span className="pal-line" />
          <span className="pal-line" data-short />
          <span className="pal-mark" />
        </span>
        {pack?.hasPreview && failedPreview !== pack && (
          <img
            className="pal-shot"
            src={`/themes/${encodeURIComponent(pack.id)}/preview`}
            alt=""
            loading="lazy"
            onError={() => setFailedPreview(pack)}
          />
        )}
      </span>
      <span className="pal-nm">
        <b>{name}</b>
        <em>{pack ? pack.author || t("appearance.thirdParty") : t("common.builtIn")}</em>
      </span>
    </button>
  );
}

// App writes the resolved scheme onto the document, which is the only place
// "auto" has an answer.
function activeScheme(theme?: string): "light" | "dark" {
  if (theme === "light" || theme === "dark") return theme;
  return document.documentElement.dataset.theme === "dark" ? "dark" : "light";
}
