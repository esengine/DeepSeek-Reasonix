import { describe, expect, it, vi } from "vitest";
import { TelegramChannel, splitTelegramMessage } from "../src/telegram/channel.js";

describe("splitTelegramMessage", () => {
  it("keeps every chunk within the character budget", () => {
    const chunks = splitTelegramMessage("a".repeat(8001), 3900);
    expect(chunks.length).toBe(3);
    for (const chunk of chunks) {
      expect(chunk.length).toBeLessThanOrEqual(3900);
    }
  });
});

describe("TelegramChannel.sendResponse", () => {
  it("sends replies to the last chat and replies to the last message", async () => {
    const bot = { sendMessage: vi.fn().mockResolvedValue(undefined) };
    const channel = new TelegramChannel({
      onSubmitMessage: () => undefined,
    }) as unknown as {
      bot: typeof bot;
      chatId: number;
      messageId: number;
      sendResponse: TelegramChannel["sendResponse"];
    };
    channel.bot = bot;
    channel.chatId = 123;
    channel.messageId = 456;

    await channel.sendResponse("hello");

    expect(bot.sendMessage).toHaveBeenCalledWith(123, "hello", 456);
  });
});
