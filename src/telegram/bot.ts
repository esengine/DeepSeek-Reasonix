import { EventEmitter } from "node:events";
import { Bot, type Context } from "grammy";

interface TelegramBotConfig {
  token: string;
}

export type TelegramParseMode = "MarkdownV2";

export interface TelegramInlineButton {
  text: string;
  callbackData: string;
}

export interface TelegramBotCommand {
  command: string;
  description: string;
}

export interface TelegramMessage {
  message_id: number;
  text?: string;
  chat: { id: number; type: string };
  from?: {
    id: number;
    is_bot?: boolean;
    username?: string;
    first_name?: string;
  };
  date: number;
}

export interface TelegramCallbackQuery {
  id: string;
  data: string;
  message?: {
    message_id: number;
    chat: { id: number; type: string };
  };
  from: {
    id: number;
    is_bot?: boolean;
    username?: string;
    first_name?: string;
  };
}

function getTelegramErrorCode(err: unknown): number | null {
  if (!err || typeof err !== "object") return null;
  const record = err as Record<string, unknown>;
  const direct = record.error_code ?? record.status;
  if (typeof direct === "number") return direct;
  const nested = record.error;
  if (nested && typeof nested === "object") {
    const code = (nested as Record<string, unknown>).error_code;
    if (typeof code === "number") return code;
  }
  return null;
}

export function formatTelegramBotError(
  err: unknown,
  token: string,
  context = "Telegram bot",
): string {
  const raw =
    err instanceof Error
      ? err.message
      : err &&
          typeof err === "object" &&
          typeof (err as Record<string, unknown>).description === "string"
        ? ((err as Record<string, unknown>).description as string)
        : String(err);
  const escapedToken = token.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  let message = escapedToken ? raw.replace(new RegExp(escapedToken, "g"), "<redacted>") : raw;
  message = message.replace(/bot\d+:[A-Za-z0-9_-]+/g, "bot<redacted>");

  const code = getTelegramErrorCode(err);
  if (code === 401) {
    return `${context}: Telegram rejected the bot token (401). Check telegram.botToken.`;
  }
  if (code === 409) {
    return `${context}: Telegram polling conflict (409). Stop the other bot instance or clear the webhook.`;
  }
  if (code !== null) {
    return `${context}: Telegram API error ${code}: ${message}`;
  }
  return `${context}: ${message}`;
}

export class TelegramBot extends EventEmitter {
  private readonly bot: Bot<Context>;
  private readonly token: string;

  constructor(config: TelegramBotConfig) {
    super();
    this.token = config.token;
    this.bot = new Bot(config.token);
    this.bot.on("message:text", (ctx) => {
      const msg = ctx.message;
      this.emit("message", {
        message_id: msg.message_id,
        text: msg.text,
        chat: { id: msg.chat.id, type: msg.chat.type },
        from: msg.from
          ? {
              id: msg.from.id,
              is_bot: msg.from.is_bot,
              username: msg.from.username,
              first_name: msg.from.first_name,
            }
          : undefined,
        date: msg.date,
      } satisfies TelegramMessage);
    });
    this.bot.on("callback_query:data", async (ctx) => {
      const query = ctx.callbackQuery;
      const message = query.message;
      this.emit("callback_query", {
        id: query.id,
        data: query.data,
        message: message
          ? {
              message_id: message.message_id,
              chat: { id: message.chat.id, type: message.chat.type },
            }
          : undefined,
        from: {
          id: query.from.id,
          is_bot: query.from.is_bot,
          username: query.from.username,
          first_name: query.from.first_name,
        },
      } satisfies TelegramCallbackQuery);
      await ctx.answerCallbackQuery().catch((err) => {
        this.emit("bot_error", formatTelegramBotError(err, this.token, "Telegram callback"));
      });
    });
    this.bot.catch((err) => {
      this.emit("bot_error", formatTelegramBotError(err, this.token, "Telegram polling"));
    });
  }

  async start(): Promise<void> {
    try {
      await this.bot.init();
    } catch (err) {
      throw new Error(formatTelegramBotError(err, this.token, "Telegram initialization"));
    }
    this.emit("online");
    void this.bot
      .start({
        allowed_updates: ["message", "callback_query"],
        drop_pending_updates: false,
        onStart: () => undefined,
      })
      .catch((err) => {
        this.emit("bot_error", formatTelegramBotError(err, this.token, "Telegram polling"));
      });
  }

  async stop(): Promise<void> {
    await this.bot.stop();
  }

  async setCommands(commands: readonly TelegramBotCommand[]): Promise<void> {
    try {
      await this.bot.api.setMyCommands(
        commands.map(({ command, description }) => ({ command, description })),
      );
    } catch (err) {
      throw new Error(formatTelegramBotError(err, this.token, "Telegram command registration"));
    }
  }

  async sendMessage(
    chatId: number,
    text: string,
    replyToMessageId?: number,
    parseMode?: TelegramParseMode,
    buttons?: TelegramInlineButton[][],
  ): Promise<void> {
    try {
      await this.bot.api.sendMessage(chatId, text, {
        link_preview_options: { is_disabled: true },
        parse_mode: parseMode,
        reply_parameters: replyToMessageId ? { message_id: replyToMessageId } : undefined,
        reply_markup: buttons
          ? {
              inline_keyboard: buttons.map((row) =>
                row.map((button) => ({
                  text: button.text,
                  callback_data: button.callbackData,
                })),
              ),
            }
          : undefined,
      });
    } catch (err) {
      throw new Error(formatTelegramBotError(err, this.token, "Telegram sendMessage"));
    }
  }
}
