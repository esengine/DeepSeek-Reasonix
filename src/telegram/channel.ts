import { mkdirSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { loadTelegramConfig } from "../config.js";
import { loadDotenv } from "../env.js";
import { t } from "../i18n/index.js";
import { decideTelegramAccess, describeTelegramAccess, redactTelegramUserId } from "./access.js";
import { TelegramBot, type TelegramMessage } from "./bot.js";

const TELEGRAM_LOCK_FILE = join(homedir(), ".reasonix", "telegram-channel.pid");
const TELEGRAM_MAX_CHARS = 3900;
const NATURAL_SPLIT_MIN_FRACTION = 0.6;

function pickNaturalSplit(candidate: string): number {
  const minSplit = Math.floor(candidate.length * NATURAL_SPLIT_MIN_FRACTION);
  const splitters = ["\n\n", "\n", " "];
  for (const splitter of splitters) {
    const at = candidate.lastIndexOf(splitter);
    if (at >= minSplit) return at + splitter.length;
  }
  return candidate.length;
}

export function splitTelegramMessage(text: string, maxChars = TELEGRAM_MAX_CHARS): string[] {
  const chunks: string[] = [];
  let remaining = text;
  while (remaining.length > 0) {
    if (remaining.length <= maxChars) {
      chunks.push(remaining);
      break;
    }
    const candidate = remaining.slice(0, maxChars);
    const splitAt = pickNaturalSplit(candidate);
    chunks.push(candidate.slice(0, splitAt));
    remaining = remaining.slice(splitAt).trimStart();
  }
  return chunks;
}

export class TelegramChannel {
  private bot: TelegramBot | null = null;
  private chatId: number | null = null;
  private messageId: number | null = null;
  private ownerUserId: string | undefined;
  private allowlist: string[] | undefined;
  private runtimeBoundUserId: string | null = null;
  private processedUpdateIds = new Set<string>();
  private processedUpdateIdQueue: string[] = [];
  private lockAcquired = false;

  constructor(
    private callbacks: {
      onSubmitMessage: (text: string) => void;
      onError?: (msg: string) => void;
    },
  ) {}

  private rememberMessage(id: string): boolean {
    if (this.processedUpdateIds.has(id)) return false;
    this.processedUpdateIds.add(id);
    this.processedUpdateIdQueue.push(id);
    if (this.processedUpdateIdQueue.length > 200) {
      const oldest = this.processedUpdateIdQueue.shift();
      if (oldest) this.processedUpdateIds.delete(oldest);
    }
    return true;
  }

  private acquireLock(): void {
    try {
      const existing = Number(readFileSync(TELEGRAM_LOCK_FILE, "utf8").trim());
      if (Number.isInteger(existing) && existing > 0 && existing !== process.pid) {
        try {
          process.kill(existing, 0);
          throw new Error(t("handlers.telegram.lockAlreadyRunning", { pid: existing }));
        } catch (err) {
          const e = err as NodeJS.ErrnoException;
          if (e.code !== "ESRCH") throw err;
        }
      }
    } catch (err) {
      const e = err as NodeJS.ErrnoException;
      if (e.code !== "ENOENT") throw err;
    }

    mkdirSync(dirname(TELEGRAM_LOCK_FILE), { recursive: true });
    writeFileSync(TELEGRAM_LOCK_FILE, String(process.pid), "utf8");
    this.lockAcquired = true;
  }

  private releaseLock(): void {
    if (!this.lockAcquired) return;
    try {
      const existing = Number(readFileSync(TELEGRAM_LOCK_FILE, "utf8").trim());
      if (existing === process.pid) unlinkSync(TELEGRAM_LOCK_FILE);
    } catch {}
    this.lockAcquired = false;
  }

  private applyAccessConfig(config: ReturnType<typeof loadTelegramConfig>): void {
    this.ownerUserId = config.ownerUserId;
    this.allowlist = config.allowlist;
    if (this.ownerUserId || (this.allowlist?.length ?? 0) > 0) {
      this.runtimeBoundUserId = null;
    }
  }

  private hasConfiguredAccess(): boolean {
    return !!this.ownerUserId || (this.allowlist?.length ?? 0) > 0;
  }

  private handleMessage(msg: TelegramMessage): void {
    const text = msg.text?.trim();
    if (!text || msg.from?.is_bot) return;
    const fromId = msg.from?.id;
    if (typeof fromId !== "number") return;
    if (!this.rememberMessage(`${msg.chat.id}:${msg.message_id}`)) return;

    const userId = String(fromId);
    const verdict = decideTelegramAccess(
      {
        ownerUserId: this.ownerUserId,
        allowlist: this.allowlist,
        runtimeBoundUserId: this.runtimeBoundUserId,
      },
      userId,
    );
    if (!verdict.accept) {
      this.callbacks.onError?.(
        t("handlers.telegram.unauthorizedMessage", {
          userId: redactTelegramUserId(userId),
          access: this.describeAccess(),
        }),
      );
      return;
    }
    if (verdict.bindRuntime) {
      this.runtimeBoundUserId = userId;
      this.callbacks.onError?.(
        t("handlers.telegram.runtimeBound", {
          userId: redactTelegramUserId(userId),
        }),
      );
    }

    this.chatId = msg.chat.id;
    this.messageId = msg.message_id;
    this.callbacks.onSubmitMessage(`[TG] ${text}`);
  }

  refreshAccessConfig(): void {
    this.applyAccessConfig(loadTelegramConfig());
  }

  describeAccess(): string {
    return describeTelegramAccess({
      ownerUserId: this.ownerUserId,
      allowlist: this.allowlist,
      runtimeBoundUserId: this.runtimeBoundUserId,
    });
  }

  getRuntimeBoundUserId(): string | null {
    return this.runtimeBoundUserId;
  }

  async start(): Promise<void> {
    loadDotenv();
    this.acquireLock();

    const config = loadTelegramConfig();
    if (!config.botToken) {
      this.releaseLock();
      throw new Error(t("handlers.telegram.missingBotToken"));
    }
    this.applyAccessConfig(config);
    if (!this.hasConfiguredAccess()) {
      this.releaseLock();
      throw new Error(t("handlers.telegram.accessRequired"));
    }

    const bot = new TelegramBot({ token: config.botToken });
    bot.on("online", () => {
      process.stderr.write("Telegram bot is online!\n");
    });
    bot.on("bot_error", (msg: string) => {
      this.callbacks.onError?.(msg);
    });
    bot.on("message", (msg: TelegramMessage) => {
      this.handleMessage(msg);
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
    if (!this.bot || this.chatId === null) return;
    const chunks = splitTelegramMessage(text.trim());
    for (let index = 0; index < chunks.length; index++) {
      const chunk = chunks[index];
      if (!chunk) continue;
      try {
        await this.bot.sendMessage(this.chatId, chunk, this.messageId ?? undefined);
      } catch (err) {
        this.callbacks.onError?.(
          `Telegram sendResponse chunk ${index + 1}/${chunks.length} failed: ${(err as Error).message}`,
        );
        break;
      }
    }
  }

  async stop(): Promise<void> {
    await this.bot?.stop();
    this.releaseLock();
  }
}
