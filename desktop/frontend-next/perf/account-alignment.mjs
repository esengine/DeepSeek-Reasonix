import { chromium } from "playwright";

const browser = await chromium.launch();
const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const failures = [];
try {
  for (const scheme of ["light", "dark"]) for (const scale of [1, 1.25]) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: scheme, deviceScaleFactor: scale });
    await page.addInitScript(() => {
      localStorage.setItem("rx-nav-rail", "off");
      localStorage.setItem("reasonix.mock.account", "1");
    });
    await page.goto(`${base}?pref=zh&turns=3`, { waitUntil: "networkidle" });
    await page.waitForSelector(".studio-user-foot .ini");
    await page.evaluate(() => document.fonts.ready);
    for (const initial of ["D", "季"]) {
      await page.locator(".studio-user-foot .ini").evaluate((el, text) => { el.textContent = text; }, initial);
      await page.evaluate(() => document.fonts.ready);
      const centers = [];
      for (const selector of [".studio-user-foot .ini", ".studio-workspace-kind"]) {
        const bounds = await page.locator(selector).evaluate((el) => {
          const range = document.createRange();
          range.selectNodeContents(el);
          const text = range.getBoundingClientRect();
          const row = el.closest(".studio-user-foot").getBoundingClientRect();
          return { x: text.x, y: row.y + 4, width: text.width, height: 30, color: getComputedStyle(el).color };
        });
        const { color, ...clip } = bounds;
        const shot = await page.screenshot({ clip });
        centers.push(await page.evaluate(async ({ data, color, scale }) => {
          const image = await createImageBitmap(await (await fetch(`data:image/png;base64,${data}`)).blob());
          const canvas = new OffscreenCanvas(image.width, image.height);
          const ctx = canvas.getContext("2d");
          ctx.drawImage(image, 0, 0);
          const pixels = ctx.getImageData(0, 0, image.width, image.height).data;
          const rgb = color.match(/[\d.]+/g).slice(0, 3).map(Number);
          let top = Infinity, bottom = -Infinity;
          for (let y = 0; y < image.height; y++) for (let x = 0; x < image.width; x++) {
            const offset = (y * image.width + x) * 4;
            if (rgb.every((value, channel) => Math.abs(pixels[offset + channel] - value) < 45)) {
              top = Math.min(top, y);
              bottom = Math.max(bottom, y);
            }
          }
          return (top + bottom) / (2 * scale);
        }, { data: shot.toString("base64"), color, scale }));
      }
      const aligned = centers.every(Number.isFinite) && Math.abs(centers[0] - centers[1]) <= 1;
      const name = `${scheme} ${scale * 100}% ${initial}`;
      console.log(`${aligned ? "ok" : "FAIL"} ${name}: avatar/workspace ink centers ${centers.join(" / ")}`);
      if (!aligned) failures.push(name);
    }
    await page.locator('[data-action="chrome.account"]').click();
    await page.waitForSelector(".acct");
    console.log(`ok ${scheme} ${scale * 100}%: avatar opens account settings`);
    await page.close();
  }
} finally {
  await browser.close();
}
if (failures.length) process.exitCode = 1;
