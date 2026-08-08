// Run: tsx src/__tests__/host-http-sse.test.ts
import assert from "node:assert/strict";
import http from "node:http";
import { createHttpSseHost } from "../lib/host/httpSseHost";
import { mapServeEventToWire, parseSseChunk } from "../lib/host/mapEvent";
import { SERVE_ROUTES } from "../lib/host/routes";
import { POC_CAPABILITIES, unsupportedDesktopFeatures } from "../lib/host/capabilities";
import { makeElectronHttpApp } from "../lib/host/electronAppBindings";

/** Real eventwire-shaped samples (internal/eventwire ToWire). */
const WIRE_TEXT = { kind: "text", text: "ts" };
const WIRE_APPROVAL = {
  kind: "approval_request",
  approval: { id: "a1", tool: "shell", subject: "echo hi", kind: "tool" },
};

function startFake(): Promise<{
  baseUrl: string;
  close: () => Promise<void>;
  posts: { path: string; body: string }[];
}> {
  const posts: { path: string; body: string }[] = [];
  const server = http.createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    const token = url.searchParams.get("token") || "";
    if (token !== "t") {
      res.writeHead(401);
      res.end("no");
      return;
    }
    if (req.method === "POST") {
      const ct = String(req.headers["content-type"] ?? "");
      if (!ct.includes("application/json")) {
        res.writeHead(415);
        res.end("need json");
        return;
      }
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        posts.push({ path: url.pathname, body });
        res.writeHead(url.pathname === "/submit" ? 202 : 204);
        res.end();
      });
      return;
    }
    if (url.pathname === "/status") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ ok: true, via: "ts-host" }));
      return;
    }
    if (url.pathname === "/events") {
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      res.write(`data: ${JSON.stringify(WIRE_TEXT)}\n\n`);
      res.write(`data: ${JSON.stringify(WIRE_APPROVAL)}\n\n`);
      setTimeout(() => res.end(), 30);
      return;
    }
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end("{}");
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      resolve({
        baseUrl: `http://127.0.0.1:${port}`,
        posts,
        close: () => new Promise((r) => server.close(() => r())),
      });
    });
  });
}

async function main() {
  console.log("\nhost http-sse");

  assert.equal(POC_CAPABILITIES.multiTab, true);
  assert.ok(!unsupportedDesktopFeatures().includes("multiTab"));
  assert.ok(unsupportedDesktopFeatures().includes("terminal"));
  process.stdout.write("  PASS  capability gate multiTab enabled\n");

  const state = { pending: "" };
  const parts = parseSseChunk(
    `: ping\n\ndata: ${JSON.stringify({ kind: "notice", text: "n", level: "info" })}\n\n`,
    state,
  );
  assert.equal(parts.length, 1);
  const w = mapServeEventToWire(parts[0]);
  assert.equal(w?.kind, "notice");
  assert.equal((w as { type?: string }).type, undefined);
  process.stdout.write("  PASS  parseSseChunk + mapServeEventToWire kind-based\n");

  const appr = mapServeEventToWire(WIRE_APPROVAL);
  assert.equal(appr?.kind, "approval_request");
  assert.equal((appr?.approval as { id: string }).id, "a1");
  process.stdout.write("  PASS  nested approval.id survives mapping\n");

  const fake = await startFake();
  try {
    const host = createHttpSseHost({ baseUrl: fake.baseUrl, token: "t" });
    const st = (await host.status()) as { ok: boolean; via: string };
    assert.equal(st.ok, true);
    assert.equal(st.via, "ts-host");
    await host.submit("from-ts");
    assert.ok(fake.posts.some((p) => p.path === SERVE_ROUTES.submit.path));
    assert.equal(JSON.parse(fake.posts.find((p) => p.path === "/submit")!.body).input, "from-ts");
    process.stdout.write("  PASS  status + submit over real HTTP\n");

    const events: Record<string, unknown>[] = [];
    await new Promise<void>((resolve, reject) => {
      const t = setTimeout(() => reject(new Error("timeout")), 3000);
      host.onEvent((e) => {
        events.push(e);
        if (events.length >= 2) {
          clearTimeout(t);
          resolve();
        }
      });
    });
    assert.equal(events[0].kind, "text");
    assert.equal(events[0].text, "ts");
    assert.equal(events[0]._transport, "http-sse");
    assert.equal(events[1].kind, "approval_request");
    assert.equal((events[1].approval as { id: string }).id, "a1");
    process.stdout.write("  PASS  SSE eventwire kind + approval.id\n");
    host.dispose();
  } finally {
    await fake.close();
  }

  // Electron multi-tab AppBindings must never hand App.tsx undefined arrays /
  // incomplete bot snapshots — that was the Phase-3 ErrorBoundary crash
  // (undefined.length / undefined.trim during DesktopStartupSettings hydrate).
  {
    const host = createHttpSseHost({ baseUrl: "http://127.0.0.1:9", token: "t" });
    const app = makeElectronHttpApp(host, {
      baseUrl: "http://127.0.0.1:9",
      token: "t",
      workspace: "/tmp",
    });
    const startup = await app.DesktopStartupSettings();
    assert.ok(Array.isArray(startup.statusBarItems));
    assert.ok(Array.isArray(startup.configWarnings));
    assert.ok(Array.isArray(startup.bot.connections));
    assert.ok(Array.isArray(startup.bot.allowlist.feishuUsers));
    assert.equal(typeof startup.bot.qq.appId, "string");
    assert.ok(Array.isArray(await app.BackgroundRuntimes()));
    assert.ok(Array.isArray(await app.RemoteHosts()));
    assert.ok(Array.isArray(await app.RemoteConnectionStatuses()));
    assert.ok(Array.isArray(await app.Models()));
    assert.ok(Array.isArray(await app.Jobs()));
    const settings = await app.Settings();
    assert.ok(Array.isArray(settings.providers));
    assert.ok(Array.isArray(settings.officialProviders));
    // Proxy fallback for unknown List* / *s array methods.
    const unknownList = await (app as unknown as { ListSomethingUnknown: () => Promise<unknown> }).ListSomethingUnknown();
    assert.ok(Array.isArray(unknownList));
    host.dispose();
    process.stdout.write("  PASS  electron AppBindings startup arrays are never undefined\n");
  }

  // ListProjectTree must reflect open multi-tab Controllers so "添加新项目"
  // after PickWorkspace is visible in the sidebar (was always []).
  {
    const tabs = [
      {
        id: "tab_a",
        scope: "project",
        workspaceRoot: "/Users/me/proj-a",
        workspaceName: "proj-a",
        topicId: "main",
        topicTitle: "Session",
        label: "model",
        ready: true,
        running: false,
        active: true,
        cwd: "/Users/me/proj-a",
        mode: "agent",
        collaborationMode: "default",
        toolApprovalMode: "ask",
        tokenMode: "auto",
      },
      {
        id: "tab_b",
        scope: "project",
        workspaceRoot: "/Users/me/proj-b",
        workspaceName: "proj-b",
        topicId: "main",
        topicTitle: "Session",
        label: "model",
        ready: true,
        running: false,
        active: false,
        cwd: "/Users/me/proj-b",
        mode: "agent",
        collaborationMode: "default",
        toolApprovalMode: "ask",
        tokenMode: "auto",
      },
    ];
    const server = http.createServer((req, res) => {
      const url = new URL(req.url ?? "/", "http://127.0.0.1");
      if (url.pathname === "/tabs") {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify(tabs));
        return;
      }
      res.writeHead(404);
      res.end("no");
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", () => resolve()));
    try {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      const host = createHttpSseHost({ baseUrl: `http://127.0.0.1:${port}`, token: "t" });
      const app = makeElectronHttpApp(host, {
        baseUrl: `http://127.0.0.1:${port}`,
        token: "t",
        workspace: "/tmp",
      });
      const tree = await app.ListProjectTree();
      assert.ok(Array.isArray(tree));
      assert.equal(tree.length, 2);
      assert.equal(tree[0].kind, "project");
      assert.ok(tree.some((n) => n.root === "/Users/me/proj-a"));
      assert.ok(tree.some((n) => n.root === "/Users/me/proj-b"));
      for (const n of tree) {
        assert.ok(Array.isArray(n.children) && n.children!.length >= 1);
        assert.equal(n.children![0].kind, "topic");
      }
      host.dispose();
      process.stdout.write("  PASS  ListProjectTree builds nodes from open tabs\n");
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  }

  console.log("host http-sse: all passed\n");
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
