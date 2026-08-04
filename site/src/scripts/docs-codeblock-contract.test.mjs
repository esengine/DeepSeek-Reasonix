import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const configSource = () => readFile(new URL("../../astro.config.mjs", import.meta.url), "utf8");
const docsSource = () => readFile(new URL("../pages/docs.astro", import.meta.url), "utf8");
const stylesSource = () => readFile(new URL("../styles/global.css", import.meta.url), "utf8");

test("Astro preserves documentation code sample newlines", async () => {
  const config = await configSource();

  assert.match(config, /compressHTML:\s*false/);
});

test("copyable documentation code blocks reserve space for their buttons", async () => {
  const [page, styles] = await Promise.all([docsSource(), stylesSource()]);
  const codeblocks = [...page.matchAll(/<div class="([^"]*\bcodeblock\b[^"]*)">([\s\S]*?)<\/div>/g)];
  const copyable = codeblocks.filter(([, , body]) => body.includes("data-copy="));

  assert.ok(copyable.length > 0);
  for (const [, classes] of copyable) {
    assert.match(classes, /(?:^|\s)codeblock--copy(?:\s|$)/);
  }
  assert.match(styles, /\.codeblock--copy\s*\{\s*padding-top:\s*52px;\s*\}/);
});

test("the macOS quarantine comment and command remain separate source lines", async () => {
  const page = await docsSource();

  assert.match(
    page,
    /# Quit Reasonix first, then run in Terminal\.<\/span>\nsudo xattr -rd com\.apple\.quarantine \/Applications\/Reasonix\.app/,
  );
});

test("website documents the version-matched built-in docs command", async () => {
  const page = await docsSource();

  assert.match(page, /id="embedded-docs"/);
  assert.match(page, /\/docs 1\.19\.5 changelog/);
  assert.match(page, /AI configured for the current session/);
  assert.match(page, /normally <code>\/reasonix:docs<\/code>/);
  assert.match(page, /Reasonix never overwrites the existing command/);
  assert.match(page, /Release CI rejects a build when its embedded corpus does not match/);
});
