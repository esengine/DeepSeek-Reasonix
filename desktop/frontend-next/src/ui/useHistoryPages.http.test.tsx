// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SsePort } from "../port/sse";
import { HttpError } from "../port/port";
import { useHistoryPages } from "./useHistoryPages";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function server(status: number, code?: string) {
  const paths: string[] = [];
  vi.stubGlobal("fetch", async (url: string) => {
    const path = new URL(url, "http://localhost").pathname;
    paths.push(path);
    if (path.endsWith("/history/page")) {
      return new Response(code ? JSON.stringify({ code, error: "refused" }) : "not supported", {
        status, headers: { "content-type": code ? "application/json" : "text/plain" },
      });
    }
    if (path.endsWith("/history")) {
      return Response.json([{ role: "user", content: "old conversation" }]);
    }
    throw new Error(`unexpected request: ${path}`);
  });
  const dispatch = vi.fn();
  const port = new SsePort("", "r1");
  const hook = renderHook(() => useHistoryPages(port, dispatch));
  return { ...hook, paths, dispatch };
}

it.each([404, 405, 501])("restores full history through the HTTP port when paging answers %s", async (status) => {
  const { result, paths, dispatch } = server(status);
  await act(async () => { await result.current.load(); });
  await act(async () => { await result.current.load(); });
  expect(paths.map((path) => path.slice(path.indexOf("/history")))).toEqual(["/history/page", "/history", "/history"]);
  expect(dispatch.mock.calls.at(-1)?.[0].items[0].text).toBe("old conversation");
  expect(result.current.paging).toMatchObject({ failed: false, hasMore: false });
});

it("accepts the server's explicit unsupported capability code", async () => {
  const { result, paths } = server(501, "history.pagination_unsupported");
  await act(async () => { await result.current.load(); });
  expect(paths).toHaveLength(2);
  expect(paths[1]).toMatch(/\/history$/);
});

it.each([
  [401, undefined], [403, undefined], [503, undefined], [404, "runtime.not_found"],
] as const)("preserves HTTP failure %s (%s) without a full-history request", async (status, code) => {
  const { result, paths, dispatch } = server(status, code);
  await act(async () => { await expect(result.current.load()).rejects.toBeInstanceOf(HttpError); });
  expect(paths).toHaveLength(1);
  expect(dispatch).not.toHaveBeenCalled();
  expect(result.current.paging.failed).toBe(true);
});
