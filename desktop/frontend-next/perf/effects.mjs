// Effects guard: decoration must not keep the GPU busy. A window with a sky
// pack drew a blurred cloud bank under six frosted panels, so every frame
// re-blurred 41% of the screen; the sky never stopped for a window nobody was
// looking at. Idle has to be idle, and "Reduced" has to remove those layers.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();

async function open(pack, { effects = "", reducedMotion = "no-preference" } = {}) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion, colorScheme: "dark" });
  await ctx.addInitScript((v) => {
    if (v) localStorage.setItem("rx-effects", v);
    window.__sky = 0;
    const clear = CanvasRenderingContext2D.prototype.clearRect;
    CanvasRenderingContext2D.prototype.clearRect = function (...a) {
      if (this.canvas.width === 400 && this.canvas.height === 225) window.__sky++;
      return clear.apply(this, a);
    };
  }, effects);
  const page = await ctx.newPage();
  await page.goto(`${BASE.replace(/\?.*/, "")}?pack=${pack}`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 15000 });
  await page.waitForTimeout(1200);
  return { ctx, page };
}

const skyFrames = async (page, ms) => {
  const a = await page.evaluate(() => window.__sky);
  await page.waitForTimeout(ms);
  return (await page.evaluate(() => window.__sky)) - a;
};

// Surfaces that blur what is behind them, pseudo-elements included: the
// transcript's backing layer is one, and it is the largest.
const blurred = (page) =>
  page.evaluate(() => {
    const out = [];
    for (const el of document.querySelectorAll("*")) {
      for (const pseudo of [null, "::before", "::after"]) {
        const cs = getComputedStyle(el, pseudo);
        const b = cs.backdropFilter || cs.webkitBackdropFilter;
        if (!b || b === "none") continue;
        if (pseudo && cs.content === "none") continue;
        if (!pseudo && (cs.display === "none" || cs.visibility === "hidden")) continue;
        const r = el.getBoundingClientRect();
        if (r.width * r.height === 0) continue;
        out.push(`${el.tagName.toLowerCase()}.${String(el.className).slice(0, 18)}${pseudo ?? ""}`);
      }
    }
    return out;
  });

const timeLoops = (page) =>
  page.evaluate(() =>
    document.getAnimations({ subtree: true })
      .filter((a) => a.playState === "running" && a.effect?.getTiming().iterations === Infinity && a.timeline?.constructor.name === "DocumentTimeline")
      .map((a) => `${a.animationName ?? "?"} @ ${a.effect.target?.className?.toString?.().slice(0, 20)}`),
  );

{
  const { ctx, page } = await open("sky");
  check("sky pack: the sky is mounted", (await page.locator(".sky").count()) === 1);
  const loops = await timeLoops(page);
  check("sky pack at idle: no CSS loop runs behind the window", loops.length === 0, loops.join("; ") || "0");
  const n = await skyFrames(page, 3000);
  check("sky pack at idle: the canvas redraws at most 20 times a second", n <= 60 && n > 0, `${n} redraws in 3 s`);
  const bd = await blurred(page);
  check("sky pack: at most two surfaces blur their backdrop", bd.length <= 2, bd.join(", "));
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.waitForTimeout(300);
  check("a hidden window draws no sky", (await skyFrames(page, 1500)) <= 1);
  await page.evaluate(() => {
    delete document.hidden;
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.waitForTimeout(800);
  await page.evaluate(() => {
    document.hasFocus = () => false;
    window.dispatchEvent(new Event("blur"));
  });
  await page.waitForTimeout(300);
  check("a window without the keyboard draws no sky", (await skyFrames(page, 1500)) <= 1);
  await page.evaluate(() => {
    document.hasFocus = () => true;
    window.dispatchEvent(new Event("focus"));
  });
  await page.waitForTimeout(800);
  check("it moves again once shown", (await skyFrames(page, 1500)) > 0);
  await ctx.close();
}

{
  const { ctx, page } = await open("sky", { reducedMotion: "reduce" });
  check("prefers-reduced-motion: the sky holds still", (await skyFrames(page, 2000)) === 0);
  check("prefers-reduced-motion: no CSS loop at idle", (await timeLoops(page)).length === 0);
  await ctx.close();
}

{
  const { ctx, page } = await open("photo", { effects: "reduced" });
  const bd = await blurred(page);
  check("Reduced: nothing blurs its backdrop", bd.length === 0, bd.join(", "));
  const grain = await page.evaluate(() => getComputedStyle(document.querySelector(".app"), "::after").display);
  check("Reduced: the grain layer is gone", grain === "none", grain);
  check("Reduced: data-effects is on the root", (await page.evaluate(() => document.documentElement.dataset.effects)) === "reduced");
  await ctx.close();
}

{
  const { ctx, page } = await open("sky");
  await page.keyboard.press("Meta+Comma");
  await page.waitForTimeout(500);
  await page.evaluate(() => document.getElementById("prefs-appearance")?.click());
  await page.waitForSelector('[data-action="appearance.effects"]', { state: "attached", timeout: 5000 });
  const sky0 = await page.locator(".sky").count();
  await page.evaluate(() => document.querySelector('[data-action="appearance.effects"][data-value="reduced"]')?.click());
  await page.waitForTimeout(500);
  check("choosing Reduced takes the sky away at once", sky0 === 1 && (await page.locator(".sky").count()) === 0);
  check("choosing Reduced is remembered", (await page.evaluate(() => localStorage.getItem("rx-effects"))) === "reduced");
  check("Reduced leaves no blurred surface", (await blurred(page)).length === 0);
  await page.evaluate(() => document.querySelector('[data-action="appearance.effects"][data-value="full"]')?.click());
  await page.waitForTimeout(500);
  check("choosing Full brings the sky back", (await page.locator(".sky").count()) === 1);
  await ctx.close();
}

await browser.close();
console.log(fails.length ? `\n${fails.length} failed:\n- ${fails.join("\n- ")}` : "\nall passed");
process.exit(fails.length ? 1 : 0);
