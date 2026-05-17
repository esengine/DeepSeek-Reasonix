import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { handleSlash } from "../src/cli/ui/slash/dispatch.js";
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
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("routes /qq owner through the qq host surface", async () => {
    const owner = vi.fn(async () => "QQ ownerOpenId set to abcdef...7890.");
    const result = handleSlash("qq", ["owner", "openid-123"], makeLoop(), {
      postInfo: (msg) => posts.push(msg),
      qq: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "",
        owner,
        allow: async () => "",
        unallow: async () => "",
      },
    });
    expect(result).toEqual({});
    await Promise.resolve();
    expect(owner).toHaveBeenCalledWith(["openid-123"]);
    expect(posts).toContain("QQ ownerOpenId set to abcdef...7890.");
  });

  it("routes /qq allow and /qq unallow through the qq host surface", async () => {
    const allow = vi.fn(async () => "Added abcdef...7890 to the QQ allowlist (1).");
    const unallow = vi.fn(
      async () => "Removed abcdef...7890 from the QQ allowlist. It is now empty.",
    );
    handleSlash("qq", ["allow", "openid-123"], makeLoop(), {
      postInfo: (msg) => posts.push(msg),
      qq: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "",
        owner: async () => "",
        allow,
        unallow,
      },
    });
    await Promise.resolve();
    handleSlash("qq", ["unallow", "openid-123"], makeLoop(), {
      postInfo: (msg) => posts.push(msg),
      qq: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "",
        owner: async () => "",
        allow: async () => "",
        unallow,
      },
    });
    await Promise.resolve();

    expect(allow).toHaveBeenCalledWith(["openid-123"]);
    expect(unallow).toHaveBeenCalledWith(["openid-123"]);
    expect(posts[0]).toMatch(/allowlist/);
    expect(posts[1]).toMatch(/allowlist/);
  });

  it("bare /qq status still returns synchronously", () => {
    const result = handleSlash("qq", ["status"], makeLoop(), {
      qq: {
        connect: async () => "",
        disconnect: async () => "",
        status: () => "QQ: connected, access owner abcdef...7890.",
        owner: async () => "",
        allow: async () => "",
        unallow: async () => "",
      },
    });
    expect(result.info).toMatch(/access owner/);
  });
});
