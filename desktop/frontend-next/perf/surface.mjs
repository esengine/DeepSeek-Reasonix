// A box is separated from the surface it sits on in one of three ways: a line
// around it, a step in fill, or an elevation. A box with none of them reads as
// smeared; a box with all three reads as a table cell. This guard asks which
// one each rounded box chose, and — for the ones that chose fill — whether the
// step is large enough to be seen.
//
// The threshold is set from the eye rather than copied from a mockup: under 2%
// in OKLab lightness is not a layer on a dark ground, it is a decision that did
// not take effect.
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?ws=1&sess=1&turns=4&pref=zh";
const MIN_L = Number(process.env.PERF_MIN_L ?? 2);
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();

for (const scheme of ["dark", "light"]) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: scheme });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".compose");
  await page.waitForTimeout(300);

  const found = await page.evaluate((minL) => {
    // Lightness only: a layer is read from light and dark, not from hue.
    //
    // Three notations reach this, and only one of them is 0-255. Every fill in
    // this interface comes from an oklch token and stays oklch in the computed
    // value, where the three numbers are L, C and H — read as rgb/255, the hue
    // lands in the blue channel at 255/255 and every colour in the tree
    // returns the same 45.2. Equal parent and child then read as a fill that
    // never claimed to be a layer, which is the one case exempted below, so
    // this guard passed by examining nothing for as long as it has existed.
    const lum = (rgb) => {
      const m = rgb.match(/[\d.]+/g);
      if (!m) return null;
      // L is already the answer there; a percentage is the same number, and
      // the unit has to be read off the source rather than off the digits.
      const ok = /^okl(?:ch|ab)\(\s*([\d.]+)(%?)/.exec(rgb);
      if (ok) return Number(ok[1]) * (ok[2] ? 1 : 100);
      // color(srgb r g b) carries 0-1; rgb()/rgba() carries 0-255.
      const unit = rgb.startsWith("color(");
      const [r, g, b] = m.slice(0, 3).map((v) => {
        const c = unit ? Number(v) : Number(v) / 255;
        return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
      });
      const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
      const m2 = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
      const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
      return (0.2104542553 * l + 0.793617785 * m2 - 0.0040720468 * s) * 100;
    };
    const opaque = (c) => c && c !== "transparent" && !/rgba\([^)]*,\s*0\s*\)/.test(c);

    const thin = [];
    for (const el of document.querySelectorAll("*")) {
      const s = getComputedStyle(el);
      if (s.display === "none" || s.visibility === "hidden") continue;
      const box = el.getBoundingClientRect();
      // Only what is actually drawn as a box: a radius, and an area.
      if (box.width < 40 || box.height < 24) continue;
      if (parseFloat(s.borderTopLeftRadius) < 3) continue;
      if (!opaque(s.backgroundColor)) continue;
      const hasBorder = parseFloat(s.borderTopWidth) > 0 && opaque(s.borderTopColor);
      const hasShadow = s.boxShadow !== "none" && /(^|,)\s*(?!inset)[^,]*\d/.test(s.boxShadow);
      if (hasBorder || hasShadow) continue;

      // What it sits on: the nearest ancestor with a fill of its own.
      let p = el.parentElement;
      while (p && !opaque(getComputedStyle(p).backgroundColor)) p = p.parentElement;
      if (!p) continue;
      const a = lum(s.backgroundColor);
      const b = lum(getComputedStyle(p).backgroundColor);
      if (a === null || b === null) continue;
      const d = Math.abs(a - b);
      // An identical fill is a box not claiming to be a layer, which is a
      // decision. A near-identical one is a layer that did not take effect.
      if (d > 0.05 && d < minL) {
        const cls = typeof el.className === "string" ? el.className.split(/\s+/).filter(Boolean).join(".") : "";
        thin.push(`${el.tagName.toLowerCase()}${cls ? "." + cls : ""}  ΔL=${d.toFixed(1)}`);
      }
    }
    return [...new Set(thin)];
  }, MIN_L);

  if (found.length) {
    console.log(`\n${scheme}：以底色分层但该层不可见的 ${found.length} 种：`);
    for (const f of found) console.log("  " + f);
  }
  check(
    `${scheme}：以底色分层的容器，该层可见`,
    found.length === 0,
    found.length ? `${found.length} 种低于 ΔL ${MIN_L}` : `阈值 ΔL ${MIN_L}`,
  );
  await page.close();
}

await browser.close();
console.log(fails.length ? `\n${fails.length} 项未通过` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
