import { afterEach, describe, expect, it, vi } from "vitest";
import { desktopReleaseChannel, handleDesktopReleaseManifest } from "./desktop_release";

const manifest = JSON.stringify({
  version: "v1.2.0-preview.7",
  platforms: { "windows-amd64": { url: "https://example.invalid/app.exe" } },
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("desktop Preview release gateway", () => {
  it("recognizes Preview and the legacy Canary compatibility route", () => {
    expect(desktopReleaseChannel("/v1/desktop/releases/preview/latest.json")).toBe("preview");
    expect(desktopReleaseChannel("/v1/desktop/releases/canary/latest.json")).toBe("canary");
    expect(desktopReleaseChannel("/v1/desktop/releases/rc/latest.json")).toBeNull();
  });

  it("serves Preview from the canonical pointer first", async () => {
    const fetchMock = vi.fn(async (_url: string) => new Response(manifest, { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("preview");

    expect(response.status).toBe(200);
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-preview");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://dl.reasonix.io/preview/latest.json");
  });

  it("falls back to the mirrored Canary pointer for older deployments", async () => {
    const fetchMock = vi
      .fn(async (_url: string) => new Response("missing", { status: 404 }))
      .mockResolvedValueOnce(new Response("missing", { status: 404 }))
      .mockResolvedValueOnce(new Response(manifest, { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("preview");

    expect(response.status).toBe(200);
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-canary-compat");
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      "https://dl.reasonix.io/preview/latest.json",
      "https://dl.reasonix.io/canary/latest.json",
    ]);
  });
});
