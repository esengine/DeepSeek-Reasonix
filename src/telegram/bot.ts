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

export class TelegramBot extends EventEmitter {
  private readonly bot: Bot<Context>;

  constructor(config: TelegramBotConfig) {
    super();
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
        this.emit("bot_error", err instanceof Error ? err.message : String(err));
      });
    });
    this.bot.catch((err) => {
      this.emit("bot_error", err instanceof Error ? err.message : String(err));
    });
  }

  async start(): Promise<void> {
    await this.bot.init();
    this.emit("online");
    void this.bot
      .start({
        allowed_updates: ["message", "callback_query"],
        drop_pending_updates: false,
        onStart: () => undefined,
      })
      .catch((err) => {
        this.emit("bot_error", err instanceof Error ? err.message : String(err));
      });
  }

  async stop(): Promise<void> {
    await this.bot.stop();
  }

  async sendMessage(
    chatId: number,
    text: string,
    replyToMessageId?: number,
    parseMode?: TelegramParseMode,
    buttons?: TelegramInlineButton[][],
  ): Promise<void> {
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
  }
}
