import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { handleSlash } from "../src/cli/ui/slash/dispatch.js";
import { setLanguageRuntime } from "../src/i18n/index.js";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix } from "../src/index.js";
import { ToolRegistry } from "../src/tools.js";

function makeLoop(): CacheFirstLoop {
  return new CacheFirstLoop({
    client: new DeepSeekClient({ apiKey: "sk-test" }),
    prefix: new ImmutablePrefix({ system: "s", toolSpecs: [] }),
    tools: new ToolRegistry(),
    maxToolIters: 1,
    stream: false,
  });
}

describe("/qq slash handler", () => {
  const posts: string[] = [];

  beforeEach(() => {
    posts.length = 0;
    setLanguageRuntime("EN");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    setLanguageRuntime("EN");
  });

  it("routes /qq connect through the qq host surface", async () => {
    const connect = vi.fn(async () => "QQ connected.");
    const result = handleSlash("qq", ["connect", "appid", "secret"], makeLoop(), {
      postInfo: (msg) => posts.push(msg),
      qq: {
        connect,
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(result).toEqual({});
    await Promise.resolve();
    expect(connect).toHaveBeenCalledWith(["appid", "secret"]);
    expect(posts).toContain("QQ: connecting…");
    expect(posts).toContain("QQ connected.");
  });

  it("invalid /qq subcommands now return the compact usage string", () => {
    const result = handleSlash("qq", ["owner", "openid-123"], makeLoop(), {
      qq: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(result.info).toBe(
      "Usage: /qq connect [appId appSecret [sandbox]] | /qq status | /qq disconnect",
    );
  });

  it("localizes handler prompts in zh-CN", async () => {
    setLanguageRuntime("zh-CN");
    const result = handleSlash("qq", ["connect"], makeLoop(), {
      postInfo: (msg) => posts.push(msg),
      qq: {
        connect: async () => "QQ 已连接。",
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(result).toEqual({});
    await Promise.resolve();
    expect(posts).toContain("QQ：正在连接…");

    const usage = handleSlash("qq", ["owner"], makeLoop(), {
      qq: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(usage.info).toBe(
      "用法：/qq connect [appId appSecret [sandbox]] | /qq status | /qq disconnect",
    );
  });

  it("bare /qq status still returns synchronously", () => {
    const result = handleSlash("qq", ["status"], makeLoop(), {
      qq: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "QQ: connected, access owner abcdef...7890.",
      },
    });
    expect(result.info).toMatch(/access owner/);
  });
});
