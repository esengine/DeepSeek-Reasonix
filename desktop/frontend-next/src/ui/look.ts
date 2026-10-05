import type { Appearance } from "../port/port";
import { current } from "../i18n";
import { refresh as refreshWidth } from "./viewport";

/** 正文起点。汉字同字号下笔画更密，中文高一档。求值留到调用时：模块加载早于
 *  i18n 定语言。这一档和 --doc 一起定行长：两个数只有一起看才有意义。
 *  这就是界面实际渲染的字号 —— 各个阅读面从它换算，不再各写各的字面值。 */
export const readDefault = (): number => (current() === "zh" ? 13 : 12.5);

/** 正文曾被各个阅读面写死，档位是围着那时的字号排的。存下来的是"哪一档"而不
 *  是像素，所以旧值按档位读回，选过"标准"的人还是标准。 */
const RETIRED_STEPS: Record<string, number[]> = { zh: [14.5, 16, 18, 20], en: [13.5, 15, 17, 19] };

export function readSizeOf(stored?: number): number {
  if (!stored) return readDefault();
  const retired = RETIRED_STEPS[current() === "zh" ? "zh" : "en"];
  const rung = retired.indexOf(stored);
  return rung < 0 ? stored : readSteps()[rung][0];
}

/** 字号档位。中间那档就是 readDefault()，两边一起改。 */
export const readSteps = (): [number, string][] =>
  current() === "zh"
    ? [
        [12, "小"],
        [13, "标准"],
        [15, "大"],
        [17, "更大"],
      ]
    : [
        [11.5, "小"],
        [12.5, "标准"],
        [14.5, "大"],
        [16.5, "更大"],
      ];

// The user's own settings, applied after a pack so they win: a pack is a
// palette somebody else authored, and the size someone reads at is not
// something an author gets to overrule.
//
// It shares the pack's image variables rather than adding a second layer —
// two backgrounds behind one window is a bug, not a feature — so a wallpaper
// replaces a pack's picture instead of stacking on it.
const IMAGE_VARS = ["--bg-image", "--bg-x", "--bg-y", "--bg-alpha", "--bg-overlay"];

/** apply paints the user's appearance, or clears back to the pack's own.
 *  `busy` fades the picture while a turn runs, the same as a pack's does: a
 *  photo that is right behind an idle window is in the way of one being read. */
export function apply(look: Appearance | null, busy = false) {
  const root = document.documentElement;
  const style = root.style;

  // Zoom scales everything — type, spacing, borders — which is what "make it
  // bigger" means when 1500 sizes in the stylesheet are written in pixels. It
  // rides a variable as well, because vh and vw keep measuring the unscaled
  // viewport: without dividing them back out, a scaled-up window pushes its own
  // composer off the bottom of the screen.
  if (look?.zoom && look.zoom !== 1) {
    style.setProperty("zoom", String(look.zoom));
    style.setProperty("--zoom", String(look.zoom));
  } else {
    style.removeProperty("zoom");
    style.removeProperty("--zoom");
  }
  // Zoom changes how much room the layout has without the window moving, so the
  // resize observer has nothing to report. Say so directly.
  refreshWidth();

  // Reading size moves the transcript's prose alone, so the frame around it
  // stays where the layout put it.
  style.setProperty("--read", `${readSizeOf(look?.readSize)}px`);

  // How far that prose runs is the other half of the same question, and the
  // only one with two honest answers: a line short enough to read without
  // losing your place, or a window used to the edge. Cards, commands and
  // output answer to neither — they always have the width.
  if (look?.width === "full") root.dataset.measure = "full";
  else delete root.dataset.measure;

  if (look?.fontUi) style.setProperty("--ui", `${look.fontUi}, ${FALLBACK_UI}`);
  else style.removeProperty("--ui");
  if (look?.fontMono) style.setProperty("--mono", `${look.fontMono}, ${FALLBACK_MONO}`);
  else style.removeProperty("--mono");

  const paper = look?.wallpaper;
  if (!paper) return;
  for (const v of IMAGE_VARS) style.removeProperty(v);
  style.setProperty("--bg-image", `url("${paper.url}")`);
  style.setProperty("--bg-x", `${pct(paper.focusX)}%`);
  style.setProperty("--bg-y", `${pct(paper.focusY)}%`);
  // At work it recedes to a third of what it is at rest, so the picture never
  // competes with the run it is behind.
  style.setProperty("--bg-alpha", String(busy ? paper.opacity * 0.35 : paper.opacity));
  style.setProperty("--bg-overlay", String(paper.dim));
  root.dataset.bg = "on";
}

// A chosen family is a first choice, never the only one: a name that stops
// resolving — an uninstalled font, a synced config from another machine —
// must not take the interface's legibility with it.
const FALLBACK_UI = `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "PingFang SC", "Microsoft YaHei UI", "Noto Sans SC", Arial, sans-serif`;
const FALLBACK_MONO = `ui-monospace, "Cascadia Code", "SF Mono", SFMono-Regular, Consolas, "Liberation Mono", Menlo, monospace`;

function pct(v: number): number {
  if (!Number.isFinite(v)) return 50;
  return Math.round(Math.max(0, Math.min(1, v)) * 100);
}

/** installed filters a list of families down to the ones this machine really
 *  has. It measures rather than asks: document.fonts.check() answers true for
 *  every name, including ones nobody has ever installed, so a picker built on
 *  it offers fonts that silently do nothing when chosen.
 *
 *  A family that is present renders the probe at a different width than the
 *  generic it would otherwise fall back to. Two generics are tried because a
 *  font can coincidentally match one of them. */
export function installed(families: string[]): string[] {
  const cv = document.createElement("canvas").getContext("2d");
  if (!cv) return families;
  const PROBE = "mmmmmmmmmmlliWWWW@#$%1234";
  const width = (family: string) => {
    cv.font = `72px ${family}`;
    return cv.measureText(PROBE).width;
  };
  const mono = width("monospace");
  const serif = width("serif");
  return families.filter((f) => width(`"${f}", monospace`) !== mono || width(`"${f}", serif`) !== serif);
}

// Offered because they are common, not because they are the only ones that
// work: the field next to the list takes any family the machine has.
export const UI_FAMILIES = [
  "PingFang SC", "Microsoft YaHei UI", "Noto Sans SC", "Source Han Sans SC", "HarmonyOS Sans SC",
  "LXGW WenKai", "Songti SC", "SimSun", "Inter", "Segoe UI", "Roboto", "Helvetica Neue", "Arial",
];

export const MONO_FAMILIES = [
  "SF Mono", "Menlo", "Monaco", "Cascadia Code", "Consolas", "JetBrains Mono", "Fira Code",
  "Source Code Pro", "IBM Plex Mono", "LXGW WenKai Mono", "Sarasa Mono SC", "Maple Mono",
];
