"use strict";
const { Menu } = require("electron");
const { contextTemplate, menuInstaller } = require("./editmenu");

// Without an application menu a WebContents has no copy, paste or undo at all:
// macOS routes those shortcuts through it, and a window with none reads as a
// broken text editor. Elsewhere the bar would render inside the window, and
// those platforms bind the shortcuts themselves.
const installApplicationMenu = menuInstaller(Menu);

// A production build ships without one, so a right-click in a text field offered
// nothing at all. Roles rather than handlers: the clipboard work belongs to the
// platform, and editFlags is the page's own account of what is possible here.
function installContextMenu(contents, window, languageOf = () => "en") {
  contents.on("context-menu", (_event, params) => {
    const template = contextTemplate(params, languageOf());
    if (!template.length) return;
    Menu.buildFromTemplate(template).popup({ window });
  });
}

module.exports = { installApplicationMenu, installContextMenu };
