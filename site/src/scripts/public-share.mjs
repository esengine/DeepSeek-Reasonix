const token = decodeURIComponent(location.hash.slice(1));
history.replaceState(null, "", location.pathname);
const byId = (id) => document.getElementById(id);

function text(id, value) { byId(id).textContent = String(value ?? "—"); }
function formatDate(value) { const date = new Date(value); return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString("zh-CN", { hour12: false }); }
async function post(path, body) {
  const response = await fetch(path, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || "安全分享不可用");
  return payload;
}

function renderMetrics(items) {
  const root = byId("public-wiki-metrics");
  root.replaceChildren();
  for (const item of items || []) {
    const card = document.createElement("article");
    const label = document.createElement("span");
    const value = document.createElement("strong");
    label.textContent = item.label;
    value.textContent = item.value;
    card.append(label, value);
    root.append(card);
  }
  if (!root.children.length) root.textContent = "本次对外版本未披露量化指标。";
}

function renderRelations(items) {
  const root = byId("public-wiki-relations");
  root.replaceChildren();
  for (const item of items || []) {
    const row = document.createElement("article");
    const source = document.createElement("strong");
    const relation = document.createElement("span");
    const target = document.createElement("b");
    source.textContent = item.source;
    relation.textContent = item.relation || "关联";
    target.textContent = item.target;
    row.append(source, relation, target);
    root.append(row);
  }
  if (!root.children.length) root.textContent = "本次对外版本未披露关系数据。";
}

try {
  if (!token) throw new Error("安全分享不可用");
  const { share } = await post("/api/public/shares/inspect", { token });
  text("shared-document-title", share.documentTitle);
  text("shared-recipient", share.recipient);
  text("shared-scope", "对外 Wiki（只读）");
  text("shared-expires", formatDate(share.expiresAt));
  byId("share-access-form").hidden = false;
  byId("share-access-code").focus();
} catch (error) {
  text("shared-document-title", "安全分享不可用");
  text("shared-lock-copy", "链接可能已过期、被撤销或无效。系统不会披露具体原因。");
  text("share-access-error", "请向分享方确认链接仍在有效期内。");
}

byId("share-access-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = event.currentTarget.querySelector("button");
  button.disabled = true;
  text("share-access-error", "");
  try {
    const payload = await post("/api/public/shares/access", { token, accessCode: byId("share-access-code").value });
    text("public-wiki-title", payload.wiki.title);
    text("public-wiki-meta", `${payload.wiki.version} · 对外只读 Wiki · 到期 ${formatDate(payload.share.expiresAt)}`);
    text("public-wiki-summary", payload.wiki.executiveSummary);
    text("public-wiki-mechanism", payload.wiki.keyMechanism);
    renderMetrics(payload.wiki.metrics);
    renderRelations(payload.wiki.relationships);
    text("recipient-watermark", `${payload.share.recipient} · ${payload.share.id}`);
    byId("recipient-watermark").hidden = false;
    byId("share-lock").hidden = true;
    byId("shared-wiki").hidden = false;
    document.title = `${payload.wiki.title} · intelifar 安全 Wiki`;
  } catch (error) {
    text("share-access-error", "访问码错误或分享已失效");
    byId("share-access-code").select();
    button.disabled = false;
  }
});
