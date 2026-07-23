import { safeNext } from "./safe-next.js";

// Client for the reasonix-accounts API (id.reasonix.io). Cookie-based session,
// so every call sends credentials; the API base is build-time configurable.
const API = (import.meta.env.PUBLIC_ACCOUNTS_API || "https://id.reasonix.io").replace(/\/$/, "");

async function api(path, { method = "GET", body } = {}) {
  const res = await fetch(API + path, {
    method,
    credentials: "include",
    headers: body ? { "content-type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  let data = null;
  try { data = await res.json(); } catch {}
  if (!res.ok) {
    const err = new Error(data?.error?.message || "Something went wrong. Please try again.");
    err.code = data?.error?.code;
    err.status = res.status;
    throw err;
  }
  return data;
}

// Reusable helpers for account-aware pages (nav state, gated actions). Importing
// this module also runs the form auto-wiring below, but each block is guarded by
// element presence, so pages without auth forms just get these two helpers.
export async function currentAccount() {
  try { return (await api("/me")).user; } catch { return null; }
}
export async function accountLogout() {
  try { await api("/auth/logout", { method: "POST" }); } catch {}
}

const $ = (id) => document.getElementById(id);
const qp = new URLSearchParams(location.search);
const withBase = (p) => (import.meta.env.BASE_URL.replace(/\/$/, "") + p) || p;

function msg(el, kind, text) {
  if (!el) return;
  el.className = "auth-msg " + kind;
  el.textContent = text;
  el.hidden = false;
}
function clearMsg(el) { if (el) el.hidden = true; }

function busy(btn, on) {
  if (!btn) return;
  btn.disabled = on;
  btn.classList.toggle("loading", on);
}

// login — POST /auth/login, then continue to ?next or /account.
const loginForm = $("login-form");
if (loginForm) {
  const box = $("login-msg");
  const verified = qp.get("verified");
  if (verified === "1") msg(box, "ok", "Email confirmed. Sign in to continue.");
  else if (verified === "0") msg(box, "error", "That confirmation link was invalid or has expired.");
  loginForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    clearMsg(box);
    const btn = loginForm.querySelector("button[type=submit]");
    busy(btn, true);
    try {
      await api("/auth/login", { method: "POST", body: { email: $("email").value.trim(), password: $("password").value } });
      location.href = safeNext(qp.get("next"), location.origin) || withBase("/account/");
    } catch (err) {
      msg(box, "error", err.message);
      busy(btn, false);
    }
  });
}

// register — enumeration-safe: the API returns the same message either way.
const registerForm = $("register-form");
if (registerForm) {
  const box = $("register-msg");
  registerForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    clearMsg(box);
    const btn = registerForm.querySelector("button[type=submit]");
    busy(btn, true);
    try {
      const out = await api("/auth/register", {
        method: "POST",
        body: {
          email: $("email").value.trim(),
          password: $("password").value,
          displayName: $("displayName").value.trim() || undefined,
        },
      });
      msg(box, "ok", out?.message || "Check your inbox to confirm your account.");
      registerForm.reset();
    } catch (err) {
      msg(box, "error", err.message);
    } finally {
      busy(btn, false);
    }
  });
}

// forgot — always a generic success (never reveals whether the email exists).
const forgotForm = $("forgot-form");
if (forgotForm) {
  const box = $("forgot-msg");
  forgotForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    clearMsg(box);
    const btn = forgotForm.querySelector("button[type=submit]");
    busy(btn, true);
    try {
      const out = await api("/auth/forgot", { method: "POST", body: { email: $("email").value.trim() } });
      msg(box, "ok", out?.message || "If that account exists, a reset link is on its way.");
      forgotForm.reset();
    } catch (err) {
      msg(box, "error", err.message);
    } finally {
      busy(btn, false);
    }
  });
}

// reset — token comes from the emailed link.
const resetForm = $("reset-form");
if (resetForm) {
  const box = $("reset-msg");
  const token = qp.get("token") || "";
  if (!token) {
    msg(box, "error", "This reset link is incomplete. Request a new one.");
    resetForm.querySelectorAll("input, button").forEach((el) => (el.disabled = true));
  }
  resetForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    clearMsg(box);
    const password = $("password").value;
    if (password !== $("confirm").value) {
      msg(box, "error", "The two passwords don't match.");
      return;
    }
    const btn = resetForm.querySelector("button[type=submit]");
    busy(btn, true);
    try {
      await api("/auth/reset", { method: "POST", body: { token, password } });
      msg(box, "ok", "Password updated. You can sign in now.");
      resetForm.reset();
      resetForm.querySelectorAll("input, button").forEach((el) => (el.disabled = true));
    } catch (err) {
      msg(box, "error", err.message);
      busy(btn, false);
    }
  });
}

// account — profile view/edit, password change, sign out, delete.
const accountView = $("account-view");
if (accountView) {
  const gate = $("account-gate");
  const fill = (user) => {
    $("acct-handle").textContent = "@" + user.handle;
    $("acct-email").textContent = user.email + (user.emailVerified ? "" : " · unconfirmed");
    $("acct-role").textContent = user.role;
    $("f-displayName").value = user.displayName || "";
    $("f-handle").value = user.handle || "";
    $("f-bio").value = user.bio || "";
    $("f-avatarUrl").value = user.avatarUrl || "";
    accountView.hidden = false;
    if (gate) gate.hidden = true;
  };

  api("/me")
    .then((d) => fill(d.user))
    .catch((err) => {
      if (err.status === 401) location.href = withBase("/login/?next=/account/");
      else if (gate) msg(gate, "error", err.message);
    });

  const profileForm = $("profile-form");
  const pBox = $("profile-msg");
  profileForm?.addEventListener("submit", async (e) => {
    e.preventDefault();
    clearMsg(pBox);
    const btn = profileForm.querySelector("button[type=submit]");
    busy(btn, true);
    try {
      const d = await api("/me", {
        method: "PATCH",
        body: {
          displayName: $("f-displayName").value.trim(),
          handle: $("f-handle").value.trim().toLowerCase(),
          bio: $("f-bio").value.trim(),
          avatarUrl: $("f-avatarUrl").value.trim(),
        },
      });
      fill(d.user);
      msg(pBox, "ok", "Profile saved.");
    } catch (err) {
      msg(pBox, "error", err.message);
    } finally {
      busy(btn, false);
    }
  });

  const passwordForm = $("password-form");
  const pwBox = $("password-msg");
  passwordForm?.addEventListener("submit", async (e) => {
    e.preventDefault();
    clearMsg(pwBox);
    const btn = passwordForm.querySelector("button[type=submit]");
    busy(btn, true);
    try {
      await api("/me/password", {
        method: "POST",
        body: { currentPassword: $("currentPassword").value, newPassword: $("newPassword").value },
      });
      passwordForm.reset();
      msg(pwBox, "ok", "Password changed.");
    } catch (err) {
      msg(pwBox, "error", err.message);
    } finally {
      busy(btn, false);
    }
  });

  $("logout-btn")?.addEventListener("click", async () => {
    try { await api("/auth/logout", { method: "POST" }); } catch {}
    location.href = withBase("/login/");
  });

  $("delete-btn")?.addEventListener("click", async () => {
    if (!confirm("Delete your account? This cannot be undone.")) return;
    try {
      await api("/me", { method: "DELETE" });
      location.href = withBase("/");
    } catch (err) {
      msg(pBox, "error", err.message);
    }
  });
}

// device — the human-facing half of the CLI/desktop device-authorization flow.
const deviceView = $("device-view");
if (deviceView) {
  const gate = $("device-gate");
  const box = $("device-msg");
  const codeInput = $("device-code");
  const meta = $("device-meta");
  const actions = $("device-actions");
  const preset = qp.get("code");
  if (preset && codeInput) codeInput.value = preset;

  const showGrant = async () => {
    clearMsg(box);
    if (meta) meta.hidden = true;
    if (actions) actions.hidden = true;
    const code = codeInput.value.trim();
    if (!code) { msg(box, "error", "Enter the code shown in your terminal."); return; }
    try {
      const d = await api("/device/info?userCode=" + encodeURIComponent(code));
      if (meta) {
        meta.textContent = `Requested by ${d.grant.userAgent || "a device"}`;
        meta.hidden = false;
      }
      if (actions) actions.hidden = false;
    } catch (err) {
      msg(box, "error", err.message);
    }
  };

  const decide = async (path, okText) => {
    clearMsg(box);
    const code = codeInput.value.trim();
    actions?.querySelectorAll("button").forEach((b) => (b.disabled = true));
    try {
      await api(path, { method: "POST", body: { userCode: code } });
      msg(box, "ok", okText);
      if (meta) meta.hidden = true;
      if (actions) actions.hidden = true;
      codeInput.disabled = true;
    } catch (err) {
      msg(box, "error", err.message);
      actions?.querySelectorAll("button").forEach((b) => (b.disabled = false));
    }
  };

  api("/me")
    .then(() => {
      deviceView.hidden = false;
      if (gate) gate.hidden = true;
      $("device-check")?.addEventListener("click", showGrant);
      $("device-approve")?.addEventListener("click", () => decide("/device/approve", "Approved. Return to your terminal — you're signed in."));
      $("device-deny")?.addEventListener("click", () => decide("/device/deny", "The sign-in request was rejected."));
      if (preset) showGrant();
    })
    .catch((err) => {
      if (err.status === 401) {
        const next = "/device/" + (preset ? "?code=" + encodeURIComponent(preset) : "");
        location.href = withBase("/login/?next=" + encodeURIComponent(next));
      } else if (gate) msg(gate, "error", err.message);
    });
}
