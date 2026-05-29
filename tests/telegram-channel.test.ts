import { describe, expect, it, vi } from "vitest";
import { formatTelegramBotError } from "../src/telegram/bot.js";
import {
  TelegramChannel,
  buildTelegramBotCommands,
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

describe("buildTelegramBotCommands", () => {
  it("exports slash commands in Telegram command format", () => {
    const commands = buildTelegramBotCommands();
    expect(commands.length).toBeGreaterThan(40);
    expect(commands).toContainEqual(
      expect.objectContaining({
        command: "telegram",
        description: expect.stringContaining("Telegram"),
      }),
    );
    expect(commands).toContainEqual(
      expect.objectContaining({
        command: "search_engine",
      }),
    );
    for (const command of commands) {
      expect(command.command).toMatch(/^[a-z0-9_]{1,32}$/);
      expect(command.description.length).toBeGreaterThan(0);
      expect(command.description.length).toBeLessThanOrEqual(256);
    }
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
    const bot = { sendMessage: vi.fn().mockResolvedValue(789) };
    const channel = new TelegramChannel({
      onSubmitMessage: () => undefined,
    }) as unknown as {
      bot: typeof bot;
      chatId: number;
      messageId: number;
      activeCallbackMessageId: number | null;
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
    expect(channel.activeCallbackMessageId).toBe(789);
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

describe("TelegramChannel ingress rate limiting", () => {
  it("rejects authorized users after the Telegram ingress cap", async () => {
    const bot = { sendMessage: vi.fn().mockResolvedValue(undefined) };
    const onSubmitMessage = vi.fn();
    const onError = vi.fn();
    const channel = new TelegramChannel({
      onSubmitMessage,
      onError,
    }) as unknown as {
      bot: typeof bot;
      ownerUserId: string;
      handleMessage: (msg: {
        message_id: number;
        text: string;
        chat: { id: number; type: string };
        from: { id: number; is_bot?: boolean };
        date: number;
      }) => void;
    };
    channel.bot = bot;
    channel.ownerUserId = "42";

    for (let index = 1; index <= 6; index++) {
      channel.handleMessage({
        message_id: index,
        text: `message ${index}`,
        chat: { id: 123, type: "private" },
        from: { id: 42 },
        date: 1,
      });
    }

    expect(onSubmitMessage).toHaveBeenCalledTimes(5);
    expect(onSubmitMessage).toHaveBeenLastCalledWith("[TG] message 5");
    expect(bot.sendMessage).toHaveBeenCalledTimes(1);
    expect(bot.sendMessage.mock.calls[0]?.[1]).toContain("too quickly");
    expect(onError.mock.calls[0]?.[0]).toContain("rate-limited");
  });
});

describe("TelegramChannel callback confirmation binding", () => {
  it("ignores callbacks when no confirmation message is active", () => {
    const onSubmitMessage = vi.fn();
    const onError = vi.fn();
    const channel = new TelegramChannel({
      onSubmitMessage,
      onError,
    }) as unknown as {
      ownerUserId: string;
      activeCallbackMessageId: number | null;
      handleCallbackQuery: (query: {
        id: string;
        data: string;
        message?: { message_id: number; chat: { id: number; type: string } };
        from: { id: number; is_bot?: boolean };
      }) => void;
    };
    channel.ownerUserId = "42";
    channel.activeCallbackMessageId = null;

    channel.handleCallbackQuery({
      id: "callback-1",
      data: "tg:1:1",
      message: { message_id: 200, chat: { id: 123, type: "private" } },
      from: { id: 42 },
    });

    expect(onSubmitMessage).not.toHaveBeenCalled();
    expect(onError.mock.calls[0]?.[0]).toContain("stale callback");
  });

  it("ignores callbacks from older confirmation messages", () => {
    const onSubmitMessage = vi.fn();
    const onError = vi.fn();
    const channel = new TelegramChannel({
      onSubmitMessage,
      onError,
    }) as unknown as {
      ownerUserId: string;
      activeCallbackMessageId: number | null;
      handleCallbackQuery: (query: {
        id: string;
        data: string;
        message?: { message_id: number; chat: { id: number; type: string } };
        from: { id: number; is_bot?: boolean };
      }) => void;
    };
    channel.ownerUserId = "42";
    channel.activeCallbackMessageId = 200;

    channel.handleCallbackQuery({
      id: "callback-1",
      data: "tg:1:1",
      message: { message_id: 199, chat: { id: 123, type: "private" } },
      from: { id: 42 },
    });

    expect(onSubmitMessage).not.toHaveBeenCalled();
    expect(onError.mock.calls[0]?.[0]).toContain("stale callback");
  });

  it("accepts callbacks only from the current confirmation message", () => {
    const onSubmitMessage = vi.fn();
    const channel = new TelegramChannel({
      onSubmitMessage,
    }) as unknown as {
      ownerUserId: string;
      activeCallbackMessageId: number | null;
      handleCallbackQuery: (query: {
        id: string;
        data: string;
        message?: { message_id: number; chat: { id: number; type: string } };
        from: { id: number; is_bot?: boolean };
      }) => void;
    };
    channel.ownerUserId = "42";
    channel.activeCallbackMessageId = 200;

    channel.handleCallbackQuery({
      id: "callback-1",
      data: "tg:1:1",
      message: { message_id: 200, chat: { id: 123, type: "private" } },
      from: { id: 42 },
    });

    expect(onSubmitMessage).toHaveBeenCalledWith("[TG] tg:1:1");
    expect(channel.activeCallbackMessageId).toBeNull();
  });
});

describe("formatTelegramBotError", () => {
  it("redacts bot tokens from network-facing errors", () => {
    const token = "123456:ABC_secret-token";
    expect(
      formatTelegramBotError(
        new Error(`request to https://api.telegram.org/bot${token}/sendMessage failed`),
        token,
        "Telegram polling",
      ),
    ).toBe(
      "Telegram polling: request to https://api.telegram.org/bot<redacted>/sendMessage failed",
    );
  });

  it("explains polling conflicts without leaking token details", () => {
    const token = "123456:ABC_secret-token";
    expect(
      formatTelegramBotError(
        { error_code: 409, description: `Conflict for bot${token}` },
        token,
        "Telegram polling",
      ),
    ).toBe(
      "Telegram polling: Telegram polling conflict (409). Stop the other bot instance or clear the webhook.",
    );
  });
});
