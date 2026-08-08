import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = (path) => readFile(new URL(path, import.meta.url), "utf8");

/* Extract the body of a `@media (max-width: Npx)` block (up to the next
   at-rule or EOF) so assertions stay scoped to small viewports. */
const mediaBlock = (css, px) => {
  const marker = `@media (max-width: ${px}px)`;
  const start = css.indexOf(marker);
  assert.notEqual(start, -1, `missing ${marker}`);
  const rest = css.slice(start + marker.length);
  const next = rest.search(/@media|@keyframes/);
  return next === -1 ? rest : rest.slice(0, next);
};

/* The header must fit narrow viewports while keeping a clear menu trigger. */
test("≤640px: marketing nav contracts to fit 390px viewports", async () => {
  const [css, header] = await Promise.all([
    source("../styles/global.css"),
    source("../components/SiteHeader.astro"),
  ]);
  const block = mediaBlock(css, 640);
  assert.match(block, /\.nav-sign-in \{ display: none/);
  assert.match(block, /\.nav \.brand span \{ display: none/);
  assert.match(block, /\.nav \.brand img \{ width: 64px; \}/);
  assert.match(header, /<source media=\"\(max-width: 400px\)\" srcset=\{`\$\{base\}\/favicon\.svg`\} \/>/);
  assert.match(block, /\.theme-switch button \{ padding: 6px 9px/);
});

test("≤1100px: marketing navigation collapses before controls overlap", async () => {
  const css = await source("../styles/global.css");
  const tabletBlock = mediaBlock(css, 1100);
  const mobileBlock = mediaBlock(css, 900);

  assert.match(tabletBlock, /\.nav-links \{ display: none/);
  assert.match(tabletBlock, /\.nav-github \{ margin-left: auto; padding-inline: 8px/);
  assert.match(tabletBlock, /\.nav-github-label \{ display: none/);
  assert.match(tabletBlock, /\.nav-menu-toggle \{ display: inline-flex/);
  assert.match(mobileBlock, /\.nav-github \{ display: none/);
});

test("≤360px: marketing nav keeps the menu trigger within 320px", async () => {
  const css = await source("../styles/global.css");
  const block = mediaBlock(css, 360);
  assert.match(block, /\.nav-inner \{ padding: 0 12px; gap: 6px/);
  assert.match(block, /\.lang-switch button \{ padding: 6px 8px/);
  assert.match(block, /\.theme-switch button \{ padding: 6px 7px/);
  assert.match(block, /\.nav-install \{ display: none/);
  assert.match(block, /\.nav-menu-toggle \{ width: 36px; height: 36px/);
});

test("≤400px: marketing nav uses the compact brand mark", async () => {
  const css = await source("../styles/global.css");
  const block = mediaBlock(css, 400);
  assert.match(block, /\.nav \.brand img \{ width: 28px; height: 28px; border-radius: 8px; \}/);
});

test("≤360px: hero install command keeps its copy control inside the bar", async () => {
  const [css, page] = await Promise.all([
    source("../styles/global.css"),
    source("../pages/index.astro"),
  ]);
  const block = mediaBlock(css, 360);
  assert.match(page, /class=\"install-command\">npm i -g reasonix<\/span>/);
  assert.match(css, /\.hero-install \.install-command\s*\{[\s\S]*?min-width:\s*0;/);
  assert.match(block, /\.hero-install \.install \{ gap: 6px; padding-inline: 6px; \}/);
  assert.match(block, /\.hero-install \.install button \{ padding-inline: 10px; \}/);
});

test("≤440px: community nav budgets for the async account control", async () => {
  const css = await source("../styles/community.css");
  const layout = await source("../layouts/Community.astro");
  const block = mediaBlock(css, 440);
  assert.match(layout, /class="brand-name">Reasonix/);
  assert.match(layout, /id="nav-account"/);
  assert.match(block, /\.nav \.brand-name \{ display: none/);
  assert.match(block, /\.nav-right \{ gap: 8px/);
  assert.match(block, /\.lang-switch button \{ padding: 6px 8px/);
  assert.match(block, /\.theme-switch button \{ padding: 6px 7px/);
  assert.match(block, /#nav-account \.btn \{ padding: 7px 10px/);
});

test("shared headers expose an accessible mobile navigation", async () => {
  const [header, community] = await Promise.all([
    source("../components/SiteHeader.astro"),
    source("../layouts/Community.astro"),
  ]);
  for (const sourceText of [header, community]) {
    assert.match(sourceText, /data-mobile-nav-toggle/);
    assert.match(sourceText, /data-mobile-nav/);
    assert.match(sourceText, /data-mobile-nav-close/);
    assert.match(sourceText, /aria-expanded="false"/);
  }
});

test("mobile navigation behavior supports close, Escape, and focus cycling", async () => {
  const js = await source("./mobile-nav.js");
  assert.match(js, /event\.key === "Escape"/);
  assert.match(js, /event\.key !== "Tab"/);
  assert.match(js, /document\.body\.classList\.add\("mobile-nav-open"\)/);
  assert.match(js, /document\.body\.classList\.remove\("mobile-nav-open"\)/);
});
