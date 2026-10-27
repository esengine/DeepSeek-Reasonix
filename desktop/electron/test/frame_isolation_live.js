"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");
const { checker, settledWindow, until, wait } = require("./livekit");
const { frameFixture } = require("./frame_fixture");
const { fromMainFrame } = require("../src/frameguard");
const fixture = frameFixture();
const { current } = require("../src/main");
const { check, failures } = checker();
let completed = false;
app.on("will-quit", () => { if (!completed) { console.error("Native checks interrupted before completion"); app.exit(1); } });
const deadline = setTimeout(() => { console.error("Native checks exceeded two minutes"); app.exit(1); }, 120000);

async function run() {
  console.log(`Electron ${process.versions.electron}, Chromium ${process.versions.chrome}, ${process.platform}/${process.arch}`);
  const win = await settledWindow();
  win.show();
  win.focus();
  const js = source => win.webContents.executeJavaScript(source);
  await until("the startup overlay to leave", () => js(`!document.getElementById('boot')`));
  await js(`document.querySelector('[data-action="pane.view"][data-value="browser"]').click()`);
  const runtimes = await until("a pane", () => current().client.json("GET", "/runtimes").then(list => list?.length ? list : null));
  const published = await current().client.request("POST", `${runtimes[0].base}/extensions/action`, { name: "/task-board:refresh" });
  check("the installed example publishes through its declared action", published.status === 200, published);
  await until("the task board tab", () => js(`(() => { const tab = [...document.querySelectorAll('[role="tab"]')].find(t => t.textContent.includes('Task board')); if (!tab) return false; tab.click(); return true; })()`));
  await until("the real extension iframe", () => js(`!!document.querySelector('iframe.xframe-doc')`));
  const frame = await until("the task board document", () => win.webContents.mainFrame.frames.find(f => f.url.includes("/extensions/frames/task-board/board")));
  const child = source => frame.executeJavaScript(source);
  await until("the native MessageChannel handshake", () => child(`probe.received.some(m => m.type === 'init')`));
  const url = frame.url;
  const security = await child(`(() => {
    const read = f => { try { f(); return false; } catch { return true; } };
    return { globals: [typeof require, typeof process, typeof Buffer, typeof reasonixHost],
      parent: read(() => parent.document.body), cookie: read(() => document.cookie),
      storage: read(() => localStorage.length) };
  })()`);
  check("the production frame has a separate OS process", frame.osProcessId !== win.webContents.mainFrame.osProcessId,
    { frame: frame.osProcessId, host: win.webContents.mainFrame.osProcessId });
  check("the frame has neither Node nor the privileged preload bridge", security.globals.every(v => v === "undefined"), security);
  check("the opaque frame cannot read parent DOM, cookies or storage", security.parent && security.cookie && security.storage, security);
  check("the IPC authority gate rejects an actual child frame", fromMainFrame(win, { sender: win.webContents, senderFrame: frame }) === null);
  check("the IPC authority gate accepts the actual main frame", fromMainFrame(win, { sender: win.webContents, senderFrame: win.webContents.mainFrame }) === win);
  const csp = await js(`fetch(${JSON.stringify(url)}).then(r => ({status:r.status, csp:r.headers.get('Content-Security-Policy')}))`);
  check("the real kernel route sends its restrictive CSP", csp.status === 200 && csp.csp.includes("connect-src 'none'") && csp.csp.includes("sandbox allow-scripts"), csp);
  const network = await child(`fetch(${JSON.stringify(new URL("/status", url).href)}, { credentials: 'include' }).then(() => false, () => true)`);
  check("CSP blocks credentialed kernel requests from the frame", network);
  check("permission requests deny the frame", await child(`Notification.requestPermission().then(value => value === 'denied')`));
  const beforeWindows = BrowserWindow.getAllWindows().length;
  await child(`window.open('about:blank'); location.href = ${JSON.stringify(new URL("/status", url).href)}; true`);
  await wait(400);
  check("the document cannot navigate out of the frame route", frame.url === url, frame.url);
  check("the document cannot open a popup", BrowserWindow.getAllWindows().length === beforeWindows);
  await child(`[...document.querySelectorAll('.card')].find(card => card.textContent.includes('Write the slot docs')).querySelector('button').click(); true`);
  await until("the declared action updates props", () => child(`probe.received.some(m => m.type === 'props' && m.props.columns.todo.length === 0)`));
  check("a declared action completes through the real sidecar", true);
  const artifact = process.env.REASONIX_FRAME_EVIDENCE;
  if (artifact) {
    fs.mkdirSync(artifact, { recursive: true });
    fs.writeFileSync(path.join(artifact, "frame-isolation.png"), (await win.webContents.capturePage()).toPNG());
  }
  await child(`probe.port.postMessage({type:'state',value:{draft:'same-pane'}}); true`);
  await wait(100);
  // A same-document load starts a new channel and restores only this owner's state.
  await js(`document.querySelector('iframe.xframe-doc').src = ${JSON.stringify(url)}; true`);
  const reloaded = await until("a reloaded document handshake", async () => {
    const f = win.webContents.mainFrame.frames.find(f => f.url === url);
    if (!f) return null;
    return await f.executeJavaScript(`typeof probe !== 'undefined' && probe.received.some(m => m.type === 'init' && m.reason === 'remount')`) ? f : null;
  });
  check("reload restores state within the same pane", await reloaded.executeJavaScript(`probe.received.find(m => m.type === 'init').state?.draft === 'same-pane'`));
  await reloaded.executeJavaScript(`setTimeout(() => { const cycle = {}; cycle.self = cycle;
    probe.port.postMessage({type:'state', value:cycle});
    probe.port.postMessage({type:'submit', values:{number:1n}});
    probe.port.postMessage({type:'state', value:new Map([['x',1]])}); }, 50); true`);
  await until("invalid structured clones close the frame", () => js(`!!document.querySelector('.xframe[data-phase="failed"]')`));
  check("cycles, BigInt and non-JSON objects fail closed without crashing Studio", await js(`!!document.querySelector('.chrome')`));
  await js(`document.querySelector('[data-action="extensions.frame-retry"]').click()`);
  const retried = await until("a retried frame", async () => {
    const f = win.webContents.mainFrame.frames.find(f => f.url === url);
    return f && await f.executeJavaScript(`typeof probe !== 'undefined' && !!probe.port`) ? f : null;
  });
  await retried.executeJavaScript(`setTimeout(() => { for(let i=0;i<100;i++) probe.port.postMessage({type:'ready'}); }, 50); true`);
  await until("duplicate ready closes the frame", () => js(`!!document.querySelector('.xframe[data-phase="failed"]')`));
  check("ready floods close the actual channel", true);
  check("the main renderer and kernel stay responsive", (await current().client.request("GET", "/status")).status === 200);
}

app.whenReady().then(run).catch(err => { failures.push(String(err)); console.error(err); }).finally(() => {
  completed = true;
  clearTimeout(deadline);
  console.log(failures.length ? `${failures.length} failed` : "all frame isolation checks passed");
  console.log(`Disposable fixture: ${fixture.home}`);
  app.quit();
  setTimeout(() => app.exit(failures.length ? 1 : 0), 5000);
});
