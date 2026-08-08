import assert from "node:assert/strict";
import http from "node:http";
import { describe, it } from "node:test";
import { withReasonixTokenCookie, startDesktopShell } from "../lib/desktopShell.mjs";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

describe("withReasonixTokenCookie", () => {
  it("sets token when cookie missing", () => {
    assert.equal(withReasonixTokenCookie(undefined, "abc"), "reasonix_token=abc");
    assert.equal(withReasonixTokenCookie("", "abc"), "reasonix_token=abc");
  });

  it("overwrites a stale reasonix_token instead of keeping it", () => {
    const stale = "reasonix_token=deadbeef; other=1";
    assert.equal(withReasonixTokenCookie(stale, "live"), "other=1; reasonix_token=live");
  });

  it("preserves unrelated cookies", () => {
    assert.equal(
      withReasonixTokenCookie("foo=bar; baz=qux", "t1"),
      "foo=bar; baz=qux; reasonix_token=t1",
    );
  });
});

describe("desktopShell proxy auth", () => {
  it("returns 200 for API even when browser sends a stale reasonix_token cookie", async () => {
    const serveToken = "correct-serve-token-32chars-xxxxxx";
    let sawCookie = "";
    const serve = http.createServer((req, res) => {
      sawCookie = String(req.headers.cookie || "");
      // Mimic reasonix serve token-mode: only exact cookie works.
      if (sawCookie.includes(`reasonix_token=${serveToken}`)) {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ ok: true }));
        return;
      }
      // Query-token path would 302; we assert shell uses cookie instead.
      res.writeHead(401, { "Content-Type": "text/plain" });
      res.end("Unauthorized\n");
    });
    await new Promise((r) => serve.listen(0, "127.0.0.1", r));
    const servePort = serve.address().port;

    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "rx-shell-ui-"));
    fs.writeFileSync(path.join(tmp, "index.html"), "<!doctype html><html><head></head><body>ok</body></html>");

    const shell = await startDesktopShell({
      staticDir: tmp,
      serveBaseUrl: `http://127.0.0.1:${servePort}`,
      token: serveToken,
      workspace: tmp,
    });

    try {
      // Browser-like: stale cookie + wrong/old query token (HttpSseHost still attaches both).
      const res = await fetch(`${shell.baseUrl}/status?token=stale-client-token`, {
        headers: {
          Cookie: "reasonix_token=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
        },
      });
      const body = await res.text();
      assert.equal(res.status, 200, `status=${res.status} body=${body}`);
      assert.match(body, /"ok"\s*:\s*true/);
      assert.match(sawCookie, new RegExp(`reasonix_token=${serveToken}`));
      assert.doesNotMatch(sawCookie, /deadbeef/);

      // Set-Cookie from serve must not leak to the shell origin.
      assert.equal(res.headers.get("set-cookie"), null);
    } finally {
      await shell.close();
      await new Promise((r) => serve.close(() => r()));
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("POST /tabs/open-project succeeds with stale browser cookie", async () => {
    const serveToken = "correct-serve-token-32chars-yyyyyy";
    const serve = http.createServer((req, res) => {
      const cookie = String(req.headers.cookie || "");
      if (!cookie.includes(`reasonix_token=${serveToken}`)) {
        res.writeHead(401);
        res.end("Unauthorized\n");
        return;
      }
      if (req.method === "POST" && req.url?.startsWith("/tabs/open-project")) {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ id: "tab_1", scope: "project", workspaceRoot: "/tmp/p", ready: true }));
        return;
      }
      res.writeHead(404);
      res.end("no");
    });
    await new Promise((r) => serve.listen(0, "127.0.0.1", r));
    const servePort = serve.address().port;
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "rx-shell-ui-"));
    fs.writeFileSync(path.join(tmp, "index.html"), "<!doctype html><html></html>");
    const shell = await startDesktopShell({
      staticDir: tmp,
      serveBaseUrl: `http://127.0.0.1:${servePort}`,
      token: serveToken,
    });
    try {
      const res = await fetch(`${shell.baseUrl}/tabs/open-project`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Cookie: "reasonix_token=stale-stale-stale-stale-stale-stale-stale-stale",
        },
        body: JSON.stringify({ workspaceRoot: "/tmp/p" }),
      });
      assert.equal(res.status, 200, await res.clone().text());
      const json = await res.json();
      assert.equal(json.id, "tab_1");
    } finally {
      await shell.close();
      await new Promise((r) => serve.close(() => r()));
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });
});
