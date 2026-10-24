// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { SseHub } from "./hub";

afterEach(() => vi.unstubAllGlobals());

it("reads and writes file-delivery preferences through the selected runtime", async () => {
  const fetcher = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify({ enabled: true, path: "/config.toml" })));
  vi.stubGlobal("fetch", fetcher);
  const port = new SseHub().portFor({ id: "r1", base: "/rt/r1", root: "/project", name: "Project" });
  expect((await port.presentFiles()).enabled).toBe(true);
  await port.savePresentFiles(false);
  expect(fetcher.mock.calls.map(([url]) => url)).toEqual(["/rt/r1/present-files", "/rt/r1/present-files"]);
  expect(fetcher.mock.calls[1][1]?.method).toBe("POST");
  expect(JSON.parse(fetcher.mock.calls[1][1]?.body as string)).toEqual({ enabled: false });
});
