import { mkdirSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { loadQQConfig } from "../config.js";
import { loadDotenv } from "../env.js";
import { type C2CMessage, QQBot } from "./bot.js";

const QQ_LOCK_FILE = join(homedir(), ".reasonix", "qq-channel.pid");
const QQ_MAX_CHUNK_BYTES = 1500;
const NATURAL_SPLIT_MIN_FRACTION = 0.6;

function fitUtf8Slice(text: string, maxBytes: number): string {
  let end = 0;
  let bytes = 0;
  for (const char of text) {
    const nextBytes = Buffer.byteLength(char, "utf8");
    if (bytes > 0 && bytes + nextBytes > maxBytes) break;
    end += char.length;
    bytes += nextBytes;
  }
  return end > 0 ? text.slice(0, end) : text.slice(0, 1);
}

function pickNaturalSplit(candidate: string): number {
  const minSplit = Math.floor(candidate.length * NATURAL_SPLIT_MIN_FRACTION);
  const splitters = ["\n\n", "\n", " "];
  for (const splitter of splitters) {
    const at = candidate.lastIndexOf(splitter);
    if (at >= minSplit) return at + splitter.length;
  }
  return candidate.length;
}

export function splitQQMessage(text: string, maxBytes = QQ_MAX_CHUNK_BYTES): string[] {
  const chunks: string[] = [];
  let remaining = text;
  while (remaining.length > 0) {
    if (Buffer.byteLength(remaining, "utf8") <= maxBytes) {
      chunks.push(remaining);
      break;
    }

    const candidate = fitUtf8Slice(remaining, maxBytes);
    const splitAt = pickNaturalSplit(candidate);
    chunks.push(candidate.slice(0, splitAt));
    remaining = remaining.slice(splitAt).trimStart();
  }
  return chunks;
}

export class QQChannel {
  private bot: QQBot | null = null;
  private qqUserId: string | null = null;
  private qqMessageId: string | null = null;
  private processedMsgIds = new Set<string>();
  private processedMsgIdQueue: string[] = [];
  private lockAcquired = false;
  private nextOutboundMsgSeq = 1;

  constructor(
    private callbacks: {
      onSubmitMessage: (text: string) => void;
      onError?: (msg: string) => void;
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
      const existing = Number(readFileSync(QQ_LOCK_FILE, "utf8").trim());
      if (Number.isInteger(existing) && existing > 0 && existing !== process.pid) {
        try {
          process.kill(existing, 0);
          throw new Error(
            `QQ channel is already running in process ${existing}. Stop that process before starting another QQ channel.`,
          );
        } catch (err) {
          const e = err as NodeJS.ErrnoException;
          if (e.code !== "ESRCH") throw err;
        }
      }
    } catch (err) {
      const e = err as NodeJS.ErrnoException;
      if (e.code !== "ENOENT") throw err;
    }

    mkdirSync(dirname(QQ_LOCK_FILE), { recursive: true });
    writeFileSync(QQ_LOCK_FILE, String(process.pid), "utf8");
    this.lockAcquired = true;
  }

  private releaseLock(): void {
    if (!this.lockAcquired) return;
    try {
      const existing = Number(readFileSync(QQ_LOCK_FILE, "utf8").trim());
      if (existing === process.pid) unlinkSync(QQ_LOCK_FILE);
    } catch {}
    this.lockAcquired = false;
  }

  async start(): Promise<void> {
    loadDotenv();
    this.acquireLock();

    const config = loadQQConfig();
    if (!config.appId) {
      this.releaseLock();
      throw new Error("QQ App ID is required. Run `/qq connect` to configure.");
    }
    if (!config.appSecret) {
      this.releaseLock();
      throw new Error("QQ App Secret is required. Run `/qq connect` to configure.");
    }

    const bot = new QQBot({
      appid: config.appId,
      secret: config.appSecret,
      sandbox: config.sandbox ?? false,
    });

    bot.on("online", () => {
      process.stderr.write("QQ bot is online!\n");
    });

    bot.on("bot_error", (msg: string) => {
      this.callbacks.onError?.(msg);
    });

    bot.on("message.private", (msg: C2CMessage) => {
      const text = msg.content?.trim();
      if (!text) return;
      if (!this.rememberMessage(msg.id)) return;
      this.qqUserId = msg.author.user_openid;
      this.qqMessageId = msg.id;
      this.callbacks.onSubmitMessage(`[QQ] ${text}`);
    });

    this.bot = bot;

    try {
      await bot.start();

      const readyOrError = await Promise.race([
        new Promise<"ready">((resolve) => bot.once("online", () => resolve("ready"))),
        new Promise<"error">((resolve) => bot.once("bot_error", () => resolve("error"))),
        new Promise<"timeout">((resolve) => setTimeout(() => resolve("timeout"), 15_000)),
      ]);

      if (readyOrError === "error") {
        throw new Error("QQ bot authentication failed - check your appId and appSecret");
      }
      if (readyOrError === "timeout") {
        throw new Error("QQ bot did not receive READY within 15s - check your appId and appSecret");
      }
    } catch (err) {
      this.releaseLock();
      throw err;
    }
  }

  async sendResponse(text: string): Promise<void> {
    if (!this.bot || !this.qqUserId) return;
    const chunks = splitQQMessage(text);
    for (let index = 0; index < chunks.length; index++) {
      const chunk = chunks[index];
      if (!chunk) continue;
      try {
        await this.bot.sendPrivateMessage(
          this.qqUserId,
          chunk,
          this.qqMessageId ?? undefined,
          this.nextOutboundMsgSeq++,
        );
      } catch (err) {
        const msg = `QQ sendResponse chunk ${index + 1}/${chunks.length} failed: ${(err as Error).message}`;
        this.callbacks.onError?.(msg);
        break;
      }
    }
  }

  async stop(): Promise<void> {
    await this.bot?.stop();
    this.releaseLock();
  }
}
