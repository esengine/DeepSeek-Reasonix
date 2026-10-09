// Ink contrast per tier and scheme. Each tier's inks must be at least as legible
// as the default palette stepped once, and as the recorded floors below (ratios
// on --page; --ghost shares --faint's), and every ink must clear AA on both grounds.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=1";
const STEPS = { "--text": [3.0, 10.1], "--muted": [3.5, 7.5], "--faint": [3.5, 7.0], "--ghost": [3.5, 7.0] };
const INKS = Object.keys(STEPS);
const GROUNDS = ["--page", "--surface"];
const TIERS = ["soft", "normal", "strong"];
const FLOOR = {
  "light/soft": { "--text": 11.69, "--muted": 7.63, "--faint": 5.76 },
  "light/normal": { "--text": 14.07, "--muted": 10.22, "--faint": 7.86 },
  "light/strong": { "--text": 17.86, "--muted": 13.55, "--faint": 10.42 },
  "dark/soft": { "--text": 11.48, "--muted": 7.48, "--faint": 5.63 },
  "dark/normal": { "--text": 13.92, "--muted": 9.79, "--faint": 7.57 },
  "dark/strong": { "--text": 18.01, "--muted": 13.16, "--faint": 9.92 },
};

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 800 }, locale: "zh-CN" });
page.on("pageerror", (e) => fails.push("page error: " + e.message));
await page.goto(PAGE, { waitUntil: "networkidle" });
await page.waitForSelector(".compose");

const measured = await page.evaluate(({ STEPS, INKS, GROUNDS, TIERS }) => {
  const root = document.documentElement;
  const probe = document.createElement("div");
  document.body.append(probe);
  const ctx = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
  const rgb = (css) => {
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = "#000";
    ctx.fillStyle = css;
    ctx.fillRect(0, 0, 1, 1);
    return [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3);
  };
  const lum = (c) => {
    const [r, g, b] = c.map((v) => {
      const s = v / 255;
      return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  };
  const ratio = (a, b) => {
    const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
    return (hi + 0.05) / (lo + 0.05);
  };
  const read = (name) => {
    probe.style.color = `var(${name})`;
    return getComputedStyle(probe).color;
  };
  const out = {};
  for (const scheme of ["light", "dark"]) {
    root.dataset.theme = scheme;
    delete root.dataset.contrast;
    const base = Object.fromEntries(INKS.map((n) => [n, read(n === "--ghost" ? "--faint" : n)]));
    const grounds = Object.fromEntries(GROUNDS.map((g) => [g, rgb(read(g))]));
    for (const tier of TIERS) {
      const k = TIERS.indexOf(tier);
      root.dataset.contrast = tier;
      for (const ink of INKS) {
        const by = k === 0 ? 0 : STEPS[ink][k - 1];
        probe.style.color = by ? `oklch(from ${base[ink]} calc(l ${scheme === "light" ? "-" : "+"} ${(by / 100).toFixed(3)}) c h)` : base[ink];
        const ref = rgb(getComputedStyle(probe).color);
        const got = rgb(read(ink));
        for (const g of GROUNDS) out[`${scheme}/${tier}/${ink}/${g}`] = { ref: ratio(ref, grounds[g]), got: ratio(got, grounds[g]) };
      }
    }
  }
  probe.remove();
  return out;
}, { STEPS, INKS, GROUNDS, TIERS });

for (const [key, { ref, got }] of Object.entries(measured)) {
  const [scheme, tier, ink, ground] = key.split("/");
  const floor = ground === "--page" ? (FLOOR[`${scheme}/${tier}`][ink === "--ghost" ? "--faint" : ink] ?? 0) : 0;
  check(`${key} not below the stepped palette or its floor`, got >= Math.max(ref, floor) - 0.005, `${got.toFixed(2)} vs ${Math.max(ref, floor).toFixed(2)}`);
  check(`${key} clears AA`, got >= 4.5, got.toFixed(2));
}

for (const scheme of ["light", "dark"]) {
  await page.emulateMedia({ colorScheme: scheme, contrast: "more" });
  const same = await page.evaluate((scheme) => {
    const root = document.documentElement;
    root.dataset.theme = scheme;
    delete root.dataset.contrast;
    const p = document.createElement("div");
    document.body.append(p);
    const c = (n) => ((p.style.color = `var(${n})`), getComputedStyle(p).color);
    const r = [c("--faint"), c("--ghost")];
    p.remove();
    return r;
  }, scheme);
  check(`${scheme} under prefers-contrast: more keeps ghost on faint`, same[0] === same[1], same.join(" / "));
}

await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nevery tier holds");
