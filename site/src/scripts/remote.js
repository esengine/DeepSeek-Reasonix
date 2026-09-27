const API = (import.meta.env.PUBLIC_ACCOUNTS_API || "https://id.reasonix.io").replace(/\/$/, "");
const STUDIO = (import.meta.env.PUBLIC_STUDIO_URL || "https://studio.reasonix.io").replace(/\/$/, "");
const $ = (id) => document.getElementById(id);
const local = (en, zh) => document.body.dataset.lang === "zh" ? zh : en;
const requestedDevice = new URL(location.href).searchParams.get("device")?.trim() || "";

async function api(path) {
  const response = await fetch(API + path, { credentials: "include" });
  let data = null;
  try { data = await response.json(); } catch {}
  if (!response.ok) {
    const error = new Error(data?.error?.message || local("The request failed.", "请求失败。"));
    error.status = response.status;
    throw error;
  }
  return data;
}

function studioUrl(deviceId) {
  const url = new URL(STUDIO);
  url.searchParams.set("device", deviceId);
  return url.href;
}

function deviceCard(device) {
  const card = document.createElement("article");
  card.className = "remote-device";

  const icon = document.createElement("span");
  icon.className = "remote-device-icon";
  icon.setAttribute("aria-hidden", "true");
  icon.textContent = "▣";

  const identity = document.createElement("div");
  identity.className = "remote-device-identity";
  const name = document.createElement("h3");
  name.textContent = device.name || local("Unnamed Studio", "未命名的 Studio");
  const meta = document.createElement("p");
  const seen = device.lastSeenAt
    ? new Date(device.lastSeenAt).toLocaleString()
    : local("Just connected", "刚刚连接");
  meta.textContent = `${device.platform || local("Computer", "电脑")} · ${local("Online", "在线")} · ${local("Connected", "连接于")} ${seen}`;
  identity.append(name, meta);

  const action = document.createElement("a");
  action.className = "btn btn-dark remote-open";
  action.href = studioUrl(device.id);
  action.textContent = local("Open in Web Studio", "进入网页版 Studio");
  action.setAttribute("aria-label", local(
    `Open ${name.textContent} in Web Studio`,
    `在网页版 Studio 中打开 ${name.textContent}`,
  ));

  card.append(icon, identity, action);
  return card;
}

const gate = $("remote-gate");
const view = $("remote-view");
const list = $("remote-devices");
const box = $("remote-msg");
const refresh = $("remote-refresh");

async function loadDevices() {
  refresh.disabled = true;
  try {
    const data = await api("/me/devices");
    gate.hidden = true;
    view.hidden = false;
    list.replaceChildren();
    box.hidden = true;
    if (data.presenceAvailable === false) {
      box.className = "auth-msg error";
      box.textContent = local(
        "Online status is temporarily unavailable. Refresh in a moment.",
        "暂时无法获取设备在线状态，请稍后刷新。",
      );
      box.hidden = false;
      return;
    }
    const devices = (data.devices || []).filter((device) => !device.revokedAt && device.online === true);
    if (requestedDevice) {
      const target = devices.find((device) => device.id === requestedDevice);
      if (target) {
        gate.hidden = false;
        gate.className = "auth-head";
        gate.textContent = local(
          `Connecting to ${target.name || "Studio"}…`,
          `正在连接 ${target.name || "Studio"}…`,
        );
        view.hidden = true;
        location.replace(studioUrl(target.id));
        return;
      }
      box.className = "auth-msg error";
      box.textContent = local(
        "The computer in this QR code is offline or does not belong to this account. Keep desktop Studio open and signed in, then refresh.",
        "二维码对应的电脑不在线，或不属于当前账号。请保持电脑端 Studio 已打开并登录同一账号，然后刷新。",
      );
      box.hidden = false;
    }
    if (devices.length === 0) {
      if (requestedDevice) return;
      box.className = "remote-empty-state";
      box.innerHTML = `
        <strong>${local("No computer is online", "当前没有在线电脑")}</strong>
        <span>${local(
          "Open desktop Studio and sign in with this account. The computer will appear here when it is ready to connect.",
          "请打开电脑端 Studio 并登录当前账号，设备可连接后会自动出现在这里。",
        )}</span>
        <a class="btn btn-dark" href="/?download=studio#start">${local("Download desktop Studio", "下载电脑端 Studio")}</a>
      `;
      box.hidden = false;
      return;
    }
    devices.forEach((device) => list.append(deviceCard(device)));
  } catch (error) {
    if (error.status === 401) {
      location.href = `/login/?next=${encodeURIComponent(location.pathname + location.search)}`;
      return;
    }
    gate.className = "auth-msg error";
    gate.textContent = error.message;
  } finally {
    refresh.disabled = false;
  }
}

refresh.addEventListener("click", () => void loadDevices());
void loadDevices();
window.setInterval(() => {
  if (document.visibilityState === "visible") void loadDevices();
}, 15_000);
