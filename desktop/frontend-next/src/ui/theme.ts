import type { ThemePack } from "../port/port";

// A pack names things in its own vocabulary; this is where they become ours.
// The mapping lives on this side because only the frontend knows what each
// surface in its layout is called — a pack should not have to learn our
// variable names to be worth installing. The vocabulary itself is the kernel's
// (internal/ext/theme.Tokens), and TestThemeTokenVocabularyMatchesTheFrontend holds
// the two halves together.
const SURFACE: Record<string, string[]> = {
  bg: ["--page"],
  bgSoft: ["--surface"],
  panel: ["--raised"],
  bgElev: ["--overlay"],
  border: ["--border"],
  borderSoft: ["--hair"],
  fg: ["--text"],
  fgDim: ["--muted"],
  fgFaint: ["--faint", "--ghost"],
  fgStrong: ["--text-strong"],
  accent: ["--accent"],
  accentFg: ["--accent-fg"],
  link: ["--link"],
  brand: ["--brand"],
  halo: ["--halo"],
  labelAgent: ["--label-agent"],
  float: ["--float"],
  floatHi: ["--float-hi"],
  codeBg: ["--face-code"],
  sunkBg: ["--face-sunk"],
  synKeyword: ["--syn-key"],
  synString: ["--syn-str"],
  synNumber: ["--syn-num"],
  synFunction: ["--syn-fn"],
  // Shape and type. --r-pill is absent because a pill is a shape rather than a
  // size: a pack that could set it would round a button into something else.
  radiusXs: ["--r-xs"],
  radiusSm: ["--r-sm"],
  radiusMd: ["--r-md"],
  fontUi: ["--ui"],
  fontMono: ["--mono"],
};

// What a pack's own surfaces imply for the grounds it did not name.
const DERIVED: [string, { source: string; paint: (t: Record<string, string>) => string }][] = [
  ["float", { source: "panel", paint: (t) => t.panel }],
  ["floatHi", { source: "bgElev", paint: (t) => t.bgElev }],
  ["codeBg", { source: "bgSoft", paint: (t) => `color-mix(in srgb, ${t.bgSoft} 50%, ${t.bg ?? t.bgSoft})` }],
  ["sunkBg", { source: "bg", paint: (t) => t.bg }],
];

// Tinted backgrounds that follow a decorative colour, as --accent-wash follows the accent.
const WASHES: [string, string][] = [
  ["brand", "--brand-wash"],
  ["labelAgent", "--label-agent-wash"],
];

// What a pack may not touch. ok/warn/err/net/deleg encode what is happening —
// "this broke", "this is running", "this went out to a sub-agent" — and a
// theme that could recolour them would let a failure render as success. The
// palette is the theme's; the meanings are the app's.
const RESERVED = ["--ok", "--warn", "--err", "--net", "--deleg", "--add", "--del", "--focus"];

// How far each ink moves per contrast step, in OKLCH lightness points, read off
// the built-in palette's own three steps. A pack states one set of inks, which
// is the step someone who never opened the setting sees; the stronger two are
// derived from it so choosing a palette never costs the reader their setting.
const STEPS: Record<string, [number, number]> = { fg: [3.0, 10.1], fgStrong: [3.0, 10.1], fgDim: [3.5, 7.5], fgFaint: [3.5, 7.0] };

// The reader's contrast outranks the author's palette, the same way reading
// size does: an author chose colours, not how legible they have to be for
// whoever is in front of them. Written as a lightness offset rather than a mix
// because that is what the setting means; a browser without relative colour
// drops the declaration and keeps the stylesheet's own step, which is the
// safe way to be wrong here.
function ink(value: string, name: string, scheme: "light" | "dark", contrast: string): string {
  const step = STEPS[name];
  if (!step) return value;
  const by = contrast === "normal" ? step[0] : contrast === "strong" ? step[1] : 0;
  if (!by) return value;
  return `oklch(from ${value} calc(l ${scheme === "light" ? "-" : "+"} ${(by / 100).toFixed(3)}) c h)`;
}

const SHEET_ID = "pack-theme";

// Specificity ties the built-in contrast tiers (`:root[data-theme][data-contrast]`),
// so the sheet wins them by coming later and a reader's or author's stylesheet
// that comes later still wins it without `!important`.
const SELECTOR = ":root:root[data-pack]";

/** apply paints a pack onto the document, or clears back to the stylesheet.
 *  The pack's values are the declarations of one generated rule, written through
 *  the CSSOM so a value the parser rejects is dropped rather than able to end
 *  the rule. `busy` dims the picture while a turn runs: a photo that is right
 *  behind an idle window is in the way of a transcript being read. */
export function apply(pack: ThemePack | null, scheme: "light" | "dark", busy = false, contrast = "") {
  const root = document.documentElement;
  // The flags are what let the page surface go transparent. They are removed
  // first so a pack without a picture never leaves the previous one's window open.
  delete root.dataset.bg;
  delete root.dataset.sky;
  const paint = pack ? declarations(pack, scheme, busy, contrast) : [];
  const owned = document.getElementById(SHEET_ID);
  if (!pack || !paint.length) {
    owned?.remove();
    delete root.dataset.pack;
    return;
  }
  root.dataset.pack = pack.id;
  const style = place(owned as HTMLStyleElement | null);
  const sheet = style.sheet;
  if (!sheet) return;
  while (sheet.cssRules.length) sheet.deleteRule(0);
  sheet.insertRule(`${SELECTOR} {}`);
  const decl = (sheet.cssRules[0] as CSSStyleRule).style;
  for (const [name, value] of paint) decl.setProperty(name, value);
  if (pack.sky) root.dataset.sky = "on";
  if (pack.sky || pack.background?.image) root.dataset.bg = "on";
}

// Created at the end of <head> once and then left where it is: a stylesheet
// added after it is an author's, and repainting the pack must not demote it.
function place(existing: HTMLStyleElement | null): HTMLStyleElement {
  if (existing) return existing;
  const style = document.createElement("style");
  style.id = SHEET_ID;
  document.head.append(style);
  return style;
}

function declarations(pack: ThemePack, scheme: "light" | "dark", busy: boolean, contrast: string): [string, string][] {
  const tokens = pack.tokens[scheme];
  if (!tokens) return [];
  const out: [string, string][] = [];
  for (const [name, value] of Object.entries(tokens)) {
    const paint = ink(value, name, scheme, contrast);
    for (const v of SURFACE[name] ?? []) out.push([v, paint]);
  }
  // A pack written before these grounds existed still moves them: each follows
  // the surface it sits on, instead of staying on the default palette under it.
  for (const [token, from] of DERIVED) {
    if (tokens[token] || !tokens[from.source]) continue;
    for (const v of SURFACE[token]) out.push([v, from.paint(tokens)]);
  }
  // The washes are tints of the accent, so a pack that moves the accent has to
  // move them too or the tinted backgrounds keep pointing at the old hue.
  if (tokens.accent) {
    out.push(["--accent-wash", `color-mix(in srgb, ${tokens.accent} 12%, ${tokens.bg ?? "transparent"})`]);
  }
  for (const [token, wash] of WASHES) {
    if (tokens[token]) out.push([wash, `color-mix(in srgb, ${tokens[token]} 12%, ${tokens.bg ?? "transparent"})`]);
  }

  // The sky is drawn rather than placed, so it is independent of the picture:
  // a pack can have either, both, or neither.
  const sky = pack.sky;
  if (sky) {
    if (sky.ray) out.push(["--ray", sky.ray]);
    if (sky.cloud) out.push(["--cloud-hi", sky.cloud]);
    if (sky.cloudLit) out.push(["--cloud-gilt", sky.cloudLit]);
    out.push(["--ray-a", String(sky.rayAlpha)], ["--cloud-a", String(sky.cloudAlpha)]);
  }

  const bg = pack.background;
  if (!bg?.image) return out;
  // The server owns freshness for this mutable URL. encodeURIComponent keeps
  // a pack id out of the CSS url() grammar.
  out.push(["--bg-image", `url("/themes/${encodeURIComponent(pack.id)}/background")`]);
  out.push(["--bg-x", `${pct(bg.focusX)}%`], ["--bg-y", `${pct(bg.focusY)}%`]);
  out.push(["--bg-alpha", String(busy ? bg.taskOpacity : bg.homeOpacity)]);
  // A light palette already carries dark readable ink and pale panels. Using
  // the same opaque page-colour veil as dark mode washed illustrations into a
  // nearly white sheet, especially in the empty state. Keep a gentler scrim in
  // light mode; cards and navigation provide their own local contrast.
  out.push(["--bg-overlay", String(scheme === "light" ? bg.overlayStrength * 0.58 : bg.overlayStrength)]);
  return out;
}

function pct(v: number | undefined): number {
  if (typeof v !== "number" || Number.isNaN(v)) return 50;
  return Math.round(Math.max(0, Math.min(1, v)) * 100);
}

/** reserved is exported for the test that pins the meanings a pack cannot take. */
export const reserved = RESERVED;
