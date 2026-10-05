"use strict";
// What the live tests share: a real window over a real kernel, driven by a
// scripted model on loopback, with checks that read what actually happened.
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const { BrowserWindow } = require("electron");

const wait = (ms) => new Promise((r) => setTimeout(r, ms));

function checker() {
  const failures = [];
  const check = (name, condition, detail) => {
    process.stdout.write(condition ? `  ok   ${name}\n` : `  FAIL ${name}${detail === undefined ? "" : ` — ${JSON.stringify(detail)}`}\n`);
    if (!condition) failures.push(name);
  };
  return { check, failures };
}

function listen(handler) {
  return new Promise((resolve) => {
    const server = http.createServer(handler);
    server.listen(0, "127.0.0.1", () => resolve({ server, url: `http://127.0.0.1:${server.address().port}` }));
  });
}

// scriptedModel answers each request with next(toolResults, messages): a tool
// call built from what the conversation shows so far, or a closing reply when it
// gives none.
function scriptedModel(next) {
  const requests = [];
  const handler = (req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      const parsed = JSON.parse(body || "{}");
      requests.push(parsed);
      const tools = (parsed.messages || []).filter((m) => m.role === "tool").map((m) => String(m.content || ""));
      const call = next(tools, parsed.messages || []);
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      const chunk = (delta, finish) =>
        res.write(`data: ${JSON.stringify({ id: "c", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`);
      if (call) {
        chunk({ role: "assistant", tool_calls: [{ index: 0, id: `call-${tools.length}`, type: "function", function: { name: call.name, arguments: JSON.stringify(call.arguments) } }] }, null);
        chunk({}, "tool_calls");
      } else {
        chunk({ role: "assistant", content: "done" }, null);
        chunk({}, "stop");
      }
      res.write("data: [DONE]\n\n");
      res.end();
    });
  };
  return { handler, requests };
}

// seedHome writes the configuration a live run starts from. toolApproval is
// what the person's posture would be: yolo answers every prompt, so a test of
// what a person is asked names another one.
function seedHome(home, modelURL, workspace, { toolApproval = "yolo" } = {}) {
  fs.mkdirSync(home, { recursive: true });
  fs.writeFileSync(path.join(home, "config.toml"), [
    'default_model = "fake/fake-model"',
    "",
    "[desktop]",
    "welcomed = true",
    `default_tool_approval_mode = "${toolApproval}"`,
    "",
    "[[providers]]",
    'name        = "fake"',
    'kind        = "openai"',
    `base_url    = "${modelURL}/v1"`,
    'models      = ["fake-model"]',
    'default     = "fake-model"',
    'api_key_env = "FAKE_API_KEY"',
    "",
  ].join("\n"));
  fs.writeFileSync(path.join(home, ".env"), "FAKE_API_KEY=fake\n");
  process.chdir(workspace);
}

async function settledWindow() {
  const deadline = Date.now() + 40000;
  while (Date.now() < deadline) {
    const win = BrowserWindow.getAllWindows()[0];
    if (win && !win.webContents.isLoading() && win.webContents.getURL()) return win;
    await wait(200);
  }
  throw new Error("the shell never opened a loaded window");
}

async function until(what, fn, ms = 30000) {
  const deadline = Date.now() + ms;
  while (Date.now() < deadline) {
    const got = await fn();
    if (got) return got;
    await wait(200);
  }
  throw new Error(`timed out waiting for ${what}`);
}

module.exports = { wait, checker, listen, scriptedModel, seedHome, settledWindow, until };
