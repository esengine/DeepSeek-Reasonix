import assert from "node:assert/strict";
import { test } from "node:test";
import { loadCatalog, releaseForVersion, renderGitHubRelease, validateCatalog } from "./release-notes.mjs";

test("the committed release catalog is valid and newest first", async () => {
  const catalog = await loadCatalog();
  assert.equal(catalog.schemaVersion, 1);
  assert.equal(new Set(catalog.releases.map((release) => release.version)).size, catalog.releases.length);
});

test("tag namespaces resolve to the same product release", async () => {
  const catalog = await loadCatalog();
  assert.equal(releaseForVersion(catalog, "v1.17.13").version, "1.17.13");
  assert.equal(releaseForVersion(catalog, "desktop-v1.17.13").version, "1.17.13");
  assert.equal(releaseForVersion(catalog, "npm-v1.17.13").version, "1.17.13");
});

test("GitHub rendering keeps product sections and source PR links", async () => {
  const catalog = await loadCatalog();
  const markdown = renderGitHubRelease(releaseForVersion(catalog, "1.17.13"), "zh");
  assert.match(markdown, /## 使用攻略/);
  assert.match(markdown, /## 重点内容/);
  assert.match(markdown, /## 升级提醒/);
  assert.match(markdown, /## 风险提示/);
  assert.match(markdown, /## 致谢/);
  assert.match(markdown, /\/pull\/6460/);
  assert.match(markdown, /reasonix\.io\/changelog\/v1\.17\.13/);
});

test("validation rejects bilingual drift", () => {
  assert.throws(
    () =>
      validateCatalog({
        schemaVersion: 1,
        releases: [
          {
            version: "1.0.0",
            date: "2026-01-01",
            channel: "stable",
            title: { en: "Title", zh: "" },
            summary: { en: "Summary", zh: "摘要" },
          },
        ],
      }),
    /title\.zh/,
  );
});
