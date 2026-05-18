import { render } from "ink-testing-library";
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguageRuntime } from "../src/i18n/index.js";
const { useQQChannel } = await import("../src/qq/use-qq-channel.js");

type QQConfigState = {
  appId?: string;
  appSecret?: string;
  sandbox?: boolean;
  enabled?: boolean;
  ownerOpenId?: string;
  allowlist?: readonly string[];
};

let mockConfig: QQConfigState = {};
const saveQQConfigMock = vi.fn((cfg: QQConfigState) => {
  mockConfig = { ...cfg };
});
const startMock = vi.fn(async () => undefined);
const stopMock = vi.fn(async () => undefined);
const refreshAccessConfigMock = vi.fn(() => undefined);

vi.mock("../src/config.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../src/config.js")>();
  return {
    ...actual,
    loadQQConfig: vi.fn(() => ({ ...mockConfig })),
    saveQQConfig: vi.fn((cfg: QQConfigState) => saveQQConfigMock(cfg)),
  };
});

vi.mock("../src/qq/channel.js", () => ({
  QQChannel: class {
    start = startMock;
    stop = stopMock;
    refreshAccessConfig = refreshAccessConfigMock;
    sendResponse = vi.fn(async () => undefined);
    getRuntimeBoundOpenId() {
      return null;
    }
  },
}));

describe("QQ first-connect onboarding", () => {
  type QQApi = ReturnType<typeof useQQChannel>;

  function mountHarness(log: {
    pushInfo: ReturnType<typeof vi.fn>;
    pushWarning: ReturnType<typeof vi.fn>;
  }) {
    let api: QQApi | null = null;
    function Harness() {
      api = useQQChannel({
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
    if (!api) throw new Error("QQ harness did not mount");
    return { api, ...mounted };
  }

  beforeEach(() => {
    mockConfig = {};
    saveQQConfigMock.mockClear();
    startMock.mockClear();
    stopMock.mockClear();
    refreshAccessConfigMock.mockClear();
    setLanguageRuntime("EN");
  });

  afterEach(() => {
    setLanguageRuntime("EN");
    vi.clearAllMocks();
  });

  it("guides first-time connect through staged App ID and App Secret input", async () => {
    const log = {
      pushInfo: vi.fn(),
      pushWarning: vi.fn(),
    };
    const { api, unmount } = mountHarness(log);

    const pending = api.connect([]);
    expect(log.pushInfo).toHaveBeenLastCalledWith(
      "QQ setup: enter your QQ Open Platform App ID, then press Enter. Type /cancel to abort.",
    );
    expect(api.status()).toBe("QQ: setup in progress — waiting for App ID");

    expect(api.parseSubmit("1234567890")).toMatchObject({ handled: true, fromQQ: false });
    expect(log.pushInfo).toHaveBeenLastCalledWith(
      "QQ setup: enter your QQ Open Platform App Secret, then press Enter. Type /cancel to abort.",
    );

    expect(api.parseSubmit("secret-value")).toMatchObject({ handled: true, fromQQ: false });
    await expect(pending).resolves.toBe(
      "QQ connected in chat mode. It will auto-start on future launches.",
    );
    expect(startMock).toHaveBeenCalledTimes(1);
    expect(saveQQConfigMock).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        appId: "1234567890",
        appSecret: "secret-value",
        enabled: false,
      }),
    );
    expect(saveQQConfigMock).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        appId: "1234567890",
        appSecret: "secret-value",
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
    expect(api.parseSubmit("/cancel")).toMatchObject({ handled: true, fromQQ: false });
    await expect(pending).rejects.toThrow("QQ setup cancelled.");
    expect(log.pushInfo).toHaveBeenLastCalledWith("QQ setup cancelled.");
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

    await expect(api.connect(["123456", "secret-value", "sandbox"])).resolves.toBe(
      "QQ 已在聊天模式下连接成功，后续启动会自动启用。",
    );
    expect(api.status()).toContain("QQ：已连接");
    expect(api.status()).toContain("沙箱环境");
    expect(api.status()).toContain("访问控制 开放（未绑定）");

    unmount();
  });
});
