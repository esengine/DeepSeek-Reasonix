// Reasonix Community client. Renders the forum from the forum.reasonix.io API and
// gates posting on the shared id.reasonix.io session (cookie sent cross-subdomain).
const FORUM = (import.meta.env.PUBLIC_FORUM_API || "https://forum.reasonix.io").replace(/\/$/, "");
const ACCOUNTS = (import.meta.env.PUBLIC_ACCOUNTS_API || "https://id.reasonix.io").replace(/\/$/, "");

const el = (id) => document.getElementById(id);
const qp = new URLSearchParams(location.search);

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

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
const AV = ["a", "b", "c", "d", "e"];
function avatar(handle, size = "") {
  const h = handle || "?";
  let n = 0;
  for (const ch of h) n = (n + ch.charCodeAt(0)) % AV.length;
  const initials = h.replace(/[^a-zA-Z0-9]/g, "").slice(0, 2).toUpperCase() || "?";
  return `<span class="av ${size}" style="background:linear-gradient(140deg,var(--accent),var(--violet))" data-c="${AV[n]}">${esc(initials)}</span>`;
}
function ago(iso) {
  if (!iso) return "";
  const s = Math.max(1, (Date.now() - new Date(iso).getTime()) / 1000);
  const u = [[86400, "d"], [3600, "h"], [60, "m"]];
  for (const [sec, l] of u) if (s >= sec) return Math.floor(s / sec) + l + " ago";
  return "just now";
}
// Minimal, safe markdown: escape first, then fences, inline code, paragraphs.
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
      : `<a class="btn btn-ghost sm" href="${loginUrl()}">Sign in</a>`;
  }
}

/* ── home ─────────────────────────────────────────── */
async function renderHome() {
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
        <div><h3>${esc(c.name)}</h3><p>${esc(c.description)}</p>
        <div class="meta">${c.topicCount || 0} topics${c.lastActivity ? " · " + ago(c.lastActivity) : ""}</div></div>
      </a>`).join("");
  }).catch(() => { catBox.innerHTML = `<div class="empty">Couldn't load categories.</div>`; });

  const loadTopics = (sort) => {
    topicBox.innerHTML = `<div class="skeleton"><div class="bar"></div><div class="bar short"></div></div>`.repeat(3);
    const q = new URLSearchParams();
    if (category) q.set("category", category);
    if (sort) q.set("sort", sort);
    forum("/topics?" + q).then((d) => {
      if (!d.topics.length) { topicBox.innerHTML = `<div class="empty">No topics yet — <a class="tag" href="/community/new/">start the first one</a>.</div>`; return; }
      topicBox.innerHTML = d.topics.map((t) => `
        <div class="topic">
          ${avatar(t.author.split("@")[0])}
          <div class="main">
            <div class="title">
              ${t.pinned ? '<span class="badge pinned">📌 Pinned</span>' : ""}
              ${t.status === "solved" ? '<span class="badge solved">✓ Solved</span>' : ""}
              <a href="/community/topic/?id=${t.id}">${esc(t.title)}</a>
            </div>
            <div class="sub"><span class="cat-tag">${esc(t.categoryName)}</span> <span class="who">${esc(t.author.split("@")[0])}</span> · ${ago(t.createdAt)}</div>
          </div>
          <div class="stat"><div class="n">${t.replyCount}</div><div class="l">replies</div></div>
          <div class="last">${ago(t.lastPostAt)}</div>
        </div>`).join("");
    }).catch(() => { topicBox.innerHTML = `<div class="empty">Couldn't load discussions.</div>`; });
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
function postHtml(p, topic) {
  const answer = topic.acceptedPostId && topic.acceptedPostId === p.id;
  const cls = answer ? "post answer" : p.id === firstPostId ? "post op" : "post";
  const role = p.role && p.role !== "member" ? `<span class="badge role ${esc(p.role)}">${esc(p.role)}</span>` : "";
  return `<article class="${cls}">
    ${avatar(p.handle || p.author.split("@")[0], "lg")}
    <div>
      ${answer ? '<div class="answer-flag">✓ Accepted answer</div>' : ""}
      <div class="who"><span class="name">${esc(p.handle || p.author.split("@")[0])}</span>${role}<span class="when">${ago(p.createdAt)}</span></div>
      <div class="body">${md(p.body)}</div>
      <div class="actions">
        <button class="react" data-like="${p.id}">👍 <span>${p.likeCount || 0}</span></button>
        <button class="link-act" data-flag="${p.id}">Report</button>
      </div>
    </div>
  </article>`;
}
let firstPostId = 0;

async function renderThread() {
  const id = Number(qp.get("id"));
  if (!id) { location.href = "/community/"; return; }
  let data;
  try { data = await forum(`/topics/${id}`); }
  catch { el("posts").innerHTML = `<div class="empty">That discussion doesn't exist or was removed.</div>`; return; }
  const { topic, posts } = data;
  firstPostId = posts[0]?.id || 0;

  document.title = `${topic.title} — Reasonix Community`;
  el("crumb-cat").textContent = topic.category;
  el("crumb-title").textContent = topic.title;
  el("t-title").textContent = topic.title;
  el("t-meta").innerHTML =
    `${topic.status === "solved" ? '<span class="badge solved">✓ Solved</span>' : ""}
     <span>${topic.replyCount} replies · ${topic.viewCount} views · started ${ago(topic.createdAt)}</span>`;
  el("posts").innerHTML = posts.map((p) => postHtml(p, topic)).join("");

  const seen = new Set();
  el("parti").innerHTML = posts.filter((p) => !seen.has(p.author) && seen.add(p.author)).slice(0, 8).map((p) => avatar(p.handle || p.author.split("@")[0])).join("");

  el("posts").addEventListener("click", async (e) => {
    const flag = e.target.closest("[data-flag]");
    if (flag && account) {
      if (!confirm("Report this post as spam or abuse?")) return;
      try { await forum(`/posts/${flag.dataset.flag}/flags`, { method: "POST", body: { reason: "spam" } }); flag.textContent = "Reported ✓"; flag.disabled = true; }
      catch (err) { alert(err.message); }
    } else if (flag) { location.href = loginUrl(); }
  });

  const zone = el("reply-zone");
  if (!account) {
    zone.innerHTML = `<div class="composer"><div class="gate"><p>Sign in with your Reasonix account to reply.</p><a class="btn btn-primary" href="${loginUrl()}">Sign in</a></div></div>`;
    return;
  }
  zone.innerHTML = `
    <div class="msg error" id="reply-msg" hidden></div>
    <div class="composer">
      <textarea id="reply-body" placeholder="Write a reply… Markdown and \`\`\` code blocks supported."></textarea>
      <div class="foot"><span class="hint">Signed in as <b>${esc(account.handle)}</b></span><button class="btn btn-primary" id="reply-submit">Post reply</button></div>
    </div>`;
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
      msg.textContent = err.message; msg.hidden = false;
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
      o.value = c.id; o.textContent = c.name;
      sel.appendChild(o);
    }
    const pre = qp.get("category");
    if (pre) { const m = categories.find((c) => c.slug === pre); if (m) sel.value = m.id; }
  } catch {}

  el("f-submit").addEventListener("click", async () => {
    const msg = el("new-msg");
    msg.hidden = true;
    const categoryId = Number(sel.value);
    const title = el("f-title").value.trim();
    const body = el("f-body").value.trim();
    if (!categoryId) { msg.textContent = "Choose a category."; msg.hidden = false; return; }
    if (title.length < 6 || body.length < 10) { msg.textContent = "Add a title (6+ chars) and a bit more detail (10+ chars)."; msg.hidden = false; return; }
    el("f-submit").disabled = true;
    try {
      const { topic } = await forum("/topics", { method: "POST", body: { categoryId, title, body } });
      location.href = `/community/topic/?id=${topic.id}`;
    } catch (err) {
      msg.textContent = err.message; msg.hidden = false;
      el("f-submit").disabled = false;
    }
  });
}

(async function () {
  await loadAccount();
  if (el("topic-list")) renderHome();
  else if (el("posts")) renderThread();
  else if (el("new-form")) renderNew();
})();
