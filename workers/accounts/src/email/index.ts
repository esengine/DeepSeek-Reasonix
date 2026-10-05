import type { Bindings } from "../env";
import type { EmailMessage, Mailer } from "./types";
import { ResendMailer } from "./resend";

export type { EmailMessage, Mailer } from "./types";

// Dev-time fallback: write the message (and any link inside it) to the worker
// console so flows can be exercised without configuring a mail provider.
class ConsoleMailer implements Mailer {
  constructor(private readonly from: string) {}
  async send(msg: EmailMessage): Promise<void> {
    console.log(`[email:stub] from=${this.from} to=${msg.to}\nsubject: ${msg.subject}\n${msg.text}`);
  }
}

export function buildMailer(env: Bindings): Mailer {
  if (env.EMAIL_PROVIDER?.toLowerCase() === "resend" && env.RESEND_API_KEY) {
    return new ResendMailer(env.RESEND_API_KEY, env.MAIL_FROM);
  }
  return new ConsoleMailer(env.MAIL_FROM);
}

function layout(heading: string, intro: string, ctaLabel: string, ctaUrl: string, footer: string): string {
  return `<!doctype html>
<html><body style="margin:0;background:#0b0d12;font-family:-apple-system,Segoe UI,Roboto,sans-serif;color:#e6e8ee;padding:32px">
  <div style="max-width:480px;margin:0 auto;background:#12151c;border:1px solid #242938;border-radius:14px;padding:28px">
    <h1 style="margin:0 0 12px;font-size:18px">${heading}</h1>
    <p style="margin:0 0 20px;line-height:1.6;color:#aeb4c2">${intro}</p>
    <a href="${ctaUrl}" style="display:inline-block;background:#5b8cff;color:#fff;text-decoration:none;padding:11px 18px;border-radius:9px;font-weight:600">${ctaLabel}</a>
    <p style="margin:20px 0 0;font-size:12px;color:#717892;word-break:break-all">${footer}</p>
  </div>
</body></html>`;
}

export function verifyEmail(link: string): Omit<EmailMessage, "to"> {
  return {
    subject: "Confirm your Reasonix email",
    text: `Welcome to Reasonix! Confirm your email to activate your account:\n\n${link}\n\nThis link expires in 24 hours. If you didn't sign up, ignore this email.`,
    html: layout(
      "Confirm your email",
      "Welcome to Reasonix! Confirm your email address to activate your account. This link expires in 24 hours.",
      "Confirm email",
      link,
      `Or paste this link: ${link}`,
    ),
  };
}

export function resetEmail(link: string): Omit<EmailMessage, "to"> {
  return {
    subject: "Reset your Reasonix password",
    text: `Someone requested a password reset for your Reasonix account. Set a new password here:\n\n${link}\n\nThis link expires in 1 hour. If it wasn't you, you can safely ignore this email.`,
    html: layout(
      "Reset your password",
      "Someone requested a password reset for your Reasonix account. This link expires in 1 hour. If it wasn't you, ignore this email.",
      "Reset password",
      link,
      `Or paste this link: ${link}`,
    ),
  };
}

export function accountExistsEmail(loginUrl: string, resetUrl: string): Omit<EmailMessage, "to"> {
  return {
    subject: "You already have a Reasonix account",
    text: `Someone tried to register with this email, but an account already exists.\n\nSign in: ${loginUrl}\nForgot your password? ${resetUrl}\n\nIf this wasn't you, no action is needed.`,
    html: layout(
      "You already have an account",
      "Someone tried to register with this email, but an account already exists. You can sign in, or reset your password if you've forgotten it.",
      "Sign in",
      loginUrl,
      `Forgot your password? ${resetUrl}`,
    ),
  };
}
