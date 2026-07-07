// Moderation console for user-published skills/MCP servers. Gated on the unified
// dashboard admin role; reads and writes the registry database.
import { esc, page } from "./shell";
import { type User, userNav } from "./auth";
import type { PackageRow } from "./registry/types";

const STATUS_TABS = [
  { key: "pending", label: "Pending" },
  { key: "active", label: "Active" },
  { key: "hidden", label: "Hidden" },
  { key: "rejected", label: "Rejected" },
];

function actionForm(pkg: PackageRow, action: string, label: string, cls: string, backStatus: string, confirm?: string): string {
  const onsubmit = confirm ? ` onsubmit="return confirm('${esc(confirm)}')"` : "";
  return `<form method="post" action="/community/${esc(pkg.scope_handle)}/${esc(pkg.name)}/${action}" class="inline"${onsubmit}>
<input type="hidden" name="status" value="${esc(backStatus)}"><button class="btn ${cls} sm" type="submit">${esc(label)}</button></form>`;
}

function rowActions(pkg: PackageRow, backStatus: string): string {
  if (pkg.status === "active") {
    const verify = pkg.verified
      ? actionForm(pkg, "unverify", "Unverify", "ghost", backStatus)
      : actionForm(pkg, "verify", "Verify", "ghost", backStatus);
    return `${verify}${actionForm(pkg, "hide", "Hide", "danger", backStatus, `Hide ${pkg.slug}?`)}`;
  }
  const approve = actionForm(pkg, "approve", "Approve", "", backStatus);
  const reject = pkg.status === "rejected" ? "" : actionForm(pkg, "reject", "Reject", "danger", backStatus, `Reject ${pkg.slug}?`);
  return `${approve}${reject}`;
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
    `Skill submission — ${pkg.slug}`,
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

export function renderCommunity(viewer: User, packages: PackageRow[], status: string): string {
  const tabs = STATUS_TABS.map(
    (t) => `<a class="filter-tab${t.key === status ? " active" : ""}" href="/community?status=${t.key}">${t.label}</a>`,
  ).join("");
  const rows = packages.length
    ? packages
        .map((p) => {
          const verified = p.verified ? ` <span class="badge admin">verified</span>` : "";
          const links = sourceLinks(p);
          return `<tr>
<td><div class="crash-summary"><span>${esc(p.slug)}${verified}</span><small>${esc(p.summary || "—")}</small></div></td>
<td><span class="pill">${esc(p.kind)}</span></td>
<td>@${esc(p.scope_handle)}</td>
<td class="n">${esc(p.latest_version || "—")} · ${p.install_count} inst · ${p.star_count}★</td>
<td class="n">${esc(p.created_at.slice(0, 10))}</td>
<td><div class="actions">${rowActions(p, status)}</div><div class="rowlinks">${copyButton(p)}${links ? `<span class="muted">${links}</span>` : ""}</div></td>
</tr>`;
        })
        .join("")
    : `<tr><td colspan="6"><div class="empty">No ${esc(status)} packages</div></td></tr>`;
  return page(
    "Reasonix · Community",
    "community",
    `<h1>Community</h1><p class="sub">Review user-published skills and MCP servers — approve to publish, verify to badge, hide to take down</p>
<div class="filter-tabs">${tabs}</div>
<div class="card full"><table class="reg-table"><colgroup><col class="c-pkg"><col class="c-kind"><col class="c-pub"><col class="c-ver"><col class="c-sub"><col class="c-act"></colgroup><thead><tr><th>package</th><th>kind</th><th>publisher</th><th>version · installs · stars</th><th>submitted</th><th></th></tr></thead>
<tbody>${rows}</tbody></table></div>`,
    userNav(viewer),
  );
}
