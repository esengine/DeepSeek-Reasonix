import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguageRuntime } from "../src/i18n/index.js";
import { render } from "./helpers/ink-test.js";
const { useFeishuChannel } = await import("../src/feishu/use-feishu-channel.js");

type FeishuConfigState = {
  appId?: string;
  appSecret?: string;
  enabled?: boolean;
  ownerOpenId?: string;
  allowlist?: readonly string[];
};

let mockConfig: FeishuConfigState = {};
const saveFeishuConfigMock = vi.fn((cfg: FeishuConfigState) => {
  mockConfig = { ...cfg };
});
const startMock = vi.fn(async () => undefined);
const stopMock = vi.fn(async () => undefined);
const refreshAccessConfigMock = vi.fn(() => undefined);

vi.mock("../src/config.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../src/config.js")>();
  return {
    ...actual,
    loadFeishuConfig: vi.fn(() => ({ ...mockConfig })),
    saveFeishuConfig: vi.fn((cfg: FeishuConfigState) => saveFeishuConfigMock(cfg)),
  };
});

vi.mock("../src/feishu/channel.js", () => ({
  FeishuChannel: class {
    start = startMock;
    stop = stopMock;
    refreshAccessConfig = refreshAccessConfigMock;
    sendResponse = vi.fn(async () => undefined);
  },
}));

describe("Feishu first-connect onboarding", () => {
  type FeishuApi = ReturnType<typeof useFeishuChannel>;

  function mountHarness(log: {
    pushInfo: ReturnType<typeof vi.fn>;
    pushWarning: ReturnType<typeof vi.fn>;
  }) {
    let api: FeishuApi | null = null;
    function Harness() {
      api = useFeishuChannel({
        codeMode: false,
        log,
        setQueuedSubmit: () => undefined,
        currentRootDir: process.cwd(),
        pendingGateIdRef: { current: null },
        completedStepIdsRef: { current: new Set<string>() },
        planStepsRef: { current: null },
        onModelPick: () => "",
        onThemePick: () => "",
        onShellConfirmRef: { current: () => undefined },
        onPathConfirmRef: { current: () => undefined },
        onPlanCancelRef: { current: () => undefined },
        onPlanFeedbackRef: { current: () => undefined },
        onCheckpointConfirmRef: { current: () => undefined },
        onCheckpointReviseRef: { current: () => undefined },
        onPlanRevisionRef: { current: () => undefined },
        onChoiceResolveRef: { current: () => undefined },
      });
      return null;
    }
    const mounted = render(<Harness />);
    if (!api) throw new Error("Feishu harness did not mount");
    return { api, ...mounted };
  }

  beforeEach(() => {
    mockConfig = {};
    saveFeishuConfigMock.mockClear();
    startMock.mockClear();
    stopMock.mockClear();
    refreshAccessConfigMock.mockClear();
    setLanguageRuntime("EN");
  });

  afterEach(() => {
    setLanguageRuntime("EN");
    vi.clearAllMocks();
  });

  it("guides first-time connect through staged App ID, App Secret, and owner openid input", async () => {
    const log = {
      pushInfo: vi.fn(),
      pushWarning: vi.fn(),
    };
    const { api, unmount } = mountHarness(log);

    const pending = api.connect([]);
    expect(log.pushInfo).toHaveBeenLastCalledWith(
      "Feishu setup: enter your Feishu Open Platform App ID, then press Enter. Type /cancel to abort.",
    );
    expect(api.status()).toBe("Feishu: setup in progress - waiting for App ID");

    expect(api.parseSubmit("cli_appid")).toMatchObject({
      handled: true,
      fromFeishu: false,
      text: "cli_appid",
    });
    expect(log.pushInfo).toHaveBeenLastCalledWith(
      "Feishu setup: enter your Feishu Open Platform App Secret, then press Enter. Type /cancel to abort.",
    );

    expect(api.parseSubmit("/help")).toMatchObject({
      handled: true,
      fromFeishu: false,
      text: "/help",
    });
    expect(log.pushInfo).toHaveBeenLastCalledWith(
      "Feishu setup: enter your Feishu Open Platform App Secret, then press Enter. Type /cancel to abort.",
    );
    expect(startMock).not.toHaveBeenCalled();

    expect(api.parseSubmit("secret-value")).toMatchObject({
      handled: true,
      fromFeishu: false,
      text: "secret-value",
    });
    expect(log.pushInfo).toHaveBeenLastCalledWith(
      "Feishu setup: enter the user openid that should be allowed to control this channel, then press Enter. Type /cancel to abort.",
    );

    expect(api.parseSubmit("/mode")).toMatchObject({
      handled: true,
      fromFeishu: false,
      text: "/mode",
    });
    expect(log.pushInfo).toHaveBeenLastCalledWith(
      "Feishu setup: enter the user openid that should be allowed to control this channel, then press Enter. Type /cancel to abort.",
    );

    expect(api.parseSubmit("ou_owner_123")).toMatchObject({
      handled: true,
      fromFeishu: false,
      text: "ou_owner_123",
    });
    await expect(pending).resolves.toBe(
      "Feishu connected in chat mode. It will auto-start on future launches.",
    );
    expect(startMock).toHaveBeenCalledTimes(1);
    expect(saveFeishuConfigMock).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        appId: "cli_appid",
        appSecret: "secret-value",
        ownerOpenId: "ou_owner_123",
        enabled: false,
      }),
    );
    expect(saveFeishuConfigMock).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        appId: "cli_appid",
        appSecret: "secret-value",
        ownerOpenId: "ou_owner_123",
        enabled: true,
      }),
    );

    unmount();
  });

  it("allows cancelling staged first-time setup", async () => {
    const log = {
      pushInfo: vi.fn(),
      pushWarning: vi.fn(),
    };
    const { api, unmount } = mountHarness(log);

    const pending = api.connect([]);
    expect(api.parseSubmit("/cancel")).toMatchObject({
      handled: true,
      fromFeishu: false,
      text: "/cancel",
    });
    await expect(pending).rejects.toThrow("Feishu setup cancelled.");
    expect(log.pushInfo).toHaveBeenLastCalledWith("Feishu setup cancelled.");
    expect(startMock).not.toHaveBeenCalled();

    unmount();
  });

  it("localizes connect results and status in zh-CN", async () => {
    setLanguageRuntime("zh-CN");
    const log = {
      pushInfo: vi.fn(),
      pushWarning: vi.fn(),
    };
    const { api, unmount } = mountHarness(log);

    await expect(api.connect(["cli_appid", "secret-value", "ou_owner_123"])).resolves.toBe(
      "飞书已在聊天模式下连接成功，后续启动会自动启用。",
    );
    expect(api.status()).toContain("飞书：已连接");
    expect(api.status()).toContain("访问控制 所有者");

    unmount();
  });
});
