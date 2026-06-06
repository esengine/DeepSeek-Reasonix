import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadTelegramConfig, saveTelegramConfig } from "../src/config.js";

describe("Telegram config", () => {
  let dir: string;
  let path: string;
  const originalOwner = process.env.TELEGRAM_OWNER_USER_ID;
  const originalAllowlist = process.env.TELEGRAM_ALLOWLIST;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-telegram-config-"));
    path = join(dir, "config.json");
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.TELEGRAM_OWNER_USER_ID;
    // biome-ignore lint/performance/noDelete: tests must restore exact env absence
    delete process.env.TELEGRAM_ALLOWLIST;
  });

  afterEach(() => {
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
    if (originalOwner === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.TELEGRAM_OWNER_USER_ID;
    } else process.env.TELEGRAM_OWNER_USER_ID = originalOwner;
    if (originalAllowlist === undefined) {
      // biome-ignore lint/performance/noDelete: tests must restore exact env absence
      delete process.env.TELEGRAM_ALLOWLIST;
    } else process.env.TELEGRAM_ALLOWLIST = originalAllowlist;
  });

  it("round-trips ownerUserId and allowlist", () => {
    saveTelegramConfig(
      {
        botToken: "token",
        enabled: true,
        ownerUserId: "1001",
        allowlist: ["1002", "1003"],
      },
      path,
    );
    expect(loadTelegramConfig(path)).toMatchObject({
      botToken: "token",
      enabled: true,
      ownerUserId: "1001",
      allowlist: ["1002", "1003"],
    });
  });

  it("lets env override ownerUserId and allowlist", () => {
    saveTelegramConfig({ ownerUserId: "1001", allowlist: ["1002"] }, path);
    process.env.TELEGRAM_OWNER_USER_ID = "2001";
    process.env.TELEGRAM_ALLOWLIST = "2002, 2003 2002";
    expect(loadTelegramConfig(path)).toMatchObject({
      ownerUserId: "2001",
      allowlist: ["2002", "2003"],
    });
  });
});
