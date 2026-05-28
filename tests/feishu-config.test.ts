import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadFeishuConfig, saveFeishuConfig } from "../src/config.js";

describe("Feishu config", () => {
  let dir: string;
  let path: string;
  const originalAppId = process.env.FEISHU_APP_ID;
  const originalAppSecret = process.env.FEISHU_APP_SECRET;
  const originalOwner = process.env.FEISHU_OWNER_OPENID;
  const originalAllowlist = process.env.FEISHU_ALLOWLIST;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-feishu-config-"));
    path = join(dir, "config.json");
    process.env.FEISHU_APP_ID = "";
    process.env.FEISHU_APP_SECRET = "";
    process.env.FEISHU_OWNER_OPENID = "";
    process.env.FEISHU_ALLOWLIST = "";
  });

  afterEach(() => {
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
    if (originalAppId === undefined) process.env.FEISHU_APP_ID = "";
    else process.env.FEISHU_APP_ID = originalAppId;
    if (originalAppSecret === undefined) process.env.FEISHU_APP_SECRET = "";
    else process.env.FEISHU_APP_SECRET = originalAppSecret;
    if (originalOwner === undefined) process.env.FEISHU_OWNER_OPENID = "";
    else process.env.FEISHU_OWNER_OPENID = originalOwner;
    if (originalAllowlist === undefined) process.env.FEISHU_ALLOWLIST = "";
    else process.env.FEISHU_ALLOWLIST = originalAllowlist;
  });

  it("round-trips app credentials, ownerOpenId and allowlist", () => {
    saveFeishuConfig(
      {
        appId: "cli_appid",
        appSecret: "secret",
        enabled: true,
        ownerOpenId: "owner-1",
        allowlist: ["member-1", "member-2"],
      },
      path,
    );
    expect(loadFeishuConfig(path)).toMatchObject({
      appId: "cli_appid",
      appSecret: "secret",
      enabled: true,
      ownerOpenId: "owner-1",
      allowlist: ["member-1", "member-2"],
    });
  });

  it("filters duplicate/empty allowlist items and removes the owner from allowlist", () => {
    saveFeishuConfig(
      {
        ownerOpenId: "owner-1",
        allowlist: ["owner-1", " member-1 ", "", "member-1"],
      },
      path,
    );
    expect(loadFeishuConfig(path)).toMatchObject({
      ownerOpenId: "owner-1",
      allowlist: ["member-1"],
    });
  });

  it("lets env override credentials, ownerOpenId and allowlist", () => {
    saveFeishuConfig(
      {
        appId: "file-app-id",
        appSecret: "file-app-secret",
        ownerOpenId: "owner-file",
        allowlist: ["file-a"],
      },
      path,
    );
    process.env.FEISHU_APP_ID = "env-app-id";
    process.env.FEISHU_APP_SECRET = "env-app-secret";
    process.env.FEISHU_OWNER_OPENID = "owner-env";
    process.env.FEISHU_ALLOWLIST = "env-a, env-b env-a";
    expect(loadFeishuConfig(path)).toMatchObject({
      appId: "env-app-id",
      appSecret: "env-app-secret",
      ownerOpenId: "owner-env",
      allowlist: ["env-a", "env-b"],
    });
  });
});
