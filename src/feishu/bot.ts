import { EventEmitter } from "node:events";
import * as Lark from "@larksuiteoapi/node-sdk";

interface FeishuBotConfig {
  appId: string;
  appSecret: string;
}

export interface FeishuPrivateMessage {
  chatId: string;
  messageId: string;
  openId: string;
  text: string;
}

function parseFeishuText(content: string | undefined, messageType: string | undefined): string {
  if (!content) return "";
  if (messageType === "text") {
    try {
      const parsed = JSON.parse(content) as { text?: string };
      return parsed.text?.trim() ?? "";
    } catch {
      return "";
    }
  }
  return "";
}

export class FeishuBot extends EventEmitter {
  private client: Lark.Client;
  private wsClient: InstanceType<typeof Lark.WSClient> | null = null;

  constructor(private config: FeishuBotConfig) {
    super();
    this.client = new Lark.Client({
      appId: config.appId,
      appSecret: config.appSecret,
      appType: Lark.AppType.SelfBuild,
      domain: Lark.Domain.Feishu,
    });
  }

  async start(): Promise<void> {
    const dispatcher = new Lark.EventDispatcher({});
    dispatcher.register({
      "im.message.receive_v1": async (data: any) => {
        try {
          const event = data as {
            sender?: { sender_id?: { open_id?: string } };
            message?: {
              chat_id?: string;
              chat_type?: string;
              content?: string;
              message_id?: string;
              message_type?: string;
            };
          };
          if (event.message?.chat_type !== "p2p") return;
          const text = parseFeishuText(event.message?.content, event.message?.message_type);
          if (!text) return;
          const openId = event.sender?.sender_id?.open_id;
          const chatId = event.message?.chat_id;
          const messageId = event.message?.message_id;
          if (!openId || !chatId || !messageId) return;
          this.emit("message.private", {
            chatId,
            messageId,
            openId,
            text,
          } satisfies FeishuPrivateMessage);
        } catch (err) {
          this.emit("bot_error", (err as Error).message);
        }
      },
    } as any);

    this.wsClient = new Lark.WSClient({
      appId: this.config.appId,
      appSecret: this.config.appSecret,
      domain: Lark.Domain.Feishu,
      loggerLevel: Lark.LoggerLevel.error,
    });
    await this.wsClient.start({ eventDispatcher: dispatcher });
    this.emit("online");
  }

  async stop(): Promise<void> {
    this.wsClient?.close();
    this.wsClient = null;
  }

  async sendPrivateMessage(chatId: string, content: string): Promise<void> {
    const resp = await this.client.im.message.create({
      params: { receive_id_type: "chat_id" },
      data: {
        receive_id: chatId,
        msg_type: "text",
        content: JSON.stringify({ text: content }),
      },
    });
    if (resp.code && resp.code !== 0) {
      throw new Error(resp.msg || `Feishu sendPrivateMessage failed (${resp.code})`);
    }
  }
}
