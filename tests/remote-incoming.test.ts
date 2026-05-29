import { describe, expect, it, vi } from "vitest";
import { resolveRemoteIncoming } from "../src/cli/ui/remote-incoming.js";

describe("resolveRemoteIncoming", () => {
  it("keeps QQ-origin submissions instead of reparsing them as Feishu", () => {
    const telegramFactory = vi.fn();
    const feishuFactory = vi.fn();

    const incoming = resolveRemoteIncoming(
      { handled: false, fromQQ: true, text: "hello from qq" },
      telegramFactory,
      feishuFactory,
    );

    expect(incoming).toEqual({ handled: false, fromQQ: true, text: "hello from qq" });
    expect(telegramFactory).not.toHaveBeenCalled();
    expect(feishuFactory).not.toHaveBeenCalled();
  });

  it("falls through to Telegram when QQ did not match", () => {
    const telegramFactory = vi
      .fn()
      .mockReturnValue({ handled: false, fromTelegram: true, text: "hello from telegram" });
    const feishuFactory = vi.fn();

    const incoming = resolveRemoteIncoming(
      { handled: false, text: "plain local input" },
      telegramFactory,
      feishuFactory,
    );

    expect(incoming).toEqual({
      handled: false,
      fromTelegram: true,
      text: "hello from telegram",
    });
    expect(telegramFactory).toHaveBeenCalledTimes(1);
    expect(feishuFactory).not.toHaveBeenCalled();
  });

  it("falls through to Feishu when neither QQ nor Telegram matched", () => {
    const telegramFactory = vi.fn().mockReturnValue({ handled: false, text: "telegram miss" });
    const feishuFactory = vi
      .fn()
      .mockReturnValue({ handled: false, fromFeishu: true, text: "hello from feishu" });

    const incoming = resolveRemoteIncoming(
      { handled: false, text: "plain local input" },
      telegramFactory,
      feishuFactory,
    );

    expect(incoming).toEqual({
      handled: false,
      fromFeishu: true,
      text: "hello from feishu",
    });
    expect(telegramFactory).toHaveBeenCalledTimes(1);
    expect(feishuFactory).toHaveBeenCalledTimes(1);
  });
});
