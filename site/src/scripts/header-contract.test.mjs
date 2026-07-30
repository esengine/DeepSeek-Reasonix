import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = (path) => readFile(new URL(path, import.meta.url), "utf8");

test("primary site pages share the same header component", async () => {
  const pages = await Promise.all([
    source("../pages/index.astro"),
    source("../pages/docs.astro"),
    source("../pages/skills.astro"),
    source("../components/ChangelogPage.astro"),
    source("../pages/404.astro"),
  ]);

  for (const page of pages) {
    assert.match(page, /<SiteHeader\b/);
    assert.doesNotMatch(page, /<header class="nav/);
  }
});

test("the shared header owns the complete global navigation", async () => {
  const header = await source("../components/SiteHeader.astro");

  for (const id of ["features", "how", "skills", "community", "docs", "changelog", "start"]) {
    assert.match(header, new RegExp(`id: '${id}'`));
  }

  assert.match(header, /nav-sign-in/);
  assert.match(header, /nav-install/);
});
