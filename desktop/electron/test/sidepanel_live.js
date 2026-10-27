"use strict";
// The side question has to be seen over the built-in browser. The browser is a
// native view drawn above the whole page, so the panel can only be seen if the
// page stops covering the place the panel is. Real kernel, real window: the agent
// opens a page, the person widens it, opens the side question, and what is judged
// is whether the view still covers the panel. Run with `pnpm sidepanel-live`.
const os = require("node:os");
const path = require("node:path");
const fs = require("node:fs");
const { app } = require("electron");
const { checker, scriptedModel, listen, seedHome, settledWindow, until, wait } = require("./livekit");

const { check, failures } = checker();

async function main() {
  const page = await listen((_req, res) => {
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    res.end("<!doctype html><title>Greeter</title><body style='margin:0;background:#2a6'><h1>hello</h1>");
  });
  let calls = 0;
  const model = await listen(scriptedModel(() => (calls++ >= 1 ? null : { name: "browser_open", arguments: { url: page.url + "/" } })).handler);
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "rx-side-home-"));
  const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "rx-side-ws-"));
  process.env.REASONIX_HOME = home;
  seedHome(home, model.url, workspace);
  const { current } = require("../src/main.js");
  const win = await settledWindow();
  const { client } = current();
  const runtimes = await until("a pane", async () => {
    const list = await client.json("GET", "/runtimes");
    return Array.isArray(list) && list.length ? list : null;
  });
  const base = runtimes[0].base;
  const js = (src) => win.webContents.executeJavaScript(src);
  const guests = () => win.contentView.children.filter((v) => v.webContents && v.webContents !== win.webContents);
  await client.request("POST", `${base}/submit`, { input: "open the page" });
  const guest = await until("a page view", async () => guests()[0], 60000);
  await until("the page beside the conversation", async () => (guest.getBounds().x >= 0 ? true : null), 30000);
  await until("the full-view control", () => js(`!!document.querySelector('[data-action="browser.full"]')`));

  const panelRect = () => js(`(() => { const e = document.querySelector('.sidepanel'); if (!e) return null; const b = e.getBoundingClientRect(); return { x: b.left, y: b.top, width: b.width, height: b.height }; })()`);
  const covers = (view, rect) => view.x < rect.x + rect.width && view.x + view.width > rect.x && view.y < rect.y + rect.height && view.y + view.height > rect.y;

  for (const [width, height, full] of [[1440, 900, false], [1440, 900, true], [1100, 760, false], [1100, 760, true], [900, 700, true], [700, 700, true], [1440, 900, "panel-first"], [1100, 760, "panel-first"]]) {
    win.setContentSize(width, height);
    await wait(900);
    const label = `${width}px ${full === "panel-first" ? "side question open, then full view" : full ? "full view" : "beside the conversation"}`;
    if (full === true && (await js(`document.querySelector('.pane')?.getAttribute('data-browser-size')`)) !== "full") {
      await js(`document.querySelector('[data-action="browser.full"]')?.click()`);
      await wait(1200);
    }
    if (full === false && (await js(`document.querySelector('.pane')?.getAttribute('data-browser-size')`)) === "full") {
      await js(`document.querySelector('[data-action="browser.full"]')?.click()`);
      await wait(1200);
    }
    if (full === "panel-first") {
      if ((await js(`document.querySelector('.pane')?.getAttribute('data-browser-size')`)) === "full") {
        await js(`document.querySelector('[data-action="browser.full"]')?.click()`);
        await wait(1000);
      }
      await js(`document.querySelector('[data-action="side.toggle"]').click()`);
      await until("the side panel", async () => (await panelRect()) || null, 10000);
      await wait(800);
      await js(`document.querySelector('[data-action="browser.full"]')?.click()`);
      await wait(1500);
    } else {
      await js(`document.querySelector('[data-action="side.toggle"]').click()`);
      await until("the side panel", async () => (await panelRect()) || null, 10000);
    }
    await wait(1500);
    const panel = await panelRect();
    const view = guest.getBounds();
    const parked = view.x < 0;
    process.stdout.write(`  [bounds] ${label}: view=${JSON.stringify(view)} panel=${JSON.stringify(panel)} parked=${parked}\n`);
    check(`${label}: the side panel is drawn`, !!panel && panel.width > 100, panel);
    check(`${label}: the page does not cover the panel`, parked || !covers(view, panel), { view, panel });
    await js(`document.querySelector('[data-action="side.toggle"]').click()`);
    await wait(800);
  }
  app.exit(failures.length ? 1 : 0);
}

app.whenReady().then(main).catch((err) => {
  process.stdout.write(`  FAIL sidepanel-live crashed — ${err && err.stack}\n`);
  app.exit(1);
});
