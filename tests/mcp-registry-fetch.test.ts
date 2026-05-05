/** Registry fetcher — mocked fetch, temp cache; verifies fallback chain + lazy paging + spec generation. */

import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  CACHE_SCHEMA_VERSION,
  CACHE_TTL_MS,
  fallbackFromCatalog,
  fetchOfficialPage,
  fetchSmitheryDetail,
  fetchSmitheryFirstPage,
  handleToFetchResult,
  loadMorePages,
  openRegistry,
  specStringFor,
} from "../src/mcp/registry-fetch.js";

interface MockResponse {
  ok: boolean;
  status?: number;
  json?: unknown;
}

function mockFetch(map: Record<string, MockResponse | MockResponse[]>): typeof fetch {
  const counters: Record<string, number> = {};
  return (async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    const matched = Object.keys(map).find((k) => url.startsWith(k));
    if (!matched) throw new Error(`unmocked URL: ${url}`);
    const slot = map[matched]!;
    const m = Array.isArray(slot) ? slot[counters[matched] ?? 0] : slot;
    counters[matched] = (counters[matched] ?? 0) + 1;
    if (!m) throw new Error(`exhausted mock for ${url}`);
    return {
      ok: m.ok,
      status: m.status ?? (m.ok ? 200 : 500),
      json: async () => m.json,
    } as unknown as Response;
  }) as typeof fetch;
}

const PAGE_1 = {
  servers: [
    {
      server: {
        name: "io.example/echo",
        title: "Echo",
        description: "A test echo server",
        packages: [
          {
            registryType: "npm",
            identifier: "@example/echo-mcp",
            version: "1.2.3",
            transport: { type: "stdio" },
            environmentVariables: [{ name: "ECHO_TOKEN" }],
          },
        ],
      },
    },
    {
      server: {
        name: "io.example/remote",
        description: "remote-only server",
        remotes: [{ type: "streamable-http", url: "https://remote.example.com/mcp" }],
      },
    },
  ],
  metadata: { nextCursor: "cursor-page-2" },
};

const PAGE_2 = {
  servers: [
    {
      server: {
        name: "io.example/page2",
        description: "from page 2",
        packages: [
          { registryType: "npm", identifier: "@example/page2", transport: { type: "stdio" } },
        ],
      },
    },
  ],
  metadata: { nextCursor: "cursor-page-3" },
};

const PAGE_3_LAST = {
  servers: [
    {
      server: {
        name: "io.example/page3",
        description: "last page",
        packages: [
          { registryType: "npm", identifier: "@example/page3", transport: { type: "stdio" } },
        ],
      },
    },
  ],
  metadata: {},
};

const SMITHERY_RESPONSE = {
  servers: [
    {
      qualifiedName: "@vendor/x",
      displayName: "Vendor X",
      description: "Vendor's tool",
      useCount: 1234,
      homepage: "https://vendor.example",
    },
  ],
  pagination: { totalPages: 1 },
};

describe("fetchOfficialPage", () => {
  it("normalizes one page and returns the next cursor", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/v0/servers": { ok: true, json: PAGE_1 },
    });
    const result = await fetchOfficialPage(null, fetchImpl);
    expect(result.entries).toHaveLength(2);
    expect(result.nextCursor).toBe("cursor-page-2");
    expect(result.entries[0]).toMatchObject({
      name: "io.example/echo",
      install: { runtime: "npm", packageId: "@example/echo-mcp", transport: "stdio" },
    });
    expect(result.entries[1]).toMatchObject({
      install: {
        runtime: "remote",
        transport: "streamable-http",
        url: "https://remote.example.com/mcp",
      },
    });
  });

  it("includes the cursor in the URL when provided", async () => {
    let seenUrl = "";
    const fetchImpl = (async (input: RequestInfo | URL) => {
      seenUrl = typeof input === "string" ? input : input.toString();
      return { ok: true, status: 200, json: async () => PAGE_2 } as unknown as Response;
    }) as typeof fetch;
    const result = await fetchOfficialPage("cursor-page-2", fetchImpl);
    expect(seenUrl).toContain("cursor=cursor-page-2");
    expect(result.entries).toHaveLength(1);
  });

  it("throws on HTTP failure", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/v0/servers": { ok: false, status: 503, json: {} },
    });
    await expect(fetchOfficialPage(null, fetchImpl)).rejects.toThrow(/503/);
  });
});

describe("fetchSmitheryFirstPage", () => {
  it("normalizes listing entries (no install info)", async () => {
    const fetchImpl = mockFetch({
      "https://registry.smithery.ai/servers": { ok: true, json: SMITHERY_RESPONSE },
    });
    const entries = await fetchSmitheryFirstPage(fetchImpl);
    expect(entries).toEqual([
      {
        name: "@vendor/x",
        title: "Vendor X",
        description: "Vendor's tool",
        source: "smithery",
        popularity: 1234,
        homepage: "https://vendor.example",
      },
    ]);
  });
});

describe("openRegistry — initial open", () => {
  let dir: string;
  let cachePath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-mcp-registry-"));
    cachePath = join(dir, "cache.json");
  });
  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("loads only one page from the official registry by default", async () => {
    let callCount = 0;
    const fetchImpl = (async (input: RequestInfo | URL) => {
      callCount++;
      const url = typeof input === "string" ? input : input.toString();
      if (url.startsWith("https://registry.modelcontextprotocol.io/")) {
        return { ok: true, status: 200, json: async () => PAGE_1 } as unknown as Response;
      }
      throw new Error(`unmocked: ${url}`);
    }) as typeof fetch;
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(callCount).toBe(1);
    expect(handle.source).toBe("official");
    expect(handle.cache.entries).toHaveLength(2);
    expect(handle.cache.pagination.pagesLoaded).toBe(1);
    expect(handle.cache.pagination.nextCursor).toBe("cursor-page-2");
  });

  it("falls back to smithery when official fails", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: false, status: 503, json: {} },
      "https://registry.smithery.ai/": { ok: true, json: SMITHERY_RESPONSE },
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(handle.source).toBe("smithery");
    expect(handle.errors[0]).toMatch(/official/);
    expect(handle.cache.entries[0]?.name).toBe("@vendor/x");
  });

  it("falls back to local catalog when both registries fail and no cache exists", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: false, status: 503, json: {} },
      "https://registry.smithery.ai/": { ok: false, status: 503, json: {} },
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(handle.source).toBe("local");
    expect(handle.cache.entries.length).toBeGreaterThan(0);
    expect(handle.errors).toHaveLength(2);
  });

  it("prefers stale cache over local catalog when both registries fail", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({
        schemaVersion: CACHE_SCHEMA_VERSION,
        fetchedAt: Date.now() - 2 * CACHE_TTL_MS,
        source: "official",
        entries: [{ name: "stale-entry", title: "Stale", description: "x", source: "official" }],
        pagination: { pagesLoaded: 1, nextCursor: "next" },
      }),
    );
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: false, status: 503, json: {} },
      "https://registry.smithery.ai/": { ok: false, status: 503, json: {} },
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(handle.source).toBe("official");
    expect(handle.fromCache).toBe(true);
    expect(handle.cache.entries[0]?.name).toBe("stale-entry");
  });

  it("returns fresh cache without hitting network", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({
        schemaVersion: CACHE_SCHEMA_VERSION,
        fetchedAt: Date.now() - 60_000,
        source: "official",
        entries: [{ name: "fresh-entry", title: "Fresh", description: "x", source: "official" }],
        pagination: { pagesLoaded: 1, nextCursor: null },
      }),
    );
    const fetchImpl = (async () => {
      throw new Error("network must not be called when cache is fresh");
    }) as typeof fetch;
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(handle.fromCache).toBe(true);
    expect(handle.cache.entries[0]?.name).toBe("fresh-entry");
  });

  it("rejects an old-schema cache file", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({
        fetchedAt: Date.now() - 60_000,
        source: "official",
        entries: [{ name: "old", title: "Old", description: "x", source: "official" }],
      }),
    );
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: true, json: PAGE_1 },
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(handle.fromCache).toBe(false);
    expect(handle.cache.entries.find((e) => e.name === "old")).toBeUndefined();
  });

  it("noCache forces a refresh past the TTL gate", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({
        schemaVersion: CACHE_SCHEMA_VERSION,
        fetchedAt: Date.now() - 60_000,
        source: "official",
        entries: [{ name: "fresh-entry", title: "Fresh", description: "x", source: "official" }],
        pagination: { pagesLoaded: 1, nextCursor: null },
      }),
    );
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: true, json: PAGE_1 },
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath, noCache: true });
    expect(handle.fromCache).toBe(false);
    expect(handle.cache.entries.some((e) => e.name === "io.example/echo")).toBe(true);
  });

  it("preferSource: local skips network entirely", async () => {
    const fetchImpl = (async () => {
      throw new Error("network must not be called when preferSource=local");
    }) as typeof fetch;
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath, preferSource: "local" });
    expect(handle.source).toBe("local");
    expect(handle.cache.entries.length).toBeGreaterThan(0);
  });

  it("writes the cache file after a successful first-page fetch", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: true, json: PAGE_1 },
    });
    await openRegistry({ fetcher: fetchImpl, cachePath });
    const persisted = JSON.parse(readFileSync(cachePath, "utf8"));
    expect(persisted.schemaVersion).toBe(CACHE_SCHEMA_VERSION);
    expect(persisted.source).toBe("official");
    expect(persisted.entries.length).toBe(2);
    expect(persisted.pagination.pagesLoaded).toBe(1);
  });
});

describe("loadMorePages", () => {
  let dir: string;
  let cachePath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-mcp-registry-"));
    cachePath = join(dir, "cache.json");
  });
  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("appends a single page on demand", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/v0/servers": [
        { ok: true, json: PAGE_1 },
        { ok: true, json: PAGE_2 },
      ],
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(handle.cache.entries).toHaveLength(2);
    const r = await loadMorePages(handle, { pages: 1, fetcher: fetchImpl });
    expect(r.pagesAdded).toBe(1);
    expect(r.newEntries).toBe(1);
    expect(r.exhausted).toBe(false);
    expect(handle.cache.entries).toHaveLength(3);
    expect(handle.cache.pagination.pagesLoaded).toBe(2);
    expect(handle.cache.pagination.nextCursor).toBe("cursor-page-3");
  });

  it("walks until the source is exhausted (nextCursor=null)", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/v0/servers": [
        { ok: true, json: PAGE_1 },
        { ok: true, json: PAGE_2 },
        { ok: true, json: PAGE_3_LAST },
      ],
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    const r = await loadMorePages(handle, { pages: 10, fetcher: fetchImpl });
    expect(r.pagesAdded).toBe(2);
    expect(r.exhausted).toBe(true);
    expect(handle.cache.pagination.nextCursor).toBeNull();
  });

  it("stops early when matchTarget is reached", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/v0/servers": [
        { ok: true, json: PAGE_1 },
        { ok: true, json: PAGE_2 },
        { ok: true, json: PAGE_3_LAST },
      ],
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    const r = await loadMorePages(handle, {
      pages: 10,
      fetcher: fetchImpl,
      matchTarget: 1,
      filter: (e) => e.name.includes("page2"),
    });
    expect(r.pagesAdded).toBe(1);
    expect(handle.cache.entries.some((e) => e.name === "io.example/page2")).toBe(true);
    expect(handle.cache.pagination.pagesLoaded).toBe(2);
  });

  it("is a no-op for non-official sources", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: false, status: 503, json: {} },
      "https://registry.smithery.ai/": { ok: true, json: SMITHERY_RESPONSE },
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    expect(handle.source).toBe("smithery");
    const r = await loadMorePages(handle, { pages: 5, fetcher: fetchImpl });
    expect(r.pagesAdded).toBe(0);
    expect(r.exhausted).toBe(true);
  });

  it("returns exhausted when nextCursor is already null", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({
        schemaVersion: CACHE_SCHEMA_VERSION,
        fetchedAt: Date.now() - 60_000,
        source: "official",
        entries: [{ name: "done", title: "Done", description: "x", source: "official" }],
        pagination: { pagesLoaded: 1, nextCursor: null },
      }),
    );
    const fetchImpl = (async () => {
      throw new Error("network must not be called when exhausted");
    }) as typeof fetch;
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    const r = await loadMorePages(handle, { pages: 10, fetcher: fetchImpl });
    expect(r.pagesAdded).toBe(0);
    expect(r.exhausted).toBe(true);
  });

  it("persists the cache after each loaded page", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/v0/servers": [
        { ok: true, json: PAGE_1 },
        { ok: true, json: PAGE_2 },
      ],
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    await loadMorePages(handle, { pages: 1, fetcher: fetchImpl });
    const persisted = JSON.parse(readFileSync(cachePath, "utf8"));
    expect(persisted.pagination.pagesLoaded).toBe(2);
    expect(persisted.entries.length).toBe(3);
  });
});

describe("handleToFetchResult", () => {
  let dir: string;
  let cachePath: string;
  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-mcp-registry-"));
    cachePath = join(dir, "cache.json");
  });
  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("flattens the handle into a result with hasMore reflecting the cursor", async () => {
    const fetchImpl = mockFetch({
      "https://registry.modelcontextprotocol.io/": { ok: true, json: PAGE_1 },
    });
    const handle = await openRegistry({ fetcher: fetchImpl, cachePath });
    const r = handleToFetchResult(handle);
    expect(r.entries.length).toBe(2);
    expect(r.hasMore).toBe(true);
    expect(r.source).toBe("official");
  });
});

describe("fetchSmitheryDetail", () => {
  it("maps http connection to remote streamable-http", async () => {
    const fetchImpl = mockFetch({
      "https://registry.smithery.ai/servers/exa": {
        ok: true,
        json: {
          qualifiedName: "exa",
          remote: true,
          deploymentUrl: "https://exa.run.tools",
          connections: [{ type: "http", deploymentUrl: "https://exa.run.tools" }],
        },
      },
    });
    const r = await fetchSmitheryDetail("exa", fetchImpl);
    expect(r).toEqual({
      runtime: "remote",
      transport: "streamable-http",
      url: "https://exa.run.tools",
    });
  });

  it("maps stdio connection to npx @smithery/cli run with extraArgs", async () => {
    const fetchImpl = mockFetch({
      "https://registry.smithery.ai/servers/hugeicons": {
        ok: true,
        json: {
          qualifiedName: "hugeicons/mcp-server",
          remote: false,
          connections: [{ type: "stdio", bundleUrl: "https://x", runtime: "node" }],
        },
      },
    });
    const r = await fetchSmitheryDetail("hugeicons", fetchImpl);
    expect(r).toEqual({
      runtime: "npm",
      packageId: "@smithery/cli",
      transport: "stdio",
      extraArgs: ["run", "hugeicons"],
    });
  });

  it("returns null on 404", async () => {
    const fetchImpl = mockFetch({
      "https://registry.smithery.ai/servers/missing": { ok: false, status: 404, json: {} },
    });
    const r = await fetchSmitheryDetail("missing", fetchImpl);
    expect(r).toBeNull();
  });

  it("returns null when connection type is unrecognized", async () => {
    const fetchImpl = mockFetch({
      "https://registry.smithery.ai/servers/x": {
        ok: true,
        json: { qualifiedName: "x", connections: [{ type: "carrier-pigeon" }] },
      },
    });
    const r = await fetchSmitheryDetail("x", fetchImpl);
    expect(r).toBeNull();
  });

  it("URL-encodes the qualifiedName", async () => {
    let seenUrl = "";
    const fetchImpl = (async (input: RequestInfo | URL) => {
      seenUrl = typeof input === "string" ? input : input.toString();
      return {
        ok: true,
        status: 200,
        json: async () => ({
          qualifiedName: "vendor/x",
          connections: [{ type: "http", deploymentUrl: "https://x.example" }],
        }),
      } as unknown as Response;
    }) as typeof fetch;
    await fetchSmitheryDetail("vendor/x", fetchImpl);
    expect(seenUrl).toContain("vendor%2Fx");
  });
});

describe("fallbackFromCatalog", () => {
  it("maps every catalog entry to a stdio npm RegistryEntry", () => {
    const entries = fallbackFromCatalog();
    expect(entries.length).toBeGreaterThan(0);
    for (const e of entries) {
      expect(e.source).toBe("local");
      expect(e.install?.runtime).toBe("npm");
      expect(e.install?.transport).toBe("stdio");
      expect(e.install?.packageId).toBeTruthy();
    }
  });
});

describe("specStringFor", () => {
  it("formats npm stdio with version pin", () => {
    expect(
      specStringFor("io.example/echo", {
        runtime: "npm",
        packageId: "@example/echo-mcp",
        version: "1.2.3",
        transport: "stdio",
      }),
    ).toBe("echo=npx -y @example/echo-mcp@1.2.3");
  });

  it("formats npm stdio without version", () => {
    expect(
      specStringFor("@vendor/foo", {
        runtime: "npm",
        packageId: "@vendor/foo-mcp",
        transport: "stdio",
      }),
    ).toBe("foo=npx -y @vendor/foo-mcp");
  });

  it("formats remote SSE", () => {
    expect(
      specStringFor("io.example/remote", {
        runtime: "remote",
        transport: "sse",
        url: "https://remote.example.com/sse",
      }),
    ).toBe("remote=https://remote.example.com/sse");
  });

  it("formats remote streamable-http with the streamable+ prefix", () => {
    expect(
      specStringFor("io.example/remote", {
        runtime: "remote",
        transport: "streamable-http",
        url: "https://remote.example.com/mcp",
      }),
    ).toBe("remote=streamable+https://remote.example.com/mcp");
  });

  it("formats pypi via uvx", () => {
    expect(
      specStringFor("io.example/py", {
        runtime: "pypi",
        packageId: "mcp-server-py",
        transport: "stdio",
      }),
    ).toBe("py=uvx mcp-server-py");
  });

  it("appends extraArgs after the package id (smithery stdio shape)", () => {
    expect(
      specStringFor("vendor/x", {
        runtime: "npm",
        packageId: "@smithery/cli",
        transport: "stdio",
        extraArgs: ["run", "vendor/x"],
      }),
    ).toBe("x=npx -y @smithery/cli run vendor/x");
  });

  it("throws when npm install lacks packageId", () => {
    expect(() => specStringFor("io.example/x", { runtime: "npm", transport: "stdio" })).toThrow(
      /no packageId/,
    );
  });

  it("throws when remote install lacks URL", () => {
    expect(() => specStringFor("io.example/x", { runtime: "remote", transport: "sse" })).toThrow(
      /no URL/,
    );
  });
});
