import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadQQConfig, saveQQConfig } from "../src/config.js";

describe("QQ config", () => {
  let dir: string;
  let path: string;
  const originalOwner = process.env.QQ_OWNER_OPENID;
  const originalAllowlist = process.env.QQ_ALLOWLIST;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-qq-config-"));
    path = join(dir, "config.json");
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.QQ_OWNER_OPENID;
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.QQ_ALLOWLIST;
  });

  afterEach(() => {
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
    if (originalOwner === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.QQ_OWNER_OPENID;
    } else process.env.QQ_OWNER_OPENID = originalOwner;
    if (originalAllowlist === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.QQ_ALLOWLIST;
    } else process.env.QQ_ALLOWLIST = originalAllowlist;
  });

  it("round-trips ownerOpenId and allowlist", () => {
    saveQQConfig(
      {
        appId: "app",
        appSecret: "secret",
        sandbox: true,
        enabled: true,
        ownerOpenId: "owner-1",
        allowlist: ["member-1", "member-2"],
      },
      path,
    );
    expect(loadQQConfig(path)).toMatchObject({
      appId: "app",
      appSecret: "secret",
      sandbox: true,
      enabled: true,
      ownerOpenId: "owner-1",
      allowlist: ["member-1", "member-2"],
    });
  });

  it("filters duplicate/empty allowlist items and removes the owner from allowlist", () => {
    saveQQConfig(
      {
        ownerOpenId: "owner-1",
        allowlist: ["owner-1", " member-1 ", "", "member-1"],
      },
      path,
    );
    expect(loadQQConfig(path)).toMatchObject({
      ownerOpenId: "owner-1",
      allowlist: ["member-1"],
    });
  });

  it("lets env override ownerOpenId and allowlist", () => {
    saveQQConfig(
      {
        ownerOpenId: "owner-file",
        allowlist: ["file-a"],
      },
      path,
    );
    process.env.QQ_OWNER_OPENID = "owner-env";
    process.env.QQ_ALLOWLIST = "env-a, env-b env-a";
    expect(loadQQConfig(path)).toMatchObject({
      ownerOpenId: "owner-env",
      allowlist: ["env-a", "env-b"],
    });
  });
});
