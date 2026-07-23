// Reasonix Community client. Renders the forum from the forum.reasonix.io API and
// gates posting on the shared id.reasonix.io session (cookie sent cross-subdomain).
// Bilingual like the rest of the site: static labels via .l-en/.l-zh spans that the
// shared `reasonix-lang` choice toggles; plain-text strings pick the current lang.
import { initTheme } from "./theme.js";

const FORUM = (import.meta.env.PUBLIC_FORUM_API || "https://forum.reasonix.io").replace(/\/$/, "");
const ACCOUNTS = (import.meta.env.PUBLIC_ACCOUNTS_API || "https://id.reasonix.io").replace(/\/$/, "");

const el = (id) => document.getElementById(id);
const qp = new URLSearchParams(location.search);

let lang = "en";
const L = (en, zh) => `<span class="l-en">${en}</span><span class="l-zh">${zh}</span>`;
const t = (en, zh) => (lang === "zh" ? zh : en);

function applyLangText() {
  const attr = lang === "zh" ? "zh" : "en";
  document.querySelectorAll("[data-ph-en]").forEach((n) => { n.placeholder = n.getAttribute(`data-ph-${attr}`); });
  document.querySelectorAll("[data-l-en]").forEach((n) => { n.textContent = n.getAttribute(`data-l-${attr}`); });
}
function setLang(l) {
  lang = l;
  document.body.dataset.lang = l;
  document.documentElement.lang = l === "zh" ? "zh-CN" : "en";
  document.querySelectorAll(".lang-switch button").forEach((b) => b.classList.toggle("active", b.dataset.lang === l));
  try { localStorage.setItem("reasonix-lang", l); } catch {}
  applyLangText();
}
function initLang() {
  let saved = "";
  try { saved = localStorage.getItem("reasonix-lang") || ""; } catch {}
  setLang(saved || ((navigator.language || "").toLowerCase().startsWith("zh") ? "zh" : "en"));
  document.querySelectorAll(".lang-switch button").forEach((b) => b.addEventListener("click", () => setLang(b.dataset.lang)));
}

async function api(base, path, opts = {}) {
  const res = await fetch(base + path, {
    method: opts.method || "GET",
    credentials: "include",
    headers: opts.body ? { "content-type": "application/json" } : undefined,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  let data = null;
  try { data = await res.json(); } catch {}
  if (!res.ok) {
    const err = new Error(data?.error?.message || "Something went wrong.");
    err.code = data?.error?.code;
    err.status = res.status;
    throw err;
  }
  return data;
}
const forum = (p, o) => api(FORUM, p, o);

// Anti-spam / gate errors are localized by code; unknown codes fall back to the
// server message.
const ERR = {
  email_unverified: ["Confirm your email address before posting.", "发帖前请先验证你的邮箱。"],
  links_restricted: ["New members can't post links yet — participate a little to unlock.", "新成员暂时不能发链接——参与一段时间后解锁。"],
  insufficient_trust: ["You don't have access to post in this category yet.", "你还没有在此分区发帖的权限。"],
  silenced: ["Your account is temporarily restricted from posting.", "你的账号暂时被限制发帖。"],
  rate_limited: ["You're posting too fast — take a short break.", "发帖太快了——稍微歇一会儿。"],
  daily_limit: ["You've hit today's posting limit for your trust level.", "你已达到当前信任等级的每日发帖上限。"],
  closed: ["This topic is closed to new replies.", "该话题已关闭，不能再回复。"],
  unauthorized: ["Sign in to continue.", "请先登录。"],
};
const errText = (err) => (ERR[err.code] ? t(ERR[err.code][0], ERR[err.code][1]) : err.message);

const CATS = {
  announcements: ["Announcements", "公告"],
  help: ["Help & Support", "帮助与支持"],
  skills: ["Skills & Plugins", "技能与插件"],
  show: ["Show & Tell", "作品展示"],
  feedback: ["Feedback & Ideas", "反馈与建议"],
};
const CATDESC = {
  announcements: ["Releases, roadmap, and community news.", "版本发布、路线图与社区动态。"],
  help: ["Stuck on setup, config, or cache behavior? Ask here.", "安装、配置或缓存问题？在这里提问。"],
  skills: ["Share, request, and review community skills and MCP servers.", "分享、求助、评审社区技能与 MCP 服务。"],
  show: ["Built something with Reasonix? Show the community.", "用 Reasonix 做了东西？来给社区看看。"],
  feedback: ["Feature requests and product feedback.", "功能建议与产品反馈。"],
};
const catName = (slug, apiName) => (CATS[slug] ? L(CATS[slug][0], CATS[slug][1]) : esc(apiName));
const catText = (slug, apiName) => (CATS[slug] ? t(CATS[slug][0], CATS[slug][1]) : apiName);
const ROLES = { admin: ["admin", "管理员"], moderator: ["moderator", "版主"] };

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
const AV_GRAD = [
  "var(--accent),var(--violet)",
  "var(--ok),oklch(0.62 0.15 200)",
  "var(--warm),var(--rose)",
  "var(--violet),var(--rose)",
  "oklch(0.6 0.15 200),var(--accent)",
];
function avatar(handle, size = "") {
  const h = handle || "?";
  let n = 0;
  for (const ch of h) n = (n + ch.charCodeAt(0)) % AV_GRAD.length;
  const initials = h.replace(/[^a-zA-Z0-9]/g, "").slice(0, 2).toUpperCase() || "?";
  return `<span class="av ${size}" style="background:linear-gradient(140deg,${AV_GRAD[n]})">${esc(initials)}</span>`;
}
function ago(iso) {
  if (!iso) return "";
  const s = Math.max(1, (Date.now() - new Date(iso).getTime()) / 1000);
  const u = [[86400, "d", "天"], [3600, "h", "小时"], [60, "m", "分钟"]];
  for (const [sec, en, zh] of u) if (s >= sec) { const v = Math.floor(s / sec); return L(`${v}${en} ago`, `${v}${zh}前`); }
  return L("just now", "刚刚");
}
function md(body) {
  const parts = esc(body).split(/```/);
  let out = "";
  parts.forEach((chunk, i) => {
    if (i % 2 === 1) { out += `<pre>${chunk.replace(/^\n/, "")}</pre>`; return; }
    const paras = chunk.split(/\n{2,}/).map((p) => p.trim()).filter(Boolean);
    out += paras.map((p) => `<p>${p.replace(/`([^`]+)`/g, "<code>$1</code>").replace(/\n/g, "<br>")}</p>`).join("");
  });
  return out || "<p></p>";
}

const CAT_ICONS = { announcements: "📣", help: "🛟", skills: "🧩", show: "✨", feedback: "💡" };
const loginUrl = () => `/login/?next=${encodeURIComponent(location.pathname + location.search)}`;

let account = null;
async function loadAccount() {
  try { account = (await api(ACCOUNTS, "/me")).user; } catch { account = null; }
  const slot = el("nav-account");
  if (slot) {
    slot.innerHTML = account
      ? `<a href="/account/" title="${esc(account.email)}">${avatar(account.handle)}</a>`
      : `<a class="btn btn-ghost sm" href="${loginUrl()}">${L("Sign in", "登录")}</a>`;
  }
}

/* ── home ─────────────────────────────────────────── */
function renderHome() {
  const catBox = el("cat-list");
  const topicBox = el("topic-list");
  const category = qp.get("category") || "";

  forum("/categories").then((d) => {
    const cats = d.categories;
    if (el("s-cats")) el("s-cats").textContent = cats.length;
    if (el("s-topics")) el("s-topics").textContent = cats.reduce((a, c) => a + (c.topicCount || 0), 0);
    catBox.innerHTML = cats.map((c) => `
      <a class="cat" href="/community/?category=${esc(c.slug)}">
        <span class="ico">${CAT_ICONS[c.slug] || "💬"}</span>
        <div><h3>${catName(c.slug, c.name)}</h3><p>${CATDESC[c.slug] ? L(CATDESC[c.slug][0], CATDESC[c.slug][1]) : esc(c.description)}</p>
        <div class="meta">${c.topicCount || 0} ${L("topics", "话题")}${c.lastActivity ? " · " + ago(c.lastActivity) : ""}</div></div>
      </a>`).join("");
  }).catch(() => { catBox.innerHTML = `<div class="empty">${L("Couldn't load categories.", "无法加载分区。")}</div>`; });

  const loadTopics = (sort) => {
    topicBox.innerHTML = `<div class="skeleton"><div class="bar"></div><div class="bar short"></div></div>`.repeat(3);
    const q = new URLSearchParams();
    if (category) q.set("category", category);
    if (sort) q.set("sort", sort);
    forum("/topics?" + q).then((d) => {
      if (!d.topics.length) { topicBox.innerHTML = `<div class="empty">${L("No topics yet —", "还没有话题 ——")} <a class="tag" href="/community/new/">${L("start the first one", "来发第一个")}</a>.</div>`; return; }
      topicBox.innerHTML = d.topics.map((tp) => `
        <div class="topic">
          ${avatar(tp.author.split("@")[0])}
          <div class="main">
            <div class="title">
              ${tp.pinned ? `<span class="badge pinned">📌 ${L("Pinned", "置顶")}</span>` : ""}
              ${tp.status === "solved" ? `<span class="badge solved">✓ ${L("Solved", "已解决")}</span>` : ""}
              <a href="/community/topic/?id=${tp.id}">${esc(tp.title)}</a>
            </div>
            <div class="sub"><span class="cat-tag">${catName(tp.category, tp.categoryName)}</span> <span class="who">${esc(tp.author.split("@")[0])}</span> · ${ago(tp.createdAt)}</div>
          </div>
          <div class="stat"><div class="n">${tp.replyCount}</div><div class="l">${L("replies", "回复")}</div></div>
          <div class="last">${ago(tp.lastPostAt)}</div>
        </div>`).join("");
    }).catch(() => { topicBox.innerHTML = `<div class="empty">${L("Couldn't load discussions.", "无法加载讨论。")}</div>`; });
  };
  loadTopics("latest");

  el("sort-tabs")?.addEventListener("click", (e) => {
    const b = e.target.closest("button[data-sort]");
    if (!b) return;
    el("sort-tabs").querySelectorAll("button").forEach((x) => x.classList.toggle("on", x === b));
    loadTopics(b.dataset.sort);
  });
}

/* ── thread ───────────────────────────────────────── */
let firstPostId = 0;
function postHtml(p, topic) {
  const answer = topic.acceptedPostId && topic.acceptedPostId === p.id;
  const cls = answer ? "post answer" : p.id === firstPostId ? "post op" : "post";
  const roleWord = ROLES[p.role] ? L(ROLES[p.role][0], ROLES[p.role][1]) : "";
  const role = roleWord ? `<span class="badge role ${esc(p.role)}">${roleWord}</span>` : "";
  return `<article class="${cls}">
    ${avatar(p.handle || p.author.split("@")[0], "lg")}
    <div>
      ${answer ? `<div class="answer-flag">✓ ${L("Accepted answer", "已采纳回答")}</div>` : ""}
      <div class="who"><span class="name">${esc(p.handle || p.author.split("@")[0])}</span>${role}<span class="when">${ago(p.createdAt)}</span></div>
      <div class="body">${md(p.body)}</div>
      <div class="actions">
        <button class="react" data-like="${p.id}">👍 <span>${p.likeCount || 0}</span></button>
        <button class="link-act" data-flag="${p.id}">${L("Report", "举报")}</button>
      </div>
    </div>
  </article>`;
}

async function renderThread() {
  const id = Number(qp.get("id"));
  if (!id) { location.href = "/community/"; return; }
  let data;
  try { data = await forum(`/topics/${id}`); }
  catch { el("posts").innerHTML = `<div class="empty">${L("That discussion doesn't exist or was removed.", "该讨论不存在或已被移除。")}</div>`; return; }
  const { topic, posts } = data;
  firstPostId = posts[0]?.id || 0;

  document.title = `${topic.title} — ${t("Reasonix Community", "Reasonix 社区")}`;
  el("crumb-cat").textContent = catText(topic.category, topic.category);
  el("crumb-title").textContent = topic.title;
  el("t-title").textContent = topic.title;
  el("t-meta").innerHTML =
    `${topic.status === "solved" ? `<span class="badge solved">✓ ${L("Solved", "已解决")}</span>` : ""}
     <span>${topic.replyCount} ${L("replies", "回复")} · ${topic.viewCount} ${L("views", "浏览")} · ${L("started", "发起于")} ${ago(topic.createdAt)}</span>`;
  el("posts").innerHTML = posts.map((p) => postHtml(p, topic)).join("");

  const seen = new Set();
  el("parti").innerHTML = posts.filter((p) => !seen.has(p.author) && seen.add(p.author)).slice(0, 8).map((p) => avatar(p.handle || p.author.split("@")[0])).join("");

  el("posts").addEventListener("click", async (e) => {
    const flag = e.target.closest("[data-flag]");
    if (flag && account) {
      if (!confirm(t("Report this post as spam or abuse?", "举报该帖为垃圾/滥用内容？"))) return;
      try { await forum(`/posts/${flag.dataset.flag}/flags`, { method: "POST", body: { reason: "spam" } }); flag.innerHTML = L("Reported ✓", "已举报 ✓"); flag.disabled = true; }
      catch (err) { alert(errText(err)); }
    } else if (flag) { location.href = loginUrl(); }
  });

  const zone = el("reply-zone");
  if (!account) {
    zone.innerHTML = `<div class="composer"><div class="gate"><p>${L("Sign in with your Reasonix account to reply.", "用你的 Reasonix 账号登录后回复。")}</p><a class="btn btn-primary" href="${loginUrl()}">${L("Sign in", "登录")}</a></div></div>`;
    return;
  }
  zone.innerHTML = `
    <div class="msg error" id="reply-msg" hidden></div>
    <div class="composer">
      <textarea id="reply-body" data-ph-en="Write a reply… Markdown and \`\`\` code blocks supported." data-ph-zh="写下回复… 支持 Markdown 和 \`\`\` 代码块。"></textarea>
      <div class="foot"><span class="hint">${L("Signed in as", "已登录")} <b>${esc(account.handle)}</b></span><button class="btn btn-primary" id="reply-submit">${L("Post reply", "发布回复")}</button></div>
    </div>`;
  applyLangText();
  el("reply-submit").addEventListener("click", async () => {
    const body = el("reply-body").value.trim();
    const msg = el("reply-msg");
    msg.hidden = true;
    if (body.length < 2) return;
    el("reply-submit").disabled = true;
    try {
      await forum(`/topics/${id}/posts`, { method: "POST", body: { body } });
      location.reload();
    } catch (err) {
      msg.textContent = errText(err); msg.hidden = false;
      el("reply-submit").disabled = false;
    }
  });
}

/* ── new topic ────────────────────────────────────── */
async function renderNew() {
  if (!account) {
    el("new-gate").hidden = false;
    el("gate-login").href = loginUrl();
    return;
  }
  el("new-form").hidden = false;
  const sel = el("f-category");
  try {
    const { categories } = await forum("/categories");
    for (const c of categories) {
      const o = document.createElement("option");
      o.value = c.id;
      o.setAttribute("data-l-en", CATS[c.slug] ? CATS[c.slug][0] : c.name);
      o.setAttribute("data-l-zh", CATS[c.slug] ? CATS[c.slug][1] : c.name);
      o.textContent = catText(c.slug, c.name);
      sel.appendChild(o);
    }
    const pre = qp.get("category");
    if (pre) { const m = categories.find((c) => c.slug === pre); if (m) sel.value = m.id; }
  } catch {}
  applyLangText();

  el("f-submit").addEventListener("click", async () => {
    const msg = el("new-msg");
    msg.hidden = true;
    const categoryId = Number(sel.value);
    const title = el("f-title").value.trim();
    const body = el("f-body").value.trim();
    if (!categoryId) { msg.textContent = t("Choose a category.", "请选择一个分区。"); msg.hidden = false; return; }
    if (title.length < 6 || body.length < 10) { msg.textContent = t("Add a title (6+ chars) and a bit more detail (10+ chars).", "标题至少 6 个字符，正文至少 10 个字符。"); msg.hidden = false; return; }
    el("f-submit").disabled = true;
    try {
      const { topic } = await forum("/topics", { method: "POST", body: { categoryId, title, body } });
      location.href = `/community/topic/?id=${topic.id}`;
    } catch (err) {
      msg.textContent = errText(err); msg.hidden = false;
      el("f-submit").disabled = false;
    }
  });
}

(async function () {
  initTheme();
  initLang();
  await loadAccount();
  if (el("topic-list")) renderHome();
  else if (el("posts")) renderThread();
  else if (el("new-form")) renderNew();
})();
