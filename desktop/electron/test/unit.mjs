import { test, mock } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import path from "node:path";
import os from "node:os";
import fs from "node:fs";
import { EventEmitter } from "node:events";

const require = createRequire(import.meta.url);
const cuaContract = require("../../../internal/platform/computer/cua-driver.lock.json");
const { parse, readActs, start } = require("../src/host.js");
const { contextTemplate, editMenuTemplate, applicationMenuTemplate, menuInstaller } = require("../src/editmenu.js");
const { textContextTemplate, textContextMenuHandler } = require("../src/textcontextmenu.js");
const { uiLanguage } = require("../src/uilang.js");
const { externalTarget } = require("../src/links.js");
const { offerCleanup, ownBundle } = require("../src/legacy.js");
const { stripPackageGrants, readReport, unpaintedWindowCause } = require("../src/packagegrants.js");
const { groundFor, themeSourceFor, GROUND } = require("../src/ground.js");
const { folderAnswer } = require("../src/folderpanel.js");
const { pick, loadPrefs, savePrefs, registerPrefs, PAGE_SLEEP_MS, pageSleepAfterMs } = require("../src/prefs.js");

const TOKEN = "a".repeat(64);
const line = (over) => JSON.stringify({ version: 1, origin: "http://127.0.0.1:8080", token: TOKEN, ...over });

test("the spawned host receives the system language and inherits explicit locale overrides", async () => {
  const script = `console.log(${JSON.stringify(line())});
    console.log(JSON.stringify({act: JSON.stringify({system: process.env.REASONIX_SYSTEM_LANG,
      explicit: process.env.REASONIX_LANG})}));`;
  const before = process.env.REASONIX_SYSTEM_LANG;
  const explicit = process.env.REASONIX_LANG;
  const state = new Promise((resolve) => {
    const host = start(process.execPath, ["-e", script], {
      systemLanguage: "zh-Hant-TW",
      onAct: (act) => resolve(JSON.parse(act)),
    });
    host.ready.catch(resolve);
  });
  assert.deepEqual(await state, { system: "zh-Hant-TW", ...(explicit === undefined ? {} : { explicit }) });
  assert.equal(process.env.REASONIX_SYSTEM_LANG, before);
});

test("the handshake is accepted only when every field accounts for itself", () => {
  assert.deepEqual(parse(line()), { origin: "http://127.0.0.1:8080", token: TOKEN });

  const refused = [
    ["not JSON at all", "hello"],
    ["a version this shell does not know", line({ version: 2 })],
    ["no version at all", JSON.stringify({ origin: "http://127.0.0.1:8080", token: TOKEN })],
    ["a scheme this shell does not load", line({ origin: "https://127.0.0.1:8080" })],
    ["an address that is not this machine", line({ origin: "http://10.0.0.5:8080" })],
    ["a name a resolver answers for", line({ origin: "http://localhost:8080" })],
    ["no port", line({ origin: "http://127.0.0.1" })],
    ["a credential too short to be one", line({ token: "short" })],
    ["no credential", JSON.stringify({ version: 1, origin: "http://127.0.0.1:8080" })],
  ];
  for (const [why, raw] of refused) {
    assert.throws(() => parse(raw), undefined, `accepted ${why}`);
  }
});

test("no refusal repeats the line it refused", () => {
  // The credential is in that line; a message carrying it would put it into
  // whatever collects this process's logs.
  for (const raw of [line({ version: 9 }), line({ origin: "https://127.0.0.1:1" }), "not json"]) {
    try {
      parse(raw);
      assert.fail("expected a refusal");
    } catch (err) {
      assert.ok(!err.message.includes(TOKEN), `the refusal carried the credential: ${err.message}`);
    }
  }
});

test("the context menu offers nothing where nothing can be edited", () => {
  assert.deepEqual(contextTemplate({ isEditable: false, selectionText: "", editFlags: {} }), []);
  assert.ok(contextTemplate({ isEditable: true, selectionText: "", editFlags: {} }).length > 0);
  assert.ok(contextTemplate({ isEditable: false, selectionText: "picked", editFlags: {} }).length > 0);
});

test("the context menu mirrors what the page says is possible", () => {
  const flags = { canUndo: true, canRedo: false, canCut: true, canCopy: true, canPaste: false, canSelectAll: true };
  const items = contextTemplate({ isEditable: true, selectionText: "x", editFlags: flags });
  const byRole = Object.fromEntries(items.filter((i) => i.role).map((i) => [i.role, i.enabled]));
  assert.deepEqual(byRole, {
    undo: true, redo: false, cut: true, copy: true, paste: false, selectAll: true,
  });
});

test("a reader's generic text menu never offers mutation roles", () => {
  const items = contextTemplate({ isEditable: false, selectionText: "picked",
    editFlags: { canCopy: true, canSelectAll: true, canCut: true, canPaste: true, canUndo: true } }, "en");
  assert.deepEqual(items.map((item) => item.role), ["copy", "selectAll"]);
  assert.ok(items.every((item) => item.enabled));
});

const textMenuRequest = (over = {}) => ({
  editable: true, selected: true, canUndo: true, canRedo: false, quote: true, x: 12.5, y: 24, ...over,
});
const menuRoles = (items) => Object.fromEntries(items.filter((item) => item.role || item.id)
  .map((item) => [item.role ?? item.id, item.enabled]));

test("workbench native menus distinguish editable and reader surfaces and translate quoting", () => {
  const editable = textContextTemplate(textMenuRequest(), "en", () => {});
  assert.deepEqual(menuRoles(editable), { undo: true, redo: false, cut: true, copy: true, paste: true, selectAll: true });
  assert.equal(editable.at(-1).label, "Add to conversation");
  assert.ok(editable.filter((item) => item.role).every((item) => !item.click));
  const empty = textContextTemplate(textMenuRequest({ selected: false }), "en", () => {});
  assert.deepEqual(menuRoles(empty), { undo: true, redo: false, cut: false, copy: false, paste: true, selectAll: true });
  assert.equal(empty.filter((item) => item.click).length, 3);
  const reader = textContextTemplate(textMenuRequest({ editable: false }), "zh", () => {});
  assert.deepEqual(menuRoles(reader), { copy: true, selectAll: true });
  assert.equal(reader.at(-1).label, "加入对话");
  assert.deepEqual(textContextTemplate(textMenuRequest({ editable: false, selected: false }), "en", () => {}), []);
  assert.equal(textContextTemplate(textMenuRequest({ quote: false }), "en", () => {}).filter((item) => item.click).length, 3);
});

function textMenuHarness() {
  const contents = Object.assign(new EventEmitter(), {
    mainFrame: { url: "http://127.0.0.1:8080/" }, getZoomFactor: () => 1.5,
  });
  const window = Object.assign(new EventEmitter(), { webContents: contents, isDestroyed: () => false });
  const menus = [];
  const Menu = { buildFromTemplate: (template) => {
    const menu = { template, popup: (options) => { menu.options = options; },
      closePopup: (target) => { assert.equal(target, window); menu.closed = true; } };
    menus.push(menu);
    return menu;
  } };
  const event = { sender: contents, senderFrame: contents.mainFrame };
  const handle = textContextMenuHandler(Menu, (e) => e.sender === contents ? window : null,
    () => "http://127.0.0.1:8080", () => "en");
  return { contents, window, menus, event, handle };
}

test("native workbench popup returns quote or dismissal and uses zoomed client coordinates", async () => {
  const h = textMenuHarness();
  const picked = h.handle(h.event, textMenuRequest());
  const menu = h.menus[0];
  assert.equal(menu.options.window, h.window);
  assert.equal(menu.options.x, 19);
  assert.equal(menu.options.y, 36);
  menu.template.at(-1).click();
  menu.options.callback();
  assert.equal(await picked, "quote");
  const dismissed = h.handle(h.event, textMenuRequest());
  h.menus[1].options.callback();
  assert.equal(await dismissed, "");
  assert.equal(h.contents.listenerCount("destroyed"), 0);
  assert.equal(h.window.listenerCount("closed"), 0);
});

test("workbench history and select-all commands return editor actions, not native roles", async () => {
  const h = textMenuHarness();
  for (const [action, accelerator] of [
    ["undo", "CommandOrControl+Z"],
    ["redo", process.platform === "darwin" ? "CommandOrControl+Shift+Z" : "CommandOrControl+Y"],
    ["selectAll", "CommandOrControl+A"],
  ]) {
    const result = h.handle(h.event, textMenuRequest({ canRedo: true }));
    const menu = h.menus.at(-1);
    const item = menu.template.find((entry) => entry.id === action);
    assert.equal(item.role, undefined);
    assert.equal(item.accelerator, accelerator);
    assert.equal(item.enabled, true);
    item.click();
    menu.options.callback();
    assert.equal(await result, action);
  }
  const disabled = textContextTemplate(textMenuRequest({ canUndo: false }), "en", () => {});
  assert.equal(disabled.find((item) => item.id === "undo").enabled, false);
  assert.equal(disabled.find((item) => item.id === "redo").enabled, false);
});

test("native workbench popup rejects foreign senders, subframes, origins and malformed capabilities", async () => {
  const h = textMenuHarness();
  for (const event of [
    { ...h.event, sender: {} },
    { ...h.event, senderFrame: { url: h.contents.mainFrame.url } },
    { ...h.event, senderFrame: null },
  ]) assert.equal(await h.handle(event, textMenuRequest()), "");
  for (const url of ["http://127.0.0.1:8081/", "http://127.0.0.1:8080.evil/", "about:blank"]) {
    h.contents.mainFrame.url = url;
    assert.equal(await h.handle(h.event, textMenuRequest()), "");
  }
  h.contents.mainFrame.url = "http://127.0.0.1:8080/";
  for (const request of [null, [], {}, textMenuRequest({ selected: "yes" }),
    textMenuRequest({ canUndo: 1 }), textMenuRequest({ quote: undefined }),
    textMenuRequest({ x: NaN }), textMenuRequest({ y: Infinity }), textMenuRequest({ x: "1" }),
    textMenuRequest({ items: [{ role: "quit" }] })]) {
    assert.equal(await h.handle(h.event, request), "");
  }
  assert.equal(await h.handle(h.event, textMenuRequest({ editable: false, selected: false })), "");
  assert.equal(h.menus.length, 0);
});

test("native workbench popups settle and clean up on navigation, destruction and replacement", async () => {
  for (const lifecycle of ["navigation", "contents", "window", "replacement"]) {
    const h = textMenuHarness();
    const pending = h.handle(h.event, textMenuRequest());
    if (lifecycle === "navigation") {
      h.contents.emit("did-start-navigation", { isMainFrame: false });
      assert.equal(h.menus[0].closed, undefined);
      h.contents.emit("did-start-navigation", { isMainFrame: true });
    } else if (lifecycle === "contents") h.contents.emit("destroyed");
    else if (lifecycle === "window") h.window.emit("closed");
    else {
      const next = h.handle(h.event, textMenuRequest());
      h.menus[1].options.callback();
      assert.equal(await next, "");
    }
    assert.equal(await pending, "");
    assert.equal(h.menus[0].closed, true);
    assert.equal(h.contents.listenerCount("did-start-navigation"), 0);
    assert.equal(h.contents.listenerCount("destroyed"), 0);
    assert.equal(h.window.listenerCount("closed"), 0);
  }
});

test("the edit menu's labels follow the interface language, not the system's", () => {
  const params = { isEditable: true, selectionText: "", editFlags: {} };
  const labels = (lang) => contextTemplate(params, lang).filter((i) => i.role).map((i) => i.label);
  assert.deepEqual(labels("zh"), ["撤销", "重做", "剪切", "复制", "粘贴", "全选"]);
  assert.deepEqual(labels("en"), ["Undo", "Redo", "Cut", "Copy", "Paste", "Select All"]);
  assert.equal(editMenuTemplate("zh").label, "编辑");
});

test("the edit menu keeps everything the platform's own one carries", () => {
  const roles = (items) => items.flatMap((i) => [i.role, ...(i.submenu ? roles(i.submenu) : [])]).filter(Boolean);
  const want = ["undo", "redo", "cut", "copy", "paste", "pasteAndMatchStyle", "delete", "selectAll",
    "showSubstitutions", "toggleSmartQuotes", "toggleSmartDashes", "toggleTextReplacement", "startSpeaking", "stopSpeaking"];
  for (const lang of ["zh", "en"]) {
    assert.deepEqual(roles(editMenuTemplate(lang).submenu), want);
    const labels = (items) => items.flatMap((i) => [i.label, ...(i.submenu ? labels(i.submenu) : [])]).filter((l) => l !== undefined);
    assert.ok(labels(editMenuTemplate(lang).submenu).every((l) => l.length > 0));
  }
  assert.deepEqual(editMenuTemplate("en").submenu.map((i) => i.role ?? i.type ?? i.label),
    ["undo", "redo", "separator", "cut", "copy", "paste", "pasteAndMatchStyle", "delete", "selectAll", "separator", "Substitutions", "Speech"]);
  assert.deepEqual(applicationMenuTemplate("zh").map((i) => i.role ?? "edit"), ["appMenu", "edit", "windowMenu"]);
});

test("the application menu is rebuilt when the language changes and not otherwise", () => {
  const built = [];
  const Menu = {
    buildFromTemplate: (t) => (built.push(t), { template: t }),
    setApplicationMenu: () => {},
    getApplicationMenu: () => null,
  };
  const install = menuInstaller(Menu, "darwin");
  let lang = "en";
  install(() => lang);
  install(() => lang);
  assert.equal(built.length, 1);
  lang = "zh";
  install(() => lang);
  assert.equal(built.length, 2);
  assert.equal(built[1][1].label, "编辑");
  let cleared = 0;
  menuInstaller({ ...Menu, setApplicationMenu: () => cleared++ }, "linux")(() => "zh");
  assert.equal(cleared, 1);
  assert.equal(built.length, 2);
});

test("the interface language is the page's own choice, else the machine's", () => {
  assert.equal(uiLanguage({ "rx-lang": "zh" }, "en-US"), "zh");
  assert.equal(uiLanguage({ "rx-lang": "en" }, "zh-CN"), "en");
  assert.equal(uiLanguage({ "rx-lang": "" }, "zh-Hans-CN"), "zh");
  assert.equal(uiLanguage({}, "fr-FR"), "en");
  assert.equal(uiLanguage(undefined, undefined), "en");
});

test("a saved preference tells the shell so it can follow it", () => {
  const handlers = {};
  const ipc = { on: (name, fn) => { handlers[name] = fn; } };
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-prefs-"));
  let saved = 0;
  registerPrefs(ipc, () => path.join(dir, "p.json"), () => true, () => { saved++; });
  handlers["prefs:save"]({ returnValue: null }, { "rx-lang": "zh" });
  assert.equal(saved, 1);
});

test("only http and https ever reach the platform opener", () => {
  assert.equal(externalTarget("https://example.test/a"), "https://example.test/a");
  assert.equal(externalTarget("http://example.test/"), "http://example.test/");
  for (const raw of [
    "file:///etc/passwd",
    "javascript:alert(1)",
    "data:text/html,<script>1</script>",
    "mailto:someone@example.test",
    "vscode://open",
    "not a url",
    "",
    null,
  ]) {
    assert.equal(externalTarget(raw), null, `let through ${String(raw)}`);
  }
});

const { StudioHost } = require("../src/hostclient.js");
const http = await import("node:http");

// The client is main's own reach into the kernel, so it has to present what the
// boundary asks for: this launch's credential on every request, and this
// listener's origin on anything that writes.
test("the host client presents the credential and names its own origin", async () => {
  const seen = [];
  const server = http.createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      seen.push({ method: req.method, path: req.url, cookie: req.headers.cookie, origin: req.headers.origin, body });
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ icon: true, live: true, closeToTray: false }));
    });
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const client = new StudioHost(origin, "the-launch-credential");

  const prefs = await client.trayPrefs();
  assert.deepEqual(prefs, { icon: true, live: true, closeToTray: false });
  await client.setTrayPrefs(true, true);
  server.close();

  assert.equal(seen[0].method, "GET");
  assert.equal(seen[0].cookie, "reasonix_token=the-launch-credential");
  // A read carries no origin, which is what a top-level navigation looks like
  // and what the gate admits; only the write has to name the listener.
  assert.equal(seen[1].method, "PUT");
  assert.equal(seen[1].cookie, "reasonix_token=the-launch-credential");
  assert.equal(seen[1].origin, origin);
  assert.deepEqual(JSON.parse(seen[1].body), { icon: true, closeToTray: true });
});

// A kernel that has already gone answers null rather than throwing: every
// caller of this is a surface that must keep working while the app shuts down.
test("an unreachable kernel is an answer, not a crash", async () => {
  const dead = new StudioHost("http://127.0.0.1:1", "x");
  assert.equal(await dead.trayPrefs(), null);
  assert.equal(await dead.trayState(), null);
});

const { reveal, revealWorkspace, revealLogs, openWorkspace } = require("../src/reveal.js");

// A kernel that answers /workspace/locate as the test says, and a shell that
// only records what it was asked to open.
async function revealRig(answer, platform = process.platform) {
  const asked = [];
  const server = http.createServer((req, res) => {
    asked.push(req.url);
    const [status, body] = answer(new URL(req.url, "http://k"));
    res.writeHead(status, { "content-type": "application/json" });
    res.end(JSON.stringify(body));
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const client = new StudioHost(`http://127.0.0.1:${server.address().port}`, "the-launch-credential");
  const opened = [];
  const shell = {
    openPath: async (p) => (opened.push(["open", p]), ""),
    showItemInFolder: (p) => opened.push(["select", p]),
  };
  return {
    asked, opened, close: () => server.close(),
    run: (base, rel) => reveal(client, shell, base, rel, platform),
    workspace: (root) => revealWorkspace(client, shell, root, platform),
    open: (root, fsApi) => openWorkspace(client, shell, root, platform, fsApi),
  };
}

// The sidebar's project entered rather than selected in its parent: the same
// kernel answer revealWorkspace asks for, through the folder-opening verb.
const folderFs = (over = {}) => ({
  stat: async () => ({ isDirectory: () => true }),
  access: async () => {
    throw new Error("ENOENT");
  },
  ...over,
});

test("openWorkspace enters the folder the kernel named", async (t) => {
  for (const [platform, target] of [[process.platform, ROOT], ["darwin", "/Users/studio/workspace"], ["linux", "/home/studio/workspace"], ["win32", "C:\\Studio\\workspace"]]) {
    await t.test(`${platform}: ${target}`, async () => {
      const rig = await revealRig(() => [200, { path: target, dir: true }], platform);
      try {
        assert.equal(await rig.open("/w", folderFs()), null);
        assert.deepEqual(rig.opened, [["open", target]]);
        assert.equal(rig.asked[0], "/host/workspaces/locate?root=%2Fw");
      } finally {
        rig.close();
      }
    });
  }
});

// openPath launches an application bundle instead of showing it, so a root
// that is one is only selected, and so is anything that is not a directory.
test("openWorkspace only selects an application bundle", async () => {
  const bundle = "/Applications/Workspace.app";
  const rig = await revealRig(() => [200, { path: bundle, dir: true }], "darwin");
  try {
    assert.equal(await rig.open("/w", folderFs({ access: async () => {} })), null);
    assert.deepEqual(rig.opened, [["select", bundle]]);
  } finally {
    rig.close();
  }
});

test("openWorkspace only selects what is not a directory or cannot be read", async () => {
  for (const fsApi of [folderFs({ stat: async () => ({ isDirectory: () => false }) }), folderFs({ stat: async () => { throw new Error("EACCES"); } })]) {
    const rig = await revealRig(() => [200, { path: ROOT, dir: true }]);
    try {
      assert.equal(await rig.open("/w", fsApi), null);
      assert.deepEqual(rig.opened, [["select", ROOT]]);
    } finally {
      rig.close();
    }
  }
});

test("openWorkspace refuses a folder the hub does not list", async () => {
  const rig = await revealRig(() => [404, { code: "host.unknown_workspace", error: "not listed" }]);
  try {
    const why = await rig.open("/nope");
    assert.equal(why.code, "host.unknown_workspace");
    assert.deepEqual(rig.opened, []);
  } finally {
    rig.close();
  }
});

const ROOT = path.resolve(os.tmpdir(), "rx-workspace");

test("file opening refuses scripts, executables and unknown types on every platform", async () => {
  const { openFile } = require("../src/reveal.js");
  const opened = [];
  const shell = { openPath: async (target) => { opened.push(target); return ""; } };
  for (const platform of ["darwin", "win32", "linux"]) {
    for (const name of ["task.command", "task.sh", "task.bat", "task.cmd", "task.ps1", "task.lnk", "task.jar", "task.desktop", "task.scpt", "task.exe", "task.EXE", "report.pdf.exe", "task", "task.unknown", "page.html", "image.svg"]) {
      const target = platform === "win32" ? `C:\\work\\${name}` : `/work/${name}`;
      const client = { request: async () => ({ status: 200, body: JSON.stringify({ path: target }) }) };
      const why = await openFile(client, shell, "/rt/r1", "report.pdf", platform, { stat: async () => ({ isFile: () => true }) });
      assert.ok(why?.error, `${platform}: ${name} must be refused`);
    }
  }
  assert.deepEqual(opened, []);
});

test("file opening accepts kernel-located documents and media on every platform", async () => {
  const { openFile } = require("../src/reveal.js");
  for (const platform of ["darwin", "win32", "linux"]) {
    for (const name of ["report.pdf", "notes.TXT", "report.docx", "table.xlsx", "slides.pptx", "data.csv", "notes.md", "image.PNG", "photo.jpg", "video.mp4", "audio.mp3"]) {
      const target = platform === "win32" ? `C:\\work\\${name}` : `/work/${name}`;
      const opened = [];
      const shell = { openPath: async (p) => { opened.push(p); return ""; } };
      const client = { request: async () => ({ status: 200, body: JSON.stringify({ path: target }) }) };
      assert.equal(await openFile(client, shell, "/rt/r1", name, platform, { stat: async () => ({ isFile: () => true }) }), null);
      assert.deepEqual(opened, [target]);
    }
  }
});

test("file opening uses only a kernel-located regular file and reports shell failures", async () => {
  const { openFile } = require("../src/reveal.js");
  const opened = [];
  const shell = { openPath: async (target) => { opened.push(target); return "no application"; } };
  const client = { request: async () => ({ status: 200, body: JSON.stringify({ path: path.join(ROOT, "report.pdf") }) }) };
  assert.deepEqual(await openFile(client, shell, "/rt/r1", "report.pdf", process.platform, { stat: async () => ({ isFile: () => true }) }), { error: "no application", code: "" });
  assert.deepEqual(opened, [path.join(ROOT, "report.pdf")]);
  opened.length = 0;
  assert.ok(await openFile(client, shell, "/rt/r1", "folder", process.platform, { stat: async () => ({ isFile: () => false }) }));
  assert.ok(await openFile(client, shell, "https://attacker.test", "report.pdf"));
  const refused = { request: async () => ({ status: 400, body: JSON.stringify({ code: "workspace.path_outside_tree", error: "outside" }) }) };
  assert.equal((await openFile(refused, shell, "/rt/r1", "../secret.pdf")).code, "workspace.path_outside_tree");
  assert.deepEqual(opened, []);
});

test("reveal opens only what the kernel located inside the pane's workspace", async () => {
  const rig = await revealRig((url) => {
    const rel = url.searchParams.get("path") ?? "";
    if (url.pathname !== "/rt/r2/workspace/locate") return [404, { code: "", error: "no route" }];
    if (rel.startsWith("..")) return [400, { code: "workspace.path_outside_tree", error: "outside" }];
    return [200, { path: path.join(ROOT, ...rel.split("/").filter(Boolean)), dir: !rel.includes(".") }];
  });
  try {
    assert.equal(await rig.run("/rt/r2", ""), null);
    assert.equal(await rig.run("/rt/r2", "out/report 1.html"), null);
    assert.equal(await rig.run("/rt/r2", "out/t620"), null);
    const outside = await rig.run("/rt/r2", "../etc");
    assert.equal(outside.code, "workspace.path_outside_tree");
    // Every entry, the root included, is selected in its folder and never opened.
    assert.deepEqual(rig.opened, [
      ["select", ROOT],
      ["select", path.join(ROOT, "out", "report 1.html")],
      ["select", path.join(ROOT, "out", "t620")],
    ]);
    assert.equal(rig.asked[1], "/rt/r2/workspace/locate?path=out%2Freport%201.html");
  } finally {
    rig.close();
  }
});

test("the page cannot steer reveal to a location the kernel did not name", async () => {
  const rig = await revealRig((url) =>
    url.searchParams.get("path") === "rel"
      ? [200, { path: "relative/answer", dir: false }]
      : [200, { path: path.join(ROOT, "a.exe"), dir: false }],
  );
  try {
    for (const base of ["", "/etc", "http://evil.test", "/rt/r1/../../x", "/rt/r1/workspace/file?path=", "rt/r1"]) {
      const why = await rig.run(base, "");
      assert.ok(why && typeof why.error === "string", `accepted base ${base}`);
    }
    assert.deepEqual(rig.asked, [], "a refused base still reached the kernel");
    assert.ok(await rig.run("/rt/r1", "rel"), "a relative answer was opened");
    // A root answered as a file is still only selected: openPath would run it.
    assert.equal(await rig.run("/rt/r1", ""), null);
    assert.deepEqual(rig.opened, [["select", path.join(ROOT, "a.exe")]]);
  } finally {
    rig.close();
  }
});

test("a listed project is shown through the hub and only on its answer", async () => {
  const rig = await revealRig((url) =>
    url.pathname === "/host/workspaces/locate" && url.searchParams.get("root") === "/work/a b"
      ? [200, { path: path.join(ROOT, "a b"), dir: true }]
      : [404, { code: "workspace.not_listed", error: "not listed" }],
  );
  try {
    assert.equal(await rig.workspace("/work/a b"), null);
    assert.equal((await rig.workspace("/etc")).code, "workspace.not_listed");
    assert.deepEqual(rig.opened, [["select", path.join(ROOT, "a b")]]);
    assert.equal(rig.asked[0], "/host/workspaces/locate?root=%2Fwork%2Fa%20b");
  } finally {
    rig.close();
  }
});

// A root that is an .app bundle, or a link to one, is a directory to stat and
// an application to `open`: handing it to openPath would launch it.
test("the workspace root is selected, never opened", async () => {
  const rig = await revealRig(() => [200, { path: path.join(ROOT, "Probe.app"), dir: true }]);
  try {
    assert.equal(await rig.run("/rt/r1", ""), null);
    assert.deepEqual(rig.opened, [["select", path.join(ROOT, "Probe.app")]]);
  } finally {
    rig.close();
  }
});

// A remote kernel's answer can name a share or a device on this machine; on
// Windows the shell would reach out to it just to select it.
test("on Windows a share or device path from the kernel is refused", async () => {
  const answers = ["\\\\attacker\\share\\x", "//attacker/share/x", "\\\\?\\C:\\x", "\\\\.\\pipe\\x", "C:relative", "\\??\\UNC\\attacker\\share\\x", "\\??\\C:\\x", "\\Windows\\x"];
  let i = 0;
  const rig = await revealRig(() => [200, { path: answers[i++], dir: false }], "win32");
  try {
    for (const answer of answers) {
      const why = await rig.run("/rt/r1", "x");
      assert.ok(why && typeof why.error === "string", `opened ${answer}`);
    }
    assert.deepEqual(rig.opened, []);
    i = 0;
    answers[0] = "C:\\work\\out\\x";
    assert.equal(await rig.run("/rt/r1", "x"), null);
    assert.deepEqual(rig.opened, [["select", "C:\\work\\out\\x"]]);
  } finally {
    rig.close();
  }
});

const { hostBinary, computerDriver, pageDir } = require("../src/layout.js");

// Packaged, both live in resources/ beside app.asar. Reading them from inside
// it is the failure this pins: a child process cannot be spawned out of an
// archive, and the kernel serves the SPA off the filesystem.
test("the kernel and the page are found in both layouts", () => {
  const dev = { packaged: false, resourcesPath: "/res", dirname: path.join("/repo", "electron", "src"), platform: "linux" };
  assert.equal(hostBinary(dev), path.join("/repo", "electron", "bin", "reasonix-studio-host"));
  assert.equal(pageDir(dev), path.join("/repo", "frontend-next", "dist"));

  const packed = { ...dev, packaged: true };
  assert.equal(hostBinary(packed), path.join("/res", "bin", "reasonix-studio-host"));
  assert.equal(pageDir(packed), path.join("/res", "frontend-next", "dist"));
  for (const p of [hostBinary(packed), pageDir(packed)]) {
    assert.doesNotMatch(p, /app\.asar/, "resolved into the archive");
  }
});

test("the CUA Driver is found beside the kernel on all supported platforms", () => {
  const mac = { packaged: true, resourcesPath: "/res", dirname: "/d/src", platform: "darwin" };
  assert.equal(computerDriver(mac), path.join("/res", "bin", "cua-driver"));
  assert.equal(computerDriver({ ...mac, packaged: false, dirname: path.join("/repo", "electron", "src") }), path.join("/repo", "electron", "bin", "cua-driver"));
  assert.equal(computerDriver({ ...mac, platform: "win32" }), path.join("/res", "bin", "cua-driver.exe"));
  assert.equal(computerDriver({ ...mac, platform: "linux" }), path.join("/res", "bin", "cua-driver"));
  assert.equal(computerDriver({ ...mac, platform: "linux", env: { REASONIX_CUA_DRIVER: "/custom/cua-driver" } }), "/custom/cua-driver");
});

test("Windows gets the suffix spawn needs, and an override wins over both", () => {
  const win = { packaged: true, resourcesPath: "/res", dirname: "/d/src", platform: "win32" };
  assert.equal(hostBinary(win), path.join("/res", "bin", "reasonix-studio-host.exe"));
  assert.equal(hostBinary({ ...win, env: { REASONIX_STUDIO_HOST: "/custom/kernel" } }), "/custom/kernel");
  assert.equal(pageDir({ ...win, env: { REASONIX_STUDIO_PAGE: "/custom/page" } }), "/custom/page");
});

const { appIcon, iconFile } = require("../src/appicon.js");
const fsSync = require("node:fs");

// The window icon is the one the taskbar draws, and an unnamed one is Electron's
// own — which is what shipped: BrowserWindow carried no icon at all.
test("every platform that draws the window icon is given one that exists", () => {
  for (const platform of ["win32", "linux", "freebsd"]) {
    const opts = appIcon(platform);
    assert.ok(opts.icon, `${platform} was given no icon`);
    assert.ok(fsSync.existsSync(opts.icon), `${platform} names a file that is not there: ${opts.icon}`);
  }
  // macOS reads the bundle instead, so the key is absent rather than wrong.
  assert.deepEqual(appIcon("darwin"), {});
  assert.equal(iconFile("darwin"), null);
});

// Windows draws the taskbar icon from the sizes inside an .ico; a PNG there is
// scaled from one bitmap and shows it.
test("Windows is given the format it reads the small sizes from", () => {
  assert.equal(iconFile("win32"), "icon.ico");
  assert.equal(iconFile("linux"), "icon.png");
});

const { profileFor } = require("../src/instance.js");

// Two homes are two Studios, and the profile is what carries that into the
// platform's own lock. Sharing one would make the second launch look like a
// duplicate of the first.
test("each instance gets a profile of its own", () => {
  // Built rather than written out: on Windows path.join answers in backslashes,
  // and a POSIX literal made the prefix check fail there for the separator.
  const base = path.join(os.tmpdir(), "profiles");
  assert.notEqual(profileFor(base, "io.reasonix.studio.aaaa"), profileFor(base, "io.reasonix.studio.bbbb"));
  assert.ok(profileFor(base, "io.reasonix.studio.aaaa").startsWith(base));
  // An identity nobody could work out leaves the default alone rather than
  // inventing a profile that no second launch would agree on.
  assert.equal(profileFor(base, ""), base);
});

// A program that cannot be started emits "error" and never "exit". Waiting for
// the handshake instead spent the full timeout and then blamed the kernel for
// saying nothing, which is where the launch failure on Windows hid: go build
// had written a binary Node could not spawn.
test("a kernel that cannot be started says so, rather than going quiet", async () => {
  const { start } = require("../src/host.js");
  const { ready } = start(path.join(os.tmpdir(), "reasonix-no-such-kernel-b3f1"), []);
  await assert.rejects(ready, (err) => {
    assert.match(err.message, /could not be started/);
    assert.doesNotMatch(err.message, /sent no handshake/);
    return true;
  });
});

test("a launch failure carries a typed cause, and a slow kernel is waited for, not restarted", async () => {
  const { start } = require("../src/host.js");
  const node = process.execPath;
  let slow = 0;
  const silent = start(node, ["-e", "setTimeout(()=>{},5000)"], { timeoutMs: 400, onSlow: () => slow++ });
  await assert.rejects(silent.ready, (err) => {
    assert.equal(err.code, "timeout");
    assert.match(err.message, /still running/);
    return true;
  });
  silent.child.kill();
  assert.equal(slow, 1, "the slow state was not reported once");

  const quick = start(node, ["-e", "process.exit(7)"]);
  await assert.rejects(quick.ready, (err) => err.code === "exited" && err.exitCode === 7 && /exited with 7/.test(err.message));

  const missing = start(path.join(os.tmpdir(), "reasonix-no-such-kernel-c9"), []);
  await assert.rejects(missing.ready, (err) => err.code === "spawn");

  const handshake = JSON.stringify({ version: 1, origin: "http://127.0.0.1:1", token: "x".repeat(32) });
  const fine = start(node, ["-e", `console.log(${JSON.stringify(handshake)});setTimeout(()=>{},300)`], { timeoutMs: 400 });
  assert.equal((await fine.ready).origin, "http://127.0.0.1:1");
});

test("a retry needs an early stop, is capped at one, and never follows a timeout", () => {
  const { shouldRetry } = require("../src/host.js");
  const exited = { code: "exited" };
  assert.equal(shouldRetry(exited, 1, 500), true);
  assert.equal(shouldRetry({ code: "spawn" }, 1, 500), true);
  assert.equal(shouldRetry(exited, 1, 10001), false, "an exit after the early window was retried");
  assert.equal(shouldRetry(exited, 2, 500), false, "a second retry was allowed");
  assert.equal(shouldRetry({ code: "timeout" }, 1, 500), false);
  assert.equal(shouldRetry(new Error("x"), 1, 500), false);
});

test("a handshake that does not parse is a typed failure", async () => {
  const { start } = require("../src/host.js");
  const bad = start(process.execPath, ["-e", "console.log('hello')"]);
  await assert.rejects(bad.ready, (err) => err.code === "malformed");
});

test("a kernel that never answers is started once and not retried", { skip: process.platform === "win32" }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-silent-"));
  const host = path.join(dir, "host.sh");
  fs.writeFileSync(host, "#!/bin/sh\nexec sleep 5\n", { mode: 0o755 });
  process.env.REASONIX_STUDIO_HANDSHAKE_TIMEOUT_MS = "400";
  const shell = loadShell({ lock: true, host });
  try {
    await shell.quitted;
    const log = shell.shellLog();
    assert.equal((log.match(/host: starting /g) || []).length, 1);
    assert.doesNotMatch(log, /retrying once/);
    assert.match(shell.calls[0][2], /still running/);
  } finally {
    delete process.env.REASONIX_STUDIO_HANDSHAKE_TIMEOUT_MS;
    shell.cleanup();
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("each failure cause gets its own explanation in both languages", () => {
  const { startupFailure } = require("../src/shelllog.js");
  const seen = new Set();
  for (const code of ["timeout", "exited", "spawn"]) {
    for (const locale of ["en", "zh-CN"]) {
      const { detail } = startupFailure(locale, "r", "/logs", code);
      assert.ok(!seen.has(detail), `${code}/${locale} repeated another cause's text`);
      seen.add(detail);
    }
  }
  assert.equal(startupFailure("en", "r", "/logs", "overflow").detail, "r\n\nLogs are in:\n/logs");
});

// A fake child: only the stdout half readActs touches, and a way to push bytes
// through it in whatever chunks the test wants -- which is the point, since the
// bug this guards is a line split across two of them.
function fakeChild() {
  const listeners = [];
  return {
    stdout: { setEncoding() {}, on: (_e, fn) => listeners.push(fn) },
    push: (chunk) => listeners.forEach((fn) => fn(chunk)),
  };
}

test("an act arriving in the handshake's own chunk is not lost", () => {
  const child = fakeChild();
  const seen = [];
  // What the pipe handed the handshake reader after it took its first line.
  readActs(child, '{"act":"quit"}\n', (act) => seen.push(act));
  assert.deepEqual(seen, ["quit"]);
});

test("acts are read a line at a time, however the pipe flushed them", () => {
  const child = fakeChild();
  const seen = [];
  readActs(child, "", (act) => seen.push(act));

  child.push('{"act":"relaunch"}\n{"act":');
  assert.deepEqual(seen, ["relaunch"], "a half line is not an act");
  child.push('"quit"}\n');
  assert.deepEqual(seen, ["relaunch", "quit"]);
});

test("a line this process cannot read is one it does not act on", () => {
  const child = fakeChild();
  const seen = [];
  readActs(child, "", (act) => seen.push(act));

  // Nothing here is an act, and none of it may stop the next line from being
  // one: a kernel that logged to the wrong stream must not end the handover.
  child.push("not json at all\n");
  child.push('{"version":1}\n');
  child.push('{"act":42}\n');
  child.push("\n");
  assert.deepEqual(seen, []);
  child.push('{"act":"quit"}\n');
  assert.deepEqual(seen, ["quit"]);
});

test("a child writing without newlines cannot grow this process", () => {
  const child = fakeChild();
  const seen = [];
  readActs(child, "", (act) => seen.push(act));

  child.push("x".repeat(8192));
  // Dropped rather than held, and the next real line still reads: the buffer
  // is a line assembler, not a log.
  child.push('{"act":"quit"}\n');
  assert.deepEqual(seen, ["quit"]);
});

// The legacy cleanup removes an application from somebody's disk, so what it
// will not touch is asserted before what it will.
test("the running application is never offered for removal", async (t) => {
  if (process.platform !== "darwin") return t.skip("the cleanup is macOS only");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "legacy-"));
  const own = path.join(home, "Reasonix Studio.app");
  const exe = path.join(own, "Contents", "MacOS", "Reasonix Studio");
  const legacy = path.join(home, "ReasonixStudio.app");
  fs.mkdirSync(legacy, { recursive: true });

  assert.equal(ownBundle(exe), own);

  // own is handed to the filter on purpose: a search that never returns it
  // would let this pass with the guard removed, which is how it read first.
  const asked = [];
  const trashed = [];
  const removed = await offerCleanup({
    packaged: true,
    execPath: exe,
    userData: home,
    find: () => [own, legacy],
    ask: async (b) => {
      asked.push(b);
      return true;
    },
    trash: async (b) => void trashed.push(b),
  });
  assert.equal(asked.includes(own), false, "asked about the bundle it is running from");
  assert.equal(trashed.includes(own), false, "trashed the bundle it is running from");
  assert.deepEqual(removed, [legacy], "the leftover install was not the one removed");
});

test("an unpackaged build removes nothing", async () => {
  let asked = 0;
  const removed = await offerCleanup({
    packaged: false,
    execPath: "/Applications/Reasonix Studio.app/Contents/MacOS/Reasonix Studio",
    userData: fs.mkdtempSync(path.join(os.tmpdir(), "legacy-")),
    ask: async () => {
      asked += 1;
      return true;
    },
    trash: async () => {},
  });
  assert.deepEqual(removed, []);
  assert.equal(asked, 0, "a development build asked to remove an installed one");
});

test("a refusal is remembered, so the next launch does not ask again", async (t) => {
  if (process.platform !== "darwin") return t.skip("the cleanup is macOS only");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "legacy-"));
  const exe = path.join(home, "Reasonix Studio.app", "Contents", "MacOS", "Reasonix Studio");
  const legacy = path.join(home, "ReasonixStudio.app");
  fs.mkdirSync(legacy, { recursive: true });

  let asked = 0;
  const decline = {
    packaged: true,
    execPath: exe,
    userData: home,
    find: () => [legacy],
    ask: async () => {
      asked += 1;
      return false;
    },
    trash: async () => {
      throw new Error("trashed a bundle the answer was no for");
    },
  };
  await offerCleanup(decline);
  assert.equal(asked, 1, "the first launch did not ask");
  await offerCleanup(decline);
  assert.equal(asked, 1, "a refusal was asked again on the next launch");
});

test("consent trashes exactly what was answered for", async (t) => {
  if (process.platform !== "darwin") return t.skip("the cleanup is macOS only");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "legacy-"));
  const exe = path.join(home, "Reasonix Studio.app", "Contents", "MacOS", "Reasonix Studio");
  const legacy = path.join(home, "ReasonixStudio.app");
  fs.mkdirSync(legacy, { recursive: true });

  const trashed = [];
  const removed = await offerCleanup({
    packaged: true,
    execPath: exe,
    userData: home,
    find: () => [legacy],
    ask: async () => true,
    trash: async (b) => void trashed.push(b),
  });
  assert.deepEqual(trashed, [legacy]);
  assert.deepEqual(removed, [legacy]);
});

test("package grants are only stripped from a packaged Windows install", () => {
  const never = () => assert.fail("the kernel was run");
  const exe = "C:\\Studio\\Reasonix Studio.exe";
  assert.equal(stripPackageGrants("host", { platform: "darwin", packaged: true, execPath: exe }, never), null);
  assert.equal(stripPackageGrants("host", { platform: "linux", packaged: true, execPath: exe }, never), null);
  assert.equal(stripPackageGrants("host", { platform: "win32", packaged: false, execPath: exe }, never), null);

  let called;
  const run = (binary, args) => {
    called = { binary, args };
    return JSON.stringify({ stripped: ["C:\\Studio"], refused: [{ path: "C:\\Studio\\ffmpeg.dll" }] }) + "\n";
  };
  const report = stripPackageGrants("host", { platform: "win32", packaged: true, execPath: exe }, run);
  // The kernel is told which application, never which directory: it derives the
  // tree from the executable it is shown.
  assert.deepEqual(called, { binary: "host", args: ["-strip-package-grants", "-studio-app", exe] });
  assert.deepEqual(report, { stripped: ["C:\\Studio"], refused: ["C:\\Studio\\ffmpeg.dll"] });
});

test("a grant report that does not parse is no report at all", () => {
  const quiet = console.error;
  console.error = () => {};
  try {
    const opts = { platform: "win32", packaged: true, execPath: "C:\\S.exe" };
    assert.equal(stripPackageGrants("host", opts, () => { throw new Error("exit 2"); }), null);
    assert.equal(stripPackageGrants("host", opts, () => "not json"), null);
  } finally {
    console.error = quiet;
  }
  assert.throws(() => readReport(JSON.stringify({ stripped: null, refused: [] })));
  assert.throws(() => readReport(JSON.stringify({ stripped: [] })));
});

test("an unpainted window is attributed only to grants the kernel could not remove", () => {
  assert.equal(unpaintedWindowCause(null, "en-US"), null);
  assert.equal(unpaintedWindowCause({ stripped: ["C:\\Studio"], refused: [] }, "en-US"), null);

  const refused = Array.from({ length: 7 }, (_, i) => `C:\\Studio\\${i}.dll`);
  const en = unpaintedWindowCause({ stripped: [], refused }, "en-US");
  assert.ok(en.detail.includes("C:\\Studio\\4.dll") && !en.detail.includes("C:\\Studio\\5.dll"));
  assert.ok(en.detail.includes("2 more"));
  const zh = unpaintedWindowCause({ stripped: [], refused: refused.slice(0, 1) }, "zh-CN");
  assert.ok(zh.title.includes("无法打开窗口") && zh.detail.includes("C:\\Studio\\0.dll"));
});

const { BrowserProtocol, PAGE_SESSION } = require("../src/browserprotocol.js");
const { guestNavigationAllowed, typedAddress, typed } = require("../src/browserguard.js");
const { localPath } = require("../src/localpath.js");
const { sseData } = require("../src/browserrelay.js");

function fakeBrowser() {
  const posted = [];
  const views = [];
  const protocol = new BrowserProtocol({
    post: (frame) => posted.push(frame),
    createView: (spec) => {
      const view = {
        targetId: `view-${views.length + 1}`,
        spec,
        sent: [],
        closed: false,
        send: async (method, params) => {
          view.sent.push([method, params]);
          if (method === "Page.fail") throw new Error("nope");
          return { echoed: method };
        },
        close: () => {
          view.closed = true;
          spec.onClosed();
        },
        mainFrame: () => "F1",
      };
      views.push(view);
      return view;
    },
  });
  const say = (conn, message) => protocol.receive({ conn, message });
  const replies = (conn) => posted.filter((f) => f.conn === conn).map((f) => f.message);
  return { protocol, posted, views, say, replies };
}

test("the window answers for targets and hands a page's commands to that page", async () => {
  const b = fakeBrowser();
  b.protocol.receive({ conn: "1", open: "reasonix-browser-abc" });
  b.say("1", { id: 1, method: "Browser.getVersion" });
  b.say("1", { id: 2, method: "Target.createTarget", params: { url: "about:blank" } });
  assert.equal(b.views[0].spec.partition, "reasonix-browser-abc");
  b.say("1", { id: 3, method: "Target.attachToTarget", params: { targetId: "view-1", flatten: true } });
  b.say("1", { id: 4, method: "Page.navigate", params: { url: "https://example.com/" }, sessionId: PAGE_SESSION + "view-1" });
  b.say("1", { id: 5, method: "Page.fail", sessionId: PAGE_SESSION + "view-1" });
  b.say("1", { id: 6, method: "Tracing.start" });
  await new Promise((r) => setImmediate(r));
  const byId = Object.fromEntries(b.replies("1").filter((m) => m.id).map((m) => [m.id, m]));
  assert.equal(byId[2].result.targetId, "view-1");
  assert.equal(byId[3].result.sessionId, PAGE_SESSION + "view-1");
  assert.deepEqual(byId[4].result, { echoed: "Page.navigate" });
  assert.equal(byId[5].error.message, "nope");
  assert.equal(byId[6].error.code, -32601);

  b.views[0].spec.onEvent("Page.loadEventFired", {});
  assert.deepEqual(b.replies("1").at(-1), { method: "Page.loadEventFired", params: {}, sessionId: PAGE_SESSION + "view-1" });
});

test("a popup becomes a target the kernel is told about, and closing ends every page", () => {
  const b = fakeBrowser();
  b.protocol.receive({ conn: "1", open: "p" });
  b.say("1", { id: 1, method: "Target.createTarget", params: { url: "about:blank" } });
  b.views[0].spec.onPopup("https://example.com/next");
  const created = b.replies("1").find((m) => m.method === "Target.targetCreated");
  assert.deepEqual(created.params.targetInfo, { targetId: "view-2", type: "page", openerId: "view-1", url: "https://example.com/next" });

  b.views[0].spec.onDownload({ url: "https://example.com/a.csv", suggestedFilename: "a.csv" });
  assert.equal(b.replies("1").at(-1).method, "Browser.downloadWillBegin");

  b.protocol.receive({ conn: "1", close: true });
  assert.ok(b.views.every((v) => v.closed), "a closed connection left pages open");
  const destroyed = b.replies("1").filter((m) => m.method === "Target.targetDestroyed").map((m) => m.params.targetId);
  assert.deepEqual(destroyed.sort(), ["view-1", "view-2"]);
  b.say("1", { id: 9, method: "Target.createTarget" });
  assert.equal(b.views.length, 2, "a closed connection still opened a page");
});

test("frames for connections nobody opened, or after the stream dropped, are ignored", () => {
  const b = fakeBrowser();
  b.say("ghost", { id: 1, method: "Target.createTarget" });
  assert.equal(b.views.length, 0);
  b.protocol.receive({ conn: "1", open: "p" });
  b.say("1", { id: 1, method: "Target.createTarget" });
  b.protocol.drop();
  assert.ok(b.views[0].closed);
  b.say("1", { id: 2, method: "Target.createTarget" });
  assert.equal(b.views.length, 1);
});

test("a page may go to the web and never to the kernel's own origin", () => {
  const kernel = "http://127.0.0.1:4455";
  assert.equal(guestNavigationAllowed("https://example.com/a", kernel), true);
  assert.equal(guestNavigationAllowed("http://127.0.0.1:5173/", kernel), true);
  assert.equal(guestNavigationAllowed("about:blank", kernel), true);
  for (const refused of ["http://127.0.0.1:4455/_studio/", "http://127.0.0.1:4455/rt/1/approve", "file:///etc/hosts", "javascript:alert(1)", "chrome://settings", "devtools://x", "not a url"]) {
    assert.equal(guestNavigationAllowed(refused, kernel), false, refused);
  }
  assert.deepEqual(typedAddress("example.com/docs"), { url: "https://example.com/docs", fallback: "http://example.com/docs" });
  assert.deepEqual(typedAddress("http://localhost:3000"), { url: "http://localhost:3000", fallback: "" });
  assert.deepEqual(typedAddress("  "), { url: "", fallback: "" });
});

test("a typed host and port is an address, never a scheme", () => {
  for (const raw of ["intranet:8080", "oa.corp.example:8080/login", "localhost:3000"]) {
    const { url } = typedAddress(raw);
    assert.ok(guestNavigationAllowed(url, "http://127.0.0.1:1"), `refused ${raw} as ${url}`);
  }
});

test("a host that cannot be public is read as http, anything else tries https first", () => {
  for (const raw of ["10.1.2.3:18081", "192.168.0.5", "172.20.0.1/app", "127.0.0.1:5173", "intranet", "localhost:3000", "[::1]:8080", "[fd00::1]"]) {
    assert.deepEqual(typedAddress(raw), { url: "http://" + raw, fallback: "" }, raw);
  }
  assert.deepEqual(typedAddress("oa.example.com"), { url: "https://oa.example.com", fallback: "http://oa.example.com" });
  assert.deepEqual(typedAddress("8.8.8.8"), { url: "https://8.8.8.8", fallback: "http://8.8.8.8" });
  assert.deepEqual(typedAddress("172.32.0.1"), { url: "https://172.32.0.1", fallback: "http://172.32.0.1" });
});

test("an address that names its scheme is loaded as written", () => {
  assert.deepEqual(typedAddress("https://10.1.2.3"), { url: "https://10.1.2.3", fallback: "" });
  assert.deepEqual(typedAddress("about:blank"), { url: "about:blank", fallback: "" });
  assert.equal(guestNavigationAllowed(typedAddress("javascript://x%0Aalert(1)").url, "http://127.0.0.1:1"), false);
  assert.equal(guestNavigationAllowed(typedAddress("file:///C:/x").url, "http://127.0.0.1:1"), false);
});

test("a typed path on disk is the file it names, read by the table the kernel is held to", () => {
  const table = JSON.parse(fs.readFileSync(new URL("../../../internal/platform/browser/testdata/local_paths.json", import.meta.url), "utf8"));
  for (const { in: raw, out, kind } of table) {
    const got = localPath(raw);
    assert.deepEqual(got ? { url: got.url, kind: got.kind } : { url: "", kind: "" }, { url: out, kind }, JSON.stringify(raw));
  }
});

test("a seeded corpus of hostile addresses reaches the same digest as the kernel's parser, and never accepts a share", () => {
  const tokens = [
    "file:", "file:/", "/", "//", "\\", ":", "%", "5C", "2F", "09", "0A", ".", "..", "#", "?",
    "D", "c", "$", " ", "h", "x", "\t", "\n", "\r", "﻿", "\u0085", "\u0001", "\u007f",
    "localhost", "C:", "UNC", "%5c", "%2e", "?\\", " ",
  ];
  let a = 12321;
  const next = () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return (t ^ (t >>> 14)) >>> 0;
  };
  let h = 0x811c9dc5;
  let accepted = 0;
  for (let i = 0; i < 30000; i++) {
    let input = next() % 2 === 0 ? "file:" : "";
    for (let n = 1 + (next() % 12); n > 0; n--) input += tokens[next() % tokens.length];
    const got = localPath(input);
    const out = got ? got.url : "";
    const kind = got ? got.kind : "";
    for (const byte of Buffer.from(`${input}\0${out}\0${kind}\n`, "utf8")) h = Math.imul(h ^ byte, 0x01000193) >>> 0;
    if (kind !== "local") continue;
    accepted++;
    const u = new URL(out);
    assert.ok(u.protocol === "file:" && u.hostname === "" && !u.pathname.startsWith("//"), `${JSON.stringify(input)} accepted as ${out}`);
    assert.ok(!/[\u0000-\u001f\u007f]/.test(out), `${JSON.stringify(input)} kept a control character`);
  }
  assert.ok(accepted > 1000, `only ${accepted} accepted`);
  assert.equal(h.toString(16).padStart(8, "0"), "67b363c1");
});

test("a path typed in the address bar loads as a file, not as a host named by its drive letter", () => {
  const kernel = "http://127.0.0.1:4455";
  const refusal = (raw) => typed(raw, kernel).refusal;
  const reporter = typed("D:/DevCode/MyProjects/aglo/aglo.html", kernel);
  assert.deepEqual(reporter, { url: "file:///D:/DevCode/MyProjects/aglo/aglo.html", fallback: "", refusal: "" });
  for (const ok of ["/Users/me/页面.html", "file:///D:/x.html", "file:/tmp/x.html", "file://localhost/tmp/x.html", "localhost:3000/x"]) {
    assert.equal(refusal(ok), "", ok);
  }
  for (const share of ["\\\\nas\\share\\x.html", "file://nas/share/x.html", "file:////host/x", "file://localhost//host/x", "file://///host/share", "file:///\\\\host\\x", "file:///%5C%5Chost/x", "file:///%2F%2Fhost/x", "\\\\?\\UNC\\host\\x"]) {
    assert.deepEqual(typed(share, kernel), { url: "", fallback: "", refusal: "network_file" }, share);
  }
  assert.equal(refusal("javascript://x%0Aalert(1)"), "scheme");
  assert.equal(refusal("http://127.0.0.1:4455/_studio/"), "scheme");
  assert.equal(refusal(""), "scheme");
  assert.deepEqual(typedAddress("localhost:3000/x"), { url: "http://localhost:3000/x", fallback: "" });
  assert.deepEqual(typedAddress("a.b/c"), { url: "https://a.b/c", fallback: "http://a.b/c" });
});

test("the relay reads whole SSE data frames and keeps what is unfinished", () => {
  const first = sseData(': connected\n\ndata: {"conn":"1"}\n\ndata: {"co');
  assert.deepEqual(first.data, ['{"conn":"1"}']);
  const second = sseData(first.rest + 'nn":"2"}\n\n: ping\n\n');
  assert.deepEqual(second.data, ['{"conn":"2"}']);
  assert.equal(second.rest, "");
});

// The kernel listens on a new port each launch, and localStorage is keyed by
// origin; these are what carries the page's choices from one launch to the next.
test("the page's preferences survive a launch, and nothing else rides along", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-prefs-"));
  const file = path.join(dir, "nested", "window-prefs.json");
  assert.deepEqual(loadPrefs(file), {}, "a first launch starts empty");
  savePrefs(file, { "rx-theme": "dark", "rx-weight": "heavy", other: "x", "rx-bad": 1 });
  assert.deepEqual(loadPrefs(file), { "rx-theme": "dark", "rx-weight": "heavy" });
  fs.writeFileSync(file, "{not json");
  assert.deepEqual(loadPrefs(file), {}, "a damaged file reads as nothing kept, not as a crash");
  assert.deepEqual(pick(["rx-theme"]), {});
  fs.rmSync(dir, { recursive: true, force: true });
});

test("only the Studio window reads or writes the preferences", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-prefs-"));
  const file = path.join(dir, "window-prefs.json");
  savePrefs(file, { "rx-theme": "dark" });
  const handlers = {};
  registerPrefs({ on: (name, fn) => (handlers[name] = fn) }, () => file, (event) => event.sender === "studio");
  const ask = (name, sender, arg) => {
    const event = { sender };
    handlers[name](event, arg);
    return event.returnValue;
  };
  assert.deepEqual(ask("prefs:load", "studio"), { "rx-theme": "dark" });
  assert.deepEqual(ask("prefs:load", "agent-page"), {}, "a browser page is told nothing");
  ask("prefs:save", "agent-page", { "rx-theme": "light" });
  assert.deepEqual(loadPrefs(file), { "rx-theme": "dark" }, "a browser page cannot write");
  ask("prefs:save", "studio", { "rx-theme": "light" });
  assert.deepEqual(loadPrefs(file), { "rx-theme": "light" });
  fs.rmSync(dir, { recursive: true, force: true });
});

test("only the Studio main frame reports a validated resolved theme", () => {
  const handlers = {};
  const studio = { mainFrame: {} };
  const browser = { mainFrame: {} };
  const paints = [];
  registerPrefs({ on: (name, fn) => (handlers[name] = fn) }, () => "", event => event.sender === studio,
    () => {}, paint => paints.push(paint));
  const send = (sender, senderFrame, paint) => handlers["prefs:theme"]({ sender, senderFrame }, paint);
  send(browser, browser.mainFrame, { mode: "scheduled", scheme: "dark" });
  send(studio, {}, { mode: "scheduled", scheme: "dark" });
  send(studio, null, { mode: "scheduled", scheme: "dark" });
  for (const paint of [null, [], "dark", { mode: "scheduled" }, { mode: "scheduled", scheme: "auto" },
    { mode: "other", scheme: "dark" }, { mode: "light", scheme: "dark" }, { mode: "dark", scheme: "light" }]) {
    send(studio, studio.mainFrame, paint);
  }
  assert.deepEqual(paints, []);
  for (const mode of ["scheduled", "auto", "dark"]) send(studio, studio.mainFrame, { mode, scheme: "dark", extra: "discarded" });
  send(studio, studio.mainFrame, { mode: "light", scheme: "light" });
  assert.deepEqual(paints, [
    { mode: "scheduled", scheme: "dark" }, { mode: "auto", scheme: "dark" },
    { mode: "dark", scheme: "dark" }, { mode: "light", scheme: "light" },
  ]);
});

function elf64(interpreter, machine = 62) {
  const phoff = 64, phentsize = 56, phnum = interpreter ? 2 : 1;
  const dataOffset = phoff + phnum * phentsize;
  const path = Buffer.from(interpreter ? `${interpreter}\0` : "", "latin1");
  const bytes = Buffer.alloc(dataOffset + path.length);
  bytes.writeUInt32BE(0x7f454c46, 0);
  bytes[4] = 2;
  bytes[5] = 1;
  bytes[6] = 1;
  bytes.writeUInt16LE(2, 16);
  bytes.writeUInt16LE(machine, 18);
  bytes.writeBigUInt64LE(BigInt(phoff), 32);
  bytes.writeUInt16LE(64, 52);
  bytes.writeUInt16LE(phentsize, 54);
  bytes.writeUInt16LE(phnum, 56);
  bytes.writeUInt32LE(1, phoff);
  if (interpreter) {
    const header = phoff + phentsize;
    bytes.writeUInt32LE(3, header);
    bytes.writeBigUInt64LE(BigInt(dataOffset), header + 8);
    bytes.writeBigUInt64LE(BigInt(path.length), header + 32);
    path.copy(bytes, dataOffset);
  }
  return bytes;
}

test("a Linux Go binary that needs the build host's dynamic loader is refused", () => {
  const { elfInterpreter, dynamicallyLinked } = require("../packaging/elf.js");
  assert.equal(elfInterpreter(elf64("/lib64/ld-linux-x86-64.so.2")), "/lib64/ld-linux-x86-64.so.2");
  assert.equal(elfInterpreter(elf64(null)), null);
  assert.throws(() => elfInterpreter(Buffer.from("#!/bin/sh\n")), /not an ELF/);
  assert.throws(() => elfInterpreter(elf64("/lib/ld.so").subarray(0, 100)), /truncated/);

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-elf-"));
  try {
    const host = path.join(dir, "reasonix-studio-host");
    const helper = path.join(dir, "reasonix-studio-update-helper");
    fs.writeFileSync(host, elf64("/lib64/ld-linux-x86-64.so.2"));
    fs.writeFileSync(helper, elf64(null));
    assert.deepEqual(dynamicallyLinked([host, helper]), [{ file: host, interpreter: "/lib64/ld-linux-x86-64.so.2" }]);
    assert.deepEqual(dynamicallyLinked([helper]), []);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("a Linux package's Go binaries must be built for the architecture the package is named for", () => {
  const { elfArchitecture, assertArchitecture } = require("../packaging/elf.js");
  assert.equal(elfArchitecture(elf64(null, 62)), "amd64");
  assert.equal(elfArchitecture(elf64(null, 183)), "arm64");
  assert.throws(() => elfArchitecture(elf64(null, 40)), /unsupported ELF machine/);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-elf-"));
  try {
    const host = path.join(dir, "reasonix-studio-host");
    const helper = path.join(dir, "reasonix-studio-update-helper");
    fs.writeFileSync(host, elf64(null, 62));
    fs.writeFileSync(helper, elf64(null, 183));
    assert.throws(() => assertArchitecture([host, helper], "arm64"), /is amd64, expected arm64/);
    assert.throws(() => assertArchitecture([host, helper], "amd64"), /is arm64, expected amd64/);
    assert.doesNotThrow(() => assertArchitecture([host], "amd64"));
    assert.throws(() => assertArchitecture([host], "riscv64"), /unsupported release architecture/);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("a Wayland session with XWayland relaunches the shell on X11 before anything else starts", () => {
  const { relaunchForOzonePlatform } = require("../src/ozone.js");
  const X11 = "--ozone-platform=x11";
  const cases = [
    ["wayland with xwayland", { XDG_SESSION_TYPE: "wayland", WAYLAND_DISPLAY: "wayland-0", DISPLAY: ":0" }, ["--flag"], [X11, "--flag"]],
    ["wayland display alone", { WAYLAND_DISPLAY: "wayland-0", DISPLAY: ":0" }, [], [X11]],
    ["pure wayland", { XDG_SESSION_TYPE: "wayland", WAYLAND_DISPLAY: "wayland-0" }, ["--flag"], null],
    ["override wayland", { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0", REASONIX_OZONE_PLATFORM: "wayland" }, [], null],
    ["override x11", { XDG_SESSION_TYPE: "x11", DISPLAY: ":0", REASONIX_OZONE_PLATFORM: "x11" }, [], [X11]],
    ["override auto", { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0", REASONIX_OZONE_PLATFORM: "auto" }, [], [X11]],
    ["explicit flag", { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0" }, ["--ozone-platform=wayland"], null],
    ["explicit hint", { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0" }, ["--ozone-platform-hint=wayland"], null],
    ["already relaunched", { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0" }, [X11, "--flag"], null],
    ["x11 session", { XDG_SESSION_TYPE: "x11", DISPLAY: ":0" }, ["--flag"], null],
  ];
  for (const [why, env, args, want] of cases) {
    const calls = [];
    const app = { relaunch: (opts) => calls.push(["relaunch", opts.args]), exit: (code) => calls.push(["exit", code]) };
    const relaunched = relaunchForOzonePlatform(app, { platform: "linux", env, argv: ["/opt/Reasonix Studio/reasonix-studio", ...args] });
    if (want === null) {
      assert.equal(relaunched, false, why);
      assert.deepEqual(calls, [], why);
    } else {
      assert.equal(relaunched, true, why);
      assert.deepEqual(calls, [["relaunch", want], ["exit", 0]], why);
    }
  }
  const calls = [];
  const app = { relaunch: () => calls.push("relaunch"), exit: () => calls.push("exit") };
  for (const platform of ["darwin", "win32"]) {
    assert.equal(relaunchForOzonePlatform(app, { platform, env: { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0" }, argv: ["x"] }), false);
  }
  assert.deepEqual(calls, []);
});

test("the shell leaves for its X11 relaunch before it claims the instance lock", () => {
  const Module = require("node:module");
  const calls = [];
  const inert = new Proxy(function () {}, { get: (_t, key) => (key === "then" ? undefined : inert), apply: () => inert });
  const app = new Proxy({}, {
    get: (_t, key) => {
      if (key === "relaunch") return (opts) => calls.push(["relaunch", opts?.args]);
      if (key === "exit") return (code) => calls.push(["exit", code]);
      if (key === "requestSingleInstanceLock") return () => (calls.push(["lock"]), false);
      if (key === "whenReady") return () => new Promise(() => {});
      if (key === "getPath") return () => os.tmpdir();
      if (key === "isPackaged") return false;
      return inert;
    },
  });
  const fake = new Proxy({ app }, { get: (t, key) => t[key] ?? inert });
  const load = Module._load;
  const platform = Object.getOwnPropertyDescriptor(process, "platform");
  const saved = { XDG_SESSION_TYPE: process.env.XDG_SESSION_TYPE, DISPLAY: process.env.DISPLAY, REASONIX_OZONE_PLATFORM: process.env.REASONIX_OZONE_PLATFORM };
  const main = require.resolve("../src/main.js");
  Module._load = function (request, ...rest) {
    return request === "electron" ? fake : load.call(this, request, ...rest);
  };
  Object.defineProperty(process, "platform", { value: "linux" });
  Object.assign(process.env, { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0" });
  delete process.env.REASONIX_OZONE_PLATFORM;
  try {
    delete require.cache[main];
    require(main);
  } finally {
    Module._load = load;
    Object.defineProperty(process, "platform", platform);
    for (const [key, value] of Object.entries(saved)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    delete require.cache[main];
  }
  assert.deepEqual(calls, [["relaunch", ["--ozone-platform=x11", ...process.argv.slice(1)]], ["exit", 0]]);
});

test("F11 toggles full screen where no application menu binds it", () => {
  const { installFullScreenKey } = require("../src/fullscreen.js");
  const press = (over) => ({ type: "keyDown", key: "F11", control: false, alt: false, shift: false, meta: false, isAutoRepeat: false, ...over });
  const rig = (platform) => {
    let handler = null;
    const state = { full: false, prevented: 0 };
    const contents = { on: (name, fn) => { if (name === "before-input-event") handler = fn; } };
    const window = { isFullScreen: () => state.full, setFullScreen: (v) => { state.full = v; } };
    installFullScreenKey(contents, window, platform);
    const send = (input) => handler?.({ preventDefault: () => { state.prevented += 1; } }, input);
    return { state, send, bound: () => handler !== null };
  };

  for (const platform of ["linux", "win32"]) {
    const { state, send } = rig(platform);
    send(press());
    assert.equal(state.full, true, `${platform}: F11 did not enter full screen`);
    send(press({ type: "keyUp" }));
    send(press({ isAutoRepeat: true }));
    send(press({ control: true }));
    send(press({ key: "F10" }));
    assert.equal(state.full, true, `${platform}: something other than a fresh F11 press toggled`);
    send(press());
    assert.equal(state.full, false, `${platform}: F11 did not leave full screen`);
    assert.equal(state.prevented, 2);
  }
  // macOS keeps its own full-screen control on the window menu and title bar.
  assert.equal(rig("darwin").bound(), false);
});

function reloadRig(platform = "darwin") {
  const { installReload } = require("../src/reload.js");
  const handlers = {};
  const state = { reloads: 0, prevented: 0, visible: true, destroyed: false, clock: 0 };
  const contents = {
    on: (name, fn) => { handlers[name] = fn; },
    reload: () => { state.reloads += 1; state.crashed = false; },
    isCrashed: () => !!state.crashed,
  };
  const window = { isVisible: () => state.visible, isDestroyed: () => state.destroyed };
  const { revive } = installReload(contents, window, { platform, now: () => state.clock });
  const send = (input) => handlers["before-input-event"]?.({ preventDefault: () => { state.prevented += 1; } }, input);
  const gone = (reason) => {
    state.crashed = reason !== "clean-exit";
    handlers["render-process-gone"]?.({}, { reason, exitCode: 1 });
  };
  return { state, send, gone, revive };
}

const reloadPress = (over) => ({ type: "keyDown", key: "r", code: "KeyR", control: false, alt: false, shift: false, meta: false, isAutoRepeat: false, ...over });
const reloadMods = [
  ["darwin", { meta: true }, { control: true }],
  ["win32", { control: true }, { meta: true }],
  ["linux", { control: true }, { meta: true }],
];

test("reloadAction reads the chord, not the layout", () => {
  const { reloadAction } = require("../src/reload.js");
  for (const [platform, mod, other] of reloadMods) {
    const at = (input) => reloadAction(input, platform);
    assert.equal(at(reloadPress(mod)), "reload");
    assert.equal(at(reloadPress({ ...mod, key: "к" })), "reload");
    assert.equal(at(reloadPress({ key: "F5", code: "F5" })), "reload");
    assert.equal(at(reloadPress({ ...mod, key: "p", code: "KeyR" })), null, `${platform}: a Dvorak P reloaded`);
    assert.equal(at(reloadPress({ ...mod, key: "r", code: "KeyP" })), "reload", `${platform}: a Dvorak R did not reload`);
    assert.equal(at(reloadPress({ ...mod, shift: true })), "hard");
    assert.equal(at(reloadPress({ ...mod, shift: true, key: "R" })), "hard");
    for (const input of [
      reloadPress(), reloadPress(other), reloadPress({ ...mod, alt: true }),
      reloadPress({ ...mod, type: "keyUp" }), reloadPress({ ...mod, isAutoRepeat: true }),
      reloadPress({ ...mod, key: "t", code: "KeyT" }), reloadPress({ key: "F5", code: "F5", control: true }),
      reloadPress({ key: "F5", code: "F5", shift: true }), reloadPress({ shift: true }),
    ]) assert.equal(at(input), null, `${platform}: ${JSON.stringify(input)}`);
  }
});

test("only the hard reload chord reloads the app window; F5 and Ctrl/Cmd+R reach the page", () => {
  for (const [platform, mod] of reloadMods) {
    const { state, send } = reloadRig(platform);
    send(reloadPress(mod));
    send(reloadPress({ key: "F5", code: "F5" }));
    assert.equal(state.reloads, 0, `${platform}: a plain reload key reloaded the UI`);
    assert.equal(state.prevented, 0, `${platform}: a plain reload key was swallowed`);
    send(reloadPress({ ...mod, shift: true, key: "R" }));
    assert.equal(state.reloads, 1, `${platform}: the recovery chord did not reload`);
    assert.equal(state.prevented, 1);
  }
});

test("a browser pane reloads on F5 and Ctrl/Cmd+R, ignoring cache on the shifted chord, and swallows nothing else", () => {
  const { installPaneReload } = require("../src/reload.js");
  for (const [platform, mod] of reloadMods) {
    let handler;
    const calls = { reload: 0, hard: 0, prevented: 0 };
    const contents = {
      on: (name, fn) => { if (name === "before-input-event") handler = fn; },
      reload: () => { calls.reload += 1; },
      reloadIgnoringCache: () => { calls.hard += 1; },
    };
    installPaneReload(contents, platform);
    const send = (input) => handler({ preventDefault: () => { calls.prevented += 1; } }, input);
    send(reloadPress(mod));
    send(reloadPress({ key: "F5", code: "F5" }));
    assert.deepEqual(calls, { reload: 2, hard: 0, prevented: 2 }, platform);
    send(reloadPress({ ...mod, shift: true, key: "R" }));
    assert.deepEqual(calls, { reload: 2, hard: 1, prevented: 3 }, platform);
    send(reloadPress({ ...mod, key: "f", code: "KeyF" }));
    send(reloadPress({ key: "F12", code: "F12" }));
    send(reloadPress({ ...mod, alt: true }));
    assert.deepEqual(calls, { reload: 2, hard: 1, prevented: 3 }, `${platform}: another key was handled`);
  }
});

test("a renderer that dies under a visible window is reloaded, but not in a loop", () => {
  const { CRASH_RELOADS, CRASH_WINDOW_MS } = require("../src/reload.js");
  const { state, gone } = reloadRig();
  gone("killed");
  assert.equal(state.reloads, 1, "a killed renderer was not reloaded");
  gone("clean-exit");
  assert.equal(state.reloads, 1, "a clean exit was reloaded");
  state.visible = false;
  gone("crashed");
  assert.equal(state.reloads, 1, "a hidden window was reloaded; its crash belongs to the shell's first-paint handling");
  state.visible = true;
  for (let i = 1; i < CRASH_RELOADS + 3; i++) gone("crashed");
  assert.equal(state.reloads, CRASH_RELOADS, "crashes kept reloading past the guard");
  state.clock += CRASH_WINDOW_MS;
  gone("crashed");
  assert.equal(state.reloads, CRASH_RELOADS + 1, "the guard never let a later crash reload again");
  state.destroyed = true;
  gone("crashed");
  assert.equal(state.reloads, CRASH_RELOADS + 1, "a destroyed window was reloaded");
});

test("a renderer that died while the window was hidden is reloaded before the window shows", () => {
  const { state, gone, revive } = reloadRig();
  revive();
  assert.equal(state.reloads, 0, "a live page was reloaded on show");
  state.visible = false;
  gone("crashed");
  assert.equal(state.reloads, 0);
  revive();
  assert.equal(state.reloads, 1, "the window would show a dead page");
  revive();
  assert.equal(state.reloads, 1, "a page already reloaded was reloaded again");
  state.crashed = true;
  state.destroyed = true;
  revive();
  assert.equal(state.reloads, 1, "a destroyed window was reloaded");
});

test("the shell log rotates at its cap and keeps a fixed number of files", () => {
  const { rotatingLog } = require("../src/shelllog.js");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-log-"));
  try {
    const file = path.join(dir, "shell.log");
    const log = rotatingLog(file, [], { maxBytes: 200, keep: 3, now: () => new Date(0) });
    for (let i = 0; i < 40; i++) log.line(`entry ${String(i).padStart(2, "0")}`);
    const names = fs.readdirSync(dir).sort();
    assert.deepEqual(names, ["shell.log", "shell.log.1", "shell.log.2"]);
    for (const name of names) assert.ok(fs.statSync(path.join(dir, name)).size <= 200, `${name} ran past its cap`);
    assert.match(fs.readFileSync(file, "utf8"), /entry 39\n$/, "the newest entry is not in the live file");
    assert.doesNotMatch(fs.readFileSync(path.join(dir, "shell.log.2"), "utf8"), /entry 00/, "the oldest entry outlived the rotation");

    const reopened = rotatingLog(file, [], { maxBytes: 200, keep: 3 });
    reopened.line("x".repeat(150));
    assert.doesNotMatch(fs.readFileSync(file, "utf8"), /entry 39/, "a reopened log forgot how full its file already was");
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("no secret reaches the shell or host log", () => {
  const { openLogs, redact, redactArgv } = require("../src/shelllog.js");
  const handshake = line();
  assert.ok(!redact(handshake).includes(TOKEN), "the handshake's credential survived redaction");
  assert.match(redact(handshake), /"origin":"http:\/\/127\.0\.0\.1:8080"/, "redaction took more than the secret");
  assert.ok(!redact(`GET /?token=${TOKEN}&x=1`).includes(TOKEN));
  assert.deepEqual(
    redactArgv(["studio.exe", "--token=abc", "--api-key", "sk-1", "--flag", "-page", "C:\\dist"]),
    ["studio.exe", "--token=[redacted]", "--api-key", "[redacted]", "--flag", "-page", "C:\\dist"],
  );

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-log-"));
  try {
    const logs = openLogs(dir);
    const bare = "b".repeat(40);
    logs.addSecret(bare);
    logs.host.raw(`${handshake}\n`);
    logs.host.raw(`kernel said ${bare} in passing\n`);
    logs.shell.line(`echo ${bare}`);
    for (const name of ["host.log", "shell.log"]) {
      const text = fs.readFileSync(path.join(logs.dir, name), "utf8");
      assert.ok(!text.includes(TOKEN) && !text.includes(bare), `${name} carried a credential: ${text}`);
    }
    assert.ok(!logs.host.tail().includes(bare), "the host tail shown in a dialog carried a credential");
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("a secret split across pipe chunks is still redacted", () => {
  const { openLogs } = require("../src/shelllog.js");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-log-"));
  try {
    const logs = openLogs(dir);
    logs.host.raw("mcp: using api_");
    logs.host.raw("key=sk-live-12");
    logs.host.raw("3456 for the call\nnext: Authorization: Bea");
    logs.host.raw("rer abcdef0123456789\n");
    logs.host.raw("an unterminated password=hunter22");
    logs.host.flush();
    const text = fs.readFileSync(path.join(logs.dir, "host.log"), "utf8");
    for (const leak of ["sk-live", "3456", "abcdef0123456789", "hunter22"]) {
      assert.ok(!text.includes(leak), `${leak} reached host.log: ${text}`);
    }
    assert.match(text, /for the call\n/, "the rest of the line was lost");

    logs.host.raw("x".repeat(20 * 1024));
    logs.host.raw("tail\n");
    const after = fs.readFileSync(path.join(logs.dir, "host.log"), "utf8");
    assert.match(after, /characters without a line break dropped/);
    assert.ok(!after.includes("x".repeat(100)), "an unterminated run was written instead of dropped");
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("secrets named by their structure are redacted in every common spelling", () => {
  const { redact } = require("../src/shelllog.js");
  const cases = [
    ["Authorization: Bearer sk-abc123", "sk-abc123"],
    ['{"Authorization":"Bearer sk-abc123"}', "sk-abc123"],
    ["x-api-key: sk-ant-abc123", "sk-ant-abc123"],
    ["api_key: sk-abc123", "sk-abc123"],
    ['level=INFO token="abc def"', "abc def"],
    ["dial postgres://user:pa55word@db:5432/x", "pa55word"],
    ["GET https://example.com/v1?key=AIzaSyAbc&q=1", "AIzaSyAbc"],
    ['{"token": 12345}', "12345"],
    ["cfg={Name:x Token:abc123}", "abc123"],
    ["client_secret='s3cr3t'", "s3cr3t"],
  ];
  for (const [input, secret] of cases) {
    const out = redact(input);
    assert.ok(!out.includes(secret), `${input} -> ${out}`);
  }
  assert.equal(redact("dial postgres://user:pa55word@db:5432/x"), "dial postgres://user:[redacted]@db:5432/x");
  assert.equal(redact("listening on http://127.0.0.1:8080/ in 12ms"), "listening on http://127.0.0.1:8080/ in 12ms");
  for (const plain of [
    "open /home/u/.config/reasonix/token.json: no such file or directory",
    "keyboard: us layout",
    "author: esengine",
    "oauth: token expired",
    "keyring: the name is not activatable",
    "keychain: item not found",
    "max_tokens=8192 exceeded",
    "sort_key=name",
    "primary_key: id",
  ]) {
    assert.equal(redact(plain), plain, "a diagnostic lost its cause");
  }
  for (const [input, secret] of [
    ["access_token=abc123", "abc123"],
    ["GITHUB_TOKEN=ghp_abc123", "ghp_abc123"],
    ["private_key: -----x", "-----x"],
    ["password=hunter22", "hunter22"],
  ]) {
    assert.ok(!redact(input).includes(secret), input);
  }
  // Accepted trade-offs: a whole identifier that names a secret loses the
  // word after it even where that word is not one.
  assert.equal(redact("credentials: open /x/y: denied"), "credentials: [redacted] /x/y: denied");
  assert.equal(redact("key=model"), "key=[redacted]");
});

test("a log whose live file cannot be renamed still stays under its cap", () => {
  const { rotatingLog, openLogs } = require("../src/shelllog.js");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-log-"));
  try {
    const file = path.join(dir, "shell.log");
    // A non-empty directory where the first rotation lands makes the rename fail.
    fs.mkdirSync(path.join(dir, "shell.log.1", "blocker"), { recursive: true });
    const log = rotatingLog(file, [], { maxBytes: 200, keep: 2 });
    for (let i = 0; i < 40; i++) log.line(`entry ${i}`);
    assert.ok(fs.statSync(file).size <= 200, "the live file grew past its cap");
    assert.match(fs.readFileSync(file, "utf8"), /entry 39\n$/);

    if (process.platform !== "win32") {
      const logs = openLogs(path.join(dir, "home"));
      logs.shell.line("x");
      assert.equal(fs.statSync(logs.dir).mode & 0o777, 0o700);
      assert.equal(fs.statSync(logs.shell.file).mode & 0o777, 0o600);
    }
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("a launch that cannot start says why and where the logs are, then quits", () => {
  const { failStartup } = require("../src/shelllog.js");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-log-"));
  try {
    const { openLogs } = require("../src/shelllog.js");
    for (const [locale, title] of [["zh-CN", /无法启动/], ["en-US", /could not start/]]) {
      const logs = openLogs(dir);
      const calls = [];
      const dialog = { showErrorBox: (t, d) => calls.push(["dialog", t, d]) };
      const app = { quit: () => calls.push(["quit"]) };
      failStartup({ app, dialog, logs, locale }, new Error("the kernel exited with 3 before saying anything"), "config: bad value");
      assert.deepEqual(calls.map((c) => c[0]), ["dialog", "quit"], locale);
      const [, shownTitle, detail] = calls[0];
      assert.match(shownTitle, title, locale);
      assert.ok(detail.includes("exited with 3") && detail.includes("config: bad value") && detail.includes(logs.dir), detail);
    }
    assert.match(fs.readFileSync(path.join(dir, "logs", "shell.log"), "utf8"), /startup failed: Error: the kernel exited with 3/);

    const logs = openLogs(dir);
    const calls = [];
    const dialog = { showErrorBox: () => { throw new Error("no display"); } };
    failStartup({ app: { quit: () => calls.push("quit") }, dialog, logs, locale: "en" }, new Error("x"));
    assert.deepEqual(calls, ["quit"], "a dialog that could not be shown kept the launch alive");
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

// Loads the real main.js against a stand-in electron and reports what it did.
function loadShell({ lock, host }) {
  const Module = require("node:module");
  const userData = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-shell-"));
  const calls = [];
  let quit;
  const quitted = new Promise((resolve) => { quit = resolve; });
  const inert = new Proxy(function () {}, { get: (_t, key) => (key === "then" ? undefined : inert), apply: () => inert });
  const app = new Proxy({}, {
    get: (_t, key) => {
      if (key === "requestSingleInstanceLock") return () => lock;
      if (key === "whenReady") return () => Promise.resolve();
      if (key === "getPath") return () => userData;
      if (key === "getVersion") return () => "9.9.9";
      if (key === "getLocale") return () => "en-US";
      if (key === "getPreferredSystemLanguages") return () => ["en-US"];
      if (key === "isPackaged") return false;
      if (key === "quit") return () => { calls.push(["quit"]); quit(); };
      return inert;
    },
  });
  const dialog = { showErrorBox: (title, detail) => calls.push(["dialog", title, detail]) };
  const fake = new Proxy({ app, dialog }, { get: (t, key) => t[key] ?? inert });
  const load = Module._load;
  const main = require.resolve("../src/main.js");
  const saved = { REASONIX_STUDIO_HOST: process.env.REASONIX_STUDIO_HOST, REASONIX_OZONE_PLATFORM: process.env.REASONIX_OZONE_PLATFORM };
  Module._load = function (request, ...rest) {
    return request === "electron" ? fake : load.call(this, request, ...rest);
  };
  Object.assign(process.env, { REASONIX_STUDIO_HOST: host, REASONIX_OZONE_PLATFORM: "wayland" });
  try {
    delete require.cache[main];
    require(main);
  } finally {
    Module._load = load;
    for (const [key, value] of Object.entries(saved)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    delete require.cache[main];
  }
  const logDir = path.join(userData, "logs");
  const shellLog = () => fs.readFileSync(path.join(logDir, "shell.log"), "utf8");
  const hostLog = () => fs.readFileSync(path.join(logDir, "host.log"), "utf8");
  return { calls, quitted, logDir, shellLog, hostLog, cleanup: () => fs.rmSync(userData, { recursive: true, force: true }) };
}

test("a second launch leaves a line in the log and no dialog", async () => {
  const shell = loadShell({ lock: false, host: path.join(os.tmpdir(), "no-such-host") });
  try {
    await shell.quitted;
    await new Promise((r) => setImmediate(r));
    assert.deepEqual(shell.calls, [["quit"]], "a second launch showed a dialog");
    assert.match(shell.shellLog(), /shell: start version=9\.9\.9/);
    assert.match(shell.shellLog(), /another instance holds the lock/);
  } finally {
    shell.cleanup();
  }
});

test("a host that exits before its handshake is logged and shown, not swallowed", async () => {
  // node refuses the shell's first argument, which makes it a host that
  // writes to stderr and exits non-zero without ever handshaking.
  const shell = loadShell({ lock: true, host: process.execPath });
  try {
    await shell.quitted;
    const kinds = shell.calls.map((c) => c[0]);
    assert.deepEqual(kinds, ["dialog", "quit"], "the launch ended without telling anyone");
    const [, title, detail] = shell.calls[0];
    assert.match(title, /could not start/);
    assert.match(detail, /bad option/, "the host's own words were not shown");
    assert.ok(detail.includes(shell.logDir), detail);
    assert.match(shell.hostLog(), /bad option/, "the host's stderr never reached host.log");
    const log = shell.shellLog();
    assert.match(log, /host: exited code=9 signal=null before its handshake/);
    assert.match(log, /startup failed: HostStartError: /);
    assert.equal((log.match(/host: starting /g) || []).length, 2, "an early exit was not retried exactly once");
    assert.match(log, /retrying once/);
    assert.match(detail, /after 2 attempts/);
    assert.match(detail, /stopped before it was ready/, "no cause was given for an early exit");
  } finally {
    shell.cleanup();
  }
});

test("a development shell leaves once the process that launched it is gone", () => {
  const { watchLauncher } = require("../src/launcher.js");
  mock.timers.enable({ apis: ["setInterval"] });
  try {
    // POSIX hands an orphan to another parent; Windows leaves the dead pid in place.
    for (const [why, end] of [["reparented", (s) => { s.ppid = 1; }], ["died in place", (s) => { s.alive = false; }]]) {
      const state = { ppid: 4242, alive: true };
      let lost = 0;
      watchLauncher({ packaged: false, env: {}, onLost: () => lost++, parent: () => state.ppid, alive: () => state.alive });
      mock.timers.tick(5000);
      assert.equal(lost, 0, `${why}: left while the launcher was still there`);
      end(state);
      mock.timers.tick(1000);
      assert.equal(lost, 1, `${why}: stayed after its launcher was gone`);
      mock.timers.tick(5000);
      assert.equal(lost, 1, `${why}: told more than once`);
    }

    const state = { ppid: 4242 };
    let lost = 0;
    const watch = watchLauncher({ packaged: false, env: {}, onLost: () => lost++, parent: () => state.ppid, alive: () => true });
    watch.dispose();
    state.ppid = 1;
    mock.timers.tick(5000);
    assert.equal(lost, 0, "a disposed watch still acted");
  } finally {
    mock.timers.reset();
  }
});

test("a packaged, handed-over or parentless shell belongs to no launcher", () => {
  const { watchLauncher, handOver } = require("../src/launcher.js");
  mock.timers.enable({ apis: ["setInterval"] });
  try {
    const handed = {};
    handOver(handed);
    for (const [why, options] of [
      ["packaged", { packaged: true, env: {}, parent: () => 4242 }],
      ["handed over by a relaunch", { packaged: false, env: handed, parent: () => 4242 }],
      ["started by init", { packaged: false, env: {}, parent: () => 1 }],
    ]) {
      let lost = 0;
      let ppid = options.parent();
      watchLauncher({ ...options, onLost: () => lost++, parent: () => ppid, alive: () => false });
      ppid = 7;
      mock.timers.tick(5000);
      assert.equal(lost, 0, why);
    }
    assert.deepEqual(handed, {}, "the hand-over mark reached this shell's own children");
  } finally {
    mock.timers.reset();
  }
});

test("a relaunch hands the shell over before it asks for the restart", () => {
  const { actHandler } = require("../src/acts.js");
  const { relaunchForOzonePlatform } = require("../src/ozone.js");
  const key = "REASONIX_STUDIO_HANDED_OVER";
  const saved = process.env[key];
  delete process.env[key];
  try {
    const seen = [];
    actHandler({ app: { relaunch: () => seen.push(process.env[key]) }, showWindow: () => {} })("relaunch");
    assert.deepEqual(seen, ["1"]);
  } finally {
    if (saved === undefined) delete process.env[key];
    else process.env[key] = saved;
  }
  const env = { XDG_SESSION_TYPE: "wayland", DISPLAY: ":0" };
  const seen = [];
  relaunchForOzonePlatform({ relaunch: () => seen.push(env[key]), exit: () => {} }, { platform: "linux", env, argv: ["x"] });
  assert.deepEqual(seen, ["1"]);
});

test("output for a console the launcher closed is dropped, never thrown", () => {
  const { EventEmitter } = require("node:events");
  const { consoleEcho } = require("../src/launcher.js");
  const stream = Object.assign(new EventEmitter(), { written: [], full: false, broken: false });
  stream.write = (chunk) => {
    if (stream.broken) throw Object.assign(new Error("write EIO"), { code: "EIO" });
    stream.written.push(chunk);
    return !stream.full;
  };
  const echo = consoleEcho(stream);
  echo("a");
  stream.full = true;
  echo("b");
  echo("c");
  assert.deepEqual(stream.written, ["a", "b"], "a backed-up console kept queueing");
  stream.emit("drain");
  stream.full = false;
  echo("d");
  stream.emit("error", Object.assign(new Error("write EPIPE"), { code: "EPIPE" }));
  echo("e");
  assert.deepEqual(stream.written, ["a", "b", "d"], "a failed console was written to again");

  const sync = Object.assign(new EventEmitter(), { broken: true, calls: 0 });
  sync.write = () => {
    sync.calls++;
    throw Object.assign(new Error("write EIO"), { code: "EIO" });
  };
  const echoSync = consoleEcho(sync);
  assert.doesNotThrow(() => echoSync("x"));
  echoSync("y");
  assert.equal(sync.calls, 1);
});

test("a shell whose console pipe was closed keeps running", async () => {
  const { spawn } = require("node:child_process");
  const script = `
    const { consoleEcho } = require(${JSON.stringify(require.resolve("../src/launcher.js"))});
    const echo = consoleEcho(process.stderr);
    process.stdin.once("data", () => {
      let n = 0;
      const timer = setInterval(() => {
        echo("x".repeat(512) + "\\n");
        if (++n === 50) { clearInterval(timer); process.stdout.write("survived\\n"); }
      }, 2);
    });`;
  const child = spawn(process.execPath, ["-e", script], { stdio: ["pipe", "pipe", "pipe"] });
  child.stderr.destroy();
  let out = "";
  child.stdout.on("data", (chunk) => { out += chunk; });
  child.stdin.end("go\n");
  const code = await new Promise((resolve) => child.on("exit", resolve));
  assert.equal(code, 0);
  assert.equal(out, "survived\n");
});

test("the launch credential cookie is put back when something removes it, and only then", async () => {
  const { keepCredential } = require("../src/credential.js");
  const { EventEmitter } = require("node:events");
  const jar = new EventEmitter();
  const sets = [];
  const ses = { cookies: Object.assign(jar, { set: async (c) => void sets.push(c) }) };
  const cookie = { url: "http://127.0.0.1:5000/", name: "reasonix_token", value: "t" };
  const lines = [];
  keepCredential(ses, cookie, (l) => lines.push(l));
  const gone = { name: "reasonix_token", domain: "127.0.0.1" };
  jar.emit("changed", {}, { ...gone, name: "other" }, "explicit", true);
  jar.emit("changed", {}, { ...gone, domain: "example.com" }, "explicit", true);
  jar.emit("changed", {}, gone, "overwrite", true);
  jar.emit("changed", {}, gone, "explicit", false);
  assert.equal(sets.length, 0, "an unrelated change must not touch the jar");
  jar.emit("changed", {}, gone, "evicted", true);
  assert.deepEqual(sets, [cookie]);
  assert.match(lines[0], /evicted/);
});

test("a refused request makes the shell check the credential cookie, and put it back only when it is missing or differs", async () => {
  const { restoreIfStale } = require("../src/credential.js");
  const cookie = { url: "http://127.0.0.1:5000/", name: "reasonix_token", value: "t" };
  const run = async (held) => {
    const sets = [];
    const lines = [];
    const ses = { cookies: { get: async () => held, set: async (c) => void sets.push(c) } };
    await restoreIfStale(ses, cookie, (l) => lines.push(l));
    return { sets, lines };
  };
  assert.deepEqual((await run([{ name: "reasonix_token", value: "t" }])).sets, [], "a matching cookie is left alone");
  const missing = await run([]);
  assert.deepEqual(missing.sets, [cookie]);
  assert.match(missing.lines[0], /missing/);
  const stale = await run([{ name: "reasonix_token", value: "old" }]);
  assert.deepEqual(stale.sets, [cookie]);
  assert.match(stale.lines[0], /differs/);
});

test("the tray has a file for every Windows scale, at exactly 16 * scale pixels", () => {
  const { trayAsset, SCALES } = require("../src/trayimage.js");
  const dim = (f) => {
    const b = fs.readFileSync(f);
    return [b.readUInt32BE(16), b.readUInt32BE(20)];
  };
  for (const [scale] of SCALES) {
    const { file, pixels } = trayAsset(scale);
    assert.deepEqual(dim(file), [pixels, pixels], file);
    assert.equal(pixels, Math.round(16 * scale));
  }
  assert.equal(trayAsset(1.75).pixels, 28);
  assert.equal(trayAsset(1.8).pixels, 32);
  assert.equal(trayAsset(5).pixels, 48);
  assert.equal(trayAsset(NaN).pixels, 16);
});

test("the macOS tray is a black-on-transparent Template pair, not the colour badge", () => {
  const { trayImage } = require("../src/trayimage.js");
  const asked = [];
  const nativeImage = { createFromPath: (f) => (asked.push(f), { isEmpty: () => false }) };
  const platform = Object.getOwnPropertyDescriptor(process, "platform");
  Object.defineProperty(process, "platform", { value: "darwin" });
  try {
    trayImage(nativeImage, 2);
  } finally {
    Object.defineProperty(process, "platform", platform);
  }
  assert.match(path.basename(asked[0]), /^trayTemplate\.png$/);
  for (const [file, px] of [["trayTemplate.png", 18], ["trayTemplate@2x.png", 36]]) {
    const b = fs.readFileSync(path.join(path.dirname(asked[0]), file));
    assert.deepEqual([b.readUInt32BE(16), b.readUInt32BE(20)], [px, px], file);
    assert.equal(b[25], 6, `${file} must carry an alpha channel`);
  }
});

function pe(machine) {
  const b = Buffer.alloc(0x100);
  b.writeUInt16LE(0x5a4d, 0);
  b.writeUInt32LE(0x80, 0x3c);
  b.writeUInt32LE(0x00004550, 0x80);
  b.writeUInt16LE(machine, 0x84);
  return b;
}

test("the PE check reads each image's own machine and refuses a tree of another architecture", () => {
  const { peMachine, machines, foreign } = require("../packaging/pe.js");
  assert.equal(peMachine(pe(0xaa64)), 0xaa64);
  assert.throws(() => peMachine(Buffer.from("#!/bin/sh\n".repeat(10))), /not a PE/);
  assert.throws(() => peMachine(pe(0x8664).subarray(0, 0x70)), /no PE signature/);

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-pe-"));
  try {
    assert.throws(() => foreign(dir, "arm64"), /no PE images/);
    fs.mkdirSync(path.join(dir, "resources", "bin"), { recursive: true });
    fs.writeFileSync(path.join(dir, "app.exe"), pe(0xaa64));
    fs.writeFileSync(path.join(dir, "resources", "bin", "host.exe"), pe(0xaa64));
    fs.writeFileSync(path.join(dir, "locales.pak"), "not an image");
    fs.writeFileSync(path.join(dir, "noext"), pe(0xaa64));
    assert.equal(machines(dir).length, 3);
    assert.deepEqual(foreign(dir, "arm64"), []);
    assert.deepEqual(foreign(dir, "amd64").map((f) => f.file), ["app.exe", "noext", "resources/bin/host.exe"]);
    fs.writeFileSync(path.join(dir, "ffmpeg.dll"), pe(0x8664));
    assert.deepEqual(foreign(dir, "arm64").map((f) => f.file), ["ffmpeg.dll"]);
    assert.throws(() => foreign(dir, "x64"), /unknown architecture/);
    fs.writeFileSync(path.join(dir, "broken.dll"), "text, not an image");
    const broken = foreign(dir, "arm64").find((f) => f.file === "broken.dll");
    assert.match(broken.error, /not a PE/);
    fs.rmSync(path.join(dir, "broken.dll"));
    fs.writeFileSync(path.join(dir, "ffmpeg.dll"), pe(0xaa64));
    fs.writeFileSync(path.join(dir, "resources", "elevate.exe"), pe(0x14c));
    assert.deepEqual(foreign(dir, "arm64"), []);
    fs.writeFileSync(path.join(dir, "Uninstall Reasonix Studio.exe"), pe(0x14c));
    assert.deepEqual(foreign(dir, "arm64"), []);
    fs.writeFileSync(path.join(dir, "resources", "elevate.exe"), pe(0xaa64));
    assert.deepEqual(foreign(dir, "arm64").map((f) => f.file), ["resources/elevate.exe"]);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

const { createPowerGuard, keepsAwake, IDLE_PASSES, SILENT_PASSES } = require("../src/powerguard.js");

function fakeBlocker() {
  const live = new Set();
  let next = 1;
  const calls = { start: 0, stop: 0 };
  return {
    calls,
    live,
    start(kind) {
      assert.equal(kind, "prevent-app-suspension");
      calls.start++;
      live.add(next);
      return next++;
    },
    stop(id) {
      calls.stop++;
      live.delete(id);
    },
    isStarted: (id) => live.has(id),
  };
}

// answer is what the kernel says next: { panes, unknown }, null for a kernel
// that cannot answer, or an Error to throw.
function guardOver(state) {
  const blocker = fakeBlocker();
  const guard = createPowerGuard({
    blocker,
    running: async () => {
      if (state.answer instanceof Error) throw state.answer;
      return state.answer;
    },
    enabled: () => state.enabled !== false,
  });
  return { blocker, guard };
}
const busy = (panes) => ({ panes, unknown: 0 });
const idle = { panes: 0, unknown: 0 };
async function times(n, guard) {
  for (let i = 0; i < n; i++) await guard.refresh();
}

test("a running turn holds the computer awake at once, once, however many panes run", async () => {
  const state = { answer: busy(1) };
  const { blocker, guard } = guardOver(state);
  await guard.refresh();
  assert.equal(blocker.live.size, 1);
  await guard.refresh();
  state.answer = busy(4);
  await guard.refresh();
  assert.equal(blocker.calls.start, 1);
});

test("the hold is let go only after the third idle answer in a row", async () => {
  const state = { answer: busy(2) };
  const { blocker, guard } = guardOver(state);
  await guard.refresh();
  state.answer = idle;
  await times(IDLE_PASSES - 1, guard);
  assert.equal(blocker.live.size, 1, "two idle answers are not enough");
  await guard.refresh();
  assert.equal(blocker.live.size, 0);
  assert.equal(blocker.calls.stop, 1);
  await guard.refresh();
  assert.equal(blocker.calls.stop, 1);
});

test("a turn showing up between idle answers starts the count again", async () => {
  const state = { answer: busy(1) };
  const { blocker, guard } = guardOver(state);
  await guard.refresh();
  state.answer = idle;
  await times(IDLE_PASSES - 1, guard);
  state.answer = busy(1);
  await guard.refresh();
  state.answer = idle;
  await times(IDLE_PASSES - 1, guard);
  assert.equal(blocker.live.size, 1);
  await guard.refresh();
  assert.equal(blocker.live.size, 0);
});

test("an idle kernel never takes the hold", async () => {
  const { blocker, guard } = guardOver({ answer: idle });
  await times(5, guard);
  assert.equal(blocker.calls.start, 0);
});

test("a kernel that cannot answer, or a pane that cannot be asked, changes nothing", async () => {
  for (const unsure of [null, new Error("kernel gone"), { panes: 0, unknown: 1 }, {}]) {
    const state = { answer: busy(1) };
    const { blocker, guard } = guardOver(state);
    await guard.refresh();
    state.answer = idle;
    await times(IDLE_PASSES - 1, guard);
    state.answer = unsure;
    await times(SILENT_PASSES - 1, guard);
    assert.equal(blocker.live.size, 1, `held through ${String(unsure)}`);
    assert.equal(blocker.calls.stop, 0, "silence did not count as the idle answer that was missing");
  }
});

test("silence does not take the hold either", async () => {
  const { blocker, guard } = guardOver({ answer: null });
  await times(3, guard);
  assert.equal(blocker.calls.start, 0);
});

test("a hold nobody can confirm is let go after SILENT_PASSES, so it cannot leak", async () => {
  const state = { answer: busy(1) };
  const { blocker, guard } = guardOver(state);
  await guard.refresh();
  state.answer = null;
  await times(SILENT_PASSES, guard);
  assert.equal(blocker.live.size, 0);
});

test("turning the setting off lets go at once and keeps it off while turns run", async () => {
  const state = { answer: busy(1) };
  const { blocker, guard } = guardOver(state);
  await guard.refresh();
  state.enabled = false;
  await guard.refresh();
  assert.equal(blocker.live.size, 0);
  await guard.refresh();
  assert.equal(blocker.calls.start, 1);
  state.enabled = true;
  await guard.refresh();
  assert.equal(blocker.live.size, 1);
});

test("a block the system dropped is taken again while work continues", async () => {
  const { blocker, guard } = guardOver({ answer: busy(1) });
  await guard.refresh();
  blocker.live.clear();
  await guard.refresh();
  assert.equal(blocker.live.size, 1);
  assert.equal(blocker.calls.start, 2);
});

test("an answer that arrives after a newer one does not overturn it", async () => {
  const blocker = fakeBlocker();
  const pending = [];
  const guard = createPowerGuard({
    blocker,
    running: () => new Promise((resolve) => pending.push(resolve)),
    enabled: () => true,
  });
  const slow = guard.refresh();
  const fresh = guard.refresh();
  pending[1](busy(2));
  await fresh;
  pending[0](idle);
  await slow;
  assert.equal(blocker.live.size, 1);
});

test("a refresh asked for the moment a turn is submitted takes the hold without waiting for the poll", async () => {
  const state = { answer: idle };
  const { blocker, guard } = guardOver(state);
  await guard.refresh();
  assert.equal(blocker.live.size, 0);
  state.answer = busy(1);
  await guard.refresh();
  assert.equal(blocker.live.size, 1);
});

test("closing the guard releases the hold and stops the poll", async () => {
  const timers = [];
  const blocker = fakeBlocker();
  const guard = createPowerGuard({
    blocker,
    running: async () => busy(1),
    enabled: () => true,
    setInterval: (fn, ms) => {
      timers.push({ fn, ms, cleared: false });
      return timers.at(-1);
    },
    clearInterval: (t) => (t.cleared = true),
  });
  guard.begin();
  await guard.refresh();
  assert.equal(blocker.live.size, 1);
  guard.close();
  assert.equal(blocker.live.size, 0);
  assert.equal(timers.length, 1);
  assert.equal(timers[0].cleared, true);
  await guard.refresh();
  assert.equal(blocker.live.size, 0, "a closed guard takes nothing back");
});

test("keeping awake is on unless this machine said off", () => {
  assert.equal(keepsAwake({}), true);
  assert.equal(keepsAwake(undefined), true);
  assert.equal(keepsAwake({ "rx-keep-awake": "on" }), true);
  assert.equal(keepsAwake({ "rx-keep-awake": "off" }), false);
});

test("the host client asks the kernel how many panes run", async () => {
  const server = http.createServer((req, res) => {
    res.writeHead(req.url === "/tray/running" ? 200 : 404, { "content-type": "application/json" });
    res.end(JSON.stringify({ panes: 3 }));
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const client = new StudioHost(`http://127.0.0.1:${server.address().port}`, "t");
  assert.deepEqual(await client.trayRunning(), { panes: 3 });
  server.close();
  assert.equal(await client.trayRunning(), null);
});

test("the window ground follows the stored theme and, for auto, the system", () => {
  assert.equal(groundFor({ "rx-theme": "dark" }, false), GROUND.dark);
  assert.equal(groundFor({ "rx-theme": "light" }, true), GROUND.light);
  assert.equal(groundFor({ "rx-theme": "auto" }, true), GROUND.dark);
  assert.equal(groundFor({}, false), GROUND.light);
  assert.equal(groundFor(undefined, true), GROUND.dark);
});

// Windows draws a frameless window's border from the frame's scheme, so a dark
// page on a light system must set it, or the border is a light line.
test("the window frame takes the stored theme, and auto follows the system", () => {
  assert.equal(themeSourceFor({ "rx-theme": "dark" }), "dark");
  assert.equal(themeSourceFor({ "rx-theme": "light" }), "light");
  assert.equal(themeSourceFor({ "rx-theme": "auto" }), "system");
  assert.equal(themeSourceFor({ "rx-theme": "midnight" }), "system");
  assert.equal(themeSourceFor(undefined), "system");
});

test("scheduled grounds and frames follow the painted scheme rather than the opposite OS", () => {
  const prefs = { "rx-theme": "scheduled" };
  for (const [scheme, systemDark] of [["dark", false], ["light", true]]) {
    const painted = { mode: "scheduled", scheme };
    assert.equal(themeSourceFor(prefs, painted), scheme);
    assert.equal(groundFor(prefs, systemDark, painted), GROUND[scheme]);
    const saved = { ...prefs, "rx-theme-resolved": scheme };
    assert.equal(themeSourceFor(saved), scheme);
    assert.equal(groundFor(saved, systemDark), GROUND[scheme]);
  }
  assert.equal(themeSourceFor({ ...prefs, "rx-theme-resolved": "invalid" }), "system");
});

test("the latest painted mode wins over delayed preference saves without pinning Auto", () => {
  const stale = { "rx-theme": "scheduled", "rx-theme-resolved": "dark" };
  const auto = { mode: "auto", scheme: "dark" };
  assert.equal(themeSourceFor(stale, auto), "system");
  assert.equal(groundFor(stale, false, auto), GROUND.light);
  assert.equal(groundFor(stale, true, auto), GROUND.dark);
  const light = { mode: "light", scheme: "light" };
  assert.equal(themeSourceFor(stale, light), "light");
  assert.equal(groundFor(stale, true, light), GROUND.light);
  assert.equal(themeSourceFor({ "rx-theme": "light" }, { mode: "scheduled", scheme: "dark" }), "dark");
});

const { accelerationOff, shouldDisableGpu, graphicsReport, graphicsHandler, PREF_KEY: GPU_PREF } = require("../src/graphics.js");

test("hardware acceleration is off only when the saved preference says exactly that", () => {
  assert.equal(GPU_PREF, "rx-hw-accel");
  assert.equal(accelerationOff({}), false);
  assert.equal(accelerationOff(undefined), false);
  assert.equal(accelerationOff({ "rx-hw-accel": "on" }), false);
  assert.equal(accelerationOff({ "rx-hw-accel": "" }), false);
  assert.equal(accelerationOff({ "rx-hw-accel": "off" }), true);
});

test("the graphics report says what this launch applied and passes Electron's status through untouched", () => {
  const app = (compositing) => ({ getGPUFeatureStatus: () => ({ gpu_compositing: compositing }) });
  const auto = { launchedOff: false, savedOff: false };
  for (const value of ["enabled", "enabled_force", "enabled_on", "disabled_software", "unavailable_off", ""]) {
    assert.deepEqual(graphicsReport(app(value), auto), { ...auto, compositing: value });
  }
  assert.deepEqual(graphicsReport(app("disabled_software"), { launchedOff: true, savedOff: true }), { launchedOff: true, savedOff: true, compositing: "disabled_software" });
  assert.deepEqual(graphicsReport(app("enabled"), { launchedOff: true, savedOff: false }), { launchedOff: true, savedOff: false, compositing: "enabled" });
  assert.deepEqual(graphicsReport({ getGPUFeatureStatus: () => { throw new Error("not ready"); } }, auto), { ...auto, compositing: "" });
  assert.deepEqual(graphicsReport({ getGPUFeatureStatus: () => ({}) }, auto), { ...auto, compositing: "" });
});

test("the GPU is disabled by the saved preference, the environment or the flag, and only then", () => {
  const off = (over) => shouldDisableGpu({ prefs: {}, env: {}, argv: [], ...over });
  assert.equal(off({}), false);
  assert.equal(off({ prefs: { "rx-hw-accel": "off" } }), true);
  assert.equal(off({ prefs: { "rx-hw-accel": "on" } }), false);
  assert.equal(off({ prefs: undefined }), false);
  assert.equal(off({ env: { REASONIX_DISABLE_GPU: "1" } }), true);
  assert.equal(off({ env: { REASONIX_DISABLE_GPU: "0" } }), false);
  assert.equal(off({ argv: ["studio", "--disable-gpu"] }), true);
  assert.equal(off({ argv: ["--disable-gpu-sandbox"] }), false);
});

test("a prefs file that is missing or corrupt keeps hardware acceleration", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gpu-"));
  const file = path.join(dir, "window-prefs.json");
  assert.equal(shouldDisableGpu({ prefs: loadPrefs(file), env: {}, argv: [] }), false);
  fs.writeFileSync(file, "{ not json");
  assert.equal(shouldDisableGpu({ prefs: loadPrefs(file), env: {}, argv: [] }), false);
});

test("the graphics report is refused to any sender that is not the Studio window", () => {
  const app = { getGPUFeatureStatus: () => ({ gpu_compositing: "enabled" }) };
  const win = { id: "studio" };
  const handle = graphicsHandler(app, (event) => (event.sender === "studio" ? win : null), { launchedOff: true, savedOff: false });
  assert.deepEqual(handle({ sender: "studio" }), { launchedOff: true, savedOff: false, compositing: "enabled" });
  assert.equal(handle({ sender: "agent-browser-page" }), null);
});

test("a kernel that dies after its handshake leaves its exit and last words in shell.log and a marker for the next launch", () => {
  const { openLogs } = require("../src/shelllog.js");
  const { crashDir, recordHostExit, pendingHostExit, clearHostExit } = require("../src/hostexit.js");
  const userData = fs.mkdtempSync(path.join(os.tmpdir(), "hostexit-"));
  const logs = openLogs(userData);
  const dir = crashDir(logs.dir);
  fs.mkdirSync(dir, { recursive: true });
  const started = Date.now() - 1000;
  const stale = path.join(dir, "host-20200101T000000Z-2.0.0-1.log");
  fs.writeFileSync(stale, "fatal error: an earlier run\n");
  fs.utimesSync(stale, new Date(2020, 0, 1), new Date(2020, 0, 1));
  const body = Array.from({ length: 80 }, (_, i) => `frame ${i}`).join("\n");
  fs.writeFileSync(path.join(dir, "host-20261010T000000Z-2.31.0-7.log"), `fatal error: out of memory\n${body}\n`);

  assert.equal(pendingHostExit(userData), null, "no marker before any exit");
  recordHostExit({ logs, userData, code: 2, signal: null, startedAt: started });

  const shell = fs.readFileSync(logs.shell.file, "utf8");
  assert.match(shell, /host: died code=2 signal=null crash=host-20261010T000000Z-2\.31\.0-7\.log/);
  assert.match(shell, /frame 79/, "the tail of the crash file must reach shell.log");
  assert.doesNotMatch(shell, /frame 10\b/, "only the tail is quoted");
  assert.doesNotMatch(shell, /an earlier run/, "a crash file from before this launch is not this death");

  const pending = pendingHostExit(userData);
  assert.equal(pending.code, 2);
  assert.equal(pending.crashFile, "host-20261010T000000Z-2.31.0-7.log");
  clearHostExit(userData);
  assert.equal(pendingHostExit(userData), null, "acknowledging clears the marker");
});

test("a kernel killed by a signal with no crash file is still recorded", () => {
  const { openLogs } = require("../src/shelllog.js");
  const { recordHostExit, pendingHostExit } = require("../src/hostexit.js");
  const userData = fs.mkdtempSync(path.join(os.tmpdir(), "hostexit-"));
  const logs = openLogs(userData);
  recordHostExit({ logs, userData, code: null, signal: "SIGKILL", startedAt: Date.now() });
  assert.match(fs.readFileSync(logs.shell.file, "utf8"), /host: died code=null signal=SIGKILL crash=\(none\)/);
  assert.equal(pendingHostExit(userData).signal, "SIGKILL");
});

test("the notice names where the logs are in both languages and shows how it ended", () => {
  const { hostExitNotice } = require("../src/hostexit.js");
  const en = hostExitNotice("en-US", { code: 2, signal: null }, "/logs");
  const zh = hostExitNotice("zh-CN", { code: 2, signal: null }, "/logs");
  assert.notEqual(en.message, zh.message);
  for (const text of [en, zh]) assert.match(text.detail, /\/logs/);
  assert.match(zh.message, /内核/);
  assert.match(en.detail, /code 2/);
  assert.match(zh.detail, /2/);
});

test("packaging: an rpm fpm spelled x86_64 leaves the build as the release arch", async () => {
  const arch = require("../packaging/arch.js");
  const hook = require("../packaging/canonical-artifact-names.js");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rpm-name-"));
  const deb = path.join(dir, "ReasonixStudio-linux-amd64.deb");
  const rpm = path.join(dir, "ReasonixStudio-linux-x86_64.rpm");
  fs.writeFileSync(deb, "d");
  fs.writeFileSync(rpm, "r");
  arch.record(deb, "x64");
  arch.record(rpm, "x64");
  const out = await hook({ artifactPaths: [deb, rpm] });
  assert.deepEqual(out.map((f) => path.basename(f)), ["ReasonixStudio-linux-amd64.deb", "ReasonixStudio-linux-amd64.rpm"]);
  assert.ok(fs.existsSync(path.join(dir, "ReasonixStudio-linux-amd64.rpm")));
  assert.ok(!fs.existsSync(rpm));
});

test("packaging: a name still carrying an fpm spelling is refused", async () => {
  const arch = require("../packaging/arch.js");
  const hook = require("../packaging/canonical-artifact-names.js");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rpm-name-"));
  const stray = path.join(dir, "ReasonixStudio-linux-aarch64.rpm");
  fs.writeFileSync(stray, "r");
  arch.record(stray, "x64");
  await assert.rejects(hook({ artifactPaths: [stray] }), /aarch64/);
});

test("packaging: a Windows installer named for x64 is renamed to amd64", async () => {
  const arch = require("../packaging/arch.js");
  const hook = require("../packaging/canonical-artifact-names.js");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "win-name-"));
  const installer = path.join(dir, "ReasonixStudio-windows-x64-installer.exe");
  const zip = path.join(dir, "ReasonixStudio-windows-x64.zip");
  for (const f of [installer, zip]) {
    fs.writeFileSync(f, "x");
    arch.record(f, "x64");
  }
  const out = await hook({ artifactPaths: [installer, zip] });
  assert.deepEqual(
    out.map((f) => path.basename(f)),
    ["ReasonixStudio-windows-amd64-installer.exe", "ReasonixStudio-windows-amd64.zip"],
  );
});

const { stateFile, loadState, saveState, placement, track } = require("../src/windowstate.js");

const AREA = { x: 0, y: 0, width: 1920, height: 1040 };
const SECOND = { x: 1920, y: 0, width: 1280, height: 1000 };
const MIN = { width: 760, height: 480 };
const FALLBACK = { width: 1440, height: 900 };
const open = (saved, areas = [AREA, SECOND]) => placement({ saved, areas, primary: areas[0], fallback: FALLBACK, min: MIN });

test("window state: only a whole rectangle is read back, and it survives a round trip", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "winstate-"));
  const file = stateFile(dir);
  assert.equal(loadState(file), null);
  fs.writeFileSync(file, "{not json");
  assert.equal(loadState(file), null);
  for (const bad of [{ width: 0, height: 500 }, { width: "900", height: 500 }, { width: 900.5, height: 500 }, [], null]) {
    fs.writeFileSync(file, JSON.stringify(bad));
    assert.equal(loadState(file), null, JSON.stringify(bad));
  }
  saveState(file, { x: 40, y: 60, width: 1000, height: 700, maximized: true });
  assert.deepEqual(loadState(file), { x: 40, y: 60, width: 1000, height: 700, maximized: true });
  fs.writeFileSync(file, JSON.stringify({ width: 1000, height: 700 }));
  assert.deepEqual(loadState(file), { width: 1000, height: 700, maximized: false });
});

test("window state: a first launch opens at the default size and a saved rectangle is reopened as it was", () => {
  assert.deepEqual(open(null), { ...FALLBACK, maximized: false });
  const saved = { x: 300, y: 120, width: 1100, height: 760, maximized: false };
  assert.deepEqual(open(saved), saved);
  assert.deepEqual(open({ ...saved, x: 2100, y: 100 }), { ...saved, x: 2100, y: 100 });
});

test("window state: a rectangle no display shows keeps its size and loses its place", () => {
  const gone = { x: 2400, y: 100, width: 1100, height: 760, maximized: false };
  assert.deepEqual(open(gone, [AREA]), { width: 1100, height: 760, maximized: false });
  const above = { x: 300, y: -500, width: 1100, height: 760, maximized: false };
  assert.deepEqual(open(above, [AREA]), { width: 1100, height: 760, maximized: false });
  const huge = { x: 5000, y: 0, width: 4000, height: 3000, maximized: true };
  assert.deepEqual(open(huge, [AREA]), { width: 1920, height: 1040, maximized: true });
});

test("window state: a saved size is raised to the layout's floor", () => {
  const small = open({ x: 10, y: 10, width: 300, height: 200, maximized: false });
  assert.deepEqual([small.width, small.height], [760, 480]);
});

function fakeWindow(normal) {
  const win = new EventEmitter();
  Object.assign(win, { maximized: false, destroyed: false, normal });
  win.getNormalBounds = () => win.normal;
  win.isMaximized = () => win.maximized;
  win.isDestroyed = () => win.destroyed;
  win.minimized = false;
  win.fullscreen = false;
  win.isMinimized = () => win.minimized;
  win.isFullScreen = () => win.fullscreen;
  return win;
}

test("window state: moves are written once they settle, a close is written at once, and a maximised window keeps its normal bounds", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "winstate-"));
  const file = stateFile(dir);
  const timers = [];
  const win = fakeWindow({ x: 10, y: 20, width: 900, height: 600 });
  track(win, file, { timer: (fn) => timers.push(fn) - 1, untimer: (id) => (timers[id] = () => {}) });
  win.emit("resize");
  win.emit("move");
  win.emit("resize");
  assert.equal(loadState(file), null);
  timers.at(-1)();
  assert.deepEqual(loadState(file), { x: 10, y: 20, width: 900, height: 600, maximized: false });
  win.normal = { x: 50, y: 60, width: 1000, height: 650 };
  win.maximized = true;
  win.emit("close");
  assert.deepEqual(loadState(file), { x: 50, y: 60, width: 1000, height: 650, maximized: true });
  win.destroyed = true;
  win.normal = { x: 1, y: 1, width: 5, height: 5 };
  win.emit("move");
  timers.at(-1)();
  assert.equal(loadState(file).width, 1000);
});

test("window state: a minimised or full-screen close keeps the last settled record", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "winstate-"));
  const file = stateFile(dir);
  const win = fakeWindow({ x: 10, y: 20, width: 900, height: 600 });
  track(win, file, { timer: () => 0, untimer: () => {} });
  win.emit("close");
  const kept = loadState(file);
  win.normal = { x: 0, y: 0, width: 1920, height: 1040 };
  win.minimized = true;
  win.emit("close");
  assert.deepEqual(loadState(file), kept);
  win.minimized = false;
  win.fullscreen = true;
  win.emit("close");
  assert.deepEqual(loadState(file), kept);
});

test("window state: a saved size is cut to the display it opens on and the window is moved in", () => {
  const big = { x: 1700, y: 600, width: 1400, height: 900, maximized: false };
  const out = open(big, [AREA]);
  assert.deepEqual([out.width, out.height], [1400, 900].map((v, i) => Math.min(v, [1920, 1040][i])));
  assert.ok(out.x + out.width <= 1920 && out.y + out.height <= 1040, JSON.stringify(out));
  const small = { x: 100, y: 100, width: 1800, height: 1000, maximized: false };
  const cut = open(small, [{ x: 0, y: 0, width: 1366, height: 728 }]);
  assert.deepEqual(cut, { x: 0, y: 0, width: 1366, height: 728, maximized: false });
});

test("kernel acts: show brings the window forward, relaunch arms the restart, quit ends, anything else is ignored", () => {
  const { actHandler } = require("../src/acts.js");
  const calls = [];
  const handle = actHandler({
    app: { relaunch: () => calls.push("relaunch"), quit: () => calls.push("quit") },
    showWindow: () => calls.push("show"),
  });
  for (const act of ["show", "relaunch", "quit", "unknown"]) handle(act);
  assert.deepEqual(calls, ["show", "relaunch", "quit"]);
});

test("kernel acts: a click that reaches the shell is written to its log, an unknown act is not", () => {
  const { actHandler } = require("../src/acts.js");
  const lines = [];
  const handle = actHandler({ app: { relaunch() {}, quit() {} }, showWindow() {}, log: (line) => lines.push(line) });
  for (const act of ["show", "unknown"]) handle(act);
  assert.deepEqual(lines, ["kernel act: show"]);
});

test("raise: answers whether the window is focused once it has tried", () => {
  const { raise } = require("../src/raise.js");
  const make = (grants) => {
    let focused = false;
    return { show() {}, focus: () => { if (grants()) focused = true; }, isFocused: () => focused, setAlwaysOnTop() {} };
  };
  assert.equal(raise(make(() => true), "win32"), true);
  assert.equal(raise(make(() => false), "win32"), false);
});

test("raise: a window the platform would not focus is lifted once and returned to normal stacking", () => {
  const { raise } = require("../src/raise.js");
  const run = (platform, focusedAfterFocus) => {
    const calls = [];
    let focused = false;
    const win = {
      show: () => calls.push("show"),
      focus: () => { calls.push("focus"); if (focusedAfterFocus) focused = true; },
      isFocused: () => focused,
      setAlwaysOnTop: (on) => calls.push(on ? "top" : "untop"),
    };
    raise(win, platform);
    return calls;
  };
  assert.deepEqual(run("win32", true), ["show", "focus"]);
  assert.deepEqual(run("win32", false), ["show", "focus", "top", "focus", "untop"]);
  assert.deepEqual(run("darwin", false), ["show", "focus"]);
  assert.deepEqual(run("linux", false), ["show", "focus"]);
});

test("tray: the menu is replaced only when what it says changes, so an open menu is not torn down by the poll", async () => {
  const { installTray } = require("../src/tray.js");
  const menus = [];
  let tips = 0;
  class FakeTray {
    on() {}
    setImage() {}
    setToolTip() { tips++; }
    setContextMenu(menu) { menus.push(menu); }
    destroy() {}
  }
  const electron = {
    Menu: { buildFromTemplate: (template) => ({ template }) },
    Tray: FakeTray,
    nativeImage: { createFromPath: () => ({ isEmpty: () => false }), createFromBuffer: () => ({ isEmpty: () => false }) },
    screen: { getPrimaryDisplay: () => ({ scaleFactor: 1 }), on() {}, removeListener() {} },
  };
  let fold = { line: "1 running", labels: { open: "Open", quit: "Quit", closeToTray: "Keep running" } };
  const host = { trayPrefs: async () => ({ closeToTray: false }), trayState: async () => fold, setTrayPrefs: async () => ({ closeToTray: true }) };
  const tray = installTray(host, { onOpen() {}, onQuit() {} }, electron);
  await tray.refresh();
  await tray.refresh();
  await tray.refresh();
  assert.equal(menus.length, 1, "three identical reads rebuilt the menu");
  assert.equal(tips, 1);
  fold = { ...fold, line: "2 running" };
  await tray.refresh();
  assert.equal(menus.length, 2);
  assert.equal(menus[1].template.find((item) => item.enabled === false).label, "2 running");
  tray.close();
});

test("tray: a refused close-to-tray save redraws the checkbox, and a menu that failed to draw is tried again", async () => {
  const { installTray } = require("../src/tray.js");
  const menus = [];
  let failNext = false;
  class FakeTray {
    on() {}
    setImage() {}
    setToolTip() {}
    setContextMenu(menu) { if (failNext) { failNext = false; throw new Error("indicator busy"); } menus.push(menu); }
    destroy() {}
  }
  const electron = {
    Menu: { buildFromTemplate: (template) => ({ template }) },
    Tray: FakeTray,
    nativeImage: { createFromPath: () => ({ isEmpty: () => false }), createFromBuffer: () => ({ isEmpty: () => false }) },
    screen: { getPrimaryDisplay: () => ({ scaleFactor: 1 }), on() {}, removeListener() {} },
  };
  const fold = { line: "idle", labels: { open: "Open", quit: "Quit", closeToTray: "Keep running" } };
  const host = { trayPrefs: async () => ({ closeToTray: false }), trayState: async () => fold, setTrayPrefs: async () => null };
  const tray = installTray(host, { onOpen() {}, onQuit() {} }, electron);
  failNext = true;
  await assert.rejects(tray.refresh());
  await tray.refresh();
  assert.equal(menus.length, 1, "a menu whose draw threw was never retried");
  const toggle = menus[0].template.find((item) => item.type === "checkbox");
  await toggle.click();
  assert.equal(menus.length, 2, "a refused save left the flipped checkbox on screen");
  assert.equal(menus[1].template.find((item) => item.type === "checkbox").checked, false);
  tray.close();
});

test("revealLogs opens only the folder the shell itself names, and reports why it could not", async () => {
  const opened = [];
  const shell = { openPath: async (target) => (opened.push(target), "") };
  assert.equal(await revealLogs(shell, "/user/logs"), null);
  assert.deepEqual(opened, ["/user/logs"]);
  const refusing = { openPath: async () => "no application is associated with this folder" };
  assert.deepEqual(await revealLogs(refusing, "/user/logs"), { error: "no application is associated with this folder" });
  assert.deepEqual(await revealLogs(shell, ""), { error: "the logs folder is unknown" });
});

// The built-in browser drew a white page after switching away from a tab and
// back until the window was resized: a parked view moved onscreen with
// setBounds alone does not repaint. Bringing it back attaches it again.
test("a parked page is attached again when it is shown, and a page already on screen is left alone", () => {
  const Module = require("node:module");
  const load = Module._load;
  Module._load = function (request, ...rest) {
    if (request === "electron") return { WebContentsView: class {}, session: { fromPartition: () => ({}) } };
    return load.call(this, request, ...rest);
  };
  let BrowserViews;
  try {
    ({ BrowserViews } = require("../src/browserviews.js"));
  } finally {
    Module._load = load;
  }
  const calls = [];
  const win = {
    on() {},
    isDestroyed: () => false,
    isFocused: () => false,
    webContents: Object.assign(new EventEmitter(), { isFocused: () => false }),
    getContentBounds: () => ({ width: 1200, height: 800 }),
    contentView: { addChildView: () => calls.push("add"), removeChildView: () => calls.push("remove") },
  };
  const views = new BrowserViews({ win, kernelOrigin: "http://127.0.0.1:1" });
  const view = { setBounds: (b) => calls.push("bounds " + b.x), webContents: { isDestroyed: () => false } };
  views.entries.set("t1", { view, at: null, attached: true });
  const rect = { x: 10, y: 20, width: 600, height: 400 };

  views.show("t1", rect);
  assert.deepEqual(calls, ["remove", "add", "bounds 10"]);
  calls.length = 0;
  views.show("t1", rect);
  assert.deepEqual(calls, [], "the same rectangle on a page already shown does no work");
  views.hide();
  calls.length = 0;
  views.show("t1", rect);
  assert.deepEqual(calls, ["remove", "add", "bounds 10"], "a page put away and shown again is attached again");
});

test("a page laid out wider than the pane is scaled to fit it, and keeps the viewport the agent asked for", async () => {
  const Module = require("node:module");
  const load = Module._load;
  Module._load = function (request, ...rest) {
    if (request === "electron") return { WebContentsView: class {}, session: { fromPartition: () => ({}) } };
    return load.call(this, request, ...rest);
  };
  let BrowserViews;
  try {
    ({ BrowserViews } = require("../src/browserviews.js"));
  } finally {
    Module._load = load;
  }
  const win = {
    on() {},
    isDestroyed: () => false,
    isFocused: () => false,
    webContents: Object.assign(new EventEmitter(), { isFocused: () => false }),
    getContentBounds: () => ({ width: 1200, height: 800 }),
    contentView: { addChildView() {}, removeChildView() {} },
  };
  const sent = [];
  const view = {
    setBounds() {},
    webContents: { isDestroyed: () => false, debugger: { sendCommand: async (method, params) => sent.push([method, params]) } },
  };
  const views = new BrowserViews({ win, kernelOrigin: "http://127.0.0.1:1" });
  const entry = { view, at: null, attached: true };
  views.entries.set("t1", entry);
  views.keepKeyboard = () => {};
  entry.override = { width: 1440, height: 1000, deviceScaleFactor: 0, mobile: false };
  views.show("t1", { x: 0, y: 0, width: 720, height: 800 });
  assert.equal(sent.length, 1);
  assert.equal(sent[0][1].scale, 0.5, "a 1440 wide layout in a 720 wide pane is halved");
  assert.equal(sent[0][1].width, 1440, "the layout viewport is still the one the agent asked for");

  views.show("t1", { x: 0, y: 0, width: 720, height: 800 });
  assert.equal(sent.length, 1, "the same pane sends nothing again");

  views.show("t1", { x: 0, y: 0, width: 360, height: 800 });
  assert.equal(sent.at(-1)[1].scale, 0.25, "a narrower pane scales further");

  views.show("t1", { x: 0, y: 0, width: 2000, height: 1200 });
  assert.equal(sent.at(-1)[1].scale, 1, "a pane larger than the layout never enlarges it");

  const settled = sent.length;
  views.fit(entry);
  assert.equal(sent.length, settled, "an unchanged scale is not sent again");
  views.fit(entry, true);
  assert.equal(sent.length, settled + 1, "a capture that dropped the scale gets it back");
  assert.equal(sent.at(-1)[1].scale, 1);
  entry.override = undefined;
  const before = sent.length;
  views.show("t1", { x: 0, y: 0, width: 500, height: 800 });
  assert.equal(sent.length, before, "with no override there is nothing to fit");
});

test("a page given its own viewport is fitted whole into its panel, centred, and never enlarged", () => {
  const Module = require("node:module");
  const load = Module._load;
  Module._load = function (request, ...rest) {
    if (request === "electron") return { WebContentsView: class {}, session: { fromPartition: () => ({}) } };
    return load.call(this, request, ...rest);
  };
  let placement;
  try {
    ({ placement } = require("../src/browserviews.js"));
  } finally {
    Module._load = load;
  }
  const panel = { x: 380, y: 60, width: 560, height: 620 };
  assert.deepEqual(placement(panel, null), { at: panel, scale: 1 }, "a page without a viewport of its own fills the panel");
  for (const none of [{ width: 0, height: 0 }, { width: 0, height: 600 }, { width: 390, height: 0 }, { width: NaN, height: 600 }, { width: 390, height: Infinity }, { width: -1, height: 600 }]) {
    assert.deepEqual(placement(panel, none), { at: panel, scale: 1 }, `${JSON.stringify(none)} is no viewport at all`);
  }
  const wide = placement(panel, { width: 1280, height: 900 });
  assert.equal(wide.scale, 0.4375);
  assert.deepEqual(wide.at, { x: 380, y: 173, width: 560, height: 394 });
  const phone = placement(panel, { width: 390, height: 844 });
  assert.ok(phone.scale < 1 && phone.at.height <= panel.height && phone.at.width <= panel.width);
  assert.deepEqual(placement({ x: 20, y: 20, width: 1400, height: 1000 }, { width: 1280, height: 900 }), { at: { x: 80, y: 70, width: 1280, height: 900 }, scale: 1 });
});

test("input that carries a point is scaled with the page or refused, never sent to the wrong place", () => {
  const Module = require("node:module");
  const load = Module._load;
  Module._load = function (request, ...rest) {
    if (request === "electron") return { WebContentsView: class {}, session: { fromPartition: () => ({}) } };
    return load.call(this, request, ...rest);
  };
  let scaleInput;
  try {
    ({ scaleInput } = require("../src/browserviews.js"));
  } finally {
    Module._load = load;
  }
  const mouse = { type: "mousePressed", x: 1200, y: 800, button: "left" };
  assert.equal(scaleInput("Input.dispatchMouseEvent", mouse, 1), mouse, "an unscaled page gets the point as it is");
  assert.deepEqual(scaleInput("Input.dispatchMouseEvent", mouse, 0.5), { type: "mousePressed", x: 600, y: 400, button: "left" });
  for (const method of ["Input.dispatchTouchEvent", "Input.dispatchDragEvent", "Input.emulateTouchFromMouseEvent", "Input.synthesizeTapGesture", "Input.synthesizeScrollGesture", "Input.synthesizePinchGesture"]) {
    const params = { x: 10, y: 20, touchPoints: [{ x: 10, y: 20 }] };
    assert.equal(scaleInput(method, params, 1), params, `${method} passes on an unscaled page`);
    assert.throws(() => scaleInput(method, params, 0.5), { code: "input_unscaled" }, `${method} must not reach a scaled page unscaled`);
  }
  const key = { type: "keyDown", key: "a" };
  assert.equal(scaleInput("Input.dispatchKeyEvent", key, 0.5), key, "keys carry no point");
  assert.equal(scaleInput("Input.insertText", { text: "x" }, 0.5).text, "x");
});

// The pages' idle sleep. Electron is faked the way the suites above fake it,
// but richly enough that create() itself runs: a page that never went through
// create() cannot be watched going to sleep. The module is dropped from the
// require cache first, because the suites above have already loaded it against
// their own bare mock, and a cached module never loads again.
function sleepingViews({ sleepAfterMs = 5, sweepMs = 5, maxAwake = 6, capturePage, failSpawns = 0, hang = null } = {}) {
  const { EventEmitter } = require("node:events");
  const created = [];
  const loads = [];
  class FakeContents extends EventEmitter {
    constructor() {
      super();
      this.destroyed = false;
      this.url = "about:blank";
      this.commands = [];
      // What maySleep reads off a live page; a test flips these on the fake
      // the helper's `created` array already hands back.
      this.audible = false;
      this.devTools = false;
      const contents = this;
      this.debugger = {
        attach() {},
        on(event, fn) {
          contents.on("debugger:" + event, fn);
        },
        sendCommand: async (method, params) => {
          contents.commands.push([method, params]);
          if (hang === method) return new Promise(() => {});
          return { data: "cG5n" };
        },
      };
    }
    isCurrentlyAudible() {
      return this.audible;
    }
    isDevToolsOpened() {
      return this.devTools;
    }
    setWindowOpenHandler(handler) {
      this.openHandler = handler;
    }
    loadURL(url) {
      this.url = url;
      loads.push(url);
      return Promise.resolve();
    }
    getURL() {
      return this.url;
    }
    isDestroyed() {
      return this.destroyed;
    }
    close() {
      if (this.destroyed) return;
      this.destroyed = true;
      this.emit("destroyed");
    }
    capturePage() {
      return capturePage ? capturePage() : Promise.resolve({ isEmpty: () => false, toJPEG: () => Buffer.from("jpeg") });
    }
  }
  class FakeView {
    constructor({ webPreferences }) {
      if (failSpawns > 0) {
        failSpawns--;
        throw new Error("the view would not build");
      }
      this.webPreferences = webPreferences;
      this.webContents = new FakeContents();
      created.push(this);
    }
    setBorderRadius() {}
    setBounds() {}
  }
  const ses = Object.assign(new EventEmitter(), {
    setPermissionRequestHandler() {},
    setPermissionCheckHandler() {},
    webRequest: { onBeforeRequest() {} },
  });
  const Module = require("node:module");
  const load = Module._load;
  Module._load = function (request, ...rest) {
    if (request === "electron") return { WebContentsView: FakeView, session: { fromPartition: () => ses } };
    return load.call(this, request, ...rest);
  };
  let BrowserViews;
  try {
    delete require.cache[require.resolve("../src/browserviews.js")];
    ({ BrowserViews } = require("../src/browserviews.js"));
  } finally {
    Module._load = load;
  }
  const win = {
    on() {},
    isDestroyed: () => false,
    isFocused: () => false,
    webContents: Object.assign(new EventEmitter(), { isFocused: () => false }),
    getContentBounds: () => ({ width: 1200, height: 800 }),
    contentView: { addChildView() {}, removeChildView() {} },
  };
  const views = new BrowserViews({ win, kernelOrigin: "http://127.0.0.1:1", sleepAfterMs, sweepMs, maxAwake });
  views.keepKeyboard = () => {};
  const page = () => {
    const events = { closed: 0, popups: [], downloads: [] };
    const handle = views.create({
      partition: "p" + created.length,
      url: "https://page.test/",
      onEvent: () => {},
      onClosed: () => events.closed++,
      onPopup: (to) => events.popups.push(to),
      onDownload: (download) => events.downloads.push(download),
    });
    return { handle, events };
  };
  return { views, created, loads, page };
}

test("an idle page nobody is looking at is slept by the sweep and lives on as a tombstone", async (t) => {
  const { views, created, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle, events } = page();
  await later(40);
  assert.deepEqual(views.asleep(handle.targetId), { url: "https://page.test/" }, "the idle page is asleep");
  assert.ok(views.entries.has(handle.targetId), "the entry survives as a tombstone");
  assert.equal(views.entries.get(handle.targetId).view, null);
  assert.match(views.entries.get(handle.targetId).slept.picture, /^data:image\/jpeg;base64,/);
  assert.equal(created.length, 1, "no view was built for a sleep");
  assert.equal(events.closed, 0, "the kernel is not told of a sleep");
});

test("a send to a slept page wakes it, and the call lands on the page it comes back as", async (t) => {
  const { views, created, loads, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle, events } = page();
  await later(40);
  assert.ok(views.asleep(handle.targetId));
  const built = created.length;
  await handle.send("Page.captureScreenshot", { format: "png" });
  assert.equal(created.length, built + 1, "waking built the view again");
  assert.equal(views.asleep(handle.targetId), null, "the page is awake");
  assert.ok(views.entries.has(handle.targetId), "the targetId is the one the kernel holds");
  assert.equal(loads.length, 2, "the load was kicked off again");
  assert.equal(events.closed, 0, "a wake is not a death");
  const contents = views.entries.get(handle.targetId).view.webContents;
  assert.deepEqual(contents.commands.at(-1), ["Page.captureScreenshot", { format: "png" }]);
});

test("closing a slept page buries it, and the kernel hears of it exactly once", async (t) => {
  const { views, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle, events } = page();
  await later(40);
  assert.ok(views.asleep(handle.targetId));
  handle.close();
  assert.equal(events.closed, 1, "the kernel is told once");
  handle.close();
  assert.equal(events.closed, 1, "and only once");
  assert.equal(views.entries.has(handle.targetId), false);
  assert.equal(views.asleep(handle.targetId), null);
});

test("a third page costs the least recently touched one its renderer, never the one on screen", async (t) => {
  const { views, page } = sleepingViews({ sleepAfterMs: 60_000, sweepMs: 60_000, maxAwake: 2 });
  t.after(() => views.closeAll());
  const first = page();
  const second = page();
  views.show(first.handle.targetId, { x: 0, y: 0, width: 600, height: 400 });
  // The page on screen is made the oldest on purpose: the cap must go past it.
  views.entries.get(first.handle.targetId).lastTouch = 1;
  const third = page();
  await later(20);
  assert.ok(views.asleep(second.handle.targetId), "the second page was slept to make room");
  assert.equal(views.asleep(first.handle.targetId), null, "the page on screen keeps its renderer");
  assert.equal(views.asleep(third.handle.targetId), null, "the page just created is awake");
});

test("with the setting off, the cap sleeps no page either", async (t) => {
  const off = sleepingViews({ sleepAfterMs: Infinity, sweepMs: 5, maxAwake: 2 });
  t.after(() => off.views.closeAll());
  const one = off.page();
  const two = off.page();
  const three = off.page();
  await later(20);
  assert.equal(off.views.asleep(one.handle.targetId), null, "off means nothing sleeps: page one");
  assert.equal(off.views.asleep(two.handle.targetId), null, "page two");
  assert.equal(off.views.asleep(three.handle.targetId), null, "page three, though the cap is passed");
  // The same cap with the setting on still sleeps the least recently touched.
  const on = sleepingViews({ sleepAfterMs: 60_000, sweepMs: 60_000, maxAwake: 2 });
  t.after(() => on.views.closeAll());
  const oldest = on.page();
  on.page();
  on.page();
  await later(20);
  assert.ok(on.views.asleep(oldest.handle.targetId), "on, the least recently touched page sleeps");
});

test("the sweep never puts the page the person is looking at to sleep", async (t) => {
  const { views, page } = sleepingViews();
  t.after(() => views.closeAll());
  const shown = page();
  views.show(shown.handle.targetId, { x: 0, y: 0, width: 600, height: 400 });
  views.entries.get(shown.handle.targetId).lastTouch = 1;
  await later(40);
  assert.equal(views.asleep(shown.handle.targetId), null, "the shown page is still awake");
  assert.ok(views.entries.get(shown.handle.targetId).view, "and its view is still there");
});

test("closeAll takes the sweep's timer with it, and a suite full of these does not hang", async () => {
  mock.timers.enable({ apis: ["setTimeout", "setInterval", "Date"] });
  try {
    const { views, page } = sleepingViews({ sleepAfterMs: 10, sweepMs: 10 });
    page();
    views.closeAll();
    assert.equal(views.sweeper, null, "the timer is gone");
    views.closeAll();
    assert.equal(views.sweeper, null, "and a second closeAll does not mind");
    // A page made after the closeAll has nothing left to sleep it: had the
    // timer survived, this tick would put it to sleep.
    const { handle } = page();
    mock.timers.tick(60_000);
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(views.asleep(handle.targetId), null, "no sweep fired after closeAll");
  } finally {
    mock.timers.reset();
  }
});

test("a wake replays the page's domain enables before the next command lands", async (t) => {
  const { views, created, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle } = page();
  // What the kernel says once at attach: the domains the page's events come from.
  await handle.send("Page.enable", {});
  await handle.send("Page.setLifecycleEventsEnabled", { enabled: true });
  // A domain switched on mid-session joins the set; one switched off leaves it.
  await handle.send("Network.enable", {});
  await handle.send("Network.disable", {});
  await later(40);
  assert.ok(views.asleep(handle.targetId));
  await handle.send("Runtime.evaluate", { expression: "1" });
  assert.equal(created.length, 2, "the wake built a new view");
  const said = created[1].webContents.commands.map(([method]) => method);
  assert.deepEqual(said, ["Page.enable", "Page.setLifecycleEventsEnabled", "Runtime.evaluate"], "the enables are said again, before the next command — and a domain switched off is not switched back on");
  assert.deepEqual(created[1].webContents.commands[1][1], { enabled: true }, "the parameters the kernel chose are the ones replayed");
  assert.equal(created[0].webContents.commands.length, 4, "nothing was replayed on the session that died");
  assert.equal(views.asleep(handle.targetId), null);
});

test("a wake yields no room: the page asked for outranks the pages left alone", async (t) => {
  const { views, page } = sleepingViews({ maxAwake: 2, sleepAfterMs: 60, sweepMs: 5 });
  t.after(() => views.closeAll());
  const first = page();
  const second = page();
  await later(90);
  assert.ok(views.asleep(first.handle.targetId), "the pages slept once they idled");
  // The kernel is using the second page when it asks for the first: the cap is
  // a brake on creation, not a quota held against a page the kernel wants.
  await second.handle.send("Runtime.evaluate", { expression: "1" });
  await first.handle.send("Runtime.evaluate", { expression: "1" });
  assert.equal(views.asleep(first.handle.targetId), null, "the asked-for page woke");
  assert.equal(views.asleep(second.handle.targetId), null, "the page in use was not slept to make room");
  assert.ok(views.entries.get(second.handle.targetId).view, "the page in use kept its renderer");
});

test("a command the sleep outlives is failed, not left hanging", async (t) => {
  const { views, page } = sleepingViews({ hang: "Runtime.evaluate" });
  t.after(() => views.closeAll());
  const { handle } = page();
  // A session that dies never settles its open calls on its own; the kernel
  // hears nothing of a sleep, so the sleep itself must answer them. The
  // failure is held from the moment it is asked, so an early rejection is
  // never an unhandled one.
  const asked = handle.send("Runtime.evaluate", { expression: "1" });
  const held = asked.then(
    () => "resolved",
    (err) => err,
  );
  await later(40);
  assert.ok(views.asleep(handle.targetId));
  const outcome = await held;
  assert.match(String(outcome && outcome.message), /went to sleep/);
});

test("a navigation that begins while the picture is taken keeps the page awake", async (t) => {
  let release;
  const held = new Promise((resolve) => { release = resolve; });
  let taken = 0;
  const { views, page } = sleepingViews({ capturePage: () => { taken++; return held; } });
  t.after(() => views.closeAll());
  const { handle } = page();
  const entry = views.entries.get(handle.targetId);
  for (let waited = 0; waited < 40 && taken === 0; waited++) await later(5);
  assert.ok(taken > 0, "the sweep is mid-capture, the picture never coming");
  // A navigation starts the only way leave() is reached for one: the page is
  // attached and the app page holds the keyboard, so it is held out of the
  // window with a settle pending.
  views.keyboard.owner = "app";
  entry.attached = true;
  views.leave(entry);
  assert.ok(entry.returning, "the settle is pending");
  release({ isEmpty: () => false, toJPEG: () => Buffer.from("jpeg") });
  await later(20);
  assert.equal(views.asleep(handle.targetId), null, "the page was not slept mid-flight");
  assert.ok(views.entries.get(handle.targetId).view, "and its view is still there");
});

test("a spawn that fails leaves no entry behind", (t) => {
  const { views } = sleepingViews({ failSpawns: 1 });
  t.after(() => views.closeAll());
  let closed = 0;
  assert.throws(
    () => views.create({ partition: "p0", url: "https://page.test/", onEvent: () => {}, onClosed: () => closed++, onPopup: () => {}, onDownload: () => {} }),
    /would not build/,
  );
  assert.equal(closed, 0, "the kernel is told nothing of a target it never had");
  assert.equal(views.entries.size, 0, "and no entry is left behind");
});

test("the nineteenth tombstone costs the oldest its place, and the kernel hears it", async (t) => {
  const { views, page } = sleepingViews({ maxAwake: 32 });
  t.after(() => views.closeAll());
  const pages = [];
  for (let i = 0; i < 19; i++) pages.push(page());
  await later(60);
  assert.ok(views.asleep(pages[18].handle.targetId), "the pages slept");
  assert.ok(views.asleep(pages[1].handle.targetId), "the younger tombstones keep their place");
  assert.equal(pages[0].events.closed, 1, "the oldest tombstone was buried and the kernel told");
  assert.equal(views.asleep(pages[0].handle.targetId), null);
  assert.equal(views.entries.size, 18, "eighteen tombstones remain");
});

test("a woken page comes back at the viewport the agent set", async (t) => {
  const { views, created, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle } = page();
  const viewport = { width: 390, height: 844, deviceScaleFactor: 0, mobile: false };
  await handle.send("Emulation.setDeviceMetricsOverride", viewport);
  await later(40);
  assert.ok(views.asleep(handle.targetId));
  await handle.send("Runtime.evaluate", { expression: "1" });
  assert.equal(created.length, 2, "the wake built a new view");
  const said = created[1].webContents.commands.map(([method]) => method);
  assert.deepEqual(said, ["Emulation.setDeviceMetricsOverride", "Runtime.evaluate"], "the viewport is said again to the new session, before the command that woke the page");
  assert.deepEqual(created[1].webContents.commands[0][1], viewport, "the parameters the agent chose are the ones replayed");
});

test("a woken page's viewport is fitted into its panel again, at the scale it had before the sleep", async (t) => {
  const { views, created, page } = sleepingViews({ sleepAfterMs: 60_000, sweepMs: 60_000 });
  t.after(() => views.closeAll());
  const { handle } = page();
  await handle.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, mobile: true });
  const rect = { x: 0, y: 0, width: 195, height: 422 };
  views.show(handle.targetId, rect);
  const overrides = (which) => created[which].webContents.commands.filter(([m]) => m === "Emulation.setDeviceMetricsOverride");
  const fitted = overrides(0).at(-1);
  assert.equal(fitted[1].scale, 0.5, "the show fitted the phone viewport into the panel with a scale");
  views.hide();
  await views.sleep(views.entries.get(handle.targetId));
  assert.ok(views.asleep(handle.targetId), "the page slept");
  await handle.send("Runtime.evaluate", { expression: "1" });
  assert.equal(created.length, 2, "the wake built a new view");
  views.show(handle.targetId, rect);
  assert.deepEqual(overrides(1).at(-1), fitted, "the new session gets the same viewport and scale the show sent before the sleep");
});

test("a page with a dialog open does not sleep", async (t) => {
  const { views, created, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle } = page();
  const contents = created[0].webContents;
  // The kernel enables Page at attach, so the dialog events reach the shell
  // through the debugger's message stream.
  contents.emit("debugger:message", {}, "Page.javascriptDialogOpening", { message: "really?" }, undefined);
  await later(40);
  assert.equal(views.asleep(handle.targetId), null, "a page waiting on its answer is not slept under the question");
  contents.emit("debugger:message", {}, "Page.javascriptDialogClosed", { result: false }, undefined);
  await later(40);
  assert.ok(views.asleep(handle.targetId), "the answered dialog sleeps again");
});

test("a page awaiting a certificate decision does not sleep", async (t) => {
  const { views, created, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle } = page();
  const contents = created[0].webContents;
  contents.emit(
    "certificate-error",
    { preventDefault() {} },
    "https://page.test/",
    "ERR_CERT_AUTHORITY_INVALID",
    { fingerprint: "ff:ee", subjectName: "page.test", issuerName: "nobody", validExpiry: 0 },
    () => {},
    true,
  );
  await later(40);
  assert.ok(views.entries.get(handle.targetId).certificate, "the certificate waits on the person");
  assert.equal(views.asleep(handle.targetId), null, "the undecided certificate holds the page awake");
  views.entries.get(handle.targetId).certificate = null;
  await later(40);
  assert.ok(views.asleep(handle.targetId), "once decided, the page sleeps again");
});

test("a page playing audio does not sleep", async (t) => {
  const { views, created, page } = sleepingViews();
  t.after(() => views.closeAll());
  const { handle } = page();
  const contents = created[0].webContents;
  contents.audible = true;
  await later(40);
  assert.equal(views.asleep(handle.targetId), null, "the sounding page keeps its renderer");
  contents.audible = false;
  await later(40);
  assert.ok(views.asleep(handle.targetId), "the quieted page sleeps again");
});

test("rewind notice dismissal survives the desktop preference disk round trip", (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rewind-notice-prefs-"));
  const file = path.join(dir, "prefs.json");
  t.after(() => { fs.rmSync(file, { force: true }); fs.rmdirSync(dir); });
  const record = { "rx-rewind-notice-dismissed:tx-saved": "1" };
  savePrefs(file, record);
  assert.deepEqual(loadPrefs(file), record);
});

test("the page-sleep preference round-trips through the desktop preference file", (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "page-sleep-prefs-"));
  const file = path.join(dir, "prefs.json");
  t.after(() => { fs.rmSync(file, { force: true }); fs.rmdirSync(dir); });
  savePrefs(file, { "rx-page-sleep": "on" });
  assert.deepEqual(loadPrefs(file), { "rx-page-sleep": "on" });
  savePrefs(file, { "rx-lang": "en" });
  assert.deepEqual(loadPrefs(file), { "rx-lang": "en" }, "a key the page no longer holds is dropped");
});

test("the shell reads the sleep threshold from the preference", () => {
  assert.equal(pageSleepAfterMs({}), Infinity, "off until the person turns it on");
  assert.equal(pageSleepAfterMs({ "rx-page-sleep": "on" }), PAGE_SLEEP_MS);
  assert.equal(pageSleepAfterMs({ "rx-page-sleep": "garbage" }), Infinity);
});

const { setUnreadBadge } = require("../src/badge.js");

test("macOS marks and clears the application Dock badge", () => {
  const badges = [];
  const app = { dock: { setBadge: (text) => badges.push(text) } };
  setUnreadBadge(true, {}, { app }, "darwin");
  setUnreadBadge(false, {}, { app }, "darwin");
  assert.deepEqual(badges, ["•", ""]);
});

test("Windows uses a red dot overlay and removes it when all sessions are read", () => {
  const calls = [];
  const win = { setOverlayIcon: (...args) => calls.push(args) };
  let pixels;
  const nativeImage = { createFromBitmap: (buffer, size) => { pixels = buffer; assert.deepEqual(size, { width: 16, height: 16 }); return "dot"; } };
  setUnreadBadge(true, win, { nativeImage }, "win32");
  setUnreadBadge(false, win, { nativeImage }, "win32");
  const at = (x, y) => [...pixels.subarray((y * 16 + x) * 4, (y * 16 + x) * 4 + 4)];
  assert.equal(pixels[3], 0);
  assert.deepEqual(at(8, 8), [64, 64, 224, 255]);
  assert.equal(at(1, 8)[3], 0, "the dot stays well inside the 16px canvas");
  assert.deepEqual(at(12, 10), [255, 255, 255, 255], "a white ring separates it from the icon");
  assert.ok(at(13, 8)[3] > 0 && at(13, 8)[3] < 255, "the rim is smoothed, not jagged");
  assert.deepEqual(calls, [["dot", "Reasonix Studio"], [null, ""]]);
});

test("Linux clears its launcher badge without requiring Dock APIs", () => {
  const counts = [];
  const app = { setBadgeCount: (count) => counts.push(count) };
  setUnreadBadge(true, {}, { app }, "linux");
  setUnreadBadge(false, {}, { app }, "linux");
  assert.deepEqual(counts, [1, 0]);
});

test("CUA readiness refuses a different process, host or driver version", () => {
  const { accepts } = require("../src/cua");
  const result = { pid: 123, embedded: true, host_bundle_id: "io.reasonix.studio", driver_version: "0.34.1", contract_version: "0.8.0", tools_list_schema_version: "1", capability_version: "1", mcp_protocol_version: "2025-06-18" };
  assert.equal(accepts({ ok: true, result }, 123, cuaContract), true);
  for (const [key, value] of Object.entries({ pid: 124, embedded: false, host_bundle_id: "other", driver_version: "0.35.0", contract_version: "0.9.0", mcp_protocol_version: "2026-07-28" })) {
    assert.equal(accepts({ ok: true, result: { ...result, [key]: value } }, 123, cuaContract), false);
  }
  assert.equal(accepts({ ok: false, result }, 123), false);
});


test("CUA interruption waits for the owned service to stop", async () => {
  const { spawn } = require("node:child_process");
  const { stopProcess } = require("../src/cua");
  const child = spawn(process.execPath, ["-e", "process.stdin.resume();setInterval(()=>{},1000);process.stdin.on('end',()=>setTimeout(()=>process.exit(0),80))"], { stdio: ["pipe", "ignore", "ignore"], windowsHide: true });
  await new Promise((resolve, reject) => { child.once("spawn", resolve); child.once("error", reject); });
  await stopProcess(child, true);
  assert.ok(child.exitCode !== null || child.signalCode !== null);
});

test("CUA embedded control stops the old service before admitting a fresh one", { skip: process.platform === "win32" }, async () => {
  const { spawn } = require("node:child_process");
  const net = require("node:net");
  const { startEmbedded, metadata } = require("../src/cua");
  const fake = path.join(path.dirname(require("node:url").fileURLToPath(import.meta.url)), "cua-service-fixture.js");
  const owned = await startEmbedded(process.execPath, cuaContract, () => {}, (_exe, args, options) => spawn(process.execPath, [fake, ...args], options));
  try {
    const before = await metadata(owned.socket);
    const reply = await new Promise((resolve, reject) => {
      const peer = net.createConnection(owned.abortSocket);
      let text = "";
      peer.once("error", reject);
      peer.on("connect", () => peer.write('{"op":"abort"}\n'));
      peer.on("data", bytes => { text += bytes; if (text.includes("\n")) { peer.destroy(); resolve(JSON.parse(text)); } });
    });
    assert.equal(reply.ok, true);
    const after = await metadata(owned.socket);
    assert.notEqual(after.result.pid, before.result.pid);
    assert.throws(() => process.kill(before.result.pid, 0));
  } finally { await owned.stop(); }
  assert.equal(fs.existsSync(owned.socket), false);
  assert.equal(fs.existsSync(owned.abortSocket), false);
});

test("CUA waits for socket permissions on initial launch and abort restart", { skip: process.platform === "win32" }, async (t) => {
  const { spawn } = require("node:child_process");
  const net = require("node:net");
  const promises = require("node:fs/promises");
  const { startEmbedded, metadata, stopProcess } = require("../src/cua");
  const fake = path.join(path.dirname(require("node:url").fileURLToPath(import.meta.url)), "cua-service-fixture.js");
  const children = [];
  const observed = new Set();
  const ready = new Set();
  let owned;
  t.after(async () => {
    await owned?.stop();
    for (const child of children) await stopProcess(child);
  });
  const lstat = promises.lstat;
  t.mock.method(promises, "lstat", async (...args) => {
    const current = await lstat(...args);
    if (path.basename(args[0]) === "driver.sock" && (current.mode & 0o777) !== 0o600) {
      const child = children.at(-1);
      if (!observed.has(child.pid)) {
        observed.add(child.pid);
        child.send("secure");
      }
    } else if (path.basename(args[0]) === "driver.sock") {
      ready.add(children.at(-1).pid);
    }
    return current;
  });
  const connect = net.createConnection;
  t.mock.method(net, "createConnection", (...args) => {
    if (path.basename(args[0]) === "driver.sock") assert.ok(ready.has(children.at(-1).pid), "metadata was requested before private permissions were observed");
    return connect(...args);
  });
  owned = await startEmbedded(process.execPath, cuaContract, () => {}, (_exe, args, options) => {
    const child = spawn(process.execPath, [fake, ...args, "--defer-permissions"], { ...options, stdio: [...options.stdio, "ipc"] });
    children.push(child);
    return child;
  });
  const before = await metadata(owned.socket);
  const reply = await new Promise((resolve, reject) => {
    const peer = net.createConnection(owned.abortSocket);
    let text = "";
    peer.once("error", reject);
    peer.on("connect", () => peer.write('{"op":"abort"}\n'));
    peer.on("data", bytes => { text += bytes; if (text.includes("\n")) { peer.destroy(); resolve(JSON.parse(text)); } });
  });
  assert.equal(reply.ok, true);
  const after = await metadata(owned.socket);
  assert.notEqual(after.result.pid, before.result.pid);
  assert.deepEqual([...observed], [before.result.pid, after.result.pid]);
  assert.equal((await lstat(owned.socket)).mode & 0o777, 0o600);
  assert.throws(() => process.kill(before.result.pid, 0));
});

test("CUA permission readiness remains bounded without connecting to a non-private socket", { skip: process.platform === "win32" }, async (t) => {
  const { spawn } = require("node:child_process");
  const promises = require("node:fs/promises");
  const { startEmbedded, stopProcess } = require("../src/cua");
  const fake = path.join(path.dirname(require("node:url").fileURLToPath(import.meta.url)), "cua-service-fixture.js");
  let child;
  t.after(async () => { if (child) await stopProcess(child); });
  const now = Date.now();
  let expired = false;
  t.mock.method(Date, "now", () => now + (expired ? 10001 : 0));
  const lstat = promises.lstat;
  t.mock.method(promises, "lstat", async (...args) => {
    const current = await lstat(...args);
    if (path.basename(args[0]) === "driver.sock" && (current.mode & 0o777) !== 0o600) expired = true;
    return current;
  });
  await assert.rejects(startEmbedded(process.execPath, cuaContract, () => {}, (_exe, args, options) => {
    child = spawn(process.execPath, [fake, ...args, "--defer-permissions"], options);
    return child;
  }), /Embedded CUA failed readiness/);
  assert.equal(expired, true);
  assert.equal(child.exitCode, 0);
});

for (const kind of ["file", "symlink"]) {
  test(`CUA readiness rejects a ${kind} instead of waiting for permissions`, { skip: process.platform === "win32" }, async (t) => {
    const { spawn } = require("node:child_process");
    const { startEmbedded, stopProcess } = require("../src/cua");
    let child, socket;
    t.after(async () => {
      if (child) await stopProcess(child);
      if (socket) fs.rmSync(path.dirname(socket), { recursive: true, force: true });
    });
    const script = `
      const fs = require("node:fs");
      const socket = process.argv[1];
      if (process.argv[2] === "symlink") fs.symlinkSync("missing-target", socket);
      else fs.writeFileSync(socket, "", { mode: 0o600 });
      process.stdin.resume();
      process.stdin.on("end", () => process.exit(0));`;
    await assert.rejects(startEmbedded(process.execPath, cuaContract, () => {}, (_exe, args, options) => {
      socket = args[args.indexOf("--socket") + 1];
      child = spawn(process.execPath, ["-e", script, socket, kind], options);
      return child;
    }), /Invalid private CUA socket/);
    assert.equal(child.exitCode, 0);
  });
}


test("CUA repeated quit waits for owned driver cleanup", async () => {
  const { createQuitGate } = require("../src/cua");
  let resolveStop;
  const stopped = new Promise(resolve => { resolveStop = resolve; });
  let stops = 0, quits = 0, prevented = 0;
  const gate = createQuitGate(() => ({ stop: () => { stops++; return stopped; } }), () => { quits++; }, assert.fail);
  const event = { preventDefault: () => { prevented++; } };
  gate(event);
  gate(event);
  await Promise.resolve();
  assert.equal(stops, 1);
  assert.equal(prevented, 2);
  assert.equal(quits, 0);
  resolveStop();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(quits, 1);
  gate(event);
  assert.equal(prevented, 2);
});


test("CUA extraction handles ZIP archives when GNU tar precedes the Windows reader", { skip: process.platform !== "win32" || !fs.existsSync("C:/Program Files/Git/usr/bin/tar.exe") }, () => {
  const { spawnSync } = require("node:child_process");
  const tar = "C:/Program Files/Git/usr/bin/tar.exe";
  const stage = fs.mkdtempSync(path.join(os.tmpdir(), "reasonix-cua-extract-"));
  try {
    fs.writeFileSync(path.join(stage, "driver.zip"), Buffer.from("UEsDBBQAAAAIANomTV3uQOUFCQAAAAcAAAAOAAAAY3VhLWRyaXZlci5leGVLy6woKS1KBQBQSwECFAAUAAAACADaJk1d7kDlBQkAAAAHAAAADgAAAAAAAAAAAAAAgAEAAAAAY3VhLWRyaXZlci5leGVQSwUGAAAAAAEAAQA8AAAANQAAAAAA", "base64"));
    const target = path.join(stage, "extracted");
    fs.mkdirSync(target);
    const script = `require(${JSON.stringify(require.resolve("../packaging/install-cua-driver"))}).extract(process.argv[1], process.argv[2])`;
    const unpacked = spawnSync(process.execPath, ["-e", script, path.join(stage, "driver.zip"), target], { env: { ...process.env, PATH: path.dirname(tar) + path.delimiter + process.env.PATH } });
    assert.equal(unpacked.status, 0, unpacked.stderr.toString());
    assert.equal(fs.readFileSync(path.join(target, "cua-driver.exe"), "utf8"), "fixture");
  } finally {
    assert.ok(path.resolve(stage).startsWith(path.resolve(os.tmpdir()) + path.sep));
    fs.rmSync(stage, { recursive: true, force: true });
  }
});


test("CUA layout resolves one canonical contract in development and packaging", () => {
 const { computerDriverContract } = require("../src/layout");
 const dirname = path.dirname(require("node:url").fileURLToPath(import.meta.url));
 const source = computerDriverContract({ packaged: false, dirname: path.join(dirname, "../src") });
 assert.deepEqual(JSON.parse(fs.readFileSync(source, "utf8")), cuaContract);
 assert.equal(computerDriverContract({ packaged: true, resourcesPath: "/res" }), path.join("/res", "bin", "cua-driver.lock.json"));
});

const { captureHandler, MAX_WIDTH } = require("../src/capture.js");

function fakeImage(width, { empty = false } = {}) {
  return {
    isEmpty: () => empty,
    getSize: () => ({ width, height: 10 }),
    resize: (opts) => fakeImage(opts.width),
    toPNG: () => Buffer.from(`png:${width}`),
  };
}

test("the window capture answers only the studio window and takes no target", async () => {
  let asked = 0;
  const win = { webContents: { capturePage: async (...args) => (asked++, assert.equal(args.length, 0), fakeImage(800)) } };
  const handle = captureHandler((event) => (event.sender === "studio" ? win : null));
  const png = await handle({ sender: "studio" }, "some-other-window-id", { x: 0, y: 0 });
  assert.equal(Buffer.from(png).toString(), "png:800");
  assert.equal(await handle({ sender: "a browser page" }), null);
  assert.equal(asked, 1);
});

test("the window capture is scaled down past the upload-friendly width and refuses an empty frame", async () => {
  const wide = captureHandler(() => ({ webContents: { capturePage: async () => fakeImage(MAX_WIDTH * 2) } }));
  assert.equal(Buffer.from(await wide({})).toString(), `png:${MAX_WIDTH}`);
  const blank = captureHandler(() => ({ webContents: { capturePage: async () => fakeImage(0, { empty: true }) } }));
  assert.equal(await blank({}), null);
});

test("a folder panel that closes before anyone could read it is reported as never drawn on Linux", () => {
  const closed = { canceled: true, filePaths: [] };
  assert.equal(folderAnswer(closed, 20, "linux"), null);
  assert.equal(folderAnswer(closed, 5000, "linux"), "");
  assert.equal(folderAnswer(closed, 20, "darwin"), "");
  assert.equal(folderAnswer(closed, 20, "win32"), "");
  assert.equal(folderAnswer({ canceled: false, filePaths: ["/home/a/p"] }, 20, "linux"), "/home/a/p");
});

// Who keeps the keyboard while the agent drives a page.
const { KeyboardKeeper, PERSON_MS } = require("../src/keyboard.js");

function keyboardWindow({ appFocused = true, windowFocused = true } = {}) {
  const contents = Object.assign(new EventEmitter(), { focused: appFocused, focus() { this.focused = true; this.emit("focus"); }, isFocused() { return this.focused; } });
  const win = { webContents: contents, isDestroyed: () => false, isFocused: () => windowFocused };
  return { win, contents };
}
function keyboardPage() {
  return new EventEmitter();
}

const turn = () => new Promise((resolve) => setTimeout(resolve, 5));

test("a page the agent opens does not take the keyboard from the app page", async () => {
  const { win, contents } = keyboardWindow();
  const keeper = new KeyboardKeeper({ win });
  const page = keyboardPage();
  page.isFocused = () => true;
  keeper.watch(page);
  contents.focused = false;
  page.emit("focus");
  assert.equal(contents.focused, false, "not from inside the platform's own focus event");
  await turn();
  assert.equal(contents.focused, true);
  assert.equal(keeper.owner, "app");
});

test("a click or a key from the person into the page is theirs to have chosen", () => {
  for (const arrive of [(page) => page.emit("before-input-event", {}, { type: "keyDown" }), (page) => page.emit("input-event", {}, { type: "mouseDown" })]) {
    const { win, contents } = keyboardWindow();
    const keeper = new KeyboardKeeper({ win });
    const page = keyboardPage();
    keeper.watch(page);
    arrive(page);
    contents.focused = false;
    page.emit("focus");
    assert.equal(contents.focused, false);
    assert.equal(keeper.owner, "view");
  }
});

test("a click the agent itself sent over the protocol is not the person's", async () => {
  const { win, contents } = keyboardWindow();
  const keeper = new KeyboardKeeper({ win });
  const page = keyboardPage();
  page.isFocused = () => true;
  keeper.watch(page, () => true);
  page.emit("input-event", {}, { type: "mouseDown" });
  contents.focused = false;
  page.emit("focus");
  await turn();
  assert.equal(contents.focused, true);
});

test("the person's choice stands until they go back to the app page", async () => {
  const { win, contents } = keyboardWindow();
  const keeper = new KeyboardKeeper({ win });
  const page = keyboardPage();
  page.isFocused = () => true;
  keeper.watch(page);
  page.emit("input-event", {}, { type: "mouseDown" });
  page.emit("focus");
  assert.equal(keeper.owner, "view");
  contents.focused = false;
  page.emit("focus");
  await turn();
  assert.equal(contents.focused, false, "the agent navigating again does not pull it back");
  contents.focus();
  assert.equal(keeper.owner, "app");
});

test("nothing is taken back when the app page did not have the keyboard, or the window is not in front", async () => {
  let { win, contents } = keyboardWindow({ appFocused: false });
  let keeper = new KeyboardKeeper({ win });
  let page = keyboardPage();
  page.isFocused = () => true;
  keeper.watch(page);
  page.emit("focus");
  await turn();
  assert.equal(contents.focused, false);
  ({ win, contents } = keyboardWindow({ windowFocused: false }));
  keeper = new KeyboardKeeper({ win });
  page = keyboardPage();
  page.isFocused = () => true;
  keeper.watch(page);
  contents.focused = false;
  page.emit("focus");
  await turn();
  assert.equal(contents.focused, false);
  ({ win, contents } = keyboardWindow());
  keeper = new KeyboardKeeper({ win });
  page = keyboardPage();
  page.isFocused = () => true;
  keeper.watch(page);
  contents.focused = false;
  page.emit("focus");
  page.emit("before-input-event", {}, { type: "keyDown" });
  await turn();
  assert.equal(contents.focused, false, "a key from the person in the meantime is theirs");
});

test("reclaim hands the keyboard back only when nobody holds it and the app page owned it", () => {
  const { win, contents } = keyboardWindow();
  const keeper = new KeyboardKeeper({ win });
  contents.focused = false;
  keeper.reclaim();
  assert.equal(contents.focused, true);
  keeper.owner = "view";
  contents.focused = false;
  keeper.reclaim();
  assert.equal(contents.focused, false);
});

test("a person's click is only a click for a moment", () => {
  assert.ok(PERSON_MS > 0 && PERSON_MS < 2000);
});

function loadedViews() {
  const Module = require("node:module");
  const load = Module._load;
  Module._load = function (request, ...rest) {
    if (request === "electron") return { WebContentsView: class {}, session: { fromPartition: () => ({}) } };
    return load.call(this, request, ...rest);
  };
  let BrowserViews;
  let LEAVE_LIMIT_MS;
  try {
    ({ BrowserViews, LEAVE_LIMIT_MS } = require("../src/browserviews.js"));
  } finally {
    Module._load = load;
  }
  const calls = [];
  const win = {
    on() {},
    isDestroyed: () => false,
    isFocused: () => true,
    webContents: Object.assign(new EventEmitter(), { isFocused: () => true }),
    getContentBounds: () => ({ width: 1200, height: 800 }),
    contentView: { addChildView: () => calls.push("add"), removeChildView: () => calls.push("remove") },
  };
  const views = new BrowserViews({ win, kernelOrigin: "http://127.0.0.1:1" });
  const view = { setBounds: (b) => calls.push("bounds " + b.x), webContents: { isDestroyed: () => false } };
  return { views, view, calls, LEAVE_LIMIT_MS };
}

test("a page navigating while the app page holds the keyboard is held out of the window, then put back where it was", () => {
  const { views, view, calls } = loadedViews();
  const entry = { view, at: { x: 10, y: 20, width: 600, height: 400 }, attached: true };
  views.leave(entry);
  assert.deepEqual(calls, ["remove"]);
  assert.equal(entry.attached, false);
  views.settle(entry);
  assert.deepEqual(calls, ["remove", "add", "bounds 10"]);
  assert.equal(entry.attached, true);
  views.settle(entry);
  assert.equal(calls.length, 3, "settling a page that is already attached does nothing");
});

test("a parked page comes back parked", () => {
  const { views, view, calls } = loadedViews();
  const entry = { view, at: null, attached: true };
  views.leave(entry);
  calls.length = 0;
  views.settle(entry);
  assert.equal(calls[0], "add");
  assert.match(calls[1], /^bounds -?\d+$/);
  assert.equal(entry.at, null);
});

test("a page is left where it is when the person, not the app page, holds the keyboard, or when it is not attached", () => {
  const { views, view, calls } = loadedViews();
  const entry = { view, at: null, attached: true };
  views.keyboard.owner = "view";
  views.leave(entry);
  assert.deepEqual(calls, []);
  assert.equal(entry.attached, true);
  views.keyboard.owner = "app";
  const loading = { view, at: null, attached: false };
  views.leave(loading);
  assert.deepEqual(calls, []);
});

test("a page still loading is not put back by a window event", () => {
  const { views, view, calls } = loadedViews();
  views.entries.set("t1", { view, at: null, attached: false });
  views.reattach();
  assert.deepEqual(calls, []);
});

test("a navigation that never finishes does not keep the page out of the window for good", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const { views, view, calls, LEAVE_LIMIT_MS } = loadedViews();
    const entry = { view, at: { x: 10, y: 20, width: 600, height: 400 }, attached: true };
    views.leave(entry);
    assert.deepEqual(calls, ["remove"]);
    mock.timers.tick(LEAVE_LIMIT_MS - 1);
    assert.equal(entry.attached, false, "still out just before the limit");
    mock.timers.tick(1);
    assert.deepEqual(calls, ["remove", "add", "bounds 10"]);
    assert.equal(entry.attached, true);
    mock.timers.tick(LEAVE_LIMIT_MS * 3);
    assert.equal(calls.length, 3, "and it is put back once");
  } finally {
    mock.timers.reset();
  }
});

test("a navigation that does finish cancels the limit, and a second one gets a fresh one", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const { views, view, calls, LEAVE_LIMIT_MS } = loadedViews();
    const entry = { view, at: null, attached: true };
    views.leave(entry);
    mock.timers.tick(LEAVE_LIMIT_MS - 100);
    views.settle(entry);
    const back = calls.length;
    mock.timers.tick(LEAVE_LIMIT_MS * 2);
    assert.equal(calls.length, back, "the limit did not fire after the page came back");
    views.leave(entry);
    mock.timers.tick(LEAVE_LIMIT_MS);
    assert.equal(entry.attached, true, "the second navigation was bounded by its own limit");
  } finally {
    mock.timers.reset();
  }
});

// Input to a page in a minimized window.
const { InputWake } = require("../src/inputwake.js");

function wakeWindow({ minimized = true, focused = false } = {}) {
  const calls = [];
  const win = {
    destroyed: false,
    isDestroyed() { return this.destroyed; },
    isMinimized: () => minimized,
    isFocused: () => focused,
    setOpacity: (v) => calls.push(`opacity ${v}`),
    setIgnoreMouseEvents: (v) => calls.push(`ignore ${v}`),
    showInactive: () => { minimized = false; calls.push("showInactive"); },
    minimize: () => { minimized = true; calls.push("minimize"); },
  };
  return { win, calls, focus: () => { focused = true; } };
}
const wakeOf = (win) => new InputWake({ win, platform: "win32", settleMs: 5, holdMs: 20 });
const later = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

test("input to a minimized window is sent with the window raised unseen, then it is minimized again", async () => {
  const { win, calls } = wakeWindow();
  const sent = [];
  await wakeOf(win).run(async () => sent.push([...calls]));
  assert.deepEqual(sent[0], ["opacity 0", "ignore true", "showInactive"]);
  await later(60);
  assert.deepEqual(calls.slice(3), ["ignore false", "opacity 1", "minimize"]);
});

test("several inputs in a row raise the window once", async () => {
  const { win, calls } = wakeWindow();
  const wake = wakeOf(win);
  await Promise.all([wake.run(async () => {}), wake.run(async () => {}), wake.run(async () => {})]);
  await later(60);
  assert.equal(calls.filter((c) => c === "showInactive").length, 1);
  assert.equal(calls.filter((c) => c === "minimize").length, 1);
});

test("a window that is not minimized, or not on Windows, is left alone", async () => {
  const open = wakeWindow({ minimized: false });
  await wakeOf(open.win).run(async () => {});
  const mac = wakeWindow();
  await new InputWake({ win: mac.win, platform: "darwin", settleMs: 5, holdMs: 20 }).run(async () => {});
  await later(40);
  assert.deepEqual([...open.calls, ...mac.calls], []);
});

test("a window the person focused meanwhile is not minimized under them", async () => {
  const { win, calls, focus } = wakeWindow();
  await wakeOf(win).run(async () => focus());
  await later(60);
  assert.ok(!calls.includes("minimize"));
  assert.deepEqual(calls.slice(-2), ["ignore false", "opacity 1"]);
});

test("a failed input still lets the window go back", async () => {
  const { win, calls } = wakeWindow();
  await assert.rejects(wakeOf(win).run(async () => { throw new Error("gone"); }), /gone/);
  await later(60);
  assert.ok(calls.includes("minimize"));
});

test("a window destroyed during the hold is not touched", async () => {
  const { win, calls } = wakeWindow();
  await wakeOf(win).run(async () => {});
  win.destroyed = true;
  const before = calls.length;
  await later(60);
  assert.equal(calls.length, before);
});

test("a window that fails to raise is given its opacity and mouse back", async () => {
  const { win, calls } = wakeWindow();
  win.showInactive = () => { throw new Error("no window"); };
  await assert.rejects(wakeOf(win).run(async () => {}), /no window/);
  assert.deepEqual(calls.slice(-3), ["ignore false", "opacity 1", "minimize"]);
});

const { processMemoryRows, processMemoryHandler } = require("../src/procmem.js");

test("the shell's process memory is only a fixed type and two whole numbers per process", () => {
  const metrics = [
    { pid: 4740, type: "Tab", name: "C:\\Users\\someone\\x", cmd: "--user-data-dir=/home/someone", memory: { workingSetSize: 3992908, privateBytes: 3900000, peakWorkingSetSize: 9 } },
    { pid: 11, type: "Browser", memory: { workingSetSize: 120000, privateBytes: 90000 } },
    { pid: 12, type: "GPU", memory: { workingSetSize: 76000 } },
    { pid: 13, type: "Utility", memory: { workingSetSize: 6000, privateBytes: 5000 } },
    { pid: 14, type: "Zygote", memory: { workingSetSize: 1000, privateBytes: 900 } },
    { pid: 15, type: "Tab", memory: { workingSetSize: Number.NaN, privateBytes: -4 } },
  ];
  const rows = processMemoryRows(metrics);
  assert.deepEqual(rows, [
    { process: "renderer", workingSetKB: 3992908, privateKB: 3900000 },
    { process: "browser", workingSetKB: 120000, privateKB: 90000 },
    { process: "gpu", workingSetKB: 76000, privateKB: 0 },
    { process: "utility", workingSetKB: 6000, privateKB: 5000 },
    { process: "utility", workingSetKB: 1000, privateKB: 900 },
    { process: "renderer", workingSetKB: 0, privateKB: 0 },
  ]);
  assert.ok(rows.every((r) => Object.keys(r).sort().join() === "privateKB,process,workingSetKB"));
  assert.doesNotMatch(JSON.stringify(rows), /Users|pid|user-data|cmd/i);
});

test("the shell reports at most eleven processes, since the kernel adds its own row to a dozen", () => {
  const many = Array.from({ length: 30 }, (_, i) => ({ pid: i, type: "Utility", memory: { workingSetSize: 1, privateBytes: 1 } }));
  assert.equal(processMemoryRows(many).length, 11);
});

test("only the app's own window is answered, and nothing is read from the asker", () => {
  const app = { getAppMetrics: () => [{ type: "Tab", memory: { workingSetSize: 5, privateBytes: 4 } }] };
  assert.deepEqual(processMemoryHandler(app, () => true)({}), [{ process: "renderer", workingSetKB: 5, privateKB: 4 }]);
  assert.deepEqual(processMemoryHandler(app, () => null)({}), []);
  assert.deepEqual(processMemoryHandler({ getAppMetrics: () => { throw new Error("x"); } }, () => true)({}), []);
});

const { clipboardFiles, fileNamePaths, fileUrlPaths, uriListPaths } = require("../src/clipboardfiles.js");
const { pathToFileURL } = require("node:url");

// The clipboard names a copied file in its own formats. A path copied as text
// is text, and the line between the two is the format, never the wording.
test("the clipboard's file format names the files a copy carried", async () => {
  const one = pathToFileURL(path.resolve("one.txt")).href;
  const two = pathToFileURL(path.resolve("two.txt")).href;
  const clipboard = {
    read: async () => [{ types: ["text/uri-list"], getType: async () => new Blob([`${one}\r\n${two}\r\n`]) }],
  };
  assert.deepEqual(await clipboardFiles(clipboard), [path.resolve("one.txt"), path.resolve("two.txt")]);
});

test("a clipboard holding only text names no file", async () => {
  const clipboard = {
    read: async () => [{ types: ["text/plain"], getType: async () => new Blob(["C:\\Users\\me\\photo.png"]) }],
  };
  assert.deepEqual(await clipboardFiles(clipboard), []);
});

test("a clipboard that cannot be read names no file", async () => {
  const clipboard = { read: async () => { throw new Error("no clipboard"); } };
  assert.deepEqual(await clipboardFiles(clipboard), []);
});

test("a uri list skips its comments and every uri that names no file", () => {
  const one = pathToFileURL(path.resolve("one.txt")).href;
  assert.deepEqual(uriListPaths(`# a comment\r\n${one}\r\nhttps://example.com/x\r\n\r\n`), [path.resolve("one.txt")]);
});

test("the windows file-name format holds its paths as utf-16, each ended by a null", () => {
  const bytes = Buffer.from("C:\\Users\\me\\a.txt\0C:\\Users\\me\\b.txt\0\0", "utf16le");
  assert.deepEqual(fileNamePaths(bytes), ["C:\\Users\\me\\a.txt", "C:\\Users\\me\\b.txt"]);
});

// When more than one format names the same file, the result collapses to a
// single entry instead of attaching that file twice.
test("formats naming the same file collapse to one entry", async () => {
  const one = pathToFileURL(path.resolve("one.txt")).href;
  const type = 'electron application/osclipboard;format="FileNameW"';
  const bytes = Buffer.from(path.resolve("one.txt") + "\0\0", "utf16le");
  const clipboard = {
    read: async () => [{
      types: ["text/uri-list", type],
      getType: async (t) => new Blob([t === "text/uri-list" ? `${one}\r\n` : bytes]),
    }],
  };
  assert.deepEqual(await clipboardFiles(clipboard), [path.resolve("one.txt")]);
});

test("the macos file url names one file", () => {
  const url = pathToFileURL(path.resolve("a.txt")).href;
  assert.deepEqual(fileUrlPaths(url), [path.resolve("a.txt")]);
  assert.deepEqual(fileUrlPaths("https://example.com/x"), []);
});
