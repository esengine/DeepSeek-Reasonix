import { describe, expect, it, vi } from "vitest";
import { QQChannel, normalizeQQMarkdownReply, splitQQMessage } from "../src/qq/channel.js";

describe("splitQQMessage", () => {
  it("keeps every chunk within the UTF-8 byte budget", () => {
    const chunks = splitQQMessage("中".repeat(600), 1500);
    expect(chunks.length).toBeGreaterThan(1);
    for (const chunk of chunks) {
      expect(Buffer.byteLength(chunk, "utf8")).toBeLessThanOrEqual(1500);
    }
  });
});

describe("normalizeQQMarkdownReply", () => {
  it("unwraps a full fenced markdown block before delivery", () => {
    expect(normalizeQQMarkdownReply("```markdown\n# Title\n\n**bold**\n\n- item\n```")).toBe(
      "# Title\n\n**bold**\n\n- item",
    );
  });

  it("keeps normal code blocks unchanged when the whole reply is not a markdown wrapper", () => {
    expect(normalizeQQMarkdownReply("Here is code:\n```ts\nconsole.log('hi')\n```")).toBe(
      "Here is code:\n```ts\nconsole.log('hi')\n```",
    );
  });
});

describe("QQChannel.sendResponse", () => {
  it("sends short replies through the markdown path by default", async () => {
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
    channel.nextOutboundMsgSeq = 1;

    await channel.sendResponse("**bold**");

    expect(bot.sendPrivateMessage).toHaveBeenCalledTimes(1);
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(
      1,
      "user-openid",
      "**bold**",
      "msg-id",
      1,
      true,
    );
  });

  it("falls back to plain text when markdown delivery fails", async () => {
    const bot = {
      sendPrivateMessage: vi
        .fn()
        .mockRejectedValueOnce(new Error("markdown rejected"))
        .mockResolvedValueOnce(undefined),
    };
    const onError = vi.fn();
    const channel = new QQChannel({
      onSubmitMessage: () => undefined,
      onError,
    }) as QQChannel & {
      bot: typeof bot;
      qqUserId: string;
      qqMessageId: string;
      nextOutboundMsgSeq: number;
    };
    channel.bot = bot;
    channel.qqUserId = "user-openid";
    channel.qqMessageId = "msg-id";
    channel.nextOutboundMsgSeq = 7;

    await channel.sendResponse("**bold**");

    expect(bot.sendPrivateMessage).toHaveBeenCalledTimes(2);
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(
      1,
      "user-openid",
      "**bold**",
      "msg-id",
      7,
      true,
    );
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(
      2,
      "user-openid",
      "**bold**",
      "msg-id",
      8,
      false,
    );
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError.mock.calls[0]?.[0]).toContain(
      "QQ markdown delivery disabled after first failure",
    );
  });

  it("disables markdown for the rest of the channel after the first rejection", async () => {
    const bot = {
      sendPrivateMessage: vi
        .fn()
        .mockRejectedValueOnce(new Error("markdown rejected"))
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
      nextOutboundMsgSeq: number;
      markdownDisabled: boolean;
    };
    channel.bot = bot;
    channel.qqUserId = "user-openid";
    channel.qqMessageId = "msg-id";
    channel.nextOutboundMsgSeq = 10;

    await channel.sendResponse("**first**");
    await channel.sendResponse("**second**");

    expect(channel.markdownDisabled).toBe(true);
    expect(bot.sendPrivateMessage).toHaveBeenCalledTimes(3);
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(
      3,
      "user-openid",
      "**second**",
      "msg-id",
      13,
      false,
    );
    expect(onError).toHaveBeenCalledTimes(1);
  });

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
      true,
    );
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(
      2,
      "user-openid",
      "a",
      "msg-id",
      42,
      true,
    );
  });

  it("stops after the first chunk whose markdown and plain-text fallback both fail", async () => {
    const bot = {
      sendPrivateMessage: vi
        .fn()
        .mockResolvedValueOnce(undefined)
        .mockRejectedValueOnce(new Error("duplicate msgseq"))
        .mockRejectedValueOnce(new Error("plain text rejected"))
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

    expect(bot.sendPrivateMessage).toHaveBeenCalledTimes(3);
    expect(onError).toHaveBeenCalledTimes(2);
    expect(onError.mock.calls[0]?.[0]).toContain(
      "QQ markdown delivery disabled after first failure",
    );
    expect(onError.mock.calls[1]?.[0]).toContain("chunk 2/3 failed");
  });
});

describe("QQChannel inbound messaging", () => {
  it("surfaces first-sender runtime binding as info instead of an error", () => {
    const onSubmitMessage = vi.fn();
    const onError = vi.fn();
    const onInfo = vi.fn();
    const channel = new QQChannel({
      onSubmitMessage,
      onError,
      onInfo,
    }) as QQChannel & {
      ownerOpenId?: string;
      allowlist?: string[];
      runtimeBoundOpenId: string | null;
      handlePrivateMessage: (msg: {
        author: { user_openid: string };
        content: string;
        id: string;
        timestamp: string;
      }) => void;
    };

    channel.handlePrivateMessage({
      author: { user_openid: "abcdefghijklmnop" },
      content: "hello",
      id: "msg-1",
      timestamp: new Date().toISOString(),
    });

    expect(onInfo).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
    expect(onSubmitMessage).toHaveBeenCalledWith("[QQ] hello");
  });
});
