import { afterEach, describe, expect, it } from "vitest";
import { setLang, t } from "../desktop/src/i18n";
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
  runtimeState: "disconnected",
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
        runtimeState: "disconnected",
        access: "owner abcd...mnop",
      }),
    ).toBe("App ID 123456... · Sandbox · Owner abcd...mnop");
  });

  it("localizes the disconnected label in zh-CN", () => {
    setLang("zh-CN");
    expect(getQQStatusLabel(DISCONNECTED)).toBe("已断开");
  });

  it("uses the connected label when runtime state is connected", () => {
    setLang("en");
    expect(
      getQQStatusLabel({
        ...DISCONNECTED,
        appId: "x",
        appSecret: "y",
        configured: true,
        enabled: true,
        runtimeState: "connected",
      }),
    ).toBe("Connected");
  });

  it("uses the connecting label when runtime state is connecting", () => {
    setLang("en");
    expect(
      getQQStatusLabel({
        ...DISCONNECTED,
        appId: "x",
        appSecret: "y",
        configured: true,
        runtimeState: "connecting",
      }),
    ).toBe("Connecting");
  });

  it("exposes the new QQ settings copy in zh-CN", () => {
    setLang("zh-CN");
    expect(t("settings.qqTitle")).toBe("QQ机器人集成");
    expect(t("settings.qqConfigureHint")).toBe("注册 QQ 机器人以接收和回复消息。");
    expect(t("settings.qqApplyAction")).toBe("去申请");
  });
});
