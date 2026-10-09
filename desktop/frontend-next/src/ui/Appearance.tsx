import { ApplyNote } from "./Group";
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import type { AgentPort, Appearance as Look, ThemePack } from "../port/port";
import { MONO_FAMILIES, UI_FAMILIES, installed, readSizeOf, readSteps } from "./look";
import { STORAGE as LANG_KEY, t } from "../i18n";
import { pct } from "../i18n/format";
import { reason } from "../i18n/kernel";
import { Notifications } from "./Notifications";
import { Folding } from "./Folding";
import { WindowSection } from "./WindowSection";
import { NavRailSection } from "./NavRailSection";
import { GraphicsSection } from "./GraphicsSection";
import { ThemeImport } from "./ThemeImport";
import { useThemeInventory } from "./useThemeInventory";

// "" follows the machine; the rest are explicit, the same shape the light/dark
// control uses.
const LANGS: [string, string][] = [
  ["", "跟随系统"],
  ["zh", "中文"],
  ["en", "英文"],
];

// Exported because the nav rail names the current one next to the tab.
export const SCHEMES: [string, string][] = [
  ["auto", "跟随系统"],
  ["light", "浅色"],
  ["dark", "深色"],
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

// Whole-interface scale. Named by what it does to reading rather than by its
// number: nobody wants "115%", they want it bigger.
const ZOOMS: [number, string][] = [
  [0.9, "紧凑"],
  [1, "标准"],
  [1.15, "宽松"],
  [1.3, "更大"],
];

// The named steps stop where a 1080p window does. Past that there is no step
// anyone could have named in advance — a 2560 display wants something a 3840 one
// does not — so the range takes over rather than the list growing a tail of
// numbers. It reaches this far safely because the layout now folds on the room
// it actually has (see viewport.ts) instead of on the unscaled window.
const ZOOM_RANGE = { min: 0.8, max: 2.5, step: 0.05 };

// Body size in the transcript alone, so the frame stays where the layout put it.
// The steps live in look.ts next to the default they have to agree with.

// "" follows the system's own accessibility setting; the rest are explicit.
// 笔画粗细是另一条轴：颜色调得再亮，400 的汉字在 11px 上仍然是细的。默认跟着
// 语言走 —— 汉字笔画密，同字号下比拉丁更早糊。
const WEIGHTS: [string, string, string][] = [
  ["", "跟随语言", "中文界面使用中等字重，西文使用常规字重"],
  ["light", "常规", "笔画更细，字距更宽松"],
  ["heavy", "加粗", "小字号下更扎实，屏幕距离较远或有反光时更易读"],
];

// Two answers, because there are two: a line short enough that the eye finds
// the next one without hunting, or a window used to its edge. A step between
// them would be a number nobody could tell apart from its neighbour.
const MEASURES: [string, string, string][] = [
  ["standard", "适宜阅读", "每行长度控制在一眼能回到行首的范围"],
  ["full", "铺满窗口", "正文跟随窗口宽度，宽屏上不留两侧空白"],
];

const CONTRASTS: [string, string, string][] = [
  ["", "跟随系统", "系统已开启「增强对比度」时使用最强档"],
  ["soft", "柔和", "正文不易刺眼，长时间阅读更省力"],
  ["normal", "标准", "介于两者之间"],
  ["strong", "更强", "环境光较强，或需要更清晰的边界"],
];

// WebKit 的滑块轨道不知道当前值，填充比例得由调用方喂进来。
const at = (v: number, min: number, max: number) =>
  ({ "--at": `${Math.round(((v - min) / (max - min)) * 100)}%` }) as React.CSSProperties;

/** useCrop answers which focal axis has room to move. A `cover` picture wider
 *  than its box is cropped left and right and nowhere else, so the other axis
 *  cannot move a pixel however far its slider is dragged. It measures the
 *  preview, which is drawn at the window's shape, rather than assuming one.
 *  Both axes count as free until the image decodes: never disable on a guess. */
function useCrop(url: string | undefined, box: HTMLElement | null) {
  const [image, setImage] = useState(0);
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    setImage(0);
    if (!url) return;
    const img = new Image();
    img.onload = () => setImage(img.naturalHeight ? img.naturalWidth / img.naturalHeight : 0);
    img.src = url;
    return () => {
      img.onload = null;
    };
  }, [url]);

  useEffect(() => {
    if (!box) return;
    const read = () => setFrame(box.clientHeight ? box.clientWidth / box.clientHeight : 0);
    read();
    const ro = new ResizeObserver(read);
    ro.observe(box);
    return () => ro.disconnect();
  }, [box]);

  // Ratios a pixel apart leave no room either way, hence the 1% of slack.
  const known = image > 0 && frame > 0;
  return { known, x: !known || image > frame * 1.01, y: !known || image < frame / 1.01 };
}

export function Appearance({ port, theme, onTheme, contrast, onContrast, weight, onWeight, reloadThemes, look, onLook }: Props) {
  const { packs, unread, load } = useThemeInventory(port);

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

  const custom = packs.some((p) => p.active);
  // Calm, workbench-like themes stay in the first shelf. Illustrated packs
  // are still first-class installed themes, but they no longer dominate the
  // page a user opens merely to switch between light and dark. If one of them
  // is active it remains visible beside the foundations, so the current state
  // never disappears into a closed disclosure.
  const foundations = packs.filter((p) => !p.background?.image);
  const activeIllustrated = packs.find((p) => p.active && p.background?.image);
  const featured = activeIllustrated ? [...foundations, activeIllustrated] : foundations;
  const illustrated = packs.filter((p) => p.background?.image && p.id !== activeIllustrated?.id);

  // A change lands on screen through App's own effect; this only sends it on
  // so the next launch opens the same way.
  const set = useCallback((patch: Partial<Look>) => onLook({ ...look, ...patch }), [look, onLook]);
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
  const crop = useCrop(look.wallpaper?.url, view);

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
    look.fontUi ? t("字体") : "",
    weight ? t("文字粗细") : "",
    contrast ? t("文字对比度") : "",
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
          <h3>{t("明暗")}</h3>
        </div>
        <p className="hint">{t("跟随系统时会随系统主题切换；手动选择后将保持固定。")}</p>
        <div className="grp-items">
          <div className="seg" data-text role="group" aria-label={t("明暗")}>
            {SCHEMES.map(([id, name]) => (
              <button key={id} data-action="appearance.scheme" data-value={id} aria-pressed={theme === id} onClick={() => onTheme(id)}>
                {t(name)}
              </button>
            ))}
          </div>
        </div>
        <ApplyNote id="mode" />
      </section>

      <section className="grp" id="set-scheme" data-setting="scheme">
        <div className="grp-hd">
          <h3>{t("界面主题")}</h3>
          <span className="now">{t("立即生效")}</span>
        </div>
        <p className="hint">{t("选择工作台的表面色与强调色。成功、警告和失败等状态色保持一致，代码字体在下方单独设置。")}</p>
        <div className="grp-items">
          <div className="palettes" role="group" aria-label={t("界面主题")}>
            <Swatch name={t("默认")} on={!custom} onPick={() => pick("")} />
            {featured.map((p) => (
              <Swatch key={p.id} pack={p} theme={theme} name={p.name} on={!!p.active} onPick={() => pick(p.id)} />
            ))}
          </div>
          {illustrated.length > 0 && (
            <details className="theme-more">
              <summary>
                <span>
                  <b>{t("更多已安装主题")}</b>
                  <em>{t("插画与背景主题，可随时切回基础主题")}</em>
                </span>
                <span className="now">{t("{n} 个", { n: illustrated.length })}</span>
              </summary>
              <div className="palettes" role="group" aria-label={t("更多已安装主题")}>
                {illustrated.map((p) => (
                  <Swatch key={p.id} pack={p} theme={theme} name={p.name} on={!!p.active} onPick={() => pick(p.id)} />
                ))}
              </div>
            </details>
          )}
          {/* A pack loads with its good tokens and says which ones it lost. The
              author is the only one who can fix that, and they will not read a
              log — so it is here, next to the thing that looks wrong. */}
          {packs.filter((p) => p.warnings?.length).map((p) => (
            <div className="find" data-lvl="warn" key={p.id}>
              <span className="t">{t("{name} 有几项没生效", { name: p.name })}</span>
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
          <h3>{t("代码外观")}</h3>
        </div>
        <p className="hint">{t("代码块、命令、差异与工具输出共用这一等宽字体，不跟随界面主题改变。")}</p>
        <div className="grp-items">
          <FontPick
            slot="mono"
            label={t("等宽字体")}
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
          <h3>{t("大小")}</h3>
        </div>
        <p className="hint">{t("「界面」会同时缩放边距与控件，「正文」仅调整对话中的文字大小，两者独立设置。")}</p>
        <div className="grp-items">
          <div className="prow">
            <span className="tx">{t("界面")}</span>
            <div className="seg" data-text role="group" aria-label={t("界面大小")}>
              {ZOOMS.map(([v, name]) => (
                <button key={v} data-action="appearance.zoom" aria-pressed={(look.zoom || 1) === v} onClick={() => set({ zoom: v })}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <div className="prow">
            <span className="tx">{t("微调")}</span>
            <input
              className="slider"
              style={at(look.zoom || 1, ZOOM_RANGE.min, ZOOM_RANGE.max)}
              type="range"
              min={ZOOM_RANGE.min}
              max={ZOOM_RANGE.max}
              step={ZOOM_RANGE.step}
              value={look.zoom || 1}
              aria-label={t("界面大小微调")}
              data-action="appearance.zoom"
              onChange={(e) => set({ zoom: Number(e.target.value) })}
            />
            <span className="now">{pct(look.zoom || 1)}</span>
          </div>
          <div className="prow">
            <span className="tx">{t("正文")}</span>
            <div className="seg" data-text role="group" aria-label={t("正文字号")}>
              {readSteps().map(([v, name]) => (
                <button key={v} data-action="appearance.reading-size" aria-pressed={readSizeOf(look.readSize) === v} onClick={() => set({ readSize: v })}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          {/* Beside the size because they are one question asked twice: how big
              the letters are, and how far they run before the eye comes back. */}
          <div className="prow">
            <span className="tx">{t("行宽")}</span>
            <div className="seg" data-text role="group" aria-label={t("正文行宽")}>
              {MEASURES.map(([id, name, why]) => (
                <button key={id} data-action="appearance.measure" data-value={id}
                  aria-pressed={(look.width || "standard") === id} title={t(why)} onClick={() => set({ width: id })}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
        </div>
        <p className="hint">{t("行宽只管正文：代码、命令与工具输出始终占满可用宽度。")}</p>
        <ApplyNote id="size" />
      </section>

      <details className="advset personal">
        <summary>
          <span className="tx">
            <span className="lb">{t("更多个性化")}</span>
            <span className="ds">{look.wallpaper ? t("已设置自定义壁纸") : t("壁纸与背景位置")}</span>
          </span>
        </summary>
      <section className="grp" id="set-wallpaper" data-setting="wallpaper">
        <div className="grp-hd">
          <h3>{t("壁纸")}</h3>
          {look.wallpaper && (
              <button className="now nowbtn" data-action="wallpaper.remove" onClick={dropPaper}>
              {t("移除")}
            </button>
          )}
        </div>
        <p className="hint">{t("壁纸只显示在窗口的空白区域，卡片和输入框保持不透明以确保文字清晰。任务运行时壁纸会自动降低透明度。")}</p>
        <div className="grp-items">
          <input
            ref={file}
              data-action="wallpaper.change"
            type="file"
            accept="image/png,image/jpeg,image/webp,image/avif,image/gif"
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
            {t(look.wallpaper ? "换一张…" : "选择图片…")}
          </button>
          {/* Composited the way the window is: the picture, then page colour
              over it. The scrim fades out with the strength, or a picture
              turned down to nothing would still be sitting under a dark wash. */}
          {look.wallpaper && (
            <div
              className="paperview"
              ref={setView}
              style={{
                backgroundImage: `url("${look.wallpaper.url}")`,
                backgroundPosition: `${Math.round(look.wallpaper.focusX * 100)}% ${Math.round(look.wallpaper.focusY * 100)}%`,
              }}
            >
              <i style={{ opacity: 1 - look.wallpaper.opacity }} />
              <b style={{ opacity: look.wallpaper.dim * Math.min(look.wallpaper.opacity * 4, 1) }} />
            </div>
          )}
          {look.wallpaper && (
            <>
              <label className="prow">
                <span className="tx">{t("浓度")}</span>
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
                <span className="tx">{t("压暗")}</span>
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
                <span className="tx">{t("横向焦点")}</span>
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
                <span className="tx">{t("纵向焦点")}</span>
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
              {crop.known && !(crop.x && crop.y) && (
                <p className="note">
                  {crop.x
                    ? t("当前窗口尺寸下，此图片只有左右会被裁剪，上下正好填满，因此纵向焦点无法调整。")
                    : crop.y
                      ? t("当前窗口尺寸下，此图片只有上下会被裁剪，左右正好填满，因此横向焦点无法调整。")
                      : t("此图片与窗口宽高比一致，四边均未裁剪；改变窗口尺寸后才可调整焦点。")}
                </p>
              )}
            </>
          )}
        </div>
        <ApplyNote id="wallpaper" />
      </section>
      </details>

      <section className="grp" id="set-language" data-setting="language">
        <div className="grp-hd">
          <h3>{t("语言")}</h3>
        </div>
        <p className="hint">
          {t("界面显示语言。模型回复所用的语言与此设置无关，将跟随你发送消息时使用的语言。")}
        </p>
        <div className="grp-items">
          <div className="seg" data-text role="group" aria-label={t("语言")}>
            {LANGS.map(([id, name]) => (
              <button key={id} data-action="appearance.language" data-value={id} aria-pressed={langNow === id} onClick={() => setLang(id)}>
                {t(name)}
              </button>
            ))}
          </div>
          <p className="note">{t("语言更改需重启窗口后生效")}</p>
        </div>
        <ApplyNote id="language" />
      </section>


      <WindowSection port={port} />
      <NavRailSection />
      <GraphicsSection />
      <Folding />
      <Notifications port={port} />

      {/* What makes this page read as a console is that all of it arrives at
          once, not that any one control is obscure. The summary says what is
          folded and whether it has been changed — a disclosure hiding a value
          somebody set is a screen disagreeing with the window itself. */}
      <details className="advset">
        <summary>
          <span className="tx">
            <span className="lb">{t("高级外观")}</span>
            <span className="ds">{changed.length ? t("已修改：{list}", { list: changed.join(" · ") }) : t("字体、文字粗细、文字对比度")}</span>
          </span>
        </summary>
        <section className="grp" id="set-font" data-setting="font">
          <div className="grp-hd">
            <h3>{t("字体")}</h3>
          </div>
          <p className="hint">{t("可直接输入字体名称，或选择本机已安装的字体。下方文本会实时预览所选字体；若字体不可用，将自动使用默认字体。")}</p>
          <div className="grp-items">
            <FontPick
              slot="ui"
              label={t("界面")}
              value={look.fontUi ?? ""}
              options={uiFonts}
              sample={t("字体预览 · 中文 Aa Bb 0123")}
              onPick={(v) => set({ fontUi: v })}
            />
          </div>
          <ApplyNote id="font" />
        </section>

        <section className="grp" id="set-weight" data-setting="weight">
          <div className="grp-hd">
            <h3>{t("文字粗细")}</h3>
          </div>
          <p className="hint">{t("调整文字的笔画粗细。中文笔画密集，在小字号下加粗有助于提升清晰度。")}</p>
          <div className="grp-items">
            <div className="seg" data-text role="group" aria-label={t("文字粗细")}>
              {WEIGHTS.map(([id, name, why]) => (
                <button key={id || "auto"} data-action="appearance.weight" data-value={id || "auto"} aria-pressed={weight === id} title={t(why)} onClick={() => onWeight(id)}>
                  {t(name)}
                </button>
              ))}
            </div>
          </div>
          <ApplyNote id="weight" />
        </section>

        <section className="grp" id="set-contrast" data-setting="contrast">
          <div className="grp-hd">
            <h3>{t("文字对比度")}</h3>
          </div>
          <p className="hint">{t("同时调整正文与次要文字的对比度。深色主题下若觉得刺眼，可调向「柔和」。")}</p>
          <div className="grp-items">
            <div className="seg" data-text role="group" aria-label={t("文字对比度")}>
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
          placeholder={options.length ? t("默认 · 本机有 {n} 个可选", { n: options.length }) : t("默认")}
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
          <button className="nowbtn" data-action="appearance.font" data-target={slot} data-value="default" onClick={() => onPick("")} title={t("恢复默认字体")}>
            {t("清除")}
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
    <button className="pal" type="button" data-action="theme.activate" data-value={pack?.id ?? ""} title={pack?.description || name} data-on={on ? "" : undefined} aria-pressed={on} onClick={onPick}>
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
        <em>{pack ? pack.author || t("第三方") : t("内置")}</em>
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
