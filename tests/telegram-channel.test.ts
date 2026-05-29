import { describe, expect, it, vi } from "vitest";
import {
  TelegramChannel,
  formatTelegramMarkdownV2,
  normalizeTelegramMarkdownReply,
  splitTelegramMessage,
} from "../src/telegram/channel.js";

describe("splitTelegramMessage", () => {
  it("keeps every chunk within the character budget", () => {
    const chunks = splitTelegramMessage("a".repeat(8001), 3900);
    expect(chunks.length).toBe(3);
    for (const chunk of chunks) {
      expect(chunk.length).toBeLessThanOrEqual(3900);
    }
  });
});

describe("normalizeTelegramMarkdownReply", () => {
  it("unwraps a full fenced markdown block before delivery", () => {
    expect(normalizeTelegramMarkdownReply("```markdown\n# Title\n\n**bold**\n\n- item\n```")).toBe(
      "# Title\n\n**bold**\n\n- item",
    );
  });

  it("keeps normal code blocks unchanged when the whole reply is not a markdown wrapper", () => {
    expect(normalizeTelegramMarkdownReply("Here is code:\n```ts\nconsole.log('hi')\n```")).toBe(
      "Here is code:\n```ts\nconsole.log('hi')\n```",
    );
  });
});

describe("formatTelegramMarkdownV2", () => {
  it("converts GitHub-flavored headings, tables, separators, and bold text for Telegram", () => {
    expect(
      formatTelegramMarkdownV2(`### 💻 宿主机配置（再次确认）

| 项目 | 值 |
|---|---|
| 操作系统 | Darwin (macOS) — Kernel 25.3.0 |
| CPU | **Apple M5** — 10 核 |

---

总结： **Apple M5** / 24GB / 926GiB 磁盘。`),
    ).toBe(`*💻 宿主机配置（再次确认）*

• *项目*: 操作系统
• *值*: Darwin \\(macOS\\) — Kernel 25\\.3\\.0

• *项目*: CPU
• *值*: *Apple M5* — 10 核

────────

总结： *Apple M5* / 24GB / 926GiB 磁盘。`);
  });
});

describe("TelegramChannel.sendResponse", () => {
  it("sends replies to the last chat with markdown rendering enabled", async () => {
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

    expect(bot.sendMessage).toHaveBeenCalledWith(123, "hello", 456, "MarkdownV2", undefined);
  });

  it("attaches inline buttons to the delivered response", async () => {
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
    const buttons = [[{ text: "Run once", callbackData: "1" }]];

    await channel.sendResponse("Need confirmation", buttons);

    expect(bot.sendMessage).toHaveBeenCalledWith(
      123,
      "Need confirmation",
      456,
      "MarkdownV2",
      buttons,
    );
  });

  it("falls back to plain text when markdown delivery fails", async () => {
    const bot = {
      sendMessage: vi
        .fn()
        .mockRejectedValueOnce(new Error("markdown rejected"))
        .mockResolvedValueOnce(undefined),
    };
    const onError = vi.fn();
    const channel = new TelegramChannel({
      onSubmitMessage: () => undefined,
      onError,
    }) as unknown as {
      bot: typeof bot;
      chatId: number;
      messageId: number;
      sendResponse: TelegramChannel["sendResponse"];
      markdownDisabled: boolean;
    };
    channel.bot = bot;
    channel.chatId = 123;
    channel.messageId = 456;

    await channel.sendResponse("**bold**");

    expect(channel.markdownDisabled).toBe(true);
    expect(bot.sendMessage).toHaveBeenCalledTimes(2);
    expect(bot.sendMessage).toHaveBeenNthCalledWith(1, 123, "*bold*", 456, "MarkdownV2", undefined);
    expect(bot.sendMessage).toHaveBeenNthCalledWith(2, 123, "*bold*", 456, undefined, undefined);
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError.mock.calls[0]?.[0]).toContain(
      "Telegram markdown delivery disabled after first failure",
    );
  });
});
