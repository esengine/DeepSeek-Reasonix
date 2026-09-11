import assert from "node:assert/strict";
import { test } from "node:test";
import { inferPullTargetHints } from "./generate-release-notes.mjs";
import { loadCatalog, releaseForVersion, renderGitHubRelease, validateCatalog } from "./release-notes.mjs";
import { validateReleaseEvent } from "./release-event.mjs";

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

test("targeted releases render Desktop and CLI sections from one shared item", async () => {
  const catalog = await loadCatalog();
  const markdown = renderGitHubRelease(releaseForVersion(catalog, "1.31.4"), "zh");
  assert.match(markdown, /## 桌面端更新/);
  assert.match(markdown, /## CLI 端更新/);
  assert.match(markdown, /## 其他项目更新/);
  assert.match(markdown, /\*\*\[Service\]\*\* \*\*降低数据库读取成本\*\*/);
  assert.equal(markdown.match(/更好的数学渲染/g)?.length, 2);
});

test("targeted release validation requires canonical item targets and matching surfaces", () => {
  const release = {
    version: "2.0.0",
    targetingVersion: 1,
    date: "2026-01-01",
    channel: "stable",
    title: { en: "Targeted", zh: "分类版本" },
    summary: { en: "Summary", zh: "摘要" },
    surfaces: ["desktop", "cli"],
    guides: [],
    highlights: [{
      kind: "fixed",
      targets: ["desktop", "cli"],
      title: { en: "Shared fix", zh: "共享修复" },
      body: { en: "A shared fix.", zh: "一项共享修复。" },
      refs: [1],
    }],
    changes: { new: [], improved: [], fixed: [] },
    upgrade: [],
    risks: [],
    contributors: [],
    links: {
      github: "https://example.com/release",
      compare: "https://example.com/compare",
      download: "https://example.com/download",
    },
  };
  assert.doesNotThrow(() => validateCatalog({ schemaVersion: 1, releases: [release] }));
  assert.throws(
    () => validateCatalog({
      schemaVersion: 1,
      releases: [{ ...release, highlights: [{ ...release.highlights[0], targets: undefined }] }],
    }),
    /targets must be a non-empty array/,
  );
  assert.throws(
    () => validateCatalog({ schemaVersion: 1, releases: [{ ...release, surfaces: ["desktop"] }] }),
    /surfaces must equal the canonical union/,
  );
  assert.throws(
    () => validateCatalog({
      schemaVersion: 1,
      releases: [{ ...release, highlights: [{ ...release.highlights[0], targets: ["cli", "desktop"] }] }],
    }),
    /targets must use canonical order/,
  );
});

test("targeting adoption boundary preserves historical releases and rejects future legacy records", () => {
  const release = {
    version: "1.31.3",
    date: "2026-01-01",
    channel: "stable",
    title: { en: "Legacy", zh: "旧版" },
    summary: { en: "Summary", zh: "摘要" },
    surfaces: ["desktop"],
    guides: [],
    highlights: [{
      kind: "fixed",
      title: { en: "Legacy fix", zh: "旧版修复" },
      body: { en: "A historical fix.", zh: "一项历史修复。" },
      refs: [1],
    }],
    changes: { new: [], improved: [], fixed: [] },
    upgrade: [],
    risks: [],
    contributors: [],
    links: {
      github: "https://example.com/release",
      compare: "https://example.com/compare",
      download: "https://example.com/download",
    },
  };

  assert.doesNotThrow(() => validateCatalog({ schemaVersion: 1, releases: [release] }));
  for (const version of ["1.31.4", "1.32.0", "2.0.0-preview.1"]) {
    assert.throws(
      () => validateCatalog({ schemaVersion: 1, releases: [{ ...release, version }] }),
      /targetingVersion must be 1 for v1\.31\.4 and newer/,
    );
  }
});

test("release target hints prefer explicit product labels and identify shared or service paths", () => {
  assert.deepEqual(inferPullTargetHints(["desktop"], ["internal/sessioncatalog/catalog.go"]), ["desktop"]);
  assert.deepEqual(inferPullTargetHints(["desktop", "tui"], ["desktop/app.go", "internal/cli/cli.go"]), ["desktop", "cli"]);
  assert.deepEqual(inferPullTargetHints([], ["internal/agent/agent.go"]), ["desktop", "cli"]);
  assert.deepEqual(inferPullTargetHints([], ["workers/crash-report/src/index.ts"]), ["service"]);
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

test("managed Preview records bind every surface to one exact ordinal", () => {
  const release = {
    version: "1.19.0-preview.3",
    releaseId: "1.19.0-preview.3",
    baseVersion: "1.19.0",
    date: "2026-07-31",
    channel: "prerelease",
    status: "reviewed",
    previousRelease: "1.19.0-preview.2",
    builds: {
      cli: "v1.19.0-preview.3",
      desktop: "v1.19.0-preview.3",
      npm: "1.19.0-canary.3",
    },
    title: { en: "Preview", zh: "预览版" },
    summary: { en: "Preview summary", zh: "预览版摘要" },
    surfaces: ["cli"],
    guides: [],
    highlights: [{
      kind: "fixed",
      title: { en: "Fix", zh: "修复" },
      body: { en: "A fix.", zh: "一项修复。" },
      refs: [1],
    }],
    changes: { new: [], improved: [], fixed: [] },
    upgrade: [],
    risks: [],
    contributors: [],
    links: {
      github: "https://github.com/esengine/DeepSeek-Reasonix/releases/tag/v1.19.0-preview.3",
      compare: "https://github.com/esengine/DeepSeek-Reasonix/compare/v1.19.0-preview.2...v1.19.0-preview.3",
      download: "https://reasonix.io/?download=desktop&channel=preview#start",
    },
  };

  assert.doesNotThrow(() => validateCatalog({ schemaVersion: 1, releases: [release] }));
  assert.throws(
    () => validateCatalog({
      schemaVersion: 1,
      releases: [{ ...release, builds: { ...release.builds, npm: "1.19.0-canary.4" } }],
    }),
    /builds\.npm/,
  );
});

test("publication marker is bound to reviewed version, channel, SHA, and builds", () => {
  const release = {
    version: "1.19.0-preview.3",
    channel: "prerelease",
    candidateSha: "a".repeat(40),
    builds: {
      cli: "v1.19.0-preview.3",
      desktop: "v1.19.0-preview.3",
      npm: "1.19.0-canary.3",
    },
  };
  const event = {
    schemaVersion: 1,
    releaseId: release.version,
    channel: "preview",
    candidateSha: release.candidateSha,
    publishedAt: "2026-07-31T00:00:00.000Z",
    releaseNotesUrl: "https://reasonix.io/changelog/v1.19.0-preview.3/",
    builds: release.builds,
  };

  assert.doesNotThrow(() => validateReleaseEvent(event, release));
  assert.throws(
    () => validateReleaseEvent({ ...event, candidateSha: "b".repeat(40) }, release),
    /candidateSha/,
  );
});
