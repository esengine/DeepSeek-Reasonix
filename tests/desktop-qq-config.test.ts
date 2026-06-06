import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadQQConfig, saveQQConfig } from "../src/config.js";
import {
  loadDesktopQQState,
  saveDesktopQQSettings,
  setDesktopQQEnabled,
} from "../src/desktop/qq-settings.js";

describe("desktop QQ settings helpers", () => {
  let dir: string;
  let path: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-desktop-qq-"));
    path = join(dir, "config.json");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("loads desktop QQ state with config-derived status and access summary", () => {
    saveQQConfig(
      {
        appId: "1234567890",
        appSecret: "secret-value",
        sandbox: true,
        enabled: true,
        ownerOpenId: "abcdefghijklmnop",
      },
      path,
    );

    expect(loadDesktopQQState(path)).toMatchObject({
      appId: "1234567890",
      appSecret: "secret-value",
      sandbox: true,
      enabled: true,
      configured: true,
      runtimeState: "disconnected",
      appIdPreview: "123456...",
      access: "owner abcdef...mnop",
    });
  });

  it("saves desktop QQ settings while preserving existing access controls", () => {
    saveQQConfig(
      {
        appId: "old-app",
        appSecret: "old-secret",
        sandbox: false,
        enabled: true,
        ownerOpenId: "owner-openid",
        allowlist: ["guest-a", "guest-b"],
      },
      path,
    );

    saveDesktopQQSettings(
      {
        appId: "  new-app-id  ",
        appSecret: "  new-secret  ",
        sandbox: true,
      },
      path,
    );

    expect(loadQQConfig(path)).toMatchObject({
      appId: "new-app-id",
      appSecret: "new-secret",
      sandbox: true,
      enabled: true,
      ownerOpenId: "owner-openid",
      allowlist: ["guest-a", "guest-b"],
    });
  });

  it("rejects enabling QQ when credentials are missing", () => {
    expect(() => setDesktopQQEnabled(true, path)).toThrow("QQ App ID and App Secret are required.");
  });
});
