import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import path from "node:path";
import os from "node:os";
import fs from "node:fs";

const require = createRequire(import.meta.url);
const { parse, readActs, start } = require("../src/host.js");
const { contextTemplate, editMenuTemplate, applicationMenuTemplate, menuInstaller } = require("../src/editmenu.js");
const { uiLanguage } = require("../src/uilang.js");
const { externalTarget } = require("../src/links.js");
const { offerCleanup, ownBundle } = require("../src/legacy.js");
const { stripPackageGrants, readReport, unpaintedWindowCause } = require("../src/packagegrants.js");
const { groundFor, GROUND } = require("../src/ground.js");
const { pick, loadPrefs, savePrefs, registerPrefs } = require("../src/prefs.js");

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

const { reveal, revealWorkspace } = require("../src/reveal.js");

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
  return { asked, opened, run: (base, rel) => reveal(client, shell, base, rel, platform), workspace: (root) => revealWorkspace(client, shell, root, platform), close: () => server.close() };
}

const ROOT = path.resolve(os.tmpdir(), "rx-workspace");

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

const { hostBinary, computerHelper, pageDir } = require("../src/layout.js");

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

test("the computer-use helper is found beside the kernel on macOS and Windows, and nowhere else", () => {
  const mac = { packaged: true, resourcesPath: "/res", dirname: "/d/src", platform: "darwin" };
  assert.equal(computerHelper(mac), path.join("/res", "bin", "reasonix-computer-helper"));
  assert.equal(computerHelper({ ...mac, packaged: false, dirname: path.join("/repo", "electron", "src") }), path.join("/repo", "electron", "bin", "reasonix-computer-helper"));
  assert.equal(computerHelper({ ...mac, platform: "win32" }), path.join("/res", "bin", "reasonix-computer-helper.exe"));
  assert.equal(computerHelper({ ...mac, platform: "linux" }), "");
  assert.equal(computerHelper({ ...mac, platform: "linux", env: { REASONIX_COMPUTER_HELPER: "/custom/helper" } }), "/custom/helper");
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

function elf64(interpreter) {
  const phoff = 64, phentsize = 56, phnum = interpreter ? 2 : 1;
  const dataOffset = phoff + phnum * phentsize;
  const path = Buffer.from(interpreter ? `${interpreter}\0` : "", "latin1");
  const bytes = Buffer.alloc(dataOffset + path.length);
  bytes.writeUInt32BE(0x7f454c46, 0);
  bytes[4] = 2;
  bytes[5] = 1;
  bytes[6] = 1;
  bytes.writeUInt16LE(2, 16);
  bytes.writeUInt16LE(62, 18);
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
