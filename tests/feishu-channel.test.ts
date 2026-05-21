import { describe, expect, it, vi } from "vitest";
import { FeishuChannel } from "../src/feishu/channel.js";

describe("FeishuChannel.sendResponse", () => {
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

  it("binds the first sender and deduplicates private messages", async () => {
    const onSubmitMessage = vi.fn();
    const onError = vi.fn();
    const channel = new FeishuChannel({ onSubmitMessage, onError }) as FeishuChannel & {
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
    channel.handlePrivateMessage({
      messageId: "om_1",
      chatId: "oc_1",
      openId: "ou_user_1",
      text: "hi",
    });

    expect(onSubmitMessage).toHaveBeenCalledTimes(1);
    expect(onSubmitMessage).toHaveBeenCalledWith("[Feishu] hi");
    expect(onError).toHaveBeenCalledWith(expect.stringContaining("temporarily bound"));
  });
});
