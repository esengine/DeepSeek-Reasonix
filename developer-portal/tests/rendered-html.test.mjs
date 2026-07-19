import assert from "node:assert/strict";
import { access, readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);
const routes = [
  ["/", "从一次用户回合"],
  ["/architecture", "一个 Agent 内核，多种前端"],
  ["/runtime", "一次回合，就是系统的主干"],
  ["/state-safety", "任何状态，都先回答"],
  ["/desktop", "原生壳，不是第二套 Agent"],
  ["/extensions", "先选扩展层，再写代码"],
  ["/ecosystem", "本地内核可以独立运行"],
  ["/develop", "不要读完代码，再开始动手"],
  ["/reference", "遇到冲突，回到可执行证据"],
];

let workerPromise;
async function getWorker() {
  if (!workerPromise) {
    const workerUrl = new URL("../dist/server/index.js", import.meta.url);
    workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}`);
    workerPromise = import(workerUrl.href).then(({ default: worker }) => worker);
  }
  return workerPromise;
}

async function render(pathname) {
  const worker = await getWorker();
  return worker.fetch(
    new Request(`http://localhost${pathname}`, { headers: { accept: "text/html" } }),
    { ASSETS: { fetch: async () => new Response("Not found", { status: 404 }) } },
    { waitUntil() {}, passThroughOnException() {} },
  );
}

test("server-renders every onboarding route", async () => {
  for (const [pathname, expected] of routes) {
    const response = await render(pathname);
    assert.equal(response.status, 200, pathname);
    assert.match(response.headers.get("content-type") ?? "", /^text\/html\b/i, pathname);
    const html = await response.text();
    assert.match(html, new RegExp(expected), pathname);
    assert.match(html, /Reasonix Developer Atlas/, pathname);
    assert.match(html, /搜索开发地图/, pathname);
    assert.doesNotMatch(html, /codex-preview|SkeletonPreview|Your site is taking shape/, pathname);
  }
});

test("publishes social and brand assets", async () => {
  await Promise.all([
    access(new URL("public/og.png", root)),
    access(new URL("public/reasonix-logo.svg", root)),
    access(new URL("public/favicon.svg", root)),
  ]);

  const [layout, packageJson] = await Promise.all([
    readFile(new URL("app/layout.tsx", root), "utf8"),
    readFile(new URL("package.json", root), "utf8"),
  ]);
  assert.match(layout, /openGraph/);
  assert.match(layout, /\/og\.png/);
  assert.match(packageJson, /reasonix-developer-atlas/);
  assert.doesNotMatch(packageJson, /react-loading-skeleton/);
});
