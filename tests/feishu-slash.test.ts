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

describe("/feishu slash handler", () => {
  const posts: string[] = [];

  beforeEach(() => {
    posts.length = 0;
    setLanguageRuntime("EN");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    setLanguageRuntime("EN");
  });

  it("routes /feishu connect through the feishu host surface", async () => {
    const connect = vi.fn(async () => "Feishu connected.");
    const result = handleSlash("feishu", ["connect", "appid", "secret"], makeLoop(), {
      postInfo: (msg) => posts.push(msg),
      feishu: {
        connect,
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(result).toEqual({});
    await Promise.resolve();
    expect(connect).toHaveBeenCalledWith(["appid", "secret"]);
    expect(posts).toContain("Feishu: connecting…");
    expect(posts).toContain("Feishu connected.");
  });

  it("invalid /feishu subcommands return the compact usage string", () => {
    const result = handleSlash("feishu", ["owner", "openid-123"], makeLoop(), {
      feishu: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(result.info).toBe(
      "Usage: /feishu connect [appId appSecret] | /feishu status | /feishu disconnect",
    );
  });

  it("localizes handler prompts in zh-CN", async () => {
    setLanguageRuntime("zh-CN");
    const result = handleSlash("feishu", ["connect"], makeLoop(), {
      postInfo: (msg) => posts.push(msg),
      feishu: {
        connect: async () => "飞书已连接。",
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(result).toEqual({});
    await Promise.resolve();
    expect(posts).toContain("飞书：正在连接…");

    const usage = handleSlash("feishu", ["owner"], makeLoop(), {
      feishu: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "",
      },
    });
    expect(usage.info).toBe(
      "用法：/feishu connect [appId appSecret] | /feishu status | /feishu disconnect",
    );
  });

  it("bare /feishu status still returns synchronously", () => {
    const result = handleSlash("feishu", ["status"], makeLoop(), {
      feishu: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "Feishu: connected, access owner ou_abc...7890.",
      },
    });
    expect(result.info).toMatch(/access owner/);
  });
});
