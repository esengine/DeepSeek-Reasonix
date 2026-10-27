import { chromium } from "playwright";

const browser = await chromium.launch();
const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const failures = [];
const check = (name, ok, detail) => {
  console.log(`${ok ? "ok" : "FAIL"} ${name}: ${detail}`);
  if (!ok) failures.push(name);
};

async function inkBounds(page, png, color) {
  return page.evaluate(async ({ image, color }) => {
    const img = new Image();
    img.src = image;
    await img.decode();
    const canvas = document.createElement("canvas");
    canvas.width = img.width;
    canvas.height = img.height;
    const context = canvas.getContext("2d");
    context.fillStyle = color;
    context.fillRect(0, 0, 1, 1);
    const foreground = context.getImageData(0, 0, 1, 1).data;
    context.drawImage(img, 0, 0);
    const pixels = context.getImageData(0, 0, img.width, img.height).data;
    let top = img.height, bottom = -1;
    for (let y = 0; y < img.height; y++) {
      for (let x = 0; x < img.width; x++) {
        const i = (y * img.width + x) * 4;
        if ([0, 1, 2].every((c) => Math.abs(pixels[i + c] - foreground[c]) < 45)) {
          top = Math.min(top, y);
          bottom = Math.max(bottom, y);
        }
      }
    }
    return { found: bottom >= top, top, bottom, height: img.height };
  }, { image: `data:image/png;base64,${png.toString("base64")}`, color });
}

try {
  for (const scheme of ["light", "dark"]) {
    for (const label of ["Demo", "季谢尔"]) for (const scale of [1, 1.25]) {
      const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: scheme, deviceScaleFactor: scale });
      await page.addInitScript(() => {
        localStorage.setItem("rx-nav-rail", "off");
        localStorage.setItem("reasonix.mock.account", "1");
      });
      await page.goto(`${base}?pref=zh&turns=3&accountLabel=${encodeURIComponent(label)}`, { waitUntil: "networkidle" });
      await page.waitForSelector(".studio-user-foot .ini");
      await page.evaluate(() => document.fonts.ready);
      const layout = await page.evaluate(() => {
        const avatar = document.querySelector(".studio-user-foot .ini");
        const box = avatar.getBoundingClientRect();
        const button = avatar.closest("button").getBoundingClientRect();
        return { initial: avatar.textContent, color: getComputedStyle(avatar).color,
          contained: box.top >= button.top && box.bottom <= button.bottom && box.left >= button.left && box.right <= button.right };
      });
      const tag = `${scheme}/${label}/${scale * 100}%`;
      check(`${tag} avatar inside click target`, layout.contained, JSON.stringify(layout));
      const png = await page.locator(".studio-user-foot .ini").screenshot();
      const ink = await inkBounds(page, png, layout.color);
      check(`${tag} visible glyph is not clipped`, ink.found && ink.top > 0 && ink.bottom < ink.height - 1, JSON.stringify(ink));
      const centers = [];
      for (const selector of [".studio-user-foot .ini > span", ".studio-workspace-kind"]) {
        const bounds = await page.locator(selector).evaluate((el) => {
          const range = document.createRange();
          range.selectNodeContents(el);
          const text = range.getBoundingClientRect();
          const row = el.closest(".studio-user-foot").getBoundingClientRect();
          return { x: text.x, y: row.y + 4, width: text.width, height: 30, color: getComputedStyle(el).color };
        });
        const { color, ...clip } = bounds;
        const pixels = await inkBounds(page, await page.screenshot({ clip }), color);
        centers.push(pixels.found ? (pixels.top + pixels.bottom) / (2 * scale) : NaN);
      }
      check(`${tag} avatar/workspace ink alignment`,
        centers.every(Number.isFinite) && Math.abs(centers[0] - centers[1]) <= 1, centers.join(" / "));
      await page.locator('[data-action="chrome.account"]').click();
      await page.waitForSelector(".acct");
      check(`${tag} account identity`, (await page.locator(".acct-who .nm").textContent()) === label, label);
      await page.close();
    }
  }
} finally {
  await browser.close();
}
if (failures.length) process.exitCode = 1;
