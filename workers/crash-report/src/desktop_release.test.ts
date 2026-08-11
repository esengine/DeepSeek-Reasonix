import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cliReleaseChannel,
  desktopReleaseChannel,
  handleCLIRelease,
  handleDesktopReleaseManifest,
} from "./desktop_release";
import worker from "./index";

const sha256 = "a".repeat(64);

function desktopManifest(version: string, base?: string) {
  const releaseBase = base ?? `https://dl.reasonix.io/desktop-${version}/`;
  const asset = (name: string) => {
    const url = releaseBase + name;
    return { url, sig: `${url}.minisig`, size: 42, sha256 };
  };
  return {
    version,
    download_page: "https://reasonix.io/?download=desktop#start",
    platforms: {
      "darwin-arm64": asset("Reasonix-darwin-arm64.zip"),
      "darwin-amd64": asset("Reasonix-darwin-amd64.zip"),
      "windows-amd64": asset("Reasonix-windows-amd64-installer.exe"),
      "windows-arm64": asset("Reasonix-windows-arm64-installer.exe"),
      "linux-amd64": asset("Reasonix-linux-amd64.tar.gz"),
    },
    native_packages: {
      "linux-amd64": asset("Reasonix-linux-amd64.deb"),
    },
    downloads: {
      "Reasonix-darwin-universal.dmg": asset("Reasonix-darwin-universal.dmg"),
      "Reasonix-windows-amd64.zip": asset("Reasonix-windows-amd64.zip"),
    },
  };
}

function desktopManifestText(version: string, base?: string): string {
  return JSON.stringify(desktopManifest(version, base));
}

function githubDesktopRelease(version: string, overrides: Record<string, unknown> = {}) {
  const tag = `desktop-${version}`;
  return {
    tag_name: tag,
    draft: false,
    prerelease: false,
    assets: [{
      name: "latest.json",
      browser_download_url:
        `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/latest.json`,
      size: 42,
    }],
    ...overrides,
  };
}

const cliAssets = [
  "reasonix-darwin-amd64.tar.gz",
  "reasonix-darwin-arm64.tar.gz",
  "reasonix-linux-amd64.tar.gz",
  "reasonix-linux-arm64.tar.gz",
  "reasonix-windows-amd64.zip",
  "reasonix-windows-arm64.zip",
  "SHA256SUMS",
];

const cliRelease = (tag: string, prerelease: boolean) => ({
  tag_name: tag,
  prerelease,
  html_url: `https://github.com/esengine/DeepSeek-Reasonix/releases/tag/${tag}`,
  assets: cliAssets.map((name) => ({
    name,
    browser_download_url: `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/${name}`,
    size: 42,
  })),
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

  it("serves a complete Preview manifest from the canonical pointer first", async () => {
    const fetchMock = vi.fn(async (_url: string) =>
      new Response(desktopManifestText("v1.2.0-preview.7"), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("preview");

    expect(response.status).toBe(200);
    expect(response.headers.get("access-control-allow-origin")).toBe("*");
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-preview");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://dl.reasonix.io/preview/latest.json");
  });

  it("keeps serving the signed rolling Preview manifest during pointer migration", async () => {
    const legacy = desktopManifest(
      "v1.18.0-preview.62",
      "https://dl.reasonix.io/desktop-preview/",
    );
    Reflect.deleteProperty(legacy, "downloads");
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify(legacy), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("preview");

    expect(response.status).toBe(200);
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-preview");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("rejects a new-format Preview manifest that still uses rolling assets", async () => {
    const rolling = desktopManifest(
      "v1.18.0-preview.63",
      "https://dl.reasonix.io/desktop-preview/",
    );
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(rolling), { status: 200 }))
      .mockResolvedValueOnce(new Response("missing", { status: 404 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("preview");

    expect(response.status).toBe(502);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("continues to the compatibility pointer after an invalid 200 response", async () => {
    const invalid = desktopManifest("v1.2.0-preview.8");
    invalid.platforms["darwin-arm64"].size = 0;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(invalid), { status: 200 }))
      .mockResolvedValueOnce(new Response(desktopManifestText("v1.2.0-preview.7"), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("preview");

    expect(response.status).toBe(200);
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-canary-compat");
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      "https://dl.reasonix.io/preview/latest.json",
      "https://dl.reasonix.io/canary/latest.json",
    ]);
  });

  it("rejects hostile URLs and incomplete Desktop manifests", async () => {
    const cases: Array<[string, (manifest: ReturnType<typeof desktopManifest>) => void]> = [
      ["malicious host", (manifest) => {
        const url = "https://evil.invalid/desktop-v1.2.0-preview.7/Reasonix-darwin-arm64.zip";
        Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
      }],
      ["userinfo", (manifest) => {
        const url = "https://dl.reasonix.io@evil.invalid/desktop-v1.2.0-preview.7/Reasonix-darwin-arm64.zip";
        Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
      }],
      ["http", (manifest) => {
        const url = "http://dl.reasonix.io/desktop-v1.2.0-preview.7/Reasonix-darwin-arm64.zip";
        Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
      }],
      ["wrong channel path", (manifest) => {
        const url = "https://dl.reasonix.io/preview/Reasonix-darwin-arm64.zip";
        Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
      }],
      ["wrong filename", (manifest) => {
        const url = "https://dl.reasonix.io/desktop-v1.2.0-preview.7/Reasonix-darwin-amd64.zip";
        Object.assign(manifest.platforms["darwin-arm64"], { url, sig: `${url}.minisig` });
      }],
      ["missing asset", (manifest) => {
        delete (manifest.platforms as Partial<typeof manifest.platforms>)["windows-arm64"];
      }],
      ["missing website download", (manifest) => {
        delete (manifest.downloads as Partial<typeof manifest.downloads>)["Reasonix-darwin-universal.dmg"];
      }],
      ["invalid website download", (manifest) => {
        manifest.downloads["Reasonix-windows-amd64.zip"].size = 0;
      }],
      ["bad SHA", (manifest) => {
        manifest.platforms["darwin-arm64"].sha256 = "A".repeat(64);
      }],
      ["zero size", (manifest) => {
        manifest.platforms["darwin-arm64"].size = 0;
      }],
      ["size above release maximum", (manifest) => {
        manifest.platforms["darwin-arm64"].size = 1073741825;
      }],
      ["bad signature", (manifest) => {
        manifest.platforms["darwin-arm64"].sig += "?mirror=1";
      }],
      ["wrong download page", (manifest) => {
        manifest.download_page = "https://evil.invalid/download";
      }],
    ];

    for (const [name, mutate] of cases) {
      const manifest = desktopManifest("v1.2.0-preview.7");
      mutate(manifest);
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(new Response(JSON.stringify(manifest), { status: 200 }))
        .mockResolvedValueOnce(new Response("missing", { status: 404 }));
      vi.stubGlobal("fetch", fetchMock);

      const response = await handleDesktopReleaseManifest("preview");

      expect(response.status, name).toBe(502);
      expect(fetchMock, name).toHaveBeenCalledTimes(2);
      vi.unstubAllGlobals();
    }
  });
});

describe("desktop Stable GitHub fallback", () => {
  it("accepts the exact versioned R2 asset directory", async () => {
    const fetchMock = vi.fn(async (_url: string) =>
      new Response(desktopManifestText("v1.18.0"), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("stable");

    expect(response.status).toBe(200);
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-stable");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("keeps serving a legacy Stable manifest that predates website downloads", async () => {
    const legacy = desktopManifest("v1.17.21");
    Reflect.deleteProperty(legacy, "downloads");
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify(legacy), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("stable");

    expect(response.status).toBe(200);
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-stable");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("accepts null but rejects an empty downloads object as legacy", async () => {
    const nullDownloads = desktopManifest("v1.17.21");
    Reflect.set(nullDownloads, "downloads", null);
    const nullFetch = vi.fn(async () =>
      new Response(JSON.stringify(nullDownloads), { status: 200 }),
    );
    vi.stubGlobal("fetch", nullFetch);

    const legacyResponse = await handleDesktopReleaseManifest("stable");
    expect(legacyResponse.status).toBe(200);
    expect(nullFetch).toHaveBeenCalledTimes(1);

    const emptyDownloads = desktopManifest("v1.17.21");
    Reflect.set(emptyDownloads, "downloads", {});
    const emptyFetch = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(emptyDownloads), { status: 200 }))
      .mockResolvedValueOnce(new Response("missing", { status: 404 }));
    vi.stubGlobal("fetch", emptyFetch);

    const invalidResponse = await handleDesktopReleaseManifest("stable");
    expect(invalidResponse.status).toBe(502);
    expect(emptyFetch).toHaveBeenCalledTimes(2);
  });

  it("uses /releases/latest and requires the release tag to match the manifest version", async () => {
    const githubBase =
      "https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.18.0/";
    const invalidR2 = desktopManifest("v1.19.0");
    invalidR2.platforms["darwin-arm64"].size = 0;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(invalidR2), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(githubDesktopRelease("v1.18.0")), { status: 200 }))
      .mockResolvedValueOnce(new Response(desktopManifestText("v1.18.0", githubBase), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("stable");
    const body = await response.json() as { version?: string };

    expect(response.status).toBe(200);
    expect(body.version).toBe("v1.18.0");
    expect(response.headers.get("x-reasonix-release-source")).toBe("github-desktop-release");
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      "https://dl.reasonix.io/latest/latest.json",
      "https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases/latest",
      `${githubBase}latest.json`,
    ]);
  });

  it("rejects a GitHub manifest whose version disagrees with the latest release tag", async () => {
    const manifestBase =
      "https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.17.9/";
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("missing", { status: 404 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(githubDesktopRelease("v1.18.0")), { status: 200 }))
      .mockResolvedValueOnce(new Response(desktopManifestText("v1.17.9", manifestBase), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleDesktopReleaseManifest("stable");

    expect(response.status).toBe(502);
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("rejects a non-canonical or zero-byte latest.json release asset", async () => {
    for (const asset of [
      {
        name: "latest.json",
        browser_download_url:
          "https://evil.invalid/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.18.0/latest.json",
        size: 42,
      },
      {
        name: "latest.json",
        browser_download_url:
          "https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.18.0/latest.json",
        size: 0,
      },
    ]) {
      const release = githubDesktopRelease("v1.18.0", { assets: [asset] });
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(new Response("missing", { status: 404 }))
        .mockResolvedValueOnce(new Response(JSON.stringify(release), { status: 200 }));
      vi.stubGlobal("fetch", fetchMock);

      const response = await handleDesktopReleaseManifest("stable");

      expect(response.status).toBe(502);
      expect(fetchMock).toHaveBeenCalledTimes(2);
      vi.unstubAllGlobals();
    }
  });
});

describe("release gateway HTTP method contract", () => {
  const env = {} as Parameters<typeof worker.fetch>[1];

  it("answers CORS preflight without loading an upstream release", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await worker.fetch(
      new Request("https://crash.reasonix.io/v1/cli/releases/stable/latest.json", {
        method: "OPTIONS",
      }),
      env,
    );

    expect(response.status).toBe(204);
    expect(response.headers.get("access-control-allow-origin")).toBe("*");
    expect(response.headers.get("access-control-allow-methods")).toBe("GET, HEAD, OPTIONS");
    expect(response.headers.get("access-control-max-age")).toBe("86400");
    expect(response.headers.get("allow")).toBe("GET, HEAD, OPTIONS");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("serves HEAD with GET status and headers but no body", async () => {
    const fetchMock = vi.fn(async (_url: string) =>
      new Response(desktopManifestText("v1.2.0-preview.7"), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await worker.fetch(
      new Request("https://crash.reasonix.io/v1/desktop/releases/preview/latest.json", {
        method: "HEAD",
      }),
      env,
    );

    expect(response.status).toBe(200);
    expect(await response.text()).toBe("");
    expect(response.headers.get("access-control-allow-origin")).toBe("*");
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-preview");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("returns a CORS-aware 405 for unsupported release methods", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await worker.fetch(
      new Request("https://crash.reasonix.io/v1/cli/releases/preview/latest.json", {
        method: "POST",
      }),
      env,
    );

    expect(response.status).toBe(405);
    expect(response.headers.get("access-control-allow-origin")).toBe("*");
    expect(response.headers.get("allow")).toBe("GET, HEAD, OPTIONS");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("CLI public release gateway", () => {
  it("recognizes only Stable and Preview routes", () => {
    expect(cliReleaseChannel("/v1/cli/releases/stable/latest.json")).toBe("stable");
    expect(cliReleaseChannel("/v1/cli/releases/preview/latest.json")).toBe("preview");
    expect(cliReleaseChannel("/v1/cli/releases/rc/latest.json")).toBeNull();
  });

  it("serves a complete strict Preview pointer from R2", async () => {
    const fetchMock = vi.fn(async (_url: string) =>
      new Response(JSON.stringify(cliRelease("v1.18.0-preview.1", true)), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleCLIRelease("preview");
    const body = await response.json() as { tag_name?: string };

    expect(body.tag_name).toBe("v1.18.0-preview.1");
    expect(response.headers.get("access-control-allow-origin")).toBe("*");
    expect(response.headers.get("x-reasonix-release-source")).toBe("r2-cli-preview");
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://dl.reasonix.io/cli/preview/latest.json");
  });

  it("rewrites release notes to the canonical repository tag URL", async () => {
    const release = cliRelease("v1.18.0", false);
    release.html_url = "https://evil.invalid/phishing";
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify(release), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleCLIRelease("stable");
    const body = await response.json() as { html_url?: string };

    expect(body.html_url).toBe(
      "https://github.com/esengine/DeepSeek-Reasonix/releases/tag/v1.18.0",
    );
  });

  it("falls back after an invalid 200 and strictly filters GitHub releases", async () => {
    const invalidPointer = cliRelease("v1.18.0-preview.20", true);
    invalidPointer.assets[0]!.size = 0;
    const releases = [
      cliRelease("v1.19.0-rc.1", true),
      cliRelease("v1.18.0-preview.2", true),
      cliRelease("v1.18.0-preview.12", true),
      cliRelease("v1.18.0-preview.13", false),
      cliRelease("v1.17.21", false),
    ];
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(invalidPointer), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(releases), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleCLIRelease("preview");
    const body = await response.json() as { tag_name?: string };

    expect(body.tag_name).toBe("v1.18.0-preview.12");
    expect(response.headers.get("x-reasonix-release-source")).toBe("github-cli-releases");
    expect(fetchMock.mock.calls[1]?.[0]).toContain("releases?per_page=100");
  });

  it("requires every CLI asset URL to be canonical", async () => {
    const invalidURLs = [
      "https://evil.invalid/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-amd64.tar.gz",
      "https://github.com@evil.invalid/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-amd64.tar.gz",
      "http://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-amd64.tar.gz",
      "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.19.0/reasonix-darwin-amd64.tar.gz",
      "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.20.0/reasonix-darwin-arm64.tar.gz",
    ];

    for (const browserDownloadURL of invalidURLs) {
      const invalid = cliRelease("v1.20.0", false);
      invalid.assets[0]!.browser_download_url = browserDownloadURL;
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(new Response(JSON.stringify(invalid), { status: 200 }))
        .mockResolvedValueOnce(new Response(JSON.stringify([cliRelease("v1.19.0", false)]), { status: 200 }));
      vi.stubGlobal("fetch", fetchMock);

      const response = await handleCLIRelease("stable");
      const body = await response.json() as { tag_name?: string };

      expect(body.tag_name).toBe("v1.19.0");
      expect(response.headers.get("x-reasonix-release-source")).toBe("github-cli-releases");
      vi.unstubAllGlobals();
    }
  });

  it("rejects non-positive, malformed, and oversized CLI asset sizes", async () => {
    const invalidSizes: unknown[] = [
      0,
      -1,
      "42",
      undefined,
      1.5,
      1073741825,
      Number.MAX_SAFE_INTEGER + 1,
      NaN,
    ];
    for (const size of invalidSizes) {
      const invalid = cliRelease("v1.20.0", false);
      (invalid.assets[0] as { size?: unknown }).size = size;
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(new Response(JSON.stringify(invalid), { status: 200 }))
        .mockResolvedValueOnce(new Response(JSON.stringify([cliRelease("v1.19.0", false)]), { status: 200 }));
      vi.stubGlobal("fetch", fetchMock);

      const response = await handleCLIRelease("stable");
      const body = await response.json() as { tag_name?: string };

      expect(body.tag_name, `size ${String(size)}`).toBe("v1.19.0");
      expect(response.headers.get("x-reasonix-release-source")).toBe("github-cli-releases");
      vi.unstubAllGlobals();
    }
  });

  it("rejects duplicate required CLI assets", async () => {
    const invalid = cliRelease("v1.20.0", false);
    invalid.assets.push({ ...invalid.assets[0]! });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(invalid), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify([cliRelease("v1.19.0", false)]), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleCLIRelease("stable");
    const body = await response.json() as { tag_name?: string };

    expect(body.tag_name).toBe("v1.19.0");
    expect(response.headers.get("x-reasonix-release-source")).toBe("github-cli-releases");
  });

  it("rejects incomplete releases and compares huge Stable versions exactly", async () => {
    const incomplete = cliRelease("v100000000000000000001.0.0", false);
    incomplete.assets.pop();
    const releases = [
      incomplete,
      cliRelease("v99999999999999999999.999.999", false),
      cliRelease("v100000000000000000000.0.0", false),
    ];
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(incomplete), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(releases), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await handleCLIRelease("stable");
    const body = await response.json() as { tag_name?: string };

    expect(body.tag_name).toBe("v100000000000000000000.0.0");
    expect(response.headers.get("x-reasonix-release-source")).toBe("github-cli-releases");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
