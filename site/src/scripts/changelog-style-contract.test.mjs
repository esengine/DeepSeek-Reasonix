import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

const css = await readFile(new URL("../styles/changelog.css", import.meta.url), "utf8");
const component = await readFile(new URL("../components/ChangelogPage.astro", import.meta.url), "utf8");
const versionPage = await readFile(new URL("../pages/changelog/[version].astro", import.meta.url), "utf8");

test("historical changelog styling remains readable", () => {
  assert.doesNotMatch(css, /var\(--(?:muted|paper)\)/);
  assert.match(css, /\.release-channel\s*\{/);
});

test("every release uses the version-ledger hero", () => {
  assert.doesNotMatch(component, /isThematicTitle|isThematic/);
  assert.doesNotMatch(component, /release-hero--compact|release-hero__actions|<blockquote>/);
  assert.match(component, /<header class="release-hero">/);
  assert.match(component, /<h1>Reasonix v\{release\.version\}<\/h1>/);
  assert.match(component, /<p class="release-lede">/);
  assert.doesNotMatch(css, /\.release-hero--compact|\.release-hero__actions|\.release-hero blockquote/);
});

test("targeted releases split Desktop and CLI updates without removing legacy sections", () => {
  assert.match(component, /release\.targetingVersion === 1/);
  assert.match(component, /id=\{target\.id\}/);
  assert.match(component, /Desktop updates/);
  assert.match(component, /CLI updates/);
  assert.match(component, /CLI users may skip this version if preferred/);
  assert.match(component, /<section id="highlights"/);
  assert.match(css, /\.release-target-section__summary/);
  assert.match(css, /\.release-item-targets/);
});

test("changelog has one official navigation and marks archives noindex", () => {
  assert.doesNotMatch(component, /release-channel-tabs/);
  assert.match(component, /Historical archive/);
  assert.match(component, /noindex=\{isPreview\}/);
});

test("reviewed exact-version routes redirect safely until their publication marker exists", () => {
  assert.match(versionPage, /publishedReleases/);
  assert.match(versionPage, /publishedVersions\.has\(release\.version\)/);
  assert.match(versionPage, /if \(!published\)/);
  assert.match(versionPage, /return Astro\.redirect/);
  assert.match(versionPage, /Astro\.redirect\('\/changelog\/'\)/);
});
