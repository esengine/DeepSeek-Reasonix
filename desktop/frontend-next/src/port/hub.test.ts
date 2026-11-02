// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { SseHub } from "./hub";
import { HttpError } from "./port";

it("uploads native archives as opaque bodies to the owning host route", async () => {
  const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ path: "imported" }) });
  vi.stubGlobal("fetch", fetch);
  try {
    const file = new File(["{ not parsed here"], "session.json");
    await new SseHub().importNativeSession(file, "/folder with space");
    expect(fetch).toHaveBeenCalledWith("/tree/sessions/import-native?workspace=%2Ffolder+with+space",
      expect.objectContaining({ method: "POST", body: file,
        headers: { "content-type": "application/json" }, credentials: "same-origin" }));
  } finally { vi.unstubAllGlobals(); }
});

function answer(status: number, body: string) {
  vi.stubGlobal("fetch", async () => ({
    ok: false,
    status,
    text: async () => body,
    json: async () => JSON.parse(body),
    headers: new Map([["content-type", "text/plain"]]),
  }));
}

it("keeps local project moves and host-qualified pin/archive calls on their owning routes", async () => {
  const fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });
  vi.stubGlobal("fetch", fetch);
  try {
    const hub = new SseHub();
    await hub.pinSession("/same/session.jsonl", true);
    await hub.pinSession("/same/session.jsonl", false, "gpu box");
    await hub.archiveSession("/same/session.jsonl", true);
    await hub.archiveSession("/same/session.jsonl", false, "gpu box");
    await hub.markSessionRead("/same/session.jsonl");
    await hub.markSessionRead("/same/session.jsonl", "gpu box");
    await hub.moveSessionTo("/same/session.jsonl", "/other-project");
    expect(fetch.mock.calls.map(([path, init]) => [path, JSON.parse(init.body)])).toEqual([
      ["/tree/sessions/pin", { path: "/same/session.jsonl", pinned: true }],
      ["/remotes/gpu%20box/sessions/pin", { path: "/same/session.jsonl", pinned: false }],
      ["/tree/sessions/archive", { path: "/same/session.jsonl", archived: true }],
      ["/remotes/gpu%20box/sessions/archive", { path: "/same/session.jsonl", archived: false }],
      ["/tree/sessions/viewed", { path: "/same/session.jsonl" }],
      ["/remotes/gpu%20box/sessions/viewed", { path: "/same/session.jsonl" }],
      ["/tree/sessions/move-to", { path: "/same/session.jsonl", target: "/other-project" }],
    ]);
  } finally { vi.unstubAllGlobals(); }
});

it("carries the same global recent-page query to local and remote tree owners", async () => {
  const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => [] });
  vi.stubGlobal("fetch", fetch);
  try {
    const hub = new SseHub();
    const window = { order: "recent" as const, scope: "active" as const, offset: 350, limit: 50, hideLegacy: true, q: "release plan" };
    await hub.tree(window);
    await hub.remoteTree("gpu box", window);
    const paths = fetch.mock.calls.map(([path]) => new URL(path, "http://localhost"));
    expect(paths[0].pathname).toBe("/tree");
    expect(paths[1].pathname).toBe("/remotes/gpu%20box/tree");
    for (const path of paths) {
      expect(Object.fromEntries(path.searchParams)).toMatchObject({ order: "recent", scope: "active", offset: "350", limit: "50", hideLegacy: "1", q: "release plan" });
    }
  } finally { vi.unstubAllGlobals(); }
});

// The same defect SseHttp was fixed for, still standing in the hub port: asking
// for JSON first threw away every answer that was not an envelope, and what
// reached the window was "this request did not reach the kernel (HTTP 502)" —
// wrong twice over, because it had, and the kernel had explained itself.
describe("a failed hub call", () => {
  it("surfaces a remote book conflict instead of treating it as an unread book", async () => {
    answer(409, JSON.stringify({ code: "remote.serve_provider_mismatch", error: "provider setting conflict", params: { host: "gpu-box" } }));
    await expect(new SseHub().remoteTree("gpu-box")).rejects.toMatchObject({ status: 409,
      reason: { code: "remote.serve_provider_mismatch", params: { host: "gpu-box" } } });
  });
  it("carries a plain-text reason through", async () => {
    answer(502, "remote /runtimes: 400 Bad Request: workspace /srv/data is not a directory");
    const err = (await new SseHub().openRemote({ host: "gpu-box" }).catch((e) => e)) as HttpError;
    expect(err).toBeInstanceOf(HttpError);
    expect(err.status).toBe(502);
    expect(err.detailed).toBe(true);
    expect(err.message).toContain("not a directory");
  });

  it("still prefers the refusal envelope when there is one", async () => {
    answer(502, JSON.stringify({
      code: "remote.kernel_refused",
      error: "remote /runtimes: 400 Bad Request",
      params: { host: "gpu-box", detail: "workspace /srv/data is not a directory" },
    }));
    const err = (await new SseHub().openRemote({ host: "gpu-box" }).catch((e) => e)) as HttpError;
    expect(err.reason?.code).toBe("remote.kernel_refused");
    expect(err.reason?.params?.host).toBe("gpu-box");
    expect(err.detailed).toBe(true);
  });

  // Nothing to report is the one case where a path and a status are all there
  // is, and the only one that may say the request never arrived.
  it("falls back to the path and status on an empty body", async () => {
    answer(502, "");
    const err = (await new SseHub().openRemote({ host: "gpu-box" }).catch((e) => e)) as HttpError;
    expect(err.message).toBe("/remotes/open: 502");
    expect(err.detailed).toBe(false);
  });
});

function jsonAnswer(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
    json: async () => body,
    headers: new Map([["content-type", "application/json"]]),
  };
}

it("serializes the remote composition choice at the HTTP request boundary", async () => {
  const fetch = vi.fn(async () => jsonAnswer(200, { id: "remote", root: "/project", host: "gpu", base: "/runtime/remote", name: "Remote" }));
  vi.stubGlobal("fetch", fetch);
  try {
    const hub = new SseHub();
    for (const agentComposition of ["builtin:minimal", "builtin:general", undefined]) {
      await hub.openRemote({ host: "gpu", workspace: "/project", agentComposition });
      const [url, init] = fetch.mock.lastCall as unknown as [string, RequestInit];
      expect(url).toBe("/remotes/open");
      expect(JSON.parse(init.body as string)).toEqual({ host: "gpu", workspace: "/project", sessionPath: "", ...(agentComposition ? { agentComposition } : {}) });
    }
  } finally { vi.unstubAllGlobals(); }
});

describe("host capabilities", () => {
  it("does not ask a kernel picker the capability probe says is absent", async () => {
    const calls: Array<[string, string]> = [];
    vi.stubGlobal("fetch", async (path: string, init?: RequestInit) => {
      calls.push([init?.method ?? "GET", path]);
      if (path === "/host/capabilities") {
        return jsonAnswer(200, { pickFolder: false, addWorkspace: true });
      }
      return answer(501, JSON.stringify({ code: "picker.unsupported", error: "native folder picker is unavailable" }));
    });

    expect(await new SseHub().hostCapabilities()).toEqual({ pickFolder: false, addWorkspace: true });
    expect(calls).toEqual([["GET", "/host/capabilities"]]);
  });
});

describe("closing a pane the kernel no longer has", () => {
  it("counts as closed, so a stale pane id never reaches the person as an error", async () => {
    answer(404, JSON.stringify({ code: "request.not_found", error: "no runtime named r1", params: { kind: "runtime", name: "r1" } }));
    await expect(new SseHub().close("r1")).resolves.toBeUndefined();
  });

  it("still surfaces every other refusal", async () => {
    answer(500, JSON.stringify({ code: "internal", error: "teardown failed" }));
    await expect(new SseHub().close("r1")).rejects.toBeInstanceOf(HttpError);
  });
});
