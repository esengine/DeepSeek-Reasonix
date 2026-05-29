import { EventEmitter } from "node:events";
import * as QRCode from "qrcode";

const ILINK_BASE_URL = "https://ilinkai.weixin.qq.com";
const CHANNEL_VERSION = "2.2.0";
const ILINK_APP_CLIENT_VERSION = (2 << 16) | (2 << 8) | 0;
const LONG_POLL_TIMEOUT_MS = 35_000;
const API_TIMEOUT_MS = 15_000;
const QR_TIMEOUT_MS = 35_000;
const SESSION_EXPIRED_ERRCODE = -14;
const RATE_LIMIT_ERRCODE = -2;
const ILINK_HOST_SUFFIX = ".weixin.qq.com";

interface WeixinBotConfig {
  token: string;
  accountId: string;
  baseUrl?: string;
  initialSyncBuf?: string;
  loadContextToken?: (userId: string) => string | undefined;
  saveContextToken?: (userId: string, token: string) => void;
  saveSyncBuf?: (syncBuf: string) => void;
}

export interface WeixinMessage {
  message_id?: string;
  from_user_id: string;
  to_user_id?: string;
  context_token?: string;
  item_list?: Array<{
    type?: number;
    text_item?: { text?: string };
    voice_item?: { text?: string };
  }>;
}

export interface WeixinInboundMessage {
  messageId: string;
  fromUserId: string;
  text: string;
}

export interface WeixinQrLoginResult {
  accountId: string;
  token: string;
  baseUrl: string;
  userId?: string;
}

function jsonBody(payload: Record<string, unknown>): string {
  return JSON.stringify(
    { ...payload, base_info: { channel_version: CHANNEL_VERSION } },
    undefined,
    0,
  );
}

function randomWechatUin(): string {
  return Buffer.from(String(Math.trunc(Math.random() * 0xffffffff))).toString("base64");
}

function normalizeIlinkBaseUrl(value: string | undefined): string {
  const url = new URL(value || ILINK_BASE_URL);
  if (url.protocol !== "https:" || !url.hostname.endsWith(ILINK_HOST_SUFFIX)) {
    throw new Error("Weixin iLink baseUrl must be an HTTPS *.weixin.qq.com endpoint.");
  }
  return url.origin;
}

function describeError(err: unknown): string {
  if (!(err instanceof Error)) return String(err);
  const cause = err.cause;
  if (cause instanceof Error && cause.message) return `${err.message}: ${cause.message}`;
  if (cause && typeof cause === "object") {
    const record = cause as Record<string, unknown>;
    const code = typeof record.code === "string" ? record.code : undefined;
    const message = typeof record.message === "string" ? record.message : undefined;
    if (code && message) return `${err.message}: ${code} ${message}`;
    if (code) return `${err.message}: ${code}`;
    if (message) return `${err.message}: ${message}`;
  }
  return err.message;
}

function headers(token: string | undefined): Record<string, string> {
  const result: Record<string, string> = {
    "Content-Type": "application/json",
    AuthorizationType: "ilink_bot_token",
    "X-WECHAT-UIN": randomWechatUin(),
    "iLink-App-Id": "bot",
    "iLink-App-ClientVersion": String(ILINK_APP_CLIENT_VERSION),
  };
  if (token) result.Authorization = `Bearer ${token}`;
  return result;
}

async function renderTerminalQr(data: string): Promise<string | null> {
  try {
    return await QRCode.toString(data, {
      type: "utf8",
      errorCorrectionLevel: "medium",
    });
  } catch {
    return null;
  }
}

async function getIlink(
  baseUrl: string,
  endpoint: string,
  timeoutMs: number,
): Promise<Record<string, unknown>> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(`${baseUrl.replace(/\/+$/, "")}/${endpoint}`, {
      headers: {
        "iLink-App-Id": "bot",
        "iLink-App-ClientVersion": String(ILINK_APP_CLIENT_VERSION),
      },
      signal: controller.signal,
    });
    const text = await res.text();
    if (!res.ok) throw new Error(`iLink ${endpoint} failed (${res.status}): ${text}`);
    return JSON.parse(text) as Record<string, unknown>;
  } finally {
    clearTimeout(timeout);
  }
}

export async function runWeixinQrLogin({
  botType = "3",
  timeoutSeconds = 480,
  onInfo,
}: {
  botType?: string;
  timeoutSeconds?: number;
  onInfo?: (message: string) => void;
} = {}): Promise<WeixinQrLoginResult> {
  let qrResponse = await getIlink(
    ILINK_BASE_URL,
    `ilink/bot/get_bot_qrcode?bot_type=${encodeURIComponent(botType)}`,
    QR_TIMEOUT_MS,
  );
  let qrcode = String(qrResponse.qrcode ?? "").trim();
  let qrcodeUrl = String(qrResponse.qrcode_img_content ?? "").trim();
  if (!qrcode) throw new Error("Weixin QR login did not return a qrcode.");
  const qrScanData = qrcodeUrl || qrcode;
  const terminalQr = await renderTerminalQr(qrScanData);
  onInfo?.(
    terminalQr
      ? `Weixin QR login: scan this QR with WeChat:\n${terminalQr}\n${qrScanData}`
      : `Weixin QR login: scan this URL with WeChat:\n${qrScanData}`,
  );

  const deadline = Date.now() + timeoutSeconds * 1000;
  let baseUrl = ILINK_BASE_URL;
  let refreshCount = 0;
  let scannedNoticeShown = false;
  while (Date.now() < deadline) {
    const statusResponse = await getIlink(
      baseUrl,
      `ilink/bot/get_qrcode_status?qrcode=${encodeURIComponent(qrcode)}`,
      QR_TIMEOUT_MS,
    ).catch((err) => {
      onInfo?.(`Weixin QR poll retrying: ${describeError(err)}`);
      return null;
    });
    if (!statusResponse) {
      await new Promise((resolve) => setTimeout(resolve, 1000));
      continue;
    }

    const status = String(statusResponse.status ?? "wait");
    if (status === "scaned" && !scannedNoticeShown) {
      scannedNoticeShown = true;
      onInfo?.("Weixin QR scanned. Confirm the login in WeChat.");
    } else if (status === "scaned_but_redirect") {
      const redirectHost = String(statusResponse.redirect_host ?? "").trim();
      if (redirectHost) baseUrl = `https://${redirectHost}`;
    } else if (status === "expired") {
      refreshCount++;
      if (refreshCount > 3) throw new Error("Weixin QR code expired too many times.");
      qrResponse = await getIlink(
        ILINK_BASE_URL,
        `ilink/bot/get_bot_qrcode?bot_type=${encodeURIComponent(botType)}`,
        QR_TIMEOUT_MS,
      );
      qrcode = String(qrResponse.qrcode ?? "").trim();
      qrcodeUrl = String(qrResponse.qrcode_img_content ?? "").trim();
      if (!qrcode) throw new Error("Weixin QR refresh did not return a qrcode.");
      const refreshedScanData = qrcodeUrl || qrcode;
      const refreshedTerminalQr = await renderTerminalQr(refreshedScanData);
      onInfo?.(
        refreshedTerminalQr
          ? `Weixin QR refreshed (${refreshCount}/3). Scan this QR:\n${refreshedTerminalQr}\n${refreshedScanData}`
          : `Weixin QR refreshed (${refreshCount}/3). Scan this URL:\n${refreshedScanData}`,
      );
    } else if (status === "confirmed") {
      const accountId = String(statusResponse.ilink_bot_id ?? "").trim();
      const token = String(statusResponse.bot_token ?? "").trim();
      const confirmedBaseUrl = String(statusResponse.baseurl ?? baseUrl).trim() || baseUrl;
      const userId = String(statusResponse.ilink_user_id ?? "").trim();
      if (!accountId || !token) {
        throw new Error("Weixin QR login was confirmed but returned incomplete credentials.");
      }
      return { accountId, token, baseUrl: confirmedBaseUrl, userId: userId || undefined };
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }

  throw new Error("Weixin QR login timed out.");
}

function isSessionExpired(ret: unknown, errcode: unknown, errmsg: unknown): boolean {
  return (
    ret === SESSION_EXPIRED_ERRCODE ||
    errcode === SESSION_EXPIRED_ERRCODE ||
    ((ret === RATE_LIMIT_ERRCODE || errcode === RATE_LIMIT_ERRCODE) &&
      String(errmsg ?? "").toLowerCase() === "unknown error")
  );
}

function extractText(items: WeixinMessage["item_list"]): string {
  for (const item of items ?? []) {
    if (item.type === 1) {
      const text = item.text_item?.text?.trim();
      if (text) return text;
    }
  }
  for (const item of items ?? []) {
    if (item.type === 4) {
      const text = item.voice_item?.text?.trim();
      if (text) return text;
    }
  }
  return "";
}

export class WeixinBot extends EventEmitter {
  private readonly token: string;
  private readonly accountId: string;
  private readonly baseUrl: string;
  private syncBuf: string;
  private stopped = true;
  private pollAbort: AbortController | null = null;

  constructor(private config: WeixinBotConfig) {
    super();
    this.token = config.token;
    this.accountId = config.accountId;
    this.baseUrl = normalizeIlinkBaseUrl(config.baseUrl);
    this.syncBuf = config.initialSyncBuf ?? "";
  }

  private async post(
    endpoint: string,
    payload: Record<string, unknown>,
    timeoutMs: number,
    abortController?: AbortController,
  ) {
    const body = jsonBody(payload);
    const controller = abortController ?? new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    try {
      // codeql[js/file-access-to-http] iLink bot tokens are only sent to validated Weixin endpoints.
      const res = await fetch(`${this.baseUrl}/${endpoint}`, {
        method: "POST",
        headers: headers(this.token),
        body,
        signal: controller.signal,
      });
      const text = await res.text();
      if (!res.ok) throw new Error(`iLink ${endpoint} failed (${res.status}): ${text}`);
      return JSON.parse(text) as Record<string, unknown>;
    } finally {
      clearTimeout(timeout);
    }
  }

  private async getUpdates(): Promise<Record<string, unknown>> {
    this.pollAbort = new AbortController();
    try {
      return await this.post(
        "ilink/bot/getupdates",
        { get_updates_buf: this.syncBuf },
        LONG_POLL_TIMEOUT_MS + 5_000,
        this.pollAbort,
      );
    } catch (err) {
      if ((err as Error).name === "AbortError") {
        return { ret: 0, msgs: [], get_updates_buf: this.syncBuf };
      }
      throw err;
    } finally {
      this.pollAbort = null;
    }
  }

  private rememberContext(message: WeixinMessage): void {
    const token = message.context_token?.trim();
    if (token) this.config.saveContextToken?.(message.from_user_id, token);
  }

  private emitMessage(message: WeixinMessage): void {
    const fromUserId = message.from_user_id?.trim();
    if (!fromUserId || fromUserId === this.accountId) return;
    const text = extractText(message.item_list);
    if (!text) return;
    this.rememberContext(message);
    this.emit("message", {
      messageId: message.message_id || `${fromUserId}:${Date.now()}`,
      fromUserId,
      text,
    } satisfies WeixinInboundMessage);
  }

  private async pollLoop(): Promise<void> {
    let failures = 0;
    while (!this.stopped) {
      try {
        const response = await this.getUpdates();
        const ret = response.ret;
        const errcode = response.errcode;
        if ((ret !== undefined && ret !== 0) || (errcode !== undefined && errcode !== 0)) {
          if (isSessionExpired(ret, errcode, response.errmsg)) {
            this.emit("bot_error", "Weixin session expired. Reconnect the channel.");
            await new Promise((resolve) => setTimeout(resolve, 30_000));
            continue;
          }
          failures++;
          this.emit("bot_error", `Weixin getUpdates failed: ${JSON.stringify(response)}`);
          await new Promise((resolve) => setTimeout(resolve, failures >= 3 ? 30_000 : 2_000));
          if (failures >= 3) failures = 0;
          continue;
        }

        failures = 0;
        const nextSyncBuf = String(response.get_updates_buf ?? "");
        if (nextSyncBuf) {
          this.syncBuf = nextSyncBuf;
          this.config.saveSyncBuf?.(nextSyncBuf);
        }
        for (const msg of (response.msgs as WeixinMessage[] | undefined) ?? []) {
          this.emitMessage(msg);
        }
      } catch (err) {
        if (this.stopped) return;
        failures++;
        this.emit("bot_error", `Weixin polling failed: ${describeError(err)}`);
        await new Promise((resolve) => setTimeout(resolve, failures >= 3 ? 30_000 : 2_000));
        if (failures >= 3) failures = 0;
      }
    }
  }

  async start(): Promise<void> {
    this.stopped = false;
    this.emit("online");
    void this.pollLoop();
  }

  async stop(): Promise<void> {
    this.stopped = true;
    this.pollAbort?.abort();
  }

  async sendMessage(toUserId: string, text: string): Promise<void> {
    const contextToken = this.config.loadContextToken?.(toUserId);
    const clientId = `reasonix-weixin-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    const buildMessage = (token?: string) => {
      const msg: Record<string, unknown> = {
        from_user_id: "",
        to_user_id: toUserId,
        client_id: clientId,
        message_type: 2,
        message_state: 2,
        item_list: [{ type: 1, text_item: { text } }],
      };
      if (token) msg.context_token = token;
      return { msg };
    };

    const response = await this.post(
      "ilink/bot/sendmessage",
      buildMessage(contextToken),
      API_TIMEOUT_MS,
    );
    const ret = response.ret;
    const errcode = response.errcode;
    if ((ret === undefined || ret === 0) && (errcode === undefined || errcode === 0)) return;
    if (contextToken && isSessionExpired(ret, errcode, response.errmsg)) {
      const retry = await this.post("ilink/bot/sendmessage", buildMessage(), API_TIMEOUT_MS);
      const retryRet = retry.ret;
      const retryErrcode = retry.errcode;
      if (
        (retryRet === undefined || retryRet === 0) &&
        (retryErrcode === undefined || retryErrcode === 0)
      ) {
        return;
      }
      throw new Error(`Weixin sendmessage failed: ${JSON.stringify(retry)}`);
    }
    throw new Error(`Weixin sendmessage failed: ${JSON.stringify(response)}`);
  }
}
