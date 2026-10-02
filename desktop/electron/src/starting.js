"use strict";
const { BrowserWindow, nativeTheme } = require("electron");

const TEXT = {
  en: ["Reasonix Studio is starting", "The first start after installing or rebooting can take a minute."],
  zh: ["Reasonix Studio 正在启动", "安装或重启电脑后的首次启动可能需要一分钟。"],
};

function startingPage(locale) {
  const [title, note] = String(locale).toLowerCase().startsWith("zh") ? TEXT.zh : TEXT.en;
  return `<!doctype html><meta charset="utf-8"><title>${title}</title><style>
:root{color-scheme:light dark;--bg:#fafafa;--fg:#1c1c1e;--mute:#6b6b70}
@media(prefers-color-scheme:dark){:root{--bg:#1b1b1d;--fg:#ececee;--mute:#9a9aa0}}
body{margin:0;height:100vh;display:flex;flex-direction:column;justify-content:center;gap:8px;padding:0 28px;box-sizing:border-box;background:var(--bg);color:var(--fg);font:14px system-ui,sans-serif}
h1{margin:0;font-size:16px;font-weight:600}p{margin:0;color:var(--mute);line-height:1.5}
.bar{height:3px;margin-top:10px;border-radius:2px;background:var(--mute);opacity:.35;overflow:hidden;position:relative}
.bar::after{content:"";position:absolute;inset:0 70% 0 0;background:var(--fg);animation:run 1.4s ease-in-out infinite}
@keyframes run{from{transform:translateX(-100%)}to{transform:translateX(340%)}}
</style><h1>${title}</h1><p>${note}</p><div class="bar"></div>`;
}

// showStarting is the only sign of life while the kernel has not answered: until
// it does there is no window of the application's own to draw.
function showStarting(locale) {
  const win = new BrowserWindow({
    width: 380,
    height: 150,
    resizable: false,
    minimizable: false,
    maximizable: false,
    frame: true,
    title: "Reasonix Studio",
    autoHideMenuBar: true,
    // The page's own scheme only applies after its first paint, and the
    // default is white: on a dark system the window showed white for a beat
    // before turning over. These are the two --bg values the page styles.
    backgroundColor: nativeTheme.shouldUseDarkColors ? "#1b1b1d" : "#fafafa",
    webPreferences: { sandbox: true, contextIsolation: true, nodeIntegration: false },
  });
  win.removeMenu();
  void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(startingPage(locale))}`);
  return win;
}

module.exports = { showStarting, startingPage };
