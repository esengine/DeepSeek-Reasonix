"use strict";

// A page gone blank, whether its tree unmounted or its renderer died, has no
// way back but quitting. The kernel owns every running turn and a loaded page
// finds it again. F5 and Ctrl/Cmd+R belong to the page, so the recovery chord
// is the shifted one.
const CRASH_RELOADS = 3;
const CRASH_WINDOW_MS = 60_000;

function reloadAction(input, platform = process.platform) {
  if (input.type !== "keyDown" || input.isAutoRepeat || input.alt) return null;
  const mod = platform === "darwin" ? input.meta && !input.control : input.control && !input.meta;
  const latin = typeof input.key === "string" && /^[a-z]$/i.test(input.key);
  const isR = latin ? input.key.toLowerCase() === "r" : input.code === "KeyR";
  if (mod && isR) return input.shift ? "hard" : "reload";
  if (!input.control && !input.meta && !input.shift && input.key === "F5") return "reload";
  return null;
}

function installPaneReload(contents, platform = process.platform) {
  contents.on("before-input-event", (event, input) => {
    const action = reloadAction(input, platform);
    if (!action) return;
    event.preventDefault();
    if (action === "hard") contents.reloadIgnoringCache();
    else contents.reload();
  });
}

// A dead renderer's input never reaches before-input-event, so the crash itself
// has to trigger the reload, or for a hidden window the next revive before it is
// shown. A page that dies on every load stops being reloaded.
function installReload(contents, window, { platform = process.platform, now = Date.now } = {}) {
  contents.on("before-input-event", (event, input) => {
    if (reloadAction(input, platform) !== "hard") return;
    event.preventDefault();
    contents.reload();
  });

  let recent = [];
  const recover = () => {
    const at = now();
    recent = recent.filter((t) => at - t < CRASH_WINDOW_MS);
    if (recent.length >= CRASH_RELOADS) return;
    recent.push(at);
    contents.reload();
  };
  contents.on("render-process-gone", (_event, details) => {
    if (details.reason === "clean-exit" || window.isDestroyed() || !window.isVisible()) return;
    recover();
  });
  return {
    revive: () => {
      if (!window.isDestroyed() && contents.isCrashed()) recover();
    },
  };
}

module.exports = { installReload, installPaneReload, reloadAction, CRASH_RELOADS, CRASH_WINDOW_MS };
