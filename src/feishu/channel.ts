import { mkdirSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { loadFeishuConfig } from "../config.js";
import { loadDotenv } from "../env.js";
import { t } from "../i18n/index.js";
import { decideFeishuAccess, describeFeishuAccess, redactFeishuOpenId } from "./access.js";
import { FeishuBot, type FeishuPrivateMessage } from "./bot.js";
import { formatFeishuAccessSummary } from "./strings.js";

const FEISHU_LOCK_FILE = join(homedir(), ".reasonix", "feishu-channel.pid");

export class FeishuChannel {
  private bot: FeishuBot | null = null;
  private chatId: string | null = null;
  private ownerOpenId: string | undefined;
  private allowlist: string[] | undefined;
  private markdownDisabled = false;
  private processedMsgIds = new Set<string>();
  private processedMsgIdQueue: string[] = [];
  private lockAcquired = false;

  constructor(
    private callbacks: {
      onSubmitMessage: (text: string) => void;
      onError?: (msg: string) => void;
      onInfo?: (msg: string) => void;
    },
  ) {}

  private rememberMessage(id: string): boolean {
    if (this.processedMsgIds.has(id)) return false;
    this.processedMsgIds.add(id);
    this.processedMsgIdQueue.push(id);
    if (this.processedMsgIdQueue.length > 200) {
      const oldest = this.processedMsgIdQueue.shift();
      if (oldest) this.processedMsgIds.delete(oldest);
    }
    return true;
  }

  private acquireLock(): void {
    try {
      const existing = Number(readFileSync(FEISHU_LOCK_FILE, "utf8").trim());
      if (Number.isInteger(existing) && existing > 0 && existing !== process.pid) {
        try {
          process.kill(existing, 0);
          throw new Error(t("handlers.feishu.lockAlreadyRunning", { pid: existing }));
        } catch (err) {
          const e = err as NodeJS.ErrnoException;
          if (e.code !== "ESRCH") throw err;
        }
      }
    } catch (err) {
      const e = err as NodeJS.ErrnoException;
      if (e.code !== "ENOENT") throw err;
    }

    mkdirSync(dirname(FEISHU_LOCK_FILE), { recursive: true });
    writeFileSync(FEISHU_LOCK_FILE, String(process.pid), "utf8");
    this.lockAcquired = true;
  }

  private releaseLock(): void {
    if (!this.lockAcquired) return;
    try {
      const existing = Number(readFileSync(FEISHU_LOCK_FILE, "utf8").trim());
      if (existing === process.pid) unlinkSync(FEISHU_LOCK_FILE);
    } catch {}
    this.lockAcquired = false;
  }

  private applyAccessConfig(config: ReturnType<typeof loadFeishuConfig>): void {
    this.ownerOpenId = config.ownerOpenId;
    this.allowlist = config.allowlist;
  }

  handlePrivateMessage(msg: FeishuPrivateMessage): void {
    const text = msg.text?.trim();
    if (!text || !this.rememberMessage(msg.messageId)) return;

    const verdict = decideFeishuAccess(
      {
        ownerOpenId: this.ownerOpenId,
        allowlist: this.allowlist,
      },
      msg.openId,
    );
    if (!verdict.accept) {
      this.callbacks.onError?.(
        t("handlers.feishu.unauthorizedMessage", {
          openid: redactFeishuOpenId(msg.openId),
          access: formatFeishuAccessSummary({
            ownerOpenId: this.ownerOpenId,
            allowlist: this.allowlist,
          }),
        }),
      );
      return;
    }

    this.chatId = msg.chatId;
    this.callbacks.onSubmitMessage(`[Feishu] ${text}`);
  }

  refreshAccessConfig(): void {
    this.applyAccessConfig(loadFeishuConfig());
  }

  describeAccess(): string {
    return describeFeishuAccess({
      ownerOpenId: this.ownerOpenId,
      allowlist: this.allowlist,
    });
  }

  async start(): Promise<void> {
    loadDotenv();
    this.acquireLock();

    const config = loadFeishuConfig();
    if (!config.appId) {
      this.releaseLock();
      throw new Error(t("handlers.feishu.missingAppId"));
    }
    if (!config.appSecret) {
      this.releaseLock();
      throw new Error(t("handlers.feishu.missingAppSecret"));
    }
    this.applyAccessConfig(config);
    if (!this.ownerOpenId && (this.allowlist?.length ?? 0) === 0) {
      this.releaseLock();
      throw new Error(t("handlers.feishu.missingOwnerOpenId"));
    }

    const bot = new FeishuBot({
      appId: config.appId,
      appSecret: config.appSecret,
    });
    bot.on("message.private", (msg: FeishuPrivateMessage) => {
      this.handlePrivateMessage(msg);
    });
    bot.on("bot_error", (msg: string) => {
      this.callbacks.onError?.(msg);
    });
    bot.on("online", () => {
      process.stderr.write("Feishu bot is online!\n");
    });

    this.bot = bot;
    try {
      await bot.start();
    } catch (err) {
      this.releaseLock();
      throw err;
    }
  }

  async sendResponse(text: string): Promise<void> {
    if (!this.bot || !this.chatId) return;
    if (!this.markdownDisabled) {
      try {
        await this.bot.sendPrivateMessage(this.chatId, text, true);
        return;
      } catch (err) {
        this.markdownDisabled = true;
        this.callbacks.onInfo?.(
          `Feishu markdown delivery disabled after first failure: ${(err as Error).message}`,
        );
      }
    }

    await this.bot.sendPrivateMessage(this.chatId, text, false);
  }

  async stop(): Promise<void> {
    await this.bot?.stop();
    this.releaseLock();
  }
}
