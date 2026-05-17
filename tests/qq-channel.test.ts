import { describe, expect, it, vi } from "vitest";
import { QQChannel, splitQQMessage } from "../src/qq/channel.js";

describe("splitQQMessage", () => {
  it("keeps every chunk within the UTF-8 byte budget", () => {
    const chunks = splitQQMessage("中".repeat(600), 1500);
    expect(chunks.length).toBeGreaterThan(1);
    for (const chunk of chunks) {
      expect(Buffer.byteLength(chunk, "utf8")).toBeLessThanOrEqual(1500);
    }
  });
});

describe("QQChannel.sendResponse", () => {
  it("assigns incrementing msg_seq values across chunks", async () => {
    const bot = { sendPrivateMessage: vi.fn().mockResolvedValue(undefined) };
    const channel = new QQChannel({ onSubmitMessage: () => undefined }) as QQChannel & {
      bot: typeof bot;
      qqUserId: string;
      qqMessageId: string;
      nextOutboundMsgSeq: number;
    };
    channel.bot = bot;
    channel.qqUserId = "user-openid";
    channel.qqMessageId = "msg-id";
    channel.nextOutboundMsgSeq = 41;

    await channel.sendResponse("a".repeat(1501));

    expect(bot.sendPrivateMessage).toHaveBeenCalledTimes(2);
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(
      1,
      "user-openid",
      "a".repeat(1500),
      "msg-id",
      41,
    );
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(2, "user-openid", "a", "msg-id", 42);
  });

  it("stops after the first failed chunk and reports which chunk failed", async () => {
    const bot = {
      sendPrivateMessage: vi
        .fn()
        .mockResolvedValueOnce(undefined)
        .mockRejectedValueOnce(new Error("duplicate msgseq"))
        .mockResolvedValue(undefined),
    };
    const onError = vi.fn();
    const channel = new QQChannel({
      onSubmitMessage: () => undefined,
      onError,
    }) as QQChannel & {
      bot: typeof bot;
      qqUserId: string;
      qqMessageId: string;
    };
    channel.bot = bot;
    channel.qqUserId = "user-openid";
    channel.qqMessageId = "msg-id";

    await channel.sendResponse("a".repeat(3001));

    expect(bot.sendPrivateMessage).toHaveBeenCalledTimes(2);
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError.mock.calls[0]?.[0]).toContain("chunk 2/3 failed");
  });
});
