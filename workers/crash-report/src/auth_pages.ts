import { esc, page } from "./shell";
import { type User, userNav } from "./auth";

export function renderAccount(user: User): string {
  const pending = user.role === "pending";
  const status = pending
    ? `<div class="notice err">Your account is awaiting approval. An admin needs to grant you access before the diagnostic reports dashboard becomes visible.</div>`
    : `<div class="notice ok">You have <b>${esc(user.role)}</b> access. <a href="/stats">Open the dashboard →</a></div>`;
  return page(
    "Reasonix · Account",
    "account",
    `<h1>Account</h1><p class="sub">${esc(user.email)} · joined ${esc(user.created_at.slice(0, 10))}</p>
${status}
<div class="card" style="max-width:480px;margin-top:24px"><h2>Identity</h2>
<p class="sub">Your email and password are managed by your Reasonix account.</p>
<a class="btn" href="https://reasonix.io/account/">Manage your account →</a></div>`,
    userNav(user),
  );
}
