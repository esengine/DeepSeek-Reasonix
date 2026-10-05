"use strict";
const { app, BrowserWindow } = require("electron");
const { until } = require("./livekit");

async function activationChecks(win, current, check) {
  const { client, origin, tray } = current();
  const contents = win.webContents;
  await contents.executeJavaScript("window.__activationProbe = 'retained'");

  for (const source of ["activate", "second-instance", "tray"]) {
    app.focus({ steal: true });
    const reopen = () => source === "tray" ? tray.icon.emit("click") : app.emit(source);
    win.hide();
    reopen();
    check(`${source} shows the hidden window`, win.isVisible());

    // Continue even on a missing handler so each entry point is weighed.
    win.show();
    win.minimize();
    await until("window minimization", () => win.isMinimized());
    reopen();
    check(`${source} restores the minimized window`, await until("reactivation", () => !win.isMinimized() && win.isVisible(), 5000).then(() => true, () => false));
    win.restore();
    await until("window restoration", () => !win.isMinimized());

    const cover = new BrowserWindow({ width: 300, height: 200, show: true });
    try {
      await cover.loadURL("data:text/html,<title>Activation test</title>");
      app.focus({ steal: true });
      cover.focus();
      await until("cover window focus", () => cover.isFocused());
      reopen();
      check(`${source} focuses the existing window`, await until("reactivation focus", () => win.isFocused(), 5000).then(() => true, () => false));
      reopen();
      reopen();
    } finally {
      cover.destroy();
    }
    check(`${source} reuses one window and renderer`,
      BrowserWindow.getAllWindows().length === 1 && current().win === win && win.webContents === contents);
    check(`${source} preserves in-memory renderer state`,
      await contents.executeJavaScript("window.__activationProbe") === "retained");
    check(`${source} preserves the running host`,
      current().client === client && current().origin === origin && !!(await client.trayPrefs()));
  }
  await contents.executeJavaScript("delete window.__activationProbe");
  app.once("before-quit", () => {
    win.hide();
    for (const source of ["activate", "second-instance"]) {
      app.emit(source);
      check(`${source} cannot reopen a quitting window`, !win.isVisible());
    }
  });
}

module.exports = { activationChecks };
