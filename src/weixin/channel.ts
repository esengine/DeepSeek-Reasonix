import { existsSync, mkdirSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { loadWeixinConfig } from "../config.js";
import { atomicWriteSync } from "../core/atomic-write.js";
import { loadDotenv } from "../env.js";
import { t } from "../i18n/index.js";
import { decideWeixinAccess, describeWeixinAccess, redactWeixinUserId } from "./access.js";
import { WeixinBot, type WeixinInboundMessage } from "./bot.js";

const WEIXIN_LOCK_FILE = join(homedir(), ".reasonix", "weixin-channel.pid");
const WEIXIN_STATE_DIR = join(homedir(), ".reasonix", "weixin");
const WEIXIN_MAX_CHARS = 2000;
const NATURAL_SPLIT_MIN_FRACTION = 0.6;
const WEIXIN_RATE_LIMIT_WINDOW_MS = 30_000;
const WEIXIN_RATE_LIMIT_MAX_MESSAGES = 5;
const WEIXIN_RATE_LIMIT_NOTICE_COOLDOWN_MS = 10_000;
const WEIXIN_MARKDOWN_WRAPPER_RE = /^```(?:markdown|md)\s*\r?\n([\s\S]*?)\r?\n```$/i;

function pickNaturalSplit(candidate: string): number {
  const minSplit = Math.floor(candidate.length * NATURAL_SPLIT_MIN_FRACTION);
  const splitters = ["\n\n", "\n", " "];
  for (const splitter of splitters) {
    const at = candidate.lastIndexOf(splitter);
    if (at >= minSplit) return at + splitter.length;
  }
  return candidate.length;
}

export function splitWeixinMessage(text: string, maxChars = WEIXIN_MAX_CHARS): string[] {
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

export function normalizeWeixinMarkdownReply(text: string): string {
  const trimmed = text.trim();
  const match = trimmed.match(WEIXIN_MARKDOWN_WRAPPER_RE);
  if (!match) return text;
  return match[1] ?? text;
}

function rewriteMarkdownForWeixin(text: string): string {
  const lines = normalizeWeixinMarkdownReply(text).trim().split(/\r?\n/);
  const formatted: string[] = [];
  let inFence = false;

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index] ?? "";
    if (/^```/.test(line.trim())) {
      inFence = !inFence;
      formatted.push(line);
      continue;
    }
    if (inFence) {
      formatted.push(line);
      continue;
    }
    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      formatted.push(heading[1]?.length === 1 ? `【${heading[2]}】` : `**${heading[2]}**`);
      continue;
    }
    if (
      line.includes("|") &&
      index + 1 < lines.length &&
      /^\s*\|?(?:\s*:?-{3,}:?\s*\|)+\s*:?-{3,}:?\s*\|?\s*$/.test(lines[index + 1] ?? "")
    ) {
      const headers = line
        .trim()
        .replace(/^\|/, "")
        .replace(/\|$/, "")
        .split("|")
        .map((cell) => cell.trim());
      index += 2;
      while (index < lines.length && (lines[index] ?? "").includes("|")) {
        const cells = (lines[index] ?? "")
          .trim()
          .replace(/^\|/, "")
          .replace(/\|$/, "")
          .split("|")
          .map((cell) => cell.trim());
        for (let cellIndex = 0; cellIndex < Math.min(headers.length, cells.length); cellIndex++) {
          const header = headers[cellIndex] ?? `Column ${cellIndex + 1}`;
          const cell = cells[cellIndex] ?? "";
          if (cell) formatted.push(`- ${header}: ${cell}`);
        }
        index++;
      }
      index--;
      continue;
    }
    formatted.push(line);
  }

  return formatted.join("\n");
}

function stateFile(accountId: string, suffix: string): string {
  return join(WEIXIN_STATE_DIR, `${accountId}.${suffix}.json`);
}

function readJsonRecord(path: string): Record<string, string> {
  if (!existsSync(path)) return {};
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8"));
    return parsed && typeof parsed === "object" ? (parsed as Record<string, string>) : {};
  } catch {
    return {};
  }
}

export class WeixinChannel {
  private bot: WeixinBot | null = null;
  private accountId = "";
  private userId: string | null = null;
  private ownerUserId: string | undefined;
  private allowlist: string[] | undefined;
  private runtimeBoundUserId: string | null = null;
  private processedMessageIds = new Set<string>();
  private processedMessageIdQueue: string[] = [];
  private userMessageTimestamps = new Map<string, number[]>();
  private rateLimitNoticeAt = new Map<string, number>();
  private lockAcquired = false;

  constructor(
    private callbacks: {
      onSubmitMessage: (text: string) => void;
      onError?: (msg: string) => void;
      onInfo?: (msg: string) => void;
    },
  ) {}

  private rememberMessage(id: string): boolean {
    if (this.processedMessageIds.has(id)) return false;
    this.processedMessageIds.add(id);
    this.processedMessageIdQueue.push(id);
    if (this.processedMessageIdQueue.length > 200) {
      const oldest = this.processedMessageIdQueue.shift();
      if (oldest) this.processedMessageIds.delete(oldest);
    }
    return true;
  }

  private acquireLock(): void {
    try {
      const existing = Number(readFileSync(WEIXIN_LOCK_FILE, "utf8").trim());
      if (Number.isInteger(existing) && existing > 0 && existing !== process.pid) {
        try {
          process.kill(existing, 0);
          throw new Error(t("handlers.weixin.lockAlreadyRunning", { pid: existing }));
        } catch (err) {
          const e = err as NodeJS.ErrnoException;
          if (e.code !== "ESRCH") throw err;
        }
      }
    } catch (err) {
      const e = err as NodeJS.ErrnoException;
      if (e.code !== "ENOENT") throw err;
    }

    mkdirSync(dirname(WEIXIN_LOCK_FILE), { recursive: true });
    writeFileSync(WEIXIN_LOCK_FILE, String(process.pid), "utf8");
    this.lockAcquired = true;
  }

  private releaseLock(): void {
    if (!this.lockAcquired) return;
    try {
      const existing = Number(readFileSync(WEIXIN_LOCK_FILE, "utf8").trim());
      if (existing === process.pid) unlinkSync(WEIXIN_LOCK_FILE);
    } catch {}
    this.lockAcquired = false;
  }

  private applyAccessConfig(config: ReturnType<typeof loadWeixinConfig>): void {
    this.ownerUserId = config.ownerUserId;
    this.allowlist = config.allowlist;
    if (this.ownerUserId || (this.allowlist?.length ?? 0) > 0) {
      this.runtimeBoundUserId = null;
    }
  }

  private hasConfiguredAccess(): boolean {
    return !!this.ownerUserId || (this.allowlist?.length ?? 0) > 0;
  }

  private acceptRemoteInput(userId: string): boolean {
    const verdict = decideWeixinAccess(
      {
        ownerUserId: this.ownerUserId,
        allowlist: this.allowlist,
        runtimeBoundUserId: this.runtimeBoundUserId,
      },
      userId,
    );
    if (!verdict.accept) {
      this.callbacks.onError?.(
        t("handlers.weixin.unauthorizedMessage", {
          userId: redactWeixinUserId(userId),
          access: this.describeAccess(),
        }),
      );
      return false;
    }
    if (verdict.bindRuntime) {
      this.runtimeBoundUserId = userId;
      this.callbacks.onInfo?.(
        t("handlers.weixin.runtimeBound", {
          userId: redactWeixinUserId(userId),
        }),
      );
    }
    return true;
  }

  private acceptRateLimit(userId: string): boolean {
    const now = Date.now();
    const since = now - WEIXIN_RATE_LIMIT_WINDOW_MS;
    const timestamps = (this.userMessageTimestamps.get(userId) ?? []).filter((at) => at > since);
    if (timestamps.length >= WEIXIN_RATE_LIMIT_MAX_MESSAGES) {
      this.userMessageTimestamps.set(userId, timestamps);
      const lastNoticeAt = this.rateLimitNoticeAt.get(userId) ?? 0;
      if (now - lastNoticeAt >= WEIXIN_RATE_LIMIT_NOTICE_COOLDOWN_MS) {
        this.rateLimitNoticeAt.set(userId, now);
        this.callbacks.onError?.(
          t("handlers.weixin.rateLimited", {
            userId: redactWeixinUserId(userId),
            seconds: Math.ceil(WEIXIN_RATE_LIMIT_WINDOW_MS / 1000),
          }),
        );
      }
      return false;
    }
    timestamps.push(now);
    this.userMessageTimestamps.set(userId, timestamps);
    return true;
  }

  private handleMessage(msg: WeixinInboundMessage): void {
    const text = msg.text.trim();
    if (!text) return;
    if (!this.rememberMessage(msg.messageId)) return;
    if (!this.acceptRemoteInput(msg.fromUserId)) return;
    if (!this.acceptRateLimit(msg.fromUserId)) return;

    this.userId = msg.fromUserId;
    this.callbacks.onSubmitMessage(`[WX] ${text}`);
  }

  private contextPath(): string {
    return stateFile(this.accountId, "context-tokens");
  }

  private syncPath(): string {
    return stateFile(this.accountId, "sync");
  }

  private loadContextToken(userId: string): string | undefined {
    return readJsonRecord(this.contextPath())[userId];
  }

  private saveContextToken(userId: string, token: string): void {
    const records = readJsonRecord(this.contextPath());
    records[userId] = token;
    const path = this.contextPath();
    atomicWriteSync(path, JSON.stringify(records, null, 2), `${path}.tmp`);
  }

  private loadSyncBuf(): string {
    return readJsonRecord(this.syncPath()).get_updates_buf ?? "";
  }

  private saveSyncBuf(syncBuf: string): void {
    const path = this.syncPath();
    atomicWriteSync(path, JSON.stringify({ get_updates_buf: syncBuf }, null, 2), `${path}.tmp`);
  }

  refreshAccessConfig(): void {
    this.applyAccessConfig(loadWeixinConfig());
  }

  describeAccess(): string {
    return describeWeixinAccess({
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
    mkdirSync(WEIXIN_STATE_DIR, { recursive: true });

    const config = loadWeixinConfig();
    if (!config.token) {
      this.releaseLock();
      throw new Error(t("handlers.weixin.missingToken"));
    }
    if (!config.accountId) {
      this.releaseLock();
      throw new Error(t("handlers.weixin.missingAccountId"));
    }
    this.applyAccessConfig(config);
    if (!this.hasConfiguredAccess()) {
      this.releaseLock();
      throw new Error(t("handlers.weixin.accessRequired"));
    }

    this.accountId = config.accountId;
    const bot = new WeixinBot({
      token: config.token,
      accountId: config.accountId,
      baseUrl: config.baseUrl,
      initialSyncBuf: this.loadSyncBuf(),
      loadContextToken: (userId) => this.loadContextToken(userId),
      saveContextToken: (userId, token) => this.saveContextToken(userId, token),
      saveSyncBuf: (syncBuf) => this.saveSyncBuf(syncBuf),
    });
    bot.on("online", () => {
      process.stderr.write("Weixin channel is online!\n");
    });
    bot.on("bot_error", (msg: string) => {
      this.callbacks.onError?.(msg);
    });
    bot.on("message", (msg: WeixinInboundMessage) => {
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
    if (!this.bot || !this.userId) return;
    const chunks = splitWeixinMessage(rewriteMarkdownForWeixin(text));
    for (let index = 0; index < chunks.length; index++) {
      const chunk = chunks[index];
      if (!chunk) continue;
      try {
        await this.bot.sendMessage(this.userId, chunk);
      } catch (err) {
        this.callbacks.onError?.(
          `Weixin sendResponse chunk ${index + 1}/${chunks.length} failed: ${(err as Error).message}`,
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
