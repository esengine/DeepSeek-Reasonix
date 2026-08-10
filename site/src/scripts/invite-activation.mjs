const token = decodeURIComponent(location.hash.slice(1));
history.replaceState(null, "", location.pathname);
const byId = (id) => document.getElementById(id);
const roleLabels = { admin: "空间管理员", editor: "知识编辑者", viewer: "只读成员" };
function text(id, value) { byId(id).textContent = String(value ?? "—"); }
async function post(path, body) { const response = await fetch(path, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) }); const payload = await response.json().catch(() => ({})); if (!response.ok) throw new Error(payload.error || "邀请不可用"); return payload; }

try {
  if (!token) throw new Error("邀请不可用");
  const { invitation } = await post("/api/public/invitations/inspect", { token });
  text("activation-name", `欢迎，${invitation.name}`);
  byId("activation-email").value = invitation.email;
  text("activation-meta", `${roleLabels[invitation.role] || invitation.role} · 邀请有效至 ${new Date(invitation.expiresAt).toLocaleString("zh-CN", { hour12: false })}`);
  byId("activation-status").hidden = true;
  byId("activation-form").hidden = false;
  byId("activation-password").focus();
} catch (error) {
  byId("activation-status").classList.add("error");
  byId("activation-status").querySelector("strong").textContent = "邀请不可用";
  byId("activation-status").querySelector("small").textContent = "链接可能已过期、撤销或已经使用";
}

byId("activation-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const password = byId("activation-password").value;
  const confirm = byId("activation-confirm").value;
  text("activation-error", "");
  if (password.length < 12 || password.length > 128) { text("activation-error", "密码必须包含 12–128 个字符"); return; }
  if (password !== confirm) { text("activation-error", "两次输入的密码不一致"); return; }
  const button = form.querySelector("button");
  button.disabled = true;
  try {
    await post("/api/public/invitations/accept", { token, password });
    form.reset();
    form.hidden = true;
    byId("activation-complete").hidden = false;
  } catch (error) {
    text("activation-error", String(error.message || "账号激活失败"));
    button.disabled = false;
  }
});
