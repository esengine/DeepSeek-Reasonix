import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  _resetForTests,
  detectProxyUrl,
  installProxyIfConfigured,
  normalizeProxyUrl,
} from "../src/net/proxy.js";

describe("detectProxyUrl (issue #646)", () => {
  it("returns null when no proxy env var is set", () => {
    expect(detectProxyUrl({})).toBeNull();
  });

  it("returns null when the proxy var is whitespace only", () => {
    expect(detectProxyUrl({ HTTPS_PROXY: "   " })).toBeNull();
  });

  it("HTTPS_PROXY wins over HTTP_PROXY (curl-style precedence)", () => {
    expect(
      detectProxyUrl({
        HTTPS_PROXY: "http://https.example:8080",
        HTTP_PROXY: "http://http.example:8080",
      }),
    ).toBe("http://https.example:8080");
  });

  it("falls back to HTTP_PROXY when HTTPS_PROXY is absent", () => {
    expect(detectProxyUrl({ HTTP_PROXY: "http://http.example:8080" })).toBe(
      "http://http.example:8080",
    );
  });

  it("falls back to ALL_PROXY last", () => {
    expect(detectProxyUrl({ ALL_PROXY: "socks5://proxy.example:1080" })).toBe(
      "socks5://proxy.example:1080",
    );
  });

  it("upper-case wins over lower-case for the same family (HTTPS_PROXY beats https_proxy)", () => {
    expect(
      detectProxyUrl({
        HTTPS_PROXY: "http://upper.example:8080",
        https_proxy: "http://lower.example:8080",
      }),
    ).toBe("http://upper.example:8080");
  });

  it("uses lower-case https_proxy when upper-case isn't set", () => {
    expect(detectProxyUrl({ https_proxy: "http://lower.example:8080" })).toBe(
      "http://lower.example:8080",
    );
  });

  it("trims surrounding whitespace", () => {
    expect(detectProxyUrl({ HTTPS_PROXY: "  http://example:8080  " })).toBe("http://example:8080");
  });
});

describe("installProxyIfConfigured", () => {
  beforeEach(() => {
    _resetForTests();
  });
  afterEach(() => {
    _resetForTests();
  });

  it("returns null when no proxy is configured (no global dispatcher change)", () => {
    expect(installProxyIfConfigured({})).toBeNull();
  });

  it("returns the detected url + reinstalled=false on the first install", () => {
    const result = installProxyIfConfigured({ HTTPS_PROXY: "http://example:8080" });
    expect(result).toEqual({ url: "http://example:8080/", reinstalled: false });
  });

  it("returns reinstalled=true on subsequent installs (idempotent at the env-detect level)", () => {
    installProxyIfConfigured({ HTTPS_PROXY: "http://first:8080" });
    const second = installProxyIfConfigured({ HTTPS_PROXY: "http://second:8080" });
    expect(second?.reinstalled).toBe(true);
    expect(second?.url).toBe("http://second:8080/");
  });

  it("auto-prefixes http:// for bare host:port (issue #1034)", () => {
    const result = installProxyIfConfigured({ HTTPS_PROXY: "127.0.0.1:10888" });
    expect(result?.url).toBe("http://127.0.0.1:10888/");
  });

  it("does not throw on a malformed env value — warns to stderr and returns null", () => {
    const writes: string[] = [];
    const orig = process.stderr.write.bind(process.stderr);
    const spy = vi.spyOn(process.stderr, "write").mockImplementation(((
      chunk: string | Uint8Array,
    ): boolean => {
      writes.push(typeof chunk === "string" ? chunk : Buffer.from(chunk).toString("utf8"));
      return true;
    }) as typeof process.stderr.write);
    try {
      expect(installProxyIfConfigured({ HTTPS_PROXY: "http://[invalid:::" })).toBeNull();
      expect(writes.join("")).toMatch(/ignoring proxy env value/);
    } finally {
      spy.mockRestore();
      process.stderr.write = orig;
    }
  });
});

describe("normalizeProxyUrl (issue #1034)", () => {
  it("returns null for empty / whitespace input", () => {
    expect(normalizeProxyUrl("")).toBeNull();
    expect(normalizeProxyUrl("   ")).toBeNull();
  });

  it("auto-prefixes http:// when the scheme is missing", () => {
    expect(normalizeProxyUrl("127.0.0.1:10888")).toBe("http://127.0.0.1:10888/");
    expect(normalizeProxyUrl("proxy.example:8080")).toBe("http://proxy.example:8080/");
  });

  it("leaves an already-prefixed URL intact", () => {
    expect(normalizeProxyUrl("http://example:8080")).toBe("http://example:8080/");
    expect(normalizeProxyUrl("socks5://example:1080")).toBe("socks5://example:1080");
  });

  it("returns null for unparseable values", () => {
    expect(normalizeProxyUrl("http://[invalid:::")).toBeNull();
  });
});
