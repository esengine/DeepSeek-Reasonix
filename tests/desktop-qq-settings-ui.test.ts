import { afterEach, describe, expect, it } from "vitest";
import { setLang } from "../desktop/src/i18n";
import {
  type QQDesktopSettingsState,
  describeQQRowSummary,
  getQQConnectIntent,
  getQQStatusLabel,
} from "../desktop/src/qq-settings";

const DISCONNECTED: QQDesktopSettingsState = {
  appId: undefined,
  appSecret: undefined,
  sandbox: true,
  enabled: false,
  configured: false,
  connected: false,
  access: "open (unbound)",
};

describe("desktop QQ settings view model", () => {
  afterEach(() => {
    setLang("en");
  });

  it("routes connect to configure when credentials are missing", () => {
    expect(getQQConnectIntent(DISCONNECTED)).toBe("configure");
  });

  it("describes a configured sandbox row concisely in EN", () => {
    setLang("en");
    expect(
      describeQQRowSummary({
        appId: "1234567890",
        appSecret: "secret",
        sandbox: true,
        enabled: false,
        configured: true,
        connected: false,
        access: "owner abcd...mnop",
      }),
    ).toBe("App ID 123456... · Sandbox · Owner abcd...mnop");
  });

  it("localizes the disconnected label in zh-CN", () => {
    setLang("zh-CN");
    expect(getQQStatusLabel(DISCONNECTED)).toBe("已断开");
  });
});
