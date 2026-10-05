"use strict";
const path = require("node:path");

// macOS takes the window icon from the bundle, never from the window, so naming
// one there is not a smaller version of the same thing — it is ignored. The two
// platforms that do read it want different formats: .ico carries the small sizes
// Windows draws in the taskbar, and GTK reads a single PNG.
function iconFile(platform) {
  switch (platform) {
    case "win32":
      return "icon.ico";
    case "darwin":
      return null;
    default:
      return "icon.png";
  }
}

// appIcon is spread into the BrowserWindow options, so the key is absent rather
// than undefined where the platform owns the icon itself.
function appIcon(platform = process.platform) {
  const file = iconFile(platform);
  return file ? { icon: path.join(__dirname, "..", "assets", file) } : {};
}

module.exports = { appIcon, iconFile };
