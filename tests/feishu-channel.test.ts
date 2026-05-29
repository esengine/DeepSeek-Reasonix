import { describe, expect, it, vi } from "vitest";
import {
  FeishuChannel,
  formatFeishuMarkdownReply,
  normalizeFeishuMarkdownReply,
} from "../src/feishu/channel.js";

describe("FeishuChannel", () => {
  it("unwraps a full fenced markdown wrapper before delivery", () => {
    expect(normalizeFeishuMarkdownReply("```markdown\n# Title\n\n**bold**\n\n- item\n```")).toBe(
      "# Title\n\n**bold**\n\n- item",
    );
  });

  it("converts headings, tables, separators, and code fences into Feishu-friendly text", () => {
    expect(
      formatFeishuMarkdownReply(`### 测试标题

| 项目 | 值 |
|---|---|
| 平台 | Feishu |
| 状态 | 测试中 |

---

\`\`\`python
def hello():
  print("Hello, World!")
\`\`\``),
    ).toBe(`**测试标题**

- **项目**：平台
- **值**：Feishu

- **项目**：状态
- **值**：测试中

────────

**代码（python）**
def hello():
  print("Hello, World!")`);
  });

  it("sends the final reply back to the most recent private chat through the markdown path first", async () => {
    const bot = { sendPrivateMessage: vi.fn().mockResolvedValue(undefined) };
    const channel = new FeishuChannel({ onSubmitMessage: () => undefined }) as FeishuChannel & {
      bot: typeof bot;
      chatId: string;
    };
    channel.bot = bot;
    channel.chatId = "oc_chat";

    await channel.sendResponse("hello from reasonix");

    expect(bot.sendPrivateMessage).toHaveBeenCalledWith("oc_chat", "hello from reasonix", true);
  });

  it("formats markdown-ish content before sending it to Feishu", async () => {
    const bot = { sendPrivateMessage: vi.fn().mockResolvedValue(undefined) };
    const channel = new FeishuChannel({ onSubmitMessage: () => undefined }) as FeishuChannel & {
      bot: typeof bot;
      chatId: string;
    };
    channel.bot = bot;
    channel.chatId = "oc_chat";

    await channel.sendResponse("### 标题\n\n```python\nprint('hi')\n```");

    expect(bot.sendPrivateMessage).toHaveBeenCalledWith(
      "oc_chat",
      "**标题**\n\n**代码（python）**\nprint('hi')",
      true,
    );
  });

  it("falls back to plain text when Feishu markdown delivery fails", async () => {
    const onInfo = vi.fn();
    const bot = {
      sendPrivateMessage: vi
        .fn()
        .mockRejectedValueOnce(new Error("markdown rejected"))
        .mockResolvedValueOnce(undefined),
    };
    const channel = new FeishuChannel({
      onSubmitMessage: () => undefined,
      onInfo,
    }) as FeishuChannel & {
      bot: typeof bot;
      chatId: string;
      markdownDisabled: boolean;
    };
    channel.bot = bot;
    channel.chatId = "oc_chat";

    await channel.sendResponse("### 标题");

    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(1, "oc_chat", "**标题**", true);
    expect(bot.sendPrivateMessage).toHaveBeenNthCalledWith(2, "oc_chat", "**标题**", false);
    expect(onInfo).toHaveBeenCalledWith(
      expect.stringContaining("Feishu markdown delivery disabled after first failure"),
    );
    expect(channel.markdownDisabled).toBe(true);
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
