import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { EventEmitter } from "node:events";

const require = createRequire(import.meta.url);
const { isFrameUrl, fromMainFrame, frameNavigationAllowed, guardFrames, guardPermissions } = require("../src/frameguard.js");

const ORIGIN = "http://127.0.0.1:8080";
const FRAME = `${ORIGIN}/extensions/frames/board/kanban?generation=3`;

test("an extension frame is a document on this origin under the frame route, and nothing else", () => {
  assert.equal(isFrameUrl(ORIGIN, FRAME), true);
  assert.equal(isFrameUrl(ORIGIN, `${ORIGIN}/rt/r1/extensions/frames/board/kanban?generation=3`), true);
  assert.equal(isFrameUrl(ORIGIN, `${ORIGIN}/rt/r1/x/extensions/frames/board/kanban`), false);
  assert.equal(isFrameUrl(ORIGIN, `${ORIGIN}/extensions/frames/board/kanban/../../../status`), false);
  assert.equal(isFrameUrl(ORIGIN, `${ORIGIN}/extensions/frames/board`), false);
  assert.equal(isFrameUrl(ORIGIN, "not a url"), false);
  assert.equal(isFrameUrl(ORIGIN, `${ORIGIN}/extensions/surfaces`), false);
  assert.equal(isFrameUrl(ORIGIN, `http://127.0.0.1:9999/extensions/frames/board/kanban`), false);
  assert.equal(isFrameUrl("", "/extensions/frames/board/kanban"), false);
  assert.equal(isFrameUrl(ORIGIN, undefined), false);
});

test("a frame holding an extension document may only load another one", () => {
  assert.equal(frameNavigationAllowed(ORIGIN, FRAME, `${ORIGIN}/extensions/frames/board/kanban?generation=4`), true);
  assert.equal(frameNavigationAllowed(ORIGIN, FRAME, "https://attacker.example/?leak"), false);
  assert.equal(frameNavigationAllowed(ORIGIN, FRAME, `${ORIGIN}/extensions/action`), false);
  assert.equal(frameNavigationAllowed(ORIGIN, FRAME, "about:blank"), false);
});

test("a frame being created may load an extension document, and other subframes keep their rules", () => {
  assert.equal(frameNavigationAllowed(ORIGIN, "", FRAME), true);
  assert.equal(frameNavigationAllowed(ORIGIN, "about:srcdoc", "about:srcdoc"), true);
  assert.equal(frameNavigationAllowed(ORIGIN, `${ORIGIN}/workspace/preview/content`, `${ORIGIN}/workspace/preview/content`), true);
});

test("the window's contents refuse a subframe navigation the rule refuses, and leave the top document to its own guard", () => {
  const contents = new EventEmitter();
  guardFrames(contents, () => ORIGIN);
  const navigate = (from, to, isMainFrame = false) => {
    let prevented = false;
    contents.emit("will-frame-navigate", { url: to, isMainFrame, frame: { url: from }, preventDefault: () => (prevented = true) });
    return prevented;
  };
  assert.equal(navigate(FRAME, "https://attacker.example/"), true);
  assert.equal(navigate(FRAME, FRAME), false);
  assert.equal(navigate(FRAME, "https://attacker.example/", true), false);
});

test("IPC answers only for the window's top document", () => {
  const mainFrame = { url: `${ORIGIN}/` };
  const webContents = { mainFrame };
  const win = { webContents, isDestroyed: () => false };
  assert.equal(fromMainFrame(win, { sender: webContents, senderFrame: mainFrame }), win);
  assert.equal(fromMainFrame(win, { sender: webContents, senderFrame: { url: FRAME } }), null);
  assert.equal(fromMainFrame(win, { sender: webContents, senderFrame: null }), null);
  assert.equal(fromMainFrame(win, { sender: {}, senderFrame: mainFrame }), null);
  assert.equal(fromMainFrame({ ...win, isDestroyed: () => true }, { sender: webContents, senderFrame: mainFrame }), null);
  assert.equal(fromMainFrame(null, { sender: webContents, senderFrame: mainFrame }), null);
});

test("a subframe is denied every permission and the top document keeps its answer", () => {
  let request;
  let check;
  guardPermissions({ setPermissionRequestHandler: (fn) => (request = fn), setPermissionCheckHandler: (fn) => (check = fn) });
  const asked = (details) => {
    let answer;
    request({}, "media", (granted) => (answer = granted), details);
    return answer;
  };
  assert.equal(asked({ isMainFrame: false, requestingUrl: FRAME }), false);
  assert.equal(asked({ isMainFrame: true, requestingUrl: `${ORIGIN}/` }), true);
  assert.equal(asked(undefined), false);
  assert.equal(check({}, "clipboard-sanitized-write", "null", { isMainFrame: false }), false);
  assert.equal(check({}, "clipboard-sanitized-write", ORIGIN, { isMainFrame: true }), true);
});
