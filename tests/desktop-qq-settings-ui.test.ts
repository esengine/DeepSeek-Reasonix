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

  it("localizes the 'not configured' label in zh-CN (was 'disconnected' pre-#1317)", () => {
    setLang("zh-CN");
    expect(getQQStatusLabel(DISCONNECTED)).toBe("未配置");
  });

  it("uses the 'enabled (CLI)' label when credentials saved and toggle on (#1317)", () => {
    setLang("en");
    expect(
      getQQStatusLabel({
        ...DISCONNECTED,
        appId: "x",
        appSecret: "y",
        configured: true,
        enabled: true,
        enabledForCli: true,
      }),
    ).toBe("Enabled (CLI)");
  });

  it("uses the 'configured · disabled' label when credentials saved but toggle off (#1317)", () => {
    setLang("en");
    expect(
      getQQStatusLabel({
        ...DISCONNECTED,
        appId: "x",
        appSecret: "y",
        configured: true,
        enabled: false,
        enabledForCli: false,
      }),
    ).toBe("Configured · disabled");
  });
});
