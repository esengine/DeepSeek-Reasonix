"use strict";

// No menu this shell installs carries a reload role, and a page gone blank,
// whether its tree unmounted or its renderer died, has no way back but
// quitting. The kernel owns every running turn and a loaded page finds it again.
const CRASH_RELOADS = 3;
const CRASH_WINDOW_MS = 60_000;

// A dead renderer's input never reaches before-input-event, so the crash itself
// has to trigger the reload, or for a hidden window the next revive before it is
// shown. A page that dies on every load stops being reloaded.
function installReload(contents, window, { platform = process.platform, now = Date.now } = {}) {
  const mac = platform === "darwin";
  contents.on("before-input-event", (event, input) => {
    if (input.type !== "keyDown" || input.isAutoRepeat || input.alt || input.shift) return;
    const mod = mac ? input.meta && !input.control : input.control && !input.meta;
    const bare = !input.control && !input.meta;
    const chord = mod && (input.key === "r" || input.key === "R" || input.code === "KeyR");
    if (!chord && !(bare && input.key === "F5")) return;
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

module.exports = { installReload, CRASH_RELOADS, CRASH_WINDOW_MS };
