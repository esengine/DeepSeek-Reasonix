import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const url = process.argv[2] ?? "http://127.0.0.1:5374/";
const output = process.argv[3];
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1400, height: 1000 }, permissions: ["clipboard-read", "clipboard-write"] });
const page = await context.newPage();
const external = [];
await page.route("**/evil.invalid/**", (route) => { external.push(route.request().url()); return route.abort(); });
try {
  await page.goto(url);
  await page.evaluate(async () => {
    const main = await (await fetch("/src/main.tsx")).text();
    const reactUrl = main.match(/from "([^"]*\/react\.js\?[^\"]*)"/)[1];
    const domUrl = main.match(/from "([^"]*\/react-dom_client\.js\?[^\"]*)"/)[1];
    const React = (await import(reactUrl)).default;
    const { createRoot } = (await import(domUrl)).default;
    const { Markdown } = await import("/src/ui/Markdown.tsx");
    const host = document.createElement("section");
    host.id = "mermaid-evidence";
    host.style.cssText = "position:fixed;inset:0;z-index:99999;background:var(--page);color:var(--text);padding:32px;overflow:auto";
    document.body.append(host);
    const root = createRoot(host);
    window.renderMarkdown = (text, streaming = false) => root.render(React.createElement(Markdown, { text, streaming }));
    window.renderMarkdown("**Ordinary Markdown**\n\n```go\nfunc main() {}\n```");
  });
  await page.locator("#mermaid-evidence .code-wrap").waitFor();
  assert.equal(await page.locator('iframe[title="Mermaid rendering sandbox"]').count(), 0, "ordinary fences must not load Mermaid");
  const source = "flowchart TD\n  Browser --> App\n  App --> Database\n";
  const markdown = "# Studio Markdown · architecture\n\n```mermaid\n" + source + "```\n\n**Neighboring Markdown** stays readable. [Documentation](https://example.com)\n\n```go\nfunc main() {}\n```";
  await page.evaluate((text) => window.renderMarkdown("```mermaid\n" + text, true), source);
  await page.locator(".mermaid-source").waitFor();
  assert.equal(await page.locator('iframe[title="Mermaid rendering sandbox"]').count(), 0, "live tail must stay source without loading the engine");
  await page.evaluate((text) => window.renderMarkdown(text), markdown);
  await page.locator(".mermaid-preview img").waitFor({ timeout: 45000 });
  await page.waitForFunction(() => document.querySelector(".mermaid-preview img")?.naturalWidth > 0);
  const svg = await page.locator(".mermaid-preview img").evaluate((image) => decodeURIComponent(image.src.split(",")[1]));
  assert.match(svg, /Browser/);
  assert.match(svg, /Database/);
  assert.match(svg, /marker/);
  assert.doesNotMatch(svg, /foreignObject|onload|<script|href=/i);
  await page.locator('[data-action="mermaid.source"]').click();
  assert.equal(await page.locator(".mermaid-source").textContent(), source);
  await page.getByRole("button", { name: "Copy Mermaid source" }).click();
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), source);
  await page.locator('[data-action="mermaid.preview"]').click();
  assert.equal(await page.locator('.code-wrap[data-lang="go"] pre').textContent(), "func main() {}\n");
  assert.equal(await page.locator('#mermaid-evidence a').getAttribute("rel"), "noreferrer noopener");
  if (output) await mkdir(output, { recursive: true });
  for (const [theme, width, label] of [["light", 1400, "light-wide"], ["dark", 1400, "dark-wide"], ["light", 390, "light-phone"], ["dark", 390, "dark-phone"]]) {
    await page.setViewportSize({ width, height: 1000 });
    const previous = await page.locator(".mermaid-preview img").getAttribute("src");
    const previousTheme = await page.evaluate(() => document.documentElement.dataset.theme);
    await page.evaluate((value) => { document.documentElement.dataset.theme = value; }, theme);
    if (previousTheme !== theme) await page.waitForFunction((src) => document.querySelector(".mermaid-preview img")?.src !== src, previous);
    await page.waitForFunction(() => document.querySelector(".mermaid-preview img")?.naturalWidth > 0);
    if (output) await page.screenshot({ path: `${output}/after-${label}.png` });
  }
  await page.evaluate(() => window.renderMarkdown("```mermaid\nflowchart TD\n A --???\n```"));
  await page.locator(".mermaid-error").waitFor();
  assert.match(await page.locator(".mermaid-error").textContent(), /Mermaid rendering failed/);
  assert.match(await page.locator(".mermaid-source").textContent(), /A --\?\?\?/);
  for (const definition of [
    'sequenceDiagram\nAlice->>Bob: Hello\nBob-->>Alice: Hi',
    'erDiagram\nUSER ||--o{ ORDER : places',
    'flowchart TD\nA[Safe] --> B[Text]\nclick A call attack()',
    '---\nconfig:\n  securityLevel: loose\n  themeCSS: "@import url(https://evil.invalid/style);"\n---\nflowchart TD\nA[Safe] --> B[Text]',
  ]) {
    await page.evaluate((text) => window.renderMarkdown("```mermaid\n" + text + "\n```"), definition);
    await page.locator(".mermaid-preview img").waitFor();
    await page.waitForFunction(() => document.querySelector(".mermaid-preview img")?.naturalWidth > 0);
    const safe = await page.locator(".mermaid-preview img").evaluate((image) => decodeURIComponent(image.src.split(",")[1]));
    assert.doesNotMatch(safe, /evil\.invalid|onclick|onload|href=|<script|foreignObject/i);
  }
  await page.evaluate(() => window.renderMarkdown('```mermaid\nflowchart TD\n A@{ img: "https://evil.invalid/image.png", label: "Image" }\n```'));
  await page.locator(".mermaid-preview img, .mermaid-error").waitFor();
  assert.deepEqual(external, [], "no graph-triggered resource request may leave the renderer");
  assert.equal(await page.evaluate(() => typeof window.attack), "undefined");
  console.log("PASS: lazy loading, live tail, real flowchart/sequence/ERD SVGs, source/copy, themes, invalid source, ordinary Markdown and CSP/resource safety");
} finally {
  await browser.close();
}
