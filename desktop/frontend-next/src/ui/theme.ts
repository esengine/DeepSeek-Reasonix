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

// What a pack may not touch. ok/warn/err/net/deleg encode what is happening —
// "this broke", "this is running", "this went out to a sub-agent" — and a
// theme that could recolour them would let a failure render as success. The
// palette is the theme's; the meanings are the app's.
const RESERVED = ["--ok", "--warn", "--err", "--net", "--deleg", "--add", "--del", "--focus"];

// Variables the picture rides on. They are set together with the colours so a
// pack never lands half-applied — an image over the previous palette.
const IMAGE_VARS = ["--bg-image", "--bg-x", "--bg-y", "--bg-alpha", "--bg-overlay"];

// The live backdrop's tints. Cleared with the image vars for the same reason:
// a pack must never land half-applied.
const SKY_VARS = ["--ray", "--ray-a", "--cloud-a", "--cloud-hi", "--cloud-gilt"];

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

/** apply paints a pack onto the document, or clears back to the stylesheet.
 *  `busy` dims the picture while a turn runs: a photo that is right behind an
 *  idle window is in the way of a transcript being read. */
export function apply(pack: ThemePack | null, scheme: "light" | "dark", busy = false, contrast = "") {
  const root = document.documentElement;
  for (const v of [...IMAGE_VARS, ...SKY_VARS]) root.style.removeProperty(v);
  // The flag is what lets the page surface go transparent. It is removed first
  // so a pack without a picture never leaves the previous one's window open.
  delete root.dataset.bg;
  delete root.dataset.sky;
  for (const vars of Object.values(SURFACE)) {
    for (const v of vars) root.style.removeProperty(v);
  }
  root.style.removeProperty("--accent-wash");
  // Contrast is the reader's, so it moves the built-in inks too — not only a
  // pack that happens to declare them. Read after the clear above, or each pass
  // would offset the one before it.
  const base = getComputedStyle(root);
  for (const name of Object.keys(STEPS)) {
    const authored = pack?.tokens[scheme]?.[name];
    const from = authored || base.getPropertyValue(SURFACE[name][0]).trim();
    if (!authored && from) {
      for (const v of SURFACE[name]) root.style.setProperty(v, ink(from, name, scheme, contrast));
    }
  }
  if (!pack) return;

  const tokens = pack.tokens[scheme];
  if (!tokens) return;
  for (const [name, value] of Object.entries(tokens)) {
    const paint = ink(value, name, scheme, contrast);
    for (const v of SURFACE[name] ?? []) root.style.setProperty(v, paint);
  }
  // A pack written before these grounds existed still moves them: each follows
  // the surface it sits on, instead of staying on the default palette under it.
  for (const [token, from] of DERIVED) {
    if (tokens[token] || !tokens[from.source]) continue;
    for (const v of SURFACE[token]) root.style.setProperty(v, from.paint(tokens));
  }
  // The washes are tints of the accent, so a pack that moves the accent has to
  // move them too or the tinted backgrounds keep pointing at the old hue.
  if (tokens.accent) {
    root.style.setProperty("--accent-wash", `color-mix(in srgb, ${tokens.accent} 12%, ${tokens.bg ?? "transparent"})`);
  }

  // The sky is drawn rather than placed, so it is independent of the picture:
  // a pack can have either, both, or neither.
  const sky = pack.sky;
  if (sky) {
    if (sky.ray) root.style.setProperty("--ray", sky.ray);
    if (sky.cloud) root.style.setProperty("--cloud-hi", sky.cloud);
    if (sky.cloudLit) root.style.setProperty("--cloud-gilt", sky.cloudLit);
    root.style.setProperty("--ray-a", String(sky.rayAlpha));
    root.style.setProperty("--cloud-a", String(sky.cloudAlpha));
    root.dataset.sky = "on";
    // data-bg is what every "let it through" rule is keyed on — the panels
    // frost, the middle column steps aside, the text starts carrying its own
    // shadow. Those rules are about there being something behind the window,
    // not about it being a photograph, and a sky needs every one of them: the
    // rail, the side and the panes together cover the whole window otherwise.
    root.dataset.bg = "on";
  }

  const bg = pack.background;
  if (!bg?.image) return;
  // The id is the address; the bytes are immutable for it, so the URL needs no
  // cache buster. encodeURIComponent is what keeps a pack id out of the CSS
  // url() grammar.
  root.style.setProperty("--bg-image", `url("/themes/${encodeURIComponent(pack.id)}/background")`);
  root.style.setProperty("--bg-x", `${pct(bg.focusX)}%`);
  root.style.setProperty("--bg-y", `${pct(bg.focusY)}%`);
  root.style.setProperty("--bg-alpha", String(busy ? bg.taskOpacity : bg.homeOpacity));
  // A light palette already carries dark readable ink and pale panels. Using
  // the same opaque page-colour veil as dark mode washed illustrations into a
  // nearly white sheet, especially in the empty state. Keep a gentler scrim in
  // light mode; cards and navigation provide their own local contrast.
  const overlay = scheme === "light" ? bg.overlayStrength * 0.58 : bg.overlayStrength;
  root.style.setProperty("--bg-overlay", String(overlay));
  root.dataset.bg = "on";
}

function pct(v: number | undefined): number {
  if (typeof v !== "number" || Number.isNaN(v)) return 50;
  return Math.round(Math.max(0, Math.min(1, v)) * 100);
}

/** reserved is exported for the test that pins the meanings a pack cannot take. */
export const reserved = RESERVED;
