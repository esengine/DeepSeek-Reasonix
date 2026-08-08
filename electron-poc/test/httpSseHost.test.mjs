import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { HttpSseHost } from "../lib/httpSseHost.mjs";
import { SERVE_ROUTES, P0_COMMANDS } from "../lib/routes.mjs";
import { mapServeEventToWire, parseSseChunk } from "../lib/mapEvent.mjs";

/**
 * eventwire-shaped samples as emitted by serve Broadcaster (json.Marshal(ToWire)).
 * See internal/eventwire/wire.go — kind top-level, nested approval/ask.
 */
const WIRE_TEXT = { kind: "text", text: "hello" };
const WIRE_APPROVAL = {
  kind: "approval_request",
  approval: {
    id: "a1",
    tool: "shell",
    subject: "ls -la",
    reason: "ask",
    kind: "tool",
  },
};
const WIRE_ASK = {
  kind: "ask_request",
  ask: {
    id: "q1",
    questions: [{ id: "q", prompt: "continue?", options: ["yes", "no"] }],
  },
};

/**
 * Minimal fake reasonix serve surface for host tests.
 * Exercises the real HttpSseHost against real HTTP — not a stub of the host.
 */
function startFakeServe() {
  const posts = [];
  const server = http.createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    const token = url.searchParams.get("token") || cookieToken(req.headers.cookie);
    if (token !== "secret-token") {
      res.writeHead(401);
      res.end("unauthorized");
      return;
    }
    if (req.method === "POST") {
      const ct = req.headers["content-type"] ?? "";
      if (!ct.includes("application/json")) {
        res.writeHead(415);
        res.end("Content-Type must be application/json");
        return;
      }
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        posts.push({ path: url.pathname, body, method: req.method });
        if (url.pathname === "/submit") {
          res.writeHead(202);
          res.end();
          return;
        }
        res.writeHead(204);
        res.end();
      });
      return;
    }
    if (url.pathname === "/status") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ label: "fake", ok: true }));
      return;
    }
    if (url.pathname === "/tabs" && req.method === "GET") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify([
          {
            id: "tab_a",
            scope: "project",
            workspaceRoot: "/a",
            workspaceName: "a",
            topicId: "main",
            topicTitle: "Session",
            label: "A",
            ready: true,
            running: false,
            cancellable: false,
            mode: "agent",
            collaborationMode: "default",
            toolApprovalMode: "ask",
            tokenMode: "auto",
            active: true,
            cwd: "/a",
          },
          {
            id: "tab_b",
            scope: "project",
            workspaceRoot: "/b",
            workspaceName: "b",
            topicId: "main",
            topicTitle: "Session",
            label: "B",
            ready: true,
            running: false,
            cancellable: false,
            mode: "agent",
            collaborationMode: "default",
            toolApprovalMode: "ask",
            tokenMode: "auto",
            active: false,
            cwd: "/b",
          },
        ]),
      );
      return;
    }
    if (url.pathname.startsWith("/tabs/") && url.pathname.endsWith("/submit") && req.method === "POST") {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        posts.push({ path: url.pathname, body, method: req.method });
        res.writeHead(202);
        res.end();
      });
      return;
    }
    if (url.pathname === "/history") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify([{ role: "user", content: "hi" }]));
      return;
    }
    if (url.pathname === "/events") {
      res.writeHead(200, {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      });
      res.write(": connected\n\n");
      // Real serve wire shape (kind + nested approval), not fictional type/request_id
      res.write(`data: ${JSON.stringify(WIRE_TEXT)}\n\n`);
      res.write(`data: ${JSON.stringify(WIRE_APPROVAL)}\n\n`);
      res.write(`data: ${JSON.stringify(WIRE_ASK)}\n\n`);
      setTimeout(() => res.end(), 50);
      return;
    }
    if (
      [
        "/context",
        "/sessions",
        "/skills",
        "/todos",
        "/checkpoints",
        "/branches",
        "/models",
        "/provider-setup",
      ].includes(url.pathname)
    ) {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ path: url.pathname, ok: true }));
      return;
    }
    res.writeHead(404);
    res.end("nope");
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      resolve({
        server,
        port,
        baseUrl: `http://127.0.0.1:${port}`,
        posts,
        close: () => new Promise((r) => server.close(() => r(undefined))),
      });
    });
  });
}

function cookieToken(cookieHeader) {
  if (!cookieHeader) return "";
  const m = String(cookieHeader).match(/reasonix_token=([^;]+)/);
  return m ? m[1] : "";
}

test("P0 route table covers required serve paths", () => {
  const paths = new Set(Object.values(SERVE_ROUTES).map((r) => r.path));
  for (const need of [
    "/events",
    "/submit",
    "/cancel",
    "/approve",
    "/answer",
    "/history",
    "/context",
    "/status",
    "/new",
    "/compact",
    "/rewind",
    "/checkpoints",
    "/fork",
    "/summarize",
    "/plan",
    "/tool-approval-mode",
    "/goal",
    "/sessions",
    "/delete-session",
    "/resume",
    "/models",
    "/todos",
    "/skills",
    "/provider-setup",
  ]) {
    assert.ok(paths.has(need), `missing route ${need}`);
  }
  assert.ok(P0_COMMANDS.length >= 20);
});

test("HttpSseHost status/history/submit/cancel/approve hit real HTTP with JSON Content-Type", async () => {
  const fake = await startFakeServe();
  try {
    const host = new HttpSseHost({ baseUrl: fake.baseUrl, token: "secret-token" });
    const st = await host.status();
    assert.equal(st.ok, true);
    assert.equal(st.label, "fake");

    const hist = await host.history();
    assert.ok(Array.isArray(hist));
    assert.equal(hist[0].content, "hi");

    await host.submit("hello world");
    await host.cancel();
    await host.approve("id1", true, false, false);
    await host.answer("q1", [{ id: "a", values: ["x"] }]);
    await host.setPlanMode(true);
    await host.setToolApprovalMode("ask");
    await host.newSession();
    await host.compact();
    await host.setGoal("ship it");

    assert.ok(fake.posts.some((p) => p.path === "/submit"));
    const submit = fake.posts.find((p) => p.path === "/submit");
    assert.equal(JSON.parse(submit.body).input, "hello world");
    assert.ok(fake.posts.some((p) => p.path === "/cancel"));
    assert.ok(fake.posts.some((p) => p.path === "/approve"));
    assert.ok(fake.posts.some((p) => p.path === "/answer"));
    assert.ok(fake.posts.some((p) => p.path === "/plan"));
    assert.ok(fake.posts.some((p) => p.path === "/tool-approval-mode"));
    assert.ok(fake.posts.some((p) => p.path === "/new"));
    assert.ok(fake.posts.some((p) => p.path === "/compact"));
    assert.ok(fake.posts.some((p) => p.path === "/goal"));
    host.dispose();
  } finally {
    await fake.close();
  }
});

test("HttpSseHost rejects non-JSON POST at server (csrf) when Content-Type wrong", async () => {
  const fake = await startFakeServe();
  try {
    const url = `${fake.baseUrl}/submit?token=secret-token`;
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "text/plain", Cookie: "reasonix_token=secret-token" },
      body: '{"input":"x"}',
    });
    assert.equal(res.status, 415);
  } finally {
    await fake.close();
  }
});

test("HttpSseHost SSE preserves eventwire kind and nested approval.id/ask.id", async () => {
  const fake = await startFakeServe();
  try {
    const host = new HttpSseHost({ baseUrl: fake.baseUrl, token: "secret-token" });
    const events = [];
    await new Promise((resolve, reject) => {
      const t = setTimeout(() => reject(new Error("sse timeout")), 3000);
      const unsub = host.onEvent((e) => {
        events.push(e);
        if (events.length >= 3) {
          clearTimeout(t);
          unsub();
          resolve();
        }
      });
    });
    // text
    assert.equal(events[0].kind, "text");
    assert.equal(events[0].text, "hello");
    assert.equal(events[0].type, undefined); // do not invent type
    assert.equal(events[0]._transport, "http-sse");
    // approval_request with nested approval.id
    assert.equal(events[1].kind, "approval_request");
    assert.ok(events[1].approval && typeof events[1].approval === "object");
    assert.equal(events[1].approval.id, "a1");
    assert.equal(events[1].approval.tool, "shell");
    assert.equal(events[1].id, undefined); // id is nested, not top-level
    // ask_request with nested ask.id
    assert.equal(events[2].kind, "ask_request");
    assert.equal(events[2].ask.id, "q1");
    host.dispose();
  } finally {
    await fake.close();
  }
});

test("HttpSseHost listTabs and submitTab hit multi-tab routes", async () => {
  const fake = await startFakeServe();
  try {
    const host = new HttpSseHost({ baseUrl: fake.baseUrl, token: "secret-token" });
    const tabs = await host.listTabs();
    assert.ok(Array.isArray(tabs));
    assert.equal(tabs.length, 2);
    assert.equal(tabs[0].id, "tab_a");
    await host.submitTab("tab_a", "hello multi");
    assert.ok(fake.posts.some((p) => p.path === "/tabs/tab_a/submit"));
    const submit = fake.posts.find((p) => p.path === "/tabs/tab_a/submit");
    assert.equal(JSON.parse(submit.body).input, "hello multi");
    host.dispose();
  } finally {
    await fake.close();
  }
});

test("mapServeEventToWire preserves kind-based eventwire payloads", () => {
  const state = { pending: "" };
  const payloads = parseSseChunk(
    `: ping\n\ndata: ${JSON.stringify(WIRE_TEXT)}\n\ndata: ${JSON.stringify(WIRE_APPROVAL)}\n\n partial`,
    state,
  );
  assert.equal(payloads.length, 2);
  assert.ok(state.pending.includes("partial"));

  const text = mapServeEventToWire(payloads[0]);
  assert.equal(text.kind, "text");
  assert.equal(text.text, "hello");
  assert.equal(text.type, undefined);
  assert.equal(text._transport, "http-sse");

  const appr = mapServeEventToWire(payloads[1]);
  assert.equal(appr.kind, "approval_request");
  assert.equal(appr.approval.id, "a1");
  assert.equal(appr.approval.tool, "shell");
  // Must not force type:"unknown" or flatten id to top-level
  assert.notEqual(appr.type, "unknown");
  assert.equal(appr.id, undefined);

  // Object form (already parsed)
  const direct = mapServeEventToWire(WIRE_ASK);
  assert.equal(direct.kind, "ask_request");
  assert.equal(direct.ask.id, "q1");
});
