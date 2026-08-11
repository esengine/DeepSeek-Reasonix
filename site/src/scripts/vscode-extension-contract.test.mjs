import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = (path) => readFile(new URL(path, import.meta.url), "utf8");

test("homepage presents one VS Code extension through both registries", async () => {
  const page = await source("../pages/index.astro");

  assert.match(page, /data-pane="vscode"/);
  assert.match(page, /data-pane="desktop"[\s\S]*data-pane="npm"[\s\S]*data-pane="brew"[\s\S]*data-pane="vscode"/);
  assert.match(page, /Editor extension[\s\S]*编辑器扩展/);
  assert.match(page, /More ways to use Reasonix:[\s\S]*更多使用方式：/);
  assert.match(page, /Local Web UI:[\s\S]*reasonix serve[\s\S]*本地 Web UI：/);
  assert.match(page, /ACP editor integration[\s\S]*ACP 编辑器接入/);
  assert.match(page, /SivanLiu\.reasonix-agent/);
  assert.match(page, /marketplace\.visualstudio\.com\/items\?itemName=SivanLiu\.reasonix-agent/);
  assert.match(page, /open-vsx\.org\/extension\/SivanLiu\/reasonix-agent/);
  assert.match(page, /does not bundle the CLI/);
  assert.match(page, /data-goto="vscode"/);
});

test("mobile download channels use a two-column tab grid", async () => {
  const css = await source("../styles/global.css");
  const mobile = css.slice(css.indexOf("@media (max-width: 640px)"), css.indexOf("@media (max-width: 360px)"));

  assert.match(mobile, /\.dl-tabs \{\s*display: grid; grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/);
});
