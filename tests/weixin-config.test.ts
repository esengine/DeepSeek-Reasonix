import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadWeixinConfig, saveWeixinConfig } from "../src/config.js";
import { loadWeixinAccount } from "../src/weixin/account.js";

describe("Weixin config", () => {
  let dir: string;
  let path: string;
  const originalToken = process.env.WEIXIN_TOKEN;
  const originalAccountId = process.env.WEIXIN_ACCOUNT_ID;
  const originalBaseUrl = process.env.WEIXIN_BASE_URL;
  const originalOwner = process.env.WEIXIN_OWNER_USER_ID;
  const originalAllowlist = process.env.WEIXIN_ALLOWLIST;
  const originalAccountsDir = process.env.REASONIX_WEIXIN_ACCOUNTS_DIR;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-weixin-config-"));
    path = join(dir, "config.json");
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.WEIXIN_TOKEN;
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.WEIXIN_ACCOUNT_ID;
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.WEIXIN_BASE_URL;
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.WEIXIN_OWNER_USER_ID;
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.WEIXIN_ALLOWLIST;
    process.env.REASONIX_WEIXIN_ACCOUNTS_DIR = join(dir, "accounts");
  });

  afterEach(() => {
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
    if (originalToken === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.WEIXIN_TOKEN;
    } else process.env.WEIXIN_TOKEN = originalToken;
    if (originalAccountId === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.WEIXIN_ACCOUNT_ID;
    } else process.env.WEIXIN_ACCOUNT_ID = originalAccountId;
    if (originalBaseUrl === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.WEIXIN_BASE_URL;
    } else process.env.WEIXIN_BASE_URL = originalBaseUrl;
    if (originalOwner === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.WEIXIN_OWNER_USER_ID;
    } else process.env.WEIXIN_OWNER_USER_ID = originalOwner;
    if (originalAllowlist === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.WEIXIN_ALLOWLIST;
    } else process.env.WEIXIN_ALLOWLIST = originalAllowlist;
    if (originalAccountsDir === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.REASONIX_WEIXIN_ACCOUNTS_DIR;
    } else process.env.REASONIX_WEIXIN_ACCOUNTS_DIR = originalAccountsDir;
  });

  it("round-trips accountId, persisted token, ownerUserId, and allowlist", () => {
    saveWeixinConfig(
      {
        token: "token",
        accountId: "account",
        baseUrl: "https://ilink.example.test",
        enabled: true,
        ownerUserId: "wx-owner",
        allowlist: ["wx-member-1", "wx-member-2"],
      },
      path,
    );
    expect(loadWeixinConfig(path)).toMatchObject({
      token: "token",
      accountId: "account",
      baseUrl: "https://ilink.example.test",
      enabled: true,
      ownerUserId: "wx-owner",
      allowlist: ["wx-member-1", "wx-member-2"],
    });
    expect(loadWeixinAccount("account")).toMatchObject({
      accountId: "account",
      token: "token",
      baseUrl: "https://ilink.example.test",
    });
  });

  it("lets env override credentials, ownerUserId, and allowlist", () => {
    saveWeixinConfig(
      {
        token: "file-token",
        accountId: "file-account",
        ownerUserId: "file-owner",
        allowlist: ["file-member"],
      },
      path,
    );
    process.env.WEIXIN_TOKEN = "env-token";
    process.env.WEIXIN_ACCOUNT_ID = "env-account";
    process.env.WEIXIN_BASE_URL = "https://env.example.test";
    process.env.WEIXIN_OWNER_USER_ID = "env-owner";
    process.env.WEIXIN_ALLOWLIST = "env-member-1, env-member-2 env-member-1";
    expect(loadWeixinConfig(path)).toMatchObject({
      token: "env-token",
      accountId: "env-account",
      baseUrl: "https://env.example.test",
      ownerUserId: "env-owner",
      allowlist: ["env-member-1", "env-member-2"],
    });
  });
});
