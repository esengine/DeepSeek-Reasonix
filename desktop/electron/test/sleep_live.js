"use strict";
// Drives page sleep in the real shell with no kernel: real Chromium renderers
// through BrowserViews, a tiny sleep threshold, and the shell's own process
// memory read around the sleep. A wake must answer a command against the page
// as it loaded, not as it was. Run with `pnpm sleep-live`; it needs no network.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");
const { BrowserViews } = require("../src/browserviews.js");

const PAGE = `<!doctype html><meta charset="utf-8"><title>heavy</title>
<style>td{padding:4px 24px}</style>
<table>${("<tr>" + "<td>payload</td>".repeat(40) + "</tr>").repeat(600)}</table>
<script>let n=0; setInterval(() => { document.title = "tick " + (++n); }, 500);</script>`;

const PAGE2 = `<!doctype html><meta charset="utf-8"><title>second</title><p>the second page</p>`;

const mb = (k) => Math.round(k / 1024);
const metrics = () =>
  app
    .getAppMetrics()
    .filter((m) => m.type === "Tab" || m.type === "Renderer")
    .reduce((acc, m) => ({ count: acc.count + 1, ws: acc.ws + m.memory.workingSetSize }), { count: 0, ws: 0 });
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
async function until(what, fn, ms = 30000) {
  const end = Date.now() + ms;
  for (;;) {
    const got = await fn();
    if (got) return got;
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`);
    await wait(250);
  }
}

async function main() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-sleep-probe-"));
  const file = path.join(dir, "page.html");
  fs.writeFileSync(file, PAGE);
  const file2 = path.join(dir, "page2.html");
  fs.writeFileSync(file2, PAGE2);

  const win = new BrowserWindow({ show: false, width: 1280, height: 900 });
  await win.loadURL("about:blank");
  const views = new BrowserViews({ win, kernelOrigin: "http://127.0.0.1:1", sleepAfterMs: 8000, sweepMs: 2000, maxAwake: 6 });

  // Every domain event each page's debugger session produces is kept, so the
  // probe can assert later that the events a wake's rebuilt session must
  // carry really flowed.
  const handles = [];
  const eventFrames = [];
  for (let i = 0; i < 5; i++) {
    const events = [];
    eventFrames.push(events);
    handles.push(
      views.create({
        partition: `probe-${i}`,
        url: "file://" + file.replace(/\\/g, "/"),
        onEvent: (method, params) => events.push({ method, params }),
        onClosed: () => {},
        onPopup: () => {},
        onDownload: () => {},
      }),
    );
  }
  await until("pages to load and their renderers to settle", async () => {
    await wait(1000);
    return metrics().count >= 5;
  });
  await wait(1500);
  // What the kernel says once at attach, said to page 3 before it dozes: the
  // wake must say them again to the session it builds, or no domain event
  // reaches the page after.
  for (const [method, params] of [
    ["Page.enable", {}],
    ["Runtime.enable", {}],
    ["Network.enable", {}],
    ["Page.setLifecycleEventsEnabled", { enabled: true }],
  ]) await handles[2].send(method, params);
  const before = metrics();
  console.log(`[probe] 5 pages live: ${before.count} renderer processes, ${mb(before.ws)} MB working set`);

  await until("the sweep to sleep every page", async () => {
    await wait(1000);
    return handles.every((h) => views.asleep(h.targetId));
  });
  await wait(2500); // let the OS reap the dead processes
  const after = metrics();
  console.log(`[probe] after sleep:   ${after.count} renderer processes, ${mb(after.ws)} MB working set`);
  console.log(`[probe] reclaimed:    ${before.count - after.count} processes, ${mb(before.ws - after.ws)} MB`);

  // A wake must answer a real command on a real page.
  const answer = await handles[2].send("Runtime.evaluate", { expression: "document.querySelectorAll('td').length", returnByValue: true });
  const cells = answer && answer.result && answer.result.value;
  const woken = metrics();
  console.log(`[probe] wake #3:      Runtime.evaluate answered cells=${cells}; renderers now ${woken.count}, ${mb(woken.ws)} MB`);
  if (after.count >= before.count) throw new Error("sleep reclaimed no renderer processes");
  if (cells !== 40 * 600) throw new Error(`wake answered wrong value: ${cells}`);
  if (woken.count !== after.count + 1) throw new Error(`wake rebuilt ${woken.count - after.count} renderers, wanted 1`);

  // Domain events must flow after a wake on a real rebuilt session: the
  // kernel's waitLoaded depends exactly on Page.frameNavigated arriving. The
  // enables said before the sleep were replayed to the session the wake built.
  await handles[2].send("Page.navigate", { url: "file://" + file2.replace(/\\/g, "/") });
  await until(
    "post-wake Page.frameNavigated for page2",
    () =>
      eventFrames[2].some(
        (f) => f.method === "Page.frameNavigated" && f.params && f.params.frame && typeof f.params.frame.url === "string" && f.params.frame.url.includes("page2"),
      ),
    15000,
  );
  const titled = await until("page2's title to read back", async () => {
    const said = await handles[2].send("Runtime.evaluate", { expression: "document.title", returnByValue: true });
    const value = said && said.result && said.result.value;
    return value === "second" ? value : null;
  }, 15000);
  console.log(`[probe] post-wake:    navigate answered frameNavigated for page2, title="${titled}"`);

  // A command still in flight when the sleep lands must fail fast, not hang:
  // the kernel reads an error it can retry, never a timeout it cannot tell
  // from a dead page. The hanging ask wakes page 5, which then idles out and
  // sleeps again underneath it — the exact window being pinned.
  const pending = handles[4].send("Runtime.evaluate", { expression: "new Promise(() => {})", awaitPromise: true, returnByValue: true });
  const held = pending.then(() => "resolved", (err) => `rejected: ${err.message}`);
  await until("the sleeping page's in-flight command to settle", async () => {
    await wait(500);
    return !!(await Promise.race([held, Promise.resolve(false)]));
  }, 25000);
  const outcome = await held;
  if (!outcome.startsWith("rejected")) throw new Error(`a command on a slept page ${outcome} instead of failing`);
  console.log("[probe] in-flight:   " + outcome);

  console.log("[probe] PASS");
  views.closeAll();
  win.destroy();
  app.exit(0);
}

app.whenReady().then(main).catch((err) => {
  console.error("[probe] FAIL:", err.message);
  app.exit(1);
});
