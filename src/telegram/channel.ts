import { mkdirSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { SLASH_COMMANDS, orderSlashCommandsByGroup } from "../cli/ui/slash/commands.js";
import { loadTelegramConfig } from "../config.js";
import { loadDotenv } from "../env.js";
import { t } from "../i18n/index.js";
import { decideTelegramAccess, describeTelegramAccess, redactTelegramUserId } from "./access.js";
import {
  TelegramBot,
  type TelegramBotCommand,
  type TelegramCallbackQuery,
  type TelegramInlineButton,
  type TelegramMessage,
} from "./bot.js";

const TELEGRAM_LOCK_FILE = join(homedir(), ".reasonix", "telegram-channel.pid");
const TELEGRAM_MAX_CHARS = 3900;
const NATURAL_SPLIT_MIN_FRACTION = 0.6;
const TELEGRAM_MARKDOWN_WRAPPER_RE = /^```(?:markdown|md)\s*\r?\n([\s\S]*?)\r?\n```$/i;
const TELEGRAM_MARKDOWN_V2_SPECIAL_RE = /([_*\[\]()~`>#+\-=|{}.!])/g;
const TELEGRAM_COMMAND_RE = /^[a-z0-9_]{1,32}$/;
const TELEGRAM_COMMAND_DESCRIPTION_MAX = 256;
const TELEGRAM_RATE_LIMIT_WINDOW_MS = 30_000;
const TELEGRAM_RATE_LIMIT_MAX_MESSAGES = 5;
const TELEGRAM_RATE_LIMIT_NOTICE_COOLDOWN_MS = 10_000;

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

function toTelegramCommandName(command: string): string | null {
  const normalized = command.toLowerCase().replace(/-/g, "_");
  return TELEGRAM_COMMAND_RE.test(normalized) ? normalized : null;
}

function toTelegramCommandDescription(summary: string): string {
  const normalized = summary.replace(/\s+/g, " ").trim();
  if (normalized.length <= TELEGRAM_COMMAND_DESCRIPTION_MAX) return normalized;
  return normalized.slice(0, TELEGRAM_COMMAND_DESCRIPTION_MAX - 1).trimEnd();
}

export function buildTelegramBotCommands(): TelegramBotCommand[] {
  const seen = new Set<string>();
  const commands: TelegramBotCommand[] = [];
  for (const spec of orderSlashCommandsByGroup(SLASH_COMMANDS)) {
    const command = toTelegramCommandName(spec.cmd);
    if (!command || seen.has(command)) continue;
    const description = toTelegramCommandDescription(spec.summary);
    if (!description) continue;
    seen.add(command);
    commands.push({ command, description });
  }
  return commands;
}

export function normalizeTelegramMarkdownReply(text: string): string {
  const trimmed = text.trim();
  const match = trimmed.match(TELEGRAM_MARKDOWN_WRAPPER_RE);
  if (!match) {
    return text;
  }
  return match[1] ?? text;
}

function escapeTelegramMarkdownV2(text: string): string {
  return text.replace(/\\/g, "\\\\").replace(TELEGRAM_MARKDOWN_V2_SPECIAL_RE, "\\$1");
}

function escapeTelegramMarkdownV2Code(text: string): string {
  return text.replace(/\\/g, "\\\\").replace(/`/g, "\\`");
}

function stripMarkdownEmphasis(text: string): string {
  return text.replace(/\*\*([^*\n]+)\*\*/g, "$1").replace(/__([^_\n]+)__/g, "$1");
}

function formatTelegramMarkdownV2Inline(text: string): string {
  let formatted = "";
  let index = 0;
  while (index < text.length) {
    if (text.startsWith("`", index)) {
      const end = text.indexOf("`", index + 1);
      if (end > index) {
        formatted += `\`${escapeTelegramMarkdownV2Code(text.slice(index + 1, end))}\``;
        index = end + 1;
        continue;
      }
    }

    if (text.startsWith("**", index)) {
      const end = text.indexOf("**", index + 2);
      if (end > index + 2) {
        formatted += `*${escapeTelegramMarkdownV2(text.slice(index + 2, end))}*`;
        index = end + 2;
        continue;
      }
    }

    if (text.startsWith("__", index)) {
      const end = text.indexOf("__", index + 2);
      if (end > index + 2) {
        formatted += `*${escapeTelegramMarkdownV2(text.slice(index + 2, end))}*`;
        index = end + 2;
        continue;
      }
    }

    formatted += escapeTelegramMarkdownV2(text[index] ?? "");
    index++;
  }
  return formatted;
}

function isMarkdownTableSeparator(line: string): boolean {
  const cells = line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((cell) => cell.trim());
  return cells.length > 1 && cells.every((cell) => /^:?-{3,}:?$/.test(cell));
}

function parseMarkdownTableRow(line: string): string[] {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((cell) => cell.trim());
}

function formatTelegramMarkdownV2Table(
  lines: string[],
  start: number,
): { text: string; next: number } {
  const headers = parseMarkdownTableRow(lines[start] ?? "");
  let index = start + 2;
  const rows: string[] = [];
  while (index < lines.length) {
    const line = lines[index] ?? "";
    if (!line.includes("|") || isMarkdownTableSeparator(line)) break;
    const cells = parseMarkdownTableRow(line);
    if (cells.length < 2) break;
    for (let cellIndex = 0; cellIndex < Math.min(headers.length, cells.length); cellIndex++) {
      const header = stripMarkdownEmphasis(headers[cellIndex] ?? "");
      const cell = cells[cellIndex] ?? "";
      if (!header && !cell) continue;
      rows.push(`• *${escapeTelegramMarkdownV2(header)}*: ${formatTelegramMarkdownV2Inline(cell)}`);
    }
    rows.push("");
    index++;
  }
  while (rows.at(-1) === "") rows.pop();
  return { text: rows.join("\n"), next: index };
}

export function formatTelegramMarkdownV2(text: string): string {
  const lines = normalizeTelegramMarkdownReply(text).trim().split(/\r?\n/);
  const formatted: string[] = [];
  let inFence = false;
  let fenceLang = "";

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index] ?? "";
    const fenceMatch = line.match(/^```([A-Za-z0-9_-]*)\s*$/);
    if (fenceMatch) {
      if (inFence) {
        formatted.push("```");
        inFence = false;
        fenceLang = "";
      } else {
        inFence = true;
        fenceLang = fenceMatch[1] ?? "";
        formatted.push(`\`\`\`${escapeTelegramMarkdownV2Code(fenceLang)}`);
      }
      continue;
    }

    if (inFence) {
      formatted.push(escapeTelegramMarkdownV2Code(line));
      continue;
    }

    if (
      line.includes("|") &&
      index + 1 < lines.length &&
      isMarkdownTableSeparator(lines[index + 1] ?? "")
    ) {
      const table = formatTelegramMarkdownV2Table(lines, index);
      if (table.text) formatted.push(table.text);
      index = table.next - 1;
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      formatted.push(`*${escapeTelegramMarkdownV2(stripMarkdownEmphasis(heading[2] ?? ""))}*`);
      continue;
    }

    if (/^\s*---+\s*$/.test(line)) {
      formatted.push("────────");
      continue;
    }

    formatted.push(formatTelegramMarkdownV2Inline(line));
  }

  if (inFence) formatted.push("```");
  return formatted.join("\n");
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
  private userMessageTimestamps = new Map<string, number[]>();
  private rateLimitNoticeAt = new Map<string, number>();
  private lockAcquired = false;
  private markdownDisabled = false;

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

  private acceptRemoteInput(userId: string): boolean {
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
      return false;
    }
    if (verdict.bindRuntime) {
      this.runtimeBoundUserId = userId;
      this.callbacks.onError?.(
        t("handlers.telegram.runtimeBound", {
          userId: redactTelegramUserId(userId),
        }),
      );
    }
    return true;
  }

  private acceptRateLimit(userId: string, chatId: number, messageId: number): boolean {
    const now = Date.now();
    const since = now - TELEGRAM_RATE_LIMIT_WINDOW_MS;
    const timestamps = (this.userMessageTimestamps.get(userId) ?? []).filter((at) => at > since);
    if (timestamps.length >= TELEGRAM_RATE_LIMIT_MAX_MESSAGES) {
      this.userMessageTimestamps.set(userId, timestamps);
      const lastNoticeAt = this.rateLimitNoticeAt.get(userId) ?? 0;
      if (now - lastNoticeAt >= TELEGRAM_RATE_LIMIT_NOTICE_COOLDOWN_MS) {
        this.rateLimitNoticeAt.set(userId, now);
        this.callbacks.onError?.(
          t("handlers.telegram.rateLimited", {
            userId: redactTelegramUserId(userId),
            seconds: Math.ceil(TELEGRAM_RATE_LIMIT_WINDOW_MS / 1000),
          }),
        );
        void this.bot
          ?.sendMessage(
            chatId,
            t("handlers.telegram.rateLimitedReply", {
              seconds: Math.ceil(TELEGRAM_RATE_LIMIT_WINDOW_MS / 1000),
            }),
            messageId,
          )
          .catch((err) => {
            this.callbacks.onError?.(
              `Telegram rate-limit notice failed: ${(err as Error).message}`,
            );
          });
      }
      return false;
    }

    timestamps.push(now);
    this.userMessageTimestamps.set(userId, timestamps);
    return true;
  }

  private handleMessage(msg: TelegramMessage): void {
    const text = msg.text?.trim();
    if (!text || msg.from?.is_bot) return;
    const fromId = msg.from?.id;
    if (typeof fromId !== "number") return;
    if (!this.rememberMessage(`${msg.chat.id}:${msg.message_id}`)) return;

    const userId = String(fromId);
    if (!this.acceptRemoteInput(userId)) return;
    if (!this.acceptRateLimit(userId, msg.chat.id, msg.message_id)) return;

    this.chatId = msg.chat.id;
    this.messageId = msg.message_id;
    this.callbacks.onSubmitMessage(`[TG] ${text}`);
  }

  private handleCallbackQuery(query: TelegramCallbackQuery): void {
    const text = query.data.trim();
    if (!text || query.from.is_bot || !query.message) return;
    if (!this.rememberMessage(`callback:${query.id}`)) return;

    const userId = String(query.from.id);
    if (!this.acceptRemoteInput(userId)) return;

    this.chatId = query.message.chat.id;
    this.messageId = query.message.message_id;
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
    bot.on("callback_query", (query: TelegramCallbackQuery) => {
      this.handleCallbackQuery(query);
    });

    this.bot = bot;
    try {
      await bot.setCommands(buildTelegramBotCommands());
      await bot.start();
    } catch (err) {
      this.releaseLock();
      throw err;
    }
  }

  async sendResponse(text: string, buttons?: TelegramInlineButton[][]): Promise<void> {
    if (!this.bot || this.chatId === null) return;
    const markdownText = formatTelegramMarkdownV2(text);
    const chunks = splitTelegramMessage(markdownText);
    for (let index = 0; index < chunks.length; index++) {
      const chunk = chunks[index];
      if (!chunk) continue;
      try {
        if (!this.markdownDisabled) {
          try {
            await this.bot.sendMessage(
              this.chatId,
              chunk,
              this.messageId ?? undefined,
              "MarkdownV2",
              index === chunks.length - 1 ? buttons : undefined,
            );
            continue;
          } catch (err) {
            this.markdownDisabled = true;
            this.callbacks.onError?.(
              `Telegram markdown delivery disabled after first failure: ${(err as Error).message}`,
            );
          }
        }

        await this.bot.sendMessage(
          this.chatId,
          chunk,
          this.messageId ?? undefined,
          undefined,
          index === chunks.length - 1 ? buttons : undefined,
        );
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
