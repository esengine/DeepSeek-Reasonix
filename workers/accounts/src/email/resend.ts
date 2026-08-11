import type { EmailMessage, Mailer } from "./types";

// Minimal Resend HTTP client. Workers can't open SMTP, so transactional mail
// goes through Resend's REST API.
export class ResendMailer implements Mailer {
  constructor(
    private readonly apiKey: string,
    private readonly from: string,
  ) {}

  async send(msg: EmailMessage): Promise<void> {
    const res = await fetch("https://api.resend.com/emails", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        from: this.from,
        to: msg.to,
        subject: msg.subject,
        text: msg.text,
        html: msg.html,
      }),
    });
    if (!res.ok) {
      const detail = await res.text().catch(() => "");
      throw new Error(`resend send failed: ${res.status} ${detail.slice(0, 300)}`);
    }
  }
}
