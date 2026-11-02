// Built-in soft deliberately lowers contrast without crossing AA, including
// small ghost text. Unset and the higher tiers retain their recorded palettes.
// Custom packs keep authored inks for soft; see TOKENS.md for that boundary.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=1";
const STEPS = { "--text": [3.0, 10.1], "--muted": [3.5, 7.5], "--faint": [3.5, 7.0], "--ghost": [3.5, 7.0] };
const INKS = Object.keys(STEPS);
const GROUNDS = ["--page", "--surface", "--raised", "--overlay", "--float", "--float-hi"];
const TIERS = ["soft", "normal", "strong"];
const FLOOR = {
  "light/normal": { "--text": 14.07, "--muted": 10.22, "--faint": 7.86 },
  "light/strong": { "--text": 17.86, "--muted": 13.55, "--faint": 10.42 },
  "dark/normal": { "--text": 13.92, "--muted": 9.79, "--faint": 7.57 },
  "dark/strong": { "--text": 18.01, "--muted": 13.16, "--faint": 9.92 },
};
const SNAPSHOT = {
  light: {
    "": ["#292b29", "#5e625c", "#666a63"],
    normal: ["oklch(.245 .0218 85)", "oklch(.34 .0196 85)", "oklch(.405 .0196 85)"],
    strong: ["oklch(.103 .0196 85)", "oklch(.26 .0184 85)", "oklch(.335 .0184 85)"],
  },
  dark: {
    "": ["#e7e8e3", "#b4b7b0", "#8e9289"],
    normal: ["oklch(.9588 .006756 115.3)", "oklch(.81 .01031 125.5)", "oklch(.728 .013 255)"],
    strong: ["oklch(1 .006756 115.3)", "oklch(.893 .011 255)", "oklch(.806 .012 255)"],
  },
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

const measured = await page.evaluate(({ STEPS, INKS, GROUNDS, TIERS, SNAPSHOT }) => {
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
    for (const [tier, expected] of Object.entries(SNAPSHOT[scheme])) {
      if (tier) root.dataset.contrast = tier;
      else delete root.dataset.contrast;
      for (const [i, ink] of INKS.entries()) {
        probe.style.color = expected[Math.min(i, 2)];
        const ref = rgb(getComputedStyle(probe).color);
        out[`${scheme}/${tier || "unset"}/${ink}/snapshot`] = { got: rgb(read(ink)).join(","), ref: ref.join(",") };
      }
    }
    const grounds = Object.fromEntries(GROUNDS.map((g) => [g, rgb(read(g))]));
    for (const tier of TIERS) {
      const k = TIERS.indexOf(tier);
      root.dataset.contrast = tier;
      for (const ink of INKS) {
        const by = k === 0 ? 0 : STEPS[ink][k - 1];
        probe.style.color = by ? `oklch(from ${base[ink]} calc(l ${scheme === "light" ? "-" : "+"} ${(by / 100).toFixed(3)}) c h)` : base[ink];
        const ref = rgb(getComputedStyle(probe).color);
        const got = rgb(read(ink));
        for (const g of GROUNDS) out[`${scheme}/${tier}/${ink}/${g}`] = { ref: ratio(ref, grounds[g]), base: ratio(rgb(base[ink]), grounds[g]), got: ratio(got, grounds[g]) };
      }
    }
  }
  probe.remove();
  return out;
}, { STEPS, INKS, GROUNDS, TIERS, SNAPSHOT });

for (const [key, { ref, base, got }] of Object.entries(measured)) {
  const [scheme, tier, ink, ground] = key.split("/");
  if (ground === "snapshot") {
    check(`${key} stays unchanged`, got === ref, `${got} vs ${ref}`);
    continue;
  }
  if (tier === "soft") {
    const lowers = ink === "--text" || ink === "--muted";
    check(`${key} ${lowers ? "is genuinely softer than" : "retains the AA floor of"} unset`, lowers ? got < base - 0.1 : Math.abs(got - base) < 0.005, `${got.toFixed(2)} vs ${base.toFixed(2)}`);
  } else {
    const floor = ground === "--page" ? (FLOOR[`${scheme}/${tier}`][ink === "--ghost" ? "--faint" : ink] ?? 0) : 0;
    check(`${key} not below the stepped palette or its floor`, got >= Math.max(ref, floor) - 0.005, `${got.toFixed(2)} vs ${Math.max(ref, floor).toFixed(2)}`);
  }
  check(`${key} clears AA`, got >= 4.5, got.toFixed(2));
}

const explicit = await page.evaluate((INKS) => {
  const root = document.documentElement;
  const p = document.createElement("div");
  document.body.append(p);
  const read = () => INKS.map((n) => ((p.style.color = `var(${n})`), getComputedStyle(p).color));
  const out = {};
  for (const scheme of ["light", "dark"]) {
    root.dataset.theme = scheme;
    for (const tier of ["soft", "normal", "strong"]) {
      root.dataset.contrast = tier;
      out[`${scheme}/${tier}`] = read();
    }
  }
  p.remove();
  return out;
}, INKS);
for (const system of ["light", "dark"]) {
  await page.emulateMedia({ colorScheme: system, contrast: "more" });
  for (const [key, expected] of Object.entries(explicit)) {
    const got = await page.evaluate(({ key, INKS }) => {
      const [scheme, tier] = key.split("/");
      document.documentElement.dataset.theme = scheme;
      document.documentElement.dataset.contrast = tier;
      const p = document.createElement("div");
      document.body.append(p);
      const out = INKS.map((n) => ((p.style.color = `var(${n})`), getComputedStyle(p).color));
      p.remove();
      return out;
    }, { key, INKS });
    check(`${key} outranks system ${system} and prefers-contrast: more`, JSON.stringify(got) === JSON.stringify(expected));
  }
}
await page.emulateMedia({ colorScheme: "dark", contrast: "no-preference" });
const automaticDark = await page.evaluate((INKS) => {
  delete document.documentElement.dataset.theme;
  document.documentElement.dataset.contrast = "soft";
  const p = document.createElement("div");
  document.body.append(p);
  const out = INKS.map((n) => ((p.style.color = `var(${n})`), getComputedStyle(p).color));
  p.remove();
  return out;
}, INKS);
check("system dark soft matches explicit dark soft", JSON.stringify(automaticDark) === JSON.stringify(explicit["dark/soft"]));

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
