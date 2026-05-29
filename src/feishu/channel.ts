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
const FEISHU_MARKDOWN_WRAPPER_RE = /^```(?:markdown|md)\s*\r?\n([\s\S]*?)\r?\n```$/i;

export function normalizeFeishuMarkdownReply(text: string): string {
  const trimmed = text.trim();
  const match = trimmed.match(FEISHU_MARKDOWN_WRAPPER_RE);
  if (!match) {
    return text;
  }
  return match[1] ?? text;
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

function formatFeishuTable(lines: string[], start: number): { text: string; next: number } {
  const headers = parseMarkdownTableRow(lines[start] ?? "");
  let index = start + 2;
  const rows: string[] = [];
  while (index < lines.length) {
    const line = lines[index] ?? "";
    if (!line.includes("|") || isMarkdownTableSeparator(line)) break;
    const cells = parseMarkdownTableRow(line);
    if (cells.length < 2) break;
    for (let cellIndex = 0; cellIndex < Math.min(headers.length, cells.length); cellIndex++) {
      const header = headers[cellIndex] ?? "";
      const cell = cells[cellIndex] ?? "";
      if (!header && !cell) continue;
      rows.push(`- **${header}**：${cell}`);
    }
    rows.push("");
    index++;
  }
  while (rows.at(-1) === "") rows.pop();
  return { text: rows.join("\n"), next: index };
}

export function formatFeishuMarkdownReply(text: string): string {
  const lines = normalizeFeishuMarkdownReply(text).trim().split(/\r?\n/);
  const formatted: string[] = [];
  let inFence = false;
  let fenceLang = "";
  let fenceLines: string[] = [];

  const flushFence = () => {
    if (!inFence) return;
    const label = fenceLang ? `**代码（${fenceLang}）**` : "**代码**";
    formatted.push(label);
    if (fenceLines.length > 0) {
      formatted.push(fenceLines.join("\n"));
    }
    inFence = false;
    fenceLang = "";
    fenceLines = [];
  };

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index] ?? "";
    const fenceMatch = line.match(/^```([A-Za-z0-9_-]*)\s*$/);
    if (fenceMatch) {
      if (inFence) {
        flushFence();
      } else {
        inFence = true;
        fenceLang = fenceMatch[1] ?? "";
        fenceLines = [];
      }
      continue;
    }

    if (inFence) {
      fenceLines.push(line);
      continue;
    }

    if (
      line.includes("|") &&
      index + 1 < lines.length &&
      isMarkdownTableSeparator(lines[index + 1] ?? "")
    ) {
      const table = formatFeishuTable(lines, index);
      if (table.text) formatted.push(table.text);
      index = table.next - 1;
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      formatted.push(`**${heading[2]}**`);
      continue;
    }

    if (/^\s*---+\s*$/.test(line)) {
      formatted.push("────────");
      continue;
    }

    formatted.push(line);
  }

  flushFence();
  return formatted.join("\n");
}

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
    const formatted = formatFeishuMarkdownReply(text);
    if (!this.markdownDisabled) {
      try {
        await this.bot.sendPrivateMessage(this.chatId, formatted, true);
        return;
      } catch (err) {
        this.markdownDisabled = true;
        this.callbacks.onInfo?.(
          `Feishu markdown delivery disabled after first failure: ${(err as Error).message}`,
        );
      }
    }

    await this.bot.sendPrivateMessage(this.chatId, formatted, false);
  }

  async stop(): Promise<void> {
    await this.bot?.stop();
    this.releaseLock();
  }
}
