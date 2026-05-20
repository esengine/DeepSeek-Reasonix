import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_NO_PROXY,
  _resetForTests,
  detectNoProxyRaw,
  detectProxyUrl,
  installProxyIfConfigured,
  matchesNoProxy,
  normalizeProxyUrl,
  parseNoProxy,
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

describe("detectNoProxyRaw", () => {
  it("returns null when neither NO_PROXY nor no_proxy is set", () => {
    expect(detectNoProxyRaw({})).toBeNull();
  });

  it("uppercase NO_PROXY wins over lowercase", () => {
    expect(detectNoProxyRaw({ NO_PROXY: "a.com", no_proxy: "b.com" })).toBe("a.com");
  });

  it("falls back to lowercase no_proxy", () => {
    expect(detectNoProxyRaw({ no_proxy: "b.com" })).toBe("b.com");
  });
});

describe("parseNoProxy + matchesNoProxy", () => {
  it("returns [] for empty / null input", () => {
    expect(parseNoProxy(null)).toEqual([]);
    expect(parseNoProxy("")).toEqual([]);
    expect(parseNoProxy("   ")).toEqual([]);
  });

  it("`*` matches every host", () => {
    const p = parseNoProxy("*");
    expect(matchesNoProxy("api.deepseek.com", p)).toBe(true);
    expect(matchesNoProxy("anything.example", p)).toBe(true);
  });

  it("bare hostname matches exact + dot-prefixed subdomain (curl compat)", () => {
    const p = parseNoProxy("deepseek.com");
    expect(matchesNoProxy("deepseek.com", p)).toBe(true);
    expect(matchesNoProxy("api.deepseek.com", p)).toBe(true);
    expect(matchesNoProxy("notdeepseek.com", p)).toBe(false);
  });

  it("`.suffix` matches subdomains only, NOT the bare apex", () => {
    const p = parseNoProxy(".deepseek.com");
    expect(matchesNoProxy("api.deepseek.com", p)).toBe(true);
    expect(matchesNoProxy("deepseek.com", p)).toBe(false);
  });

  it("`*.suffix` matches subdomains AND apex", () => {
    const p = parseNoProxy("*.deepseek.com");
    expect(matchesNoProxy("api.deepseek.com", p)).toBe(true);
    expect(matchesNoProxy("deepseek.com", p)).toBe(true);
    expect(matchesNoProxy("nondeepseek.com", p)).toBe(false);
  });

  it("case-insensitive matching", () => {
    const p = parseNoProxy("DEEPSEEK.COM");
    expect(matchesNoProxy("api.DeepSeek.com", p)).toBe(true);
  });

  it("strips `:port` suffix when present", () => {
    const p = parseNoProxy("api.deepseek.com:443");
    expect(matchesNoProxy("api.deepseek.com", p)).toBe(true);
  });

  it("comma-separated entries are merged", () => {
    const p = parseNoProxy("a.com, .b.com, *.c.com, 127.0.0.1");
    expect(matchesNoProxy("a.com", p)).toBe(true);
    expect(matchesNoProxy("foo.a.com", p)).toBe(true);
    expect(matchesNoProxy("foo.b.com", p)).toBe(true);
    expect(matchesNoProxy("b.com", p)).toBe(false);
    expect(matchesNoProxy("c.com", p)).toBe(true);
    expect(matchesNoProxy("127.0.0.1", p)).toBe(true);
    expect(matchesNoProxy("128.0.0.1", p)).toBe(false);
  });

  it("ignores blank entries between commas", () => {
    const p = parseNoProxy(",, foo.com , ,");
    expect(p).toHaveLength(1);
    expect(matchesNoProxy("foo.com", p)).toBe(true);
  });
});

describe("installProxyIfConfigured", () => {
  let writes: string[];
  let stderrSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    _resetForTests();
    writes = [];
    stderrSpy = vi.spyOn(process.stderr, "write").mockImplementation(((
      chunk: string | Uint8Array,
    ): boolean => {
      writes.push(typeof chunk === "string" ? chunk : Buffer.from(chunk).toString("utf8"));
      return true;
    }) as typeof process.stderr.write);
  });
  afterEach(() => {
    stderrSpy.mockRestore();
    _resetForTests();
  });

  it("returns null when no proxy is configured (no global dispatcher change)", () => {
    expect(installProxyIfConfigured({})).toBeNull();
  });

  it("returns url + reinstalled=false + default NO_PROXY patterns on first install", () => {
    const result = installProxyIfConfigured({ HTTPS_PROXY: "http://example:8080" });
    expect(result?.url).toBe("http://example:8080/");
    expect(result?.reinstalled).toBe(false);
    const raws = result?.noProxy.map((p) => p.raw) ?? [];
    for (const expected of DEFAULT_NO_PROXY) {
      expect(raws).toContain(expected);
    }
  });

  it("merges user NO_PROXY entries onto the default whitelist", () => {
    const result = installProxyIfConfigured({
      HTTPS_PROXY: "http://example:8080",
      NO_PROXY: "internal.corp.example, .private.lan",
    });
    const raws = result?.noProxy.map((p) => p.raw) ?? [];
    expect(raws).toContain("api.deepseek.com");
    expect(raws).toContain("internal.corp.example");
    expect(raws).toContain(".private.lan");
  });

  it("layers REASONIX_NO_PROXY on top of system NO_PROXY (app-specific override)", () => {
    const result = installProxyIfConfigured({
      HTTPS_PROXY: "http://example:8080",
      NO_PROXY: "system.corp.example",
      REASONIX_NO_PROXY: "app.specific.example, .reasonix.lan",
    });
    const raws = result?.noProxy.map((p) => p.raw) ?? [];
    expect(raws).toContain("system.corp.example");
    expect(raws).toContain("app.specific.example");
    expect(raws).toContain(".reasonix.lan");
  });

  it("layers opts.extraNoProxy (config) on top of env-derived patterns", () => {
    const result = installProxyIfConfigured(
      { HTTPS_PROXY: "http://example:8080", NO_PROXY: "env.example" },
      { extraNoProxy: ["config.example", ".workspace.lan"] },
    );
    const raws = result?.noProxy.map((p) => p.raw) ?? [];
    expect(raws).toContain("env.example");
    expect(raws).toContain("config.example");
    expect(raws).toContain(".workspace.lan");
  });

  it("opts.disabled short-circuits — no install, no log", () => {
    const result = installProxyIfConfigured(
      { HTTPS_PROXY: "http://example:8080" },
      { disabled: true },
    );
    expect(result).toBeNull();
    expect(writes.join("")).not.toMatch(/\[proxy\]/);
  });

  it("logs the proxy decision to stderr at startup", () => {
    installProxyIfConfigured({ HTTPS_PROXY: "http://example:8080" });
    expect(writes.join("")).toMatch(/\[proxy\] using http:\/\/example:8080\//);
    expect(writes.join("")).toMatch(/NO_PROXY: .*api\.deepseek\.com/);
  });

  it("returns reinstalled=true on subsequent installs", () => {
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
    expect(installProxyIfConfigured({ HTTPS_PROXY: "http://[invalid:::" })).toBeNull();
    expect(writes.join("")).toMatch(/ignoring proxy env value/);
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
