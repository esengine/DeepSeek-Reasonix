"use strict";

const PREF_KEY = "shell-global-shortcut";
const MAX_BINDING_LENGTH = 128;

function createGlobalShortcut({ globalShortcut, binding = "", save, windowOf, showWindow }) {
  let state = { binding, status: binding ? "unavailable" : "disabled" };
  let closed = false;
  const press = () => {
    if (closed) return;
    const win = windowOf();
    if (win && !win.isDestroyed() && win.isVisible() && !win.isMinimized() && win.isFocused()) win.hide();
    else showWindow();
  };
  function register(value) {
    try {
      return globalShortcut.register(value, press) ? "" : "unavailable";
    } catch {
      return "invalid";
    }
  }
  function apply(value, persist) {
    if (closed) return { ...state, error: "quitting" };
    if (typeof value !== "string" || value.length > MAX_BINDING_LENGTH) return { ...state, error: "invalid" };
    const next = value.trim();
    if (next && !next.split("+").slice(0, -1).some(part => /^(CommandOrControl|CmdOrCtrl|Command|Cmd|Control|Ctrl|Alt|Option|AltGr|Super|Meta)$/i.test(part))) {
      return { ...state, error: "invalid" };
    }
    if (next === state.binding && state.status === "registered") return { ...state };
    if (next) {
      const error = register(next);
      if (error) return { ...state, error };
    }
    try {
      if (persist) save(next);
    } catch {
      if (next) globalShortcut.unregister(next);
      return { ...state, error: "save" };
    }
    if (state.status === "registered" && state.binding !== next) globalShortcut.unregister(state.binding);
    state = { binding: next, status: next ? "registered" : "disabled" };
    return { ...state };
  }
  return {
    start() {
      const result = apply(binding, false);
      if (result.error) state = { binding, status: result.error };
      return { ...state };
    },
    read: () => ({ ...state }),
    apply: value => apply(value, true),
    close() {
      closed = true;
      if (state.status === "registered") globalShortcut.unregister(state.binding);
    },
  };
}

function registerGlobalShortcutIPC(ipcMain, fromWindow, shortcutOf) {
  const authorized = event => fromWindow(event) && event.senderFrame === event.sender.mainFrame;
  ipcMain.handle("shell:global-shortcut", event => authorized(event) ? shortcutOf()?.read() ?? { binding: "", status: "unavailable" } : null);
  ipcMain.handle("shell:set-global-shortcut", (event, value) => authorized(event) ? shortcutOf()?.apply(value) ?? { binding: "", status: "unavailable" } : null);
}

module.exports = { PREF_KEY, MAX_BINDING_LENGTH, createGlobalShortcut, registerGlobalShortcutIPC };
