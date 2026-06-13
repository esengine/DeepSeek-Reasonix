import { esc, page } from "./shell";
import { type Role, type User, userNav } from "./auth";

export interface UserRow {
  id: number;
  email: string;
  role: Role;
  created_at: string;
  approved_at: string | null;
}

export interface AuditRow {
  at: string;
  actor_email: string;
  action: string;
  target: string;
  detail: string;
}

function roleSelect(row: UserRow): string {
  const opt = (r: Role) => `<option value="${r}"${row.role === r ? " selected" : ""}>${r}</option>`;
  return `<form method="post" action="/admin/users" class="actions">
<input type="hidden" name="action" value="role"><input type="hidden" name="userId" value="${row.id}">
<select name="role" onchange="this.form.submit()">${opt("pending")}${opt("viewer")}${opt("admin")}</select>
<noscript><button class="btn sm" type="submit">Set</button></noscript></form>`;
}

function deleteForm(row: UserRow): string {
  return `<form method="post" action="/admin/users" class="inline" onsubmit="return confirm('Delete ${esc(row.email)}?')">
<input type="hidden" name="action" value="delete"><input type="hidden" name="userId" value="${row.id}">
<button class="btn danger sm" type="submit">Delete</button></form>`;
}

export function renderUsers(viewer: User, users: UserRow[], n?: { kind: "err" | "ok"; text: string }): string {
  const pending = users.filter((u) => u.role === "pending").length;
  const rows = users
    .map((u) => {
      const self = u.id === viewer.id;
      const approved = u.approved_at ? esc(u.approved_at.slice(0, 10)) : "—";
      return `<tr><td>${esc(u.email)}${self ? ' <span class="badge viewer">you</span>' : ""}</td>
<td>${self ? `<span class="badge ${u.role}">${u.role}</span>` : roleSelect(u)}</td>
<td class="n">${esc(u.created_at.slice(0, 10))}</td><td class="n">${approved}</td>
<td>${self ? "" : deleteForm(u)}</td></tr>`;
    })
    .join("");
  return page(
    "Reasonix · Users",
    "admin / users",
    `<h1>Users</h1><p class="sub"><b>${users.length}</b> accounts · <b>${pending}</b> awaiting approval · set a role to grant or revoke access</p>
${n ? `<div class="notice ${n.kind}">${esc(n.text)}</div>` : ""}
<div class="card full"><table><thead><tr><th>email</th><th>role</th><th>joined</th><th>approved</th><th></th></tr></thead>
<tbody>${rows}</tbody></table></div>
<a class="back" href="/admin/audit">View audit log →</a>`,
    userNav(viewer),
  );
}

export function renderAudit(viewer: User, rows: AuditRow[]): string {
  const body = rows.length
    ? `<table><thead><tr><th>when</th><th>actor</th><th>action</th><th>target</th><th>detail</th></tr></thead><tbody>${rows
        .map(
          (r) =>
            `<tr><td class="n">${esc(r.at.slice(0, 19).replace("T", " "))}</td><td>${esc(r.actor_email)}</td><td><span class="pill">${esc(r.action)}</span></td><td>${esc(r.target)}</td><td>${esc(r.detail)}</td></tr>`,
        )
        .join("")}</tbody></table>`
    : `<div class="empty">No actions logged yet</div>`;
  return page(
    "Reasonix · Audit",
    "admin / audit",
    `<h1>Audit log</h1><p class="sub">Permission and crash-data changes, newest first</p>
<div class="card full">${body}</div>
<a class="back" href="/admin">← Back to users</a>`,
    userNav(viewer),
  );
}
