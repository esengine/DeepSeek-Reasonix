import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { _resetForTests, detectProxyUrl, installProxyIfConfigured } from "../src/net/proxy.js";

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
    expect(result).toEqual({ url: "http://example:8080", reinstalled: false });
  });

  it("returns reinstalled=true on subsequent installs (idempotent at the env-detect level)", () => {
    installProxyIfConfigured({ HTTPS_PROXY: "http://first:8080" });
    const second = installProxyIfConfigured({ HTTPS_PROXY: "http://second:8080" });
    expect(second?.reinstalled).toBe(true);
    expect(second?.url).toBe("http://second:8080");
  });
});
