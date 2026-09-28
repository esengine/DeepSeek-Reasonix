// Moderation console for user-published skills/plugins/MCP servers. Gated on the unified
// dashboard admin role; reads and writes the registry database.
import { esc, page } from "./shell";
import { type User, userNav } from "./auth";
import type { PackageRow, ReviewRow } from "./registry/types";
import { FLAG_MAX_APPROVAL, FLAG_MIN_DOWN, approvalRate, needsReview } from "./registry/lib/ranking";

const STATUS_TABS = [
  { key: "pending", label: "Pending" },
  { key: "active", label: "Active" },
  { key: "flagged", label: "Flagged" },
  { key: "hidden", label: "Hidden" },
  { key: "rejected", label: "Rejected" },
  { key: "private", label: "Private" },
];

function actionForm(pkg: PackageRow, action: string, label: string, cls: string, backStatus: string, confirm?: string, fields = ""): string {
  const onsubmit = confirm ? ` onsubmit="return confirm('${esc(confirm)}')"` : "";
  return `<form method="post" action="/community/${esc(pkg.scope_handle)}/${esc(pkg.name)}/${action}" class="inline"${onsubmit}>
<input type="hidden" name="status" value="${esc(backStatus)}">
<input type="hidden" name="expectedVersion" value="${esc(pkg.latest_version)}">
<input type="hidden" name="expectedUpdatedAt" value="${esc(pkg.updated_at)}">
<input type="hidden" name="expectedStatus" value="${esc(pkg.status)}">${fields}
<button class="btn ${cls} sm" type="submit">${esc(label)}</button></form>`;
}

// Approving binds the digest the reviewer's own Reasonix plan printed
// (contentDigest) to the reviewed version; Studio installs only against it.
// Left empty, the version is published but not installable from Studio.
function approveForm(pkg: PackageRow, backStatus: string): string {
  const id = `approve-${pkg.id}`;
  const field = `<input form="${id}" class="digest-field" type="text" name="contentHash" placeholder="digest (optional)" pattern="sha256:[0-9a-f]{64}" aria-label="Reviewed content digest">`;
  return field + actionForm(pkg, "approve", "Approve", "", backStatus).replace("<form ", `<form id="${id}" `);
}

// A live version approved without a digest is listed but not installable from
// Studio; pinning binds the digest to that same version without moving it.
function pinForm(pkg: ReviewRow, backStatus: string): string {
  const field = `<input class="digest-field" type="text" name="contentHash" required placeholder="sha256:… contentDigest" pattern="sha256:[0-9a-f]{64}" aria-label="Reviewed content digest">`;
  const label = pkg.content_hash ? "Re-pin" : "Pin digest";
  const form = actionForm(pkg, "pin", "Save digest", "", backStatus, undefined, field).replace('class="inline"', 'class="digest-form"');
  return `<details class="pin"><summary>${label}</summary>${form}</details>`;
}

function digestLine(pkg: ReviewRow): string {
  if (pkg.status !== "active") return "";
  return pkg.content_hash
    ? `<small class="digest" title="${esc(pkg.content_hash)}">pinned ${esc(pkg.content_hash.slice(7, 19))}…</small>`
    : `<small class="digest unpinned">unpinned · not installable from Studio</small>`;
}

function rowActions(pkg: ReviewRow, backStatus: string): string {
  // Approval needs the owner's consent; submitting moves a private package to pending.
  if (pkg.status === "private") return `<span class="muted">Not submitted for review</span>`;
  if (pkg.status === "active") {
    const verify = pkg.verified
      ? actionForm(pkg, "unverify", "Unverify", "ghost", backStatus)
      : actionForm(pkg, "verify", "Verify", "ghost", backStatus);
    return `${verify}${actionForm(pkg, "hide", "Hide", "danger", backStatus, `Hide ${pkg.slug}?`)}${pinForm(pkg, backStatus)}`;
  }
  const approve = approveForm(pkg, backStatus);
  const reject = pkg.status === "rejected" ? "" : actionForm(pkg, "reject", "Reject", "danger", backStatus, `Reject ${pkg.slug}?`);
  return `${approve}${reject}`;
}

function votesCell(pkg: PackageRow): string {
  const rate = approvalRate(pkg.up_count, pkg.down_count);
  const pct = rate === null ? "—" : `${Math.round(rate * 100)}%`;
  const flag = needsReview(pkg.up_count, pkg.down_count) ? ` <span class="badge">review</span>` : "";
  return `▲${pkg.up_count} ▼${pkg.down_count} · ${pct}${flag}`;
}

function sourceLinks(pkg: PackageRow): string {
  const links: string[] = [];
  if (pkg.repo_url) links.push(`<a class="navlink" href="${esc(pkg.repo_url)}" target="_blank" rel="noopener">repo</a>`);
  if (pkg.homepage) links.push(`<a class="navlink" href="${esc(pkg.homepage)}" target="_blank" rel="noopener">home</a>`);
  return links.join(" · ");
}

// One paste-ready block of everything a reviewer needs: the install pointer,
// provenance links, and README. The manifest snapshot lives on package_versions,
// not here — source/repo_url are what actually carry the reviewable content.
function reviewBlob(pkg: PackageRow): string {
  return [
    `Registry submission — ${pkg.slug}`,
    `kind: ${pkg.kind}`,
    `status: ${pkg.status}`,
    `publisher: @${pkg.scope_handle}`,
    `version: ${pkg.latest_version || "—"}`,
    `submitted: ${pkg.created_at}`,
    `install_kind: ${pkg.install_kind}`,
    `source: ${pkg.source || "—"}`,
    `repo_url: ${pkg.repo_url || "—"}`,
    `homepage: ${pkg.homepage || "—"}`,
    `tags: ${pkg.tags || "—"}`,
    `summary: ${pkg.summary || "—"}`,
    ``,
    `--- description (README) ---`,
    pkg.description || "(none)",
  ].join("\n");
}

function copyButton(pkg: PackageRow): string {
  return `<button type="button" class="btn ghost sm copy-btn" data-copy="${esc(reviewBlob(pkg))}"><span class="copy-label">Copy for review</span></button>`;
}

export function renderCommunity(viewer: User, packages: ReviewRow[], status: string): string {
  const tabs = STATUS_TABS.map(
    (t) => `<a class="filter-tab${t.key === status ? " active" : ""}" href="/community?status=${t.key}">${t.label}</a>`,
  ).join("");
  const rows = packages.length
    ? packages
        .map((p) => {
          const verified = p.verified ? ` <span class="badge admin">verified</span>` : "";
          const links = sourceLinks(p);
          return `<tr>
<td><div class="crash-summary"><span>${esc(p.slug)}${verified}</span><small>${esc(p.summary || "—")}</small>${digestLine(p)}</div></td>
<td><span class="pill">${esc(p.kind)}</span></td>
<td>@${esc(p.scope_handle)}</td>
<td class="n">${esc(p.latest_version || "—")} · ${p.install_count} inst · ${votesCell(p)}</td>
<td class="n">${esc(p.created_at.slice(0, 10))}</td>
<td><div class="actions">${rowActions(p, status)}</div><div class="rowlinks">${copyButton(p)}${links ? `<span class="muted">${links}</span>` : ""}</div></td>
</tr>`;
        })
        .join("")
    : `<tr><td colspan="6"><div class="empty">No ${esc(status)} packages</div></td></tr>`;
  return page(
    "Reasonix · Community",
    "community",
    `<h1>Community</h1><p class="sub">Review user-published skills, plugins, and MCP servers — approve to publish, verify to badge, hide to take down. Flagged lists live packages with at least ${FLAG_MIN_DOWN} down-votes and under ${Math.round(FLAG_MAX_APPROVAL * 100)}% approval; nothing is hidden automatically</p>
<div class="filter-tabs">${tabs}</div>
<div class="card full"><table class="reg-table"><colgroup><col class="c-pkg"><col class="c-kind"><col class="c-pub"><col class="c-ver"><col class="c-sub"><col class="c-act"></colgroup><thead><tr><th>package</th><th>kind</th><th>publisher</th><th>version · installs · votes</th><th>submitted</th><th></th></tr></thead>
<tbody>${rows}</tbody></table></div>`,
    userNav(viewer),
  );
}
