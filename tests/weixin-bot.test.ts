import { afterEach, describe, expect, it, vi } from "vitest";
import { WeixinBot, runWeixinQrLogin } from "../src/weixin/bot.js";

describe("Weixin iLink QR login", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("returns confirmed iLink credentials from the QR flow", async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        calls.push(url);
        if (url.includes("get_bot_qrcode")) {
          return new Response(
            JSON.stringify({
              qrcode: "qr-token",
              qrcode_img_content: "https://example.test/qr",
            }),
          );
        }
        return new Response(
          JSON.stringify({
            status: "confirmed",
            ilink_bot_id: "bot-account",
            bot_token: "bot-token",
            baseurl: "https://ilink-redirect.example.test",
            ilink_user_id: "owner-user",
          }),
        );
      }),
    );

    const info: string[] = [];
    await expect(
      runWeixinQrLogin({
        onInfo: (message) => info.push(message),
      }),
    ).resolves.toEqual({
      accountId: "bot-account",
      token: "bot-token",
      baseUrl: "https://ilink-redirect.example.test",
      userId: "owner-user",
    });
    expect(calls.some((url) => url.includes("get_bot_qrcode?bot_type=3"))).toBe(true);
    expect(calls.some((url) => url.includes("get_qrcode_status?qrcode=qr-token"))).toBe(true);
    const output = info.join("\n");
    expect(output).toContain("https://example.test/qr");
    expect(output).toContain("█▀▀▀▀▀█");
  });

  it("rejects non-Weixin iLink base URLs before sending tokens", () => {
    expect(
      () =>
        new WeixinBot({
          token: "token",
          accountId: "account",
          baseUrl: "https://example.test",
        }),
    ).toThrow("Weixin iLink baseUrl must be an HTTPS *.weixin.qq.com endpoint.");
  });
});
