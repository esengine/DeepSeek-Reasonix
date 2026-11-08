import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import path from "node:path";

const serial = process.argv[2];
assert(serial, "Usage: node android/scripts/verify-keyboard.mjs <adb-serial> [package]");
const pkg = process.argv[3] || "io.reasonix.mobile.app.debug";
const sdk = process.env.ANDROID_HOME || process.env.ANDROID_SDK_ROOT;
const binary = process.platform === "win32" ? "adb.exe" : "adb";
const adb = sdk ? path.join(sdk, "platform-tools", binary) : binary;
const run = (...args) => execFileSync(adb, ["-s", serial, ...args], { encoding: "utf8", timeout: 15000 });
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const activity = run("shell", "dumpsys", "activity", "activities");
assert(activity.split("\n").some((line) => line.includes("topResumedActivity") && line.includes(pkg) && line.includes("MainActivity")), "Unlock the phone and open Reasonix first");
const pid = run("shell", "pidof", pkg).trim();
const port = run("forward", "tcp:0", `localabstract:webview_devtools_remote_${pid}`).trim();
let ws;
try {
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`, { signal: AbortSignal.timeout(8000) })).json();
  const target = targets.find((entry) => entry.title === "Reasonix Studio");
  assert(target, "Reasonix Studio WebView is unavailable");
  ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { ws.addEventListener("open", resolve, { once: true }); ws.addEventListener("error", reject, { once: true }); });
  const pending = new Map();
  let id = 0;
  ws.addEventListener("message", (event) => {
    const reply = JSON.parse(event.data);
    if (reply.id) pending.get(reply.id)?.(reply);
  });
  const call = (method, params = {}) => new Promise((resolve, reject) => {
    const request = ++id;
    const timer = setTimeout(() => { pending.delete(request); reject(new Error(`${method} timed out`)); }, 8000);
    pending.set(request, (reply) => {
      clearTimeout(timer); pending.delete(request);
      if (reply.error || reply.result?.exceptionDetails) reject(new Error(`${method} failed`));
      else resolve(reply.result);
    });
    ws.send(JSON.stringify({ id: request, method, params }));
  });
  const evaluate = async (expression) => (await call("Runtime.evaluate", { expression, returnByValue: true })).result.value;
  const shown = () => /mInputShown=true/.test(run("shell", "dumpsys", "input_method"));
  if (shown()) { run("shell", "input", "keyevent", "4"); await pause(500); }
  const measure = () => evaluate(`(() => {
    const input = document.querySelector('.compose textarea');
    const send = document.querySelector('[data-action="session.send"]');
    return { height: innerHeight, overflow: document.documentElement.scrollWidth - innerWidth,
      input: input?.getBoundingClientRect().toJSON(), send: send?.getBoundingClientRect().toJSON() };
  })()`);
  const before = await measure();
  assert(before.input?.width > 0, "Open a conversation with its composer visible");
  try {
    const point = { x: before.input.x + before.input.width / 2, y: before.input.y + before.input.height / 2 };
    await call("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [point] });
    await call("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    let after;
    for (let attempt = 0; attempt < 12; attempt++) {
      await pause(250); after = await measure();
      if (before.height - after.height > 100) break;
    }
    assert(shown(), "The software keyboard did not open");
    assert(before.height - after.height > 100, `Keyboard covers the page: viewport stayed ${before.height} -> ${after.height}`);
    assert(after.input.top >= 0 && after.input.bottom <= after.height, "Input is outside the keyboard viewport");
    assert(after.send.top >= 0 && after.send.bottom <= after.height, "Send button is outside the keyboard viewport");
    assert(after.overflow <= 1, "Keyboard causes horizontal overflow");
    console.log(JSON.stringify({ result: "passed", before: before.height, keyboard: after.height, inputBottom: after.input.bottom, sendBottom: after.send.bottom }));
  } finally {
    if (shown()) run("shell", "input", "keyevent", "4");
  }
} finally {
  ws?.close();
  run("forward", "--remove", `tcp:${port}`);
}
