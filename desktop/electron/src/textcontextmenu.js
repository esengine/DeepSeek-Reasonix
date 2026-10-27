"use strict";
const { editItems, wordsFor } = require("./editmenu");

const FLAGS = ["editable", "selected", "canUndo", "canRedo", "quote"];
function validRequest(request) {
  return request !== null && typeof request === "object" && !Array.isArray(request)
    && Object.keys(request).length === FLAGS.length + 2
    && FLAGS.every((key) => Object.hasOwn(request, key) && typeof request[key] === "boolean")
    && Number.isFinite(request.x) && Number.isFinite(request.y);
}

function textContextTemplate(request, lang, choose) {
  if (!request.editable && !request.selected) return [];
  let items = editItems(lang, {
    canUndo: request.canUndo, canRedo: request.canRedo,
    canCut: request.selected, canCopy: request.selected, canPaste: request.editable, canSelectAll: true,
  });
  if (!request.editable) items = items.filter((item) => item.role === "copy" || item.role === "selectAll");
  const accelerators = {
    undo: "CommandOrControl+Z",
    redo: process.platform === "darwin" ? "CommandOrControl+Shift+Z" : "CommandOrControl+Y",
    selectAll: "CommandOrControl+A",
  };
  items = items.map((item) => {
    const accelerator = accelerators[item.role];
    if (!accelerator) return item;
    return { id: item.role, label: item.label, enabled: item.enabled, accelerator,
      click: () => choose(item.role) };
  });
  if (request.quote && request.selected) {
    items.push({ type: "separator" }, { label: wordsFor(lang).quote, click: () => choose("quote") });
  }
  return items;
}

function textContextMenuHandler(Menu, fromWindow, originOf, languageOf) {
  const pending = new WeakMap();
  return function showTextContextMenu(event, request) {
    const window = fromWindow(event);
    if (!window || !validRequest(request)) return "";
    const contents = window.webContents;
    if (!event.senderFrame || event.senderFrame !== contents.mainFrame) return "";
    try {
      if (new URL(event.senderFrame.url).origin !== new URL(originOf()).origin) return "";
    } catch {
      return "";
    }
    pending.get(window)?.();
    const template = textContextTemplate(request, languageOf(), (action) => finish(action));
    if (!template.length) return "";
    const menu = Menu.buildFromTemplate(template);
    let finish;
    return new Promise((resolve, reject) => {
      let settled = false;
      const cancel = () => {
        finish("");
        if (!window.isDestroyed() && !contents.isDestroyed?.()) menu.closePopup(window);
      };
      const navigation = (details) => {
        if (details.isMainFrame) cancel();
      };
      finish = (result, error) => {
        if (settled) return;
        settled = true;
        contents.removeListener("did-start-navigation", navigation);
        contents.removeListener("destroyed", cancel);
        window.removeListener("closed", cancel);
        pending.delete(window);
        if (error) reject(error);
        else resolve(result);
      };
      pending.set(window, cancel);
      contents.on("did-start-navigation", navigation);
      contents.once("destroyed", cancel);
      window.once("closed", cancel);
      const zoom = contents.getZoomFactor();
      try {
        menu.popup({ window, x: Math.round(request.x * zoom), y: Math.round(request.y * zoom), callback: () => finish("") });
      } catch (error) {
        finish("", error);
      }
    });
  };
}

module.exports = { textContextTemplate, textContextMenuHandler };
