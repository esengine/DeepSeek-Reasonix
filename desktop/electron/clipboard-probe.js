// clipboard-probe.js — run inside the Studio Electron shell to capture what a
// file copied from the OS file manager actually looks like on the clipboard.
//
//   cd desktop/electron && electron clipboard-probe.js
//
// 1. Copy a FILE (not text) from Explorer (Windows) or Finder (macOS).
// 2. Focus the probe window and press Ctrl/Cmd+Shift+P, or pick "Probe" in the menu.
//
// Prints, for every clipboard item, its `types` and a readable slice of each
// format's payload, then the repo's own clipboardFiles() result (paths only).
"use strict";

const { app, BrowserWindow, Menu, globalShortcut, clipboard } = require("electron");

// Use the repo's real parser when it is on the current branch; otherwise fall
// back to a byte-faithful replica so the probe still runs anywhere.
let clipboardFiles;
try {
  ({ clipboardFiles } = require("./src/clipboardfiles"));
} catch {
  const { fileURLToPath } = require("node:url");
  const URI_LIST = "text/uri-list";
  const osFormat = (n) => `electron application/osclipboard;format="${n}"`;
  const FILE_NAME_W = osFormat("FileNameW");
  // The bare "FileName" format is ANSI/8.3 and not a reliable source; the
  // Unicode path already comes through text/uri-list, which is what we use.
  const PUBLIC_FILE_URL = osFormat("public.file-url");
  const filePath = (u) => {
    if (!String(u ?? "").toLowerCase().startsWith("file:")) return "";
    try { return fileURLToPath(u); } catch { return ""; }
  };
  const uriListPaths = (t) => {
    const out = [];
    for (const ln of String(t ?? "").split(/\r?\n/)) {
      const s = ln.trim();
      if (!s || s.startsWith("#")) continue;
      const p = filePath(s);
      if (p) out.push(p);
    }
    return out;
  };
  const fileNamePaths = (b) => {
    const t = Buffer.from(b).toString("utf16le");
    return t.split("\0").map((p) => p.trim()).filter(Boolean);
  };
  const fileUrlPaths = (t) => {
    const p = filePath(String(t ?? "").trim());
    return p ? [p] : [];
  };
  // getItemType() resolves to a Blob in this Electron build; read it the same
  // way the repo's clipboardfiles.js does (payload.text / payload.arrayBuffer).
  const readText = async (item, type) => {
    const p = await item.getType(type);
    if (typeof p === "string") return p;
    if (p && typeof p.text === "function") return await p.text();
    return "";
  };
  const readBytes = async (item, type) => {
    const p = await item.getType(type);
    if (typeof p === "string") return Buffer.from(p, "utf16le");
    if (p && typeof p.arrayBuffer === "function") return Buffer.from(await p.arrayBuffer());
    return Buffer.alloc(0);
  };
  clipboardFiles = async (clip) => {
    let items;
    try { items = await clip.read(); } catch { return []; }
    const paths = [];
    for (const item of items) {
      for (const type of item.types) {
        try {
          if (type === URI_LIST) paths.push(...uriListPaths(await readText(item, type)));
          else if (type === FILE_NAME_W) paths.push(...fileNamePaths(await readBytes(item, type)));
          else if (type === PUBLIC_FILE_URL) paths.push(...fileUrlPaths(await readText(item, type)));
        } catch { /* a format that will not decode names nothing */ }
      }
    }
    return [...new Set(paths)];
  };
}

// getType() returns a Blob here, so decode it the same way the repo does.
async function payloadToText(payload) {
  if (typeof payload === "string") return payload;
  if (payload && typeof payload.text === "function") return await payload.text();
  if (payload && typeof payload.arrayBuffer === "function") {
    const buf = Buffer.from(await payload.arrayBuffer());
    return `bytes[${buf.length}] ${JSON.stringify(buf.toString("utf16le", 0, 200))}`;
  }
  return String(payload);
}

async function probe() {
  console.log("\n========== clipboard probe ==========");
  let items;
  try {
    items = await clipboard.read();
  } catch (e) {
    console.log("clipboard.read() threw:", e && e.message);
    return;
  }
  if (!items || items.length === 0) {
    console.log("items: 0 — clipboard is empty. Copy a FILE from Explorer/Finder first, then press Ctrl/Cmd+Shift+P again.");
    console.log("========== end probe ==========\n");
    return;
  }
  console.log("items:", items.length);
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    console.log(`item[${i}].types = ${JSON.stringify(item.types)}`);
    for (const type of item.types) {
      try {
        const payload = await item.getType(type);
        const text = await payloadToText(payload);
        console.log(`  • ${type} => ${text.slice(0, 400)}`);
      } catch (e) {
        console.log(`  • ${type} => <getType error: ${e && e.message}>`);
      }
    }
  }
  try {
    const paths = await clipboardFiles(clipboard);
    console.log("clipboardFiles() =>", JSON.stringify(paths));
  } catch (e) {
    console.log("clipboardFiles() threw:", e && e.message);
  }
  console.log("========== end probe ==========\n");
}

app.whenReady().then(() => {
  const win = new BrowserWindow({ width: 460, height: 150 });
  win.loadURL(
    "data:text/html," +
      encodeURIComponent(
        "<h4>Clipboard probe</h4><p>Copy a <b>file</b> from Explorer/Finder, then press <b>Ctrl/Cmd+Shift+P</b> or choose <b>Probe</b> from the menu.</p>"
      )
  );

  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      { label: "Probe", click: () => probe() },
      { label: "Quit", click: () => app.quit() },
    ])
  );

  globalShortcut.register("CommandOrControl+Shift+P", () => probe());
  console.log("Clipboard probe ready. Copy a FILE, then Ctrl/Cmd+Shift+P (or menu -> Probe).");
});

app.on("window-all-closed", () => app.quit());
