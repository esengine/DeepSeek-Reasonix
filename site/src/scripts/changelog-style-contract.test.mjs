import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

const css = await readFile(new URL("../styles/changelog.css", import.meta.url), "utf8");
const component = await readFile(new URL("../components/ChangelogPage.astro", import.meta.url), "utf8");
const versionPage = await readFile(new URL("../pages/changelog/[version].astro", import.meta.url), "utf8");

test("changelog channel tabs use the shared accent hierarchy in both color schemes", () => {
  assert.doesNotMatch(css, /var\(--(?:muted|paper)\)/);
  assert.match(
    css,
    /\.release-channel-tabs a\.active\s*\{[^}]*background:\s*var\(--accent-soft\);[^}]*border-color:[^}]*var\(--accent\)[^}]*color:\s*var\(--accent-deep\);/s,
  );
});

test("changelog channel navigation exposes current-page and keyboard-focus states", () => {
  assert.equal((component.match(/aria-current=/g) ?? []).length, 2);
  assert.match(component, /aria-current=\{!isPreview \? 'page' : undefined\}/);
  assert.match(component, /aria-current=\{isPreview \? 'page' : undefined\}/);
  assert.match(
    css,
    /\.release-channel-tabs a:focus-visible\s*\{[^}]*outline:\s*2px solid var\(--accent\);[^}]*outline-offset:\s*2px;/s,
  );
});

test("reviewed exact-version routes redirect safely until their publication marker exists", () => {
  assert.match(versionPage, /import \{ allReleases, releases \}/);
  assert.match(versionPage, /publishedVersions\.has\(release\.version\)/);
  assert.match(versionPage, /if \(!published\)/);
  assert.match(versionPage, /return Astro\.redirect/);
  assert.match(versionPage, /'\/changelog\/preview\/'/);
  assert.match(versionPage, /'\/changelog\/stable\/'/);
});
