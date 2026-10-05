"use strict";

const X11 = "--ozone-platform=x11";

function waylandSession(env) {
  const type = (env.XDG_SESSION_TYPE || "").trim().toLowerCase();
  if (type === "wayland") return true;
  if (type === "x11") return false;
  return Boolean(env.WAYLAND_DISPLAY);
}

function hasOzoneSwitch(args) {
  for (const arg of args) {
    if (arg === "--") return false;
    const name = arg.split("=", 1)[0];
    if (name === "--ozone-platform" || name === "--ozone-platform-hint") return true;
  }
  return false;
}

function wantsX11(platform, env, args) {
  if (platform !== "linux" || hasOzoneSwitch(args)) return false;
  const override = (env.REASONIX_OZONE_PLATFORM || "").trim().toLowerCase();
  if (override === "wayland") return false;
  if (override === "x11") return true;
  return waylandSession(env) && Boolean(env.DISPLAY);
}

// Under Wayland with XWayland available the shell runs on X11: some drivers
// fail EGL on Electron's Wayland backend and the app dies before a window
// (#10369). Chromium picks Ozone before this script runs, so the switch only
// takes effect from argv, which means relaunching once with it.
function relaunchForOzonePlatform(app, { platform, env, argv }) {
  const args = argv.slice(1);
  if (!wantsX11(platform, env, args)) return false;
  app.relaunch({ args: [X11, ...args] });
  app.exit(0);
  return true;
}

module.exports = { relaunchForOzonePlatform };
