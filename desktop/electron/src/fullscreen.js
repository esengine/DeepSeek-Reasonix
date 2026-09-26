"use strict";

// Off macOS the shell installs no application menu, and F11 is otherwise bound
// only through a menu role. macOS keeps its own control on the title bar.
function installFullScreenKey(contents, window, platform = process.platform) {
  if (platform === "darwin") return;
  contents.on("before-input-event", (event, input) => {
    if (input.type !== "keyDown" || input.key !== "F11" || input.isAutoRepeat) return;
    if (input.control || input.alt || input.shift || input.meta) return;
    event.preventDefault();
    window.setFullScreen(!window.isFullScreen());
  });
}

module.exports = { installFullScreenKey };
