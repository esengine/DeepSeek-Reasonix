import { esc, page } from "./shell";
import { type User, userNav } from "./auth";

function notice(n?: { kind: "err" | "ok"; text: string }): string {
  return n ? `<div class="notice ${n.kind}">${esc(n.text)}</div>` : "";
}

function authShell(title: string, crumb: string, body: string): string {
  return page(title, crumb, `<div class="authwrap"><div class="authcard">${body}</div></div>`);
}

export function renderLogin(n?: { kind: "err" | "ok"; text: string }): string {
  return authShell(
    "Reasonix · Sign in",
    "sign in",
    `<h1>Sign in</h1><p class="sub">Crash &amp; telemetry dashboard</p>${notice(n)}
<form method="post" action="/login">
<div class="field"><label>Email</label><input type="email" name="email" autocomplete="username" required></div>
<div class="field"><label>Password</label><input type="password" name="password" autocomplete="current-password" required></div>
<button class="btn block" type="submit">Sign in</button>
</form>
<div class="alt">No account? <a href="/register">Register</a></div>`,
  );
}

export function renderRegister(n?: { kind: "err" | "ok"; text: string }): string {
  return authShell(
    "Reasonix · Register",
    "register",
    `<h1>Create account</h1><p class="sub">New accounts start with no access until an admin approves them.</p>${notice(n)}
<form method="post" action="/register">
<div class="field"><label>Email</label><input type="email" name="email" autocomplete="username" required></div>
<div class="field"><label>Password <span style="color:var(--ink-3)">— at least 8 characters</span></label><input type="password" name="password" autocomplete="new-password" minlength="8" required></div>
<button class="btn block" type="submit">Register</button>
</form>
<div class="alt">Already have an account? <a href="/login">Sign in</a></div>`,
  );
}

export function renderAccount(user: User, n?: { kind: "err" | "ok"; text: string }): string {
  const pending = user.role === "pending";
  const status = pending
    ? `<div class="notice err">Your account is awaiting approval. An admin needs to grant you access before the crash dashboard becomes visible.</div>`
    : `<div class="notice ok">You have <b>${esc(user.role)}</b> access. <a href="/stats">Open the dashboard →</a></div>`;
  return page(
    "Reasonix · Account",
    "account",
    `<h1>Account</h1><p class="sub">${esc(user.email)} · joined ${esc(user.created_at.slice(0, 10))}</p>
${notice(n)}${status}
<div class="card" style="max-width:480px;margin-top:24px"><h2>Change password</h2>
<form method="post" action="/account/password">
<div class="field"><label>Current password</label><input type="password" name="current" autocomplete="current-password" required></div>
<div class="field"><label>New password</label><input type="password" name="next" autocomplete="new-password" minlength="8" required></div>
<button class="btn" type="submit">Update password</button>
</form></div>`,
    userNav(user),
  );
}
