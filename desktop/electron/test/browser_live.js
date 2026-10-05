"use strict";
// Drives the agent's browser end to end in the real shell: a scripted model asks
// the real kernel to open and operate a page, and the checks read what the
// window actually drew. Run with `pnpm browser-live`; it needs no network.
const os = require("node:os");
const path = require("node:path");
const fs = require("node:fs");
const { execFileSync } = require("node:child_process");
const { app, BrowserWindow } = require("electron");
const { checker, listen, scriptedModel, seedHome, settledWindow, until, wait } = require("./livekit");

const PAGE = `<!doctype html><title>Greeter</title>
<label>Name <input></label>
<button onclick="document.querySelector('p').textContent='Hello, '+document.querySelector('input').value">Greet</button>
<p></p>
<a href="/elsewhere" target="_blank">Open elsewhere</a>
<a href="/report.csv" download>Get report</a>`;

const { check, failures } = checker();

// Each round's call is built from what the previous tool result showed.
// Which turn the script is on, and what it does in it; main() moves both before
// asking again.
const script = { turn: 1, calls: 0, act: "greet" };

function browsingModel(pageURL) {
  return scriptedModel((tools) => {
    const last = tools.length ? tools[tools.length - 1] : "";
    const ref = (role, name) => (last.match(new RegExp(`- ${role} "${name}" \\[(e\\d+)\\]`)) || [])[1];
    const greet = (name) => ({ name: "browser_act", arguments: { steps: [
      { action: "fill", ref: ref("textbox", "Name"), text: name },
      { action: "click", ref: ref("button", "Greet") },
    ] } });
    // The second turn runs with the window minimized: a fresh snapshot, then
    // the same input again.
    if (script.calls >= (script.act === "network" ? 3 : 2)) return null;
    script.calls++;
    if (script.calls === 1) return { name: "browser_open", arguments: { url: pageURL } };
    if (script.act === "greet") return greet(["李雷", "韩梅梅", "小明"][script.turn - 1]);
    // The tab is named: by now the page has opened others, and what this reads
    // has to be the one the checks hold a view of.
    if (script.act === "shot") return { name: "browser_read", arguments: { tab: "t1", what: "screenshot" } };
    if (script.act === "network") {
      return script.calls === 2
        ? { name: "browser_act", arguments: { tab: "t1", steps: [{ action: "resize", width: 390, height: 844 }] } }
        : { name: "browser_read", arguments: { tab: "t1", what: "network" } };
    }
    const link = script.act === "popup" ? "Open elsewhere" : "Get report";
    const at = (last.match(new RegExp(`- link "${link}" \\[(e\\d+)\\]`)) || [])[1];
    return { name: "browser_act", arguments: { steps: [{ action: "click", ref: at }, { action: "wait", ms: 1200 }] } };
  });
}

async function main() {
  const page = await listen((req, res) => {
    if (req.url === "/report.csv") {
      res.writeHead(200, { "Content-Type": "text/csv", "Content-Disposition": 'attachment; filename="report.csv"' });
      return res.end("a,b\n");
    }
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    res.end(req.url === "/elsewhere" ? "<title>Elsewhere</title><h1>elsewhere</h1>" : PAGE);
  });
  const { handler, requests } = browsingModel(`${page.url}/`);
  const model = await listen(handler);
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "rx-browser-live-"));
  const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "rx-browser-ws-"));
  process.env.REASONIX_HOME = home;
  seedHome(home, model.url, workspace);
  const { current } = require("../src/main.js");

  try {
    const win = await settledWindow();
    const { client } = current();
    const runtimes = await until("a pane", async () => {
      const list = await client.json("GET", "/runtimes");
      return Array.isArray(list) && list.length ? list : null;
    });
    const base = runtimes[0].base;
    await client.request("POST", `${base}/submit`, { input: "greet 李雷 on the page" });
    await until("the scripted turn", async () => requests.length >= 3, 60000);

    const guests = () => win.contentView.children.filter((v) => v.webContents && v.webContents !== win.webContents);
    const guest = await until("a page view", async () => guests()[0]);
    check("the agent's page is a view in this window", guests().length === 1, guests().length);
    check("the view is not on the window's own session", guest.webContents.session !== win.webContents.session);
    check("the page loaded what the agent opened", guest.webContents.getURL() === `${page.url}/`, guest.webContents.getURL());
    const said = await guest.webContents.executeJavaScript("document.querySelector('p').textContent");
    check("the agent's input reached the page", said === "Hello, 李雷", said);
    const actResult = String((requests[2].messages || []).filter((m) => m.role === "tool").at(-1)?.content || "");
    check("the model was told what changed", actResult.includes('+ - text: "Hello, 李雷"'), actResult.slice(0, 400));

    const tabs = await client.json("GET", `${base}/browser/tabs`);
    check("the pane lists the tab with its view's target", tabs?.length === 1 && tabs[0].url === `${page.url}/`, tabs);
    const beneath = () => guest.getBounds().x < 0;
    const width = () => guest.getBounds().width;
    // Watching the agent browse and reading what it says are one activity: the
    // page is beside the conversation from the moment it opens.
    const aside = await until("the page beside the conversation", async () => (!beneath() ? guest.getBounds() : null));
    check("the page is drawn beside the conversation as soon as the agent opens one",
      aside.width < win.getContentBounds().width, { aside, content: win.getContentBounds() });

    const js = (src) => win.webContents.executeJavaScript(src);
    const reserved = () => js(`(() => { const e = document.querySelector('.bview'); if (!e) return null; const b = e.getBoundingClientRect(); return { x: b.left, y: b.top, width: b.width, height: b.height }; })()`);
    await until("the browser tab in the pane bar", () => js(`!!document.querySelector('[data-action="pane.view"][data-value="browser"]')`));
    await js(`document.querySelector('[data-action="pane.view"][data-value="browser"]').click()`);
    const placed = await until("the page drawn full width", async () => (width() > aside.width ? guest.getBounds() : null));
    const slot = await reserved();
    const close = (a, b) => Math.abs(a - b) <= 1;
    // The rectangle itself has to be the pane: with the conversation's column
    // still holding its place, the page was squeezed into the bottom of it.
    check("the full-width view gives the page the whole pane",
      slot.height > win.getContentBounds().height * 0.6, { slot, content: win.getContentBounds() });
    check("the page is drawn exactly over the rectangle the panel reserved",
      close(placed.x, slot.x) && close(placed.y, slot.y) && close(placed.width, slot.width) && close(placed.height, slot.height),
      { placed, slot });

    const shot = path.join(os.tmpdir(), "rx-browser-live.png");
    try {
      const id = win.getMediaSourceId().split(":")[1];
      execFileSync("screencapture", ["-x", "-o", "-l", id, shot]);
      process.stdout.write(`  screenshot: ${shot}\n`);
    } catch (err) {
      process.stdout.write(`  (no screenshot: ${err.message})\n`);
    }

    await js(`document.querySelector('[data-action="pane.view"][data-value="flow"]').click()`);
    const back = await until("the page beside the conversation again", async () => (width() < placed.width ? guest.getBounds() : null));
    check("leaving the full-width view puts the page back beside the conversation", back.width === aside.width && !beneath(), { back, aside });

    await js(`document.querySelector('[data-action="pane.dock"]').click()`);
    await until("the page put away", async () => beneath());
    check("turning the side-by-side view off stops drawing the page", beneath());

    const greeting = async () => {
      const view = guests()[0];
      return view ? view.webContents.executeJavaScript("document.querySelector('p').textContent") : "";
    };
    const turn = async (act, input) => {
      script.turn += 1;
      script.calls = 0;
      script.act = act;
      const before = requests.length;
      await client.request("POST", `${base}/submit`, { input });
      // One request per call plus the one that reads the last result.
      const calls = act === "network" ? 3 : 2;
      await until(`the turn that would ${act}`, async () => requests.length > before + calls, 90000);
      return (requests.at(-1).messages || []).filter((m) => m.role === "tool").map((m) => String(m.content || "")).join("\n");
    };
    const askFor = async (name) => {
      await turn("greet", `greet ${name} as well`);
      return until(`the greeting the agent typed for ${name}`, async () => ((await greeting()) === `Hello, ${name}` ? true : null), 60000).catch(() => false);
    };

    // A page in a minimized window, or in one another application covers, counts
    // as hidden, and a hidden page drops the input the agent sends it. A person
    // is in another application for most of a run, so both are ordinary.
    win.minimize();
    await wait(1000);
    check("the agent's input reaches the page while the window is minimized", await askFor("韩梅梅"), await greeting());

    win.restore();
    const cover = new BrowserWindow({ width: 1600, height: 1000, x: 0, y: 0, alwaysOnTop: true });
    await cover.loadURL("data:text/html,<h1>cover</h1>");
    cover.focus();
    await wait(1200);
    check("the agent's input reaches the page while another window covers this one", await askFor("小明"), await greeting());
    cover.destroy();

    // A popup is a tab of its own, and a download is refused and reported: both
    // are this window's answer to the protocol, not a browser's.
    const popped = await turn("popup", "open the other page");
    check("a popup becomes a tab the model can act on", /Tab t2/.test(popped), popped.slice(0, 200));
    check("the popup is a second view in this window", guests().length === 2, guests().length);
    const downloaded = await turn("download", "get the report");
    check("a download is refused and reported", /report\.csv.*refused/s.test(downloaded), downloaded.slice(0, 300));

    // The viewport and the request list are the browser's own state, relayed
    // over this window's debugger rather than a browser's own port.
    // The picture a call took is the one thing a person watching cannot get any
    // other way, and it reached the model and stopped there until now.
    await turn("shot", "take a picture of the page");
    const drawn = await until("the screenshot in the transcript", async () =>
      win.webContents.executeJavaScript('document.querySelectorAll(".tshot img").length'), 20000).catch(() => 0);
    check("the window draws what the agent was shown", drawn >= 1, drawn);

    const observed = await turn("network", "lay the page out at phone width, then say what it requested");
    const laidOut = await guest.webContents.executeJavaScript("[innerWidth, innerHeight]");
    check("the page is laid out in the viewport the agent asked for", laidOut[0] === 390 && laidOut[1] === 844, laidOut);
    check("the model was told what the page requested",
      new RegExp(`GET ${page.url}/ \\[document\\] → 200 text/html`).test(observed), observed.slice(-400));
  } catch (err) {
    check("the run completed", false, err.message);
  }
  process.stdout.write(failures.length ? `${failures.length} check(s) failed\n` : "all checks passed\n");
  app.exit(failures.length ? 1 : 0);
}

app.whenReady().then(() => setTimeout(main, 0));
