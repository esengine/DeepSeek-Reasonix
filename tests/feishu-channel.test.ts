import { describe, expect, it, vi } from "vitest";
import { FeishuChannel } from "../src/feishu/channel.js";

describe("FeishuChannel", () => {
  it("sends the final reply back to the most recent private chat", async () => {
    const bot = { sendPrivateMessage: vi.fn().mockResolvedValue(undefined) };
    const channel = new FeishuChannel({ onSubmitMessage: () => undefined }) as FeishuChannel & {
      bot: typeof bot;
      chatId: string;
    };
    channel.bot = bot;
    channel.chatId = "oc_chat";

    await channel.sendResponse("hello from reasonix");

    expect(bot.sendPrivateMessage).toHaveBeenCalledWith("oc_chat", "hello from reasonix");
  });

  it("rejects unauthorized private messages when no access is configured", async () => {
    const onSubmitMessage = vi.fn();
    const onError = vi.fn();
    const onInfo = vi.fn();
    const channel = new FeishuChannel({ onSubmitMessage, onError, onInfo }) as FeishuChannel & {
      handlePrivateMessage: (msg: {
        messageId: string;
        chatId: string;
        openId: string;
        text: string;
      }) => void;
    };

    channel.handlePrivateMessage({
      messageId: "om_1",
      chatId: "oc_1",
      openId: "ou_user_1",
      text: "hi",
    });

    expect(onSubmitMessage).not.toHaveBeenCalled();
    expect(onInfo).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith(expect.stringContaining("unauthorized openid"));
  });
});
