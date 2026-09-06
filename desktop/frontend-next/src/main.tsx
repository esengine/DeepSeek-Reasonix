import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { boot as bootLang } from "./i18n";
import { reason } from "./i18n/kernel";
import { track as trackWidth } from "./ui/viewport";
import "./styles/tokens.css";
import "./styles/app.css";
import { App } from "./ui/App";
import { SseHub } from "./port/hub";
import type { HubPort } from "./port/hub";
import { install as installFileDrop } from "./ui/filedrop";
import { host } from "./port/host";

// The dev proxy only exists when REASONIX_SERVE was set at vite start; probing
// /status decides which port to boot on, so neither mode needs a build flag.
// Without the proxy vite answers /status with the SPA shell at 200, so the
// content type — not res.ok — is what says a kernel is really there.
async function pick(): Promise<HubPort> {
  try {
    const res = await fetch("/status", { credentials: "same-origin" });
    if (res.ok && (res.headers.get("content-type") ?? "").includes("json")) return new SseHub();
  } catch {
    // no serve reachable
  }
  // A shipped build is served by the kernel it talks to. Falling back to the
  // fixture there would put a scripted session on screen as if it had happened,
  // so only a dev build is allowed to; the import stays dynamic to keep the
  // fixture out of the production bundle.
  if (!import.meta.env.DEV) throw new Error("连不上内核：/status 没有回应。");
  const { MockHub } = await import("./port/mock_hub");
  return new MockHub();
}

// A shell that hides the native title bar lets its lights float over the page,
// so the chrome has to reserve their corner and be draggable itself. Which of
// those is true is the shell's answer, not something the page infers from a
// platform or from which globals it can see.
void host()
  .describe()
  .then(({ shell, platform, titleBar }) => {
    const root = document.documentElement.dataset;
    root.shell = shell;
    if (platform) root.platform = platform;
    if (titleBar) root.titlebar = "app";
  });

// macOS hides its traffic lights outright on an inactive window rather than
// greying them, so the corner they were reserved is simply empty. The wordmark
// takes the slot back while they are gone; the slot itself never resizes.
const focus = () => {
  document.documentElement.dataset.focused = document.hasFocus() ? "yes" : "no";
};
addEventListener("focus", focus);
addEventListener("blur", focus);
focus();

// Before the first paint: a language that arrives after one would show the
// interface in one language and then swap it.
bootLang();
trackWidth();

// The boot screen is markup in index.html, so it is on screen before this
// bundle is parsed. Handing the window over is this file's job: nothing below
// it has to know the window was ever empty.
const BOOT_HOLD_MS = 260;
const BOOT_CAP_MS = 2600;
const BOOT_OUT_MS = 900;

/** settled resolves when the boot screen has finished introducing itself: the
 *  mark written out, the line under it up. Read off the running animations
 *  rather than counted in milliseconds here, or every retune of that CSS
 *  silently starts cutting it off again. The sun and the shafts loop forever
 *  and are filtered out; the cap is for an animation that never finishes. */
function settled(boot: Element): Promise<unknown> {
  const intro = boot
    .getAnimations({ subtree: true })
    .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
    .map((a) => a.finished.catch(() => undefined));
  return Promise.race([
    Promise.all(intro),
    new Promise((done) => setTimeout(done, BOOT_CAP_MS)),
  ]);
}

function arrive() {
  const root = document.documentElement;
  // Arms the app's own entrance, then lets go of it: a .app that remounts later
  // — the welcome screen giving way to a session — must not replay the window
  // opening. Set before the render lands so the animation starts on mount.
  root.dataset.boot = "in";
  setTimeout(() => delete root.dataset.boot, 2200);
  const boot = document.getElementById("boot");
  if (!boot) return;
  const dismiss = () => {
    boot.dataset.done = "";
    setTimeout(() => boot.remove(), BOOT_OUT_MS);
  };
  // Two frames: the first commits the tree, the second is the one it is
  // painted in. Dissolving before that uncovers the empty page underneath.
  requestAnimationFrame(() =>
    requestAnimationFrame(() => {
      void settled(boot).then(() => setTimeout(dismiss, BOOT_HOLD_MS));
    }),
  );
}

const root = createRoot(document.getElementById("root")!);
pick().then(
  (hub) => {
    // Before the first render, and not from whichever view happens to want a
    // drop: a window with no drop target mounted still has to refuse a file, or
    // the webview navigates to it and the app is replaced by what was dropped.
    installFileDrop(hub);
    root.render(
      <StrictMode>
        <App hub={hub} />
      </StrictMode>,
    );
    arrive();
  },
  (e: unknown) => {
    root.render(
      <div className="app" data-run="idle">
        <div className="errbar" role="alert">
          <span>{reason(e)}</span>
        </div>
      </div>,
    );
    // The failure is the one thing that must not stay behind the boot screen.
    arrive();
  },
);
