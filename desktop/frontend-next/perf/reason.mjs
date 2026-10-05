// What one kernel refusal says in each language. The kernel sends a code; the
// wording is chosen on this side.
//
// The expectation is read from the source the code points at, never copied into
// this file. The copy went stale when the interface was rewritten into written
// Chinese, and what it reported then was a defect that had not happened. Wording
// belongs to the catalogue; what is asserted here is that the code reached it.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = process.env.PERF_SRC ?? join(HERE, "..", "src");
const CODE = "wallpaper.unsupported_type";
const quote = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const kernelSrc = readFileSync(join(SRC, "i18n", "kernel.ts"), "utf8");
const wantZh = kernelSrc.match(new RegExp(`"${CODE}":\\s*"([^"]+)"`))?.[1] ?? "";
const catalogue = readFileSync(join(SRC, "i18n", "en_kernel.ts"), "utf8");
const wantEn = wantZh ? (catalogue.match(new RegExp(`"${quote(wantZh)}":\\s*"([^"]+)"`))?.[1] ?? "") : "";
// An empty side means the code was renamed or the file moved, not that the
// two agree.
if (!wantZh || !wantEn) {
  console.log(`未能从源码读取 ${CODE} 的两种语言文本：中文=${wantZh || "(空)"} 英文=${wantEn || "(空)"}`);
  process.exit(1);
}

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (n, ok, d = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${n}${d ? "  — " + d : ""}`);
  if (!ok) fails.push(n);
};

const browser = await chromium.launch();

async function refuse(lang) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.addInitScript((l) => localStorage.setItem("rx-lang", l), lang);
  // Both sides set the same way, or adopt treats the local cache as stale
  // and reloads.
  await page.goto(`${PAGE}?pref=${lang}`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 20000 });
  await page.waitForTimeout(700);
  await page.keyboard.press("Meta+Comma");
  await page.waitForTimeout(400);
  await page.evaluate(() => document.getElementById("prefs-appearance")?.click());
  await page.waitForTimeout(400);
  // The kernel accepts five image formats; TIFF is refused, and the refusal
  // carries wallpaper.unsupported_type.
  // The appearance page also takes a theme pack; the refusal under test is the wallpaper's.
  await page.locator('.prefs input[type="file"][data-action="wallpaper.change"]').setInputFiles({
    name: "x.tiff",
    mimeType: "image/tiff",
    buffer: Buffer.from([0x49, 0x49, 0x2a, 0x00]),
  });
  await page.waitForTimeout(800);
  const shown = await page.evaluate(
    () => document.querySelector('.find[data-lvl="err"] .t')?.textContent?.trim() ?? "",
  );
  await page.close();
  return shown;
}

const zh = await refuse("zh");
const en = await refuse("en");
console.log(`\n  中文界面：${zh}\n  英文界面：${en}\n`);
check("中文界面显示该码在源码中对应的中文", zh === wantZh, `实际「${zh || "(空)"}」，源码「${wantZh}」`);
check("英文界面显示目录为该中文给出的英文", en === wantEn, `实际「${en || "(空)"}」，目录「${wantEn}」`);
check("同一个码在两种语言下不同", zh !== en);
check("码本身未泄漏给读者", !zh.includes(CODE) && !en.includes(CODE));

await browser.close();
console.log(fails.length ? `\n${fails.length} 项未通过` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
