import { ipcRenderer } from "electron";
import { IPC } from "../../shared/ipc.js";

// Runs in every frame of a website view. It exposes nothing to the page and
// only reports trusted user input so the shell can hand the tab to the user.
const KINDS = ["mousedown", "keydown", "wheel", "touchstart", "pointerdown"] as const;
const RATE_LIMIT_MS = 250;
let lastSentAt = 0;

for (const kind of KINDS) {
  window.addEventListener(
    kind,
    (event) => {
      if (!event.isTrusted) return;
      const now = Date.now();
      if (now - lastSentAt < RATE_LIMIT_MS) return;
      lastSentAt = now;
      ipcRenderer.send(IPC.browserTakeover, { kind });
    },
    { capture: true, passive: true },
  );
}
