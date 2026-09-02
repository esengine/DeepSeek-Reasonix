type CrashEventLike = { error?: unknown };

const WAILS_RUNTIME_FRAME_RE = /https?:\/\/[^/]+\/wails\/[\w.-]+\.js/;

// Ignore errors whose complete stack belongs to the injected Wails runtime.
// App frames mixed into the stack still report normally.
export function isWailsRuntimeOnlyCrashEvent(event: CrashEventLike): boolean {
  const error = event.error;
  if (!error || typeof error !== "object") return false;
  const stack = (error as { stack?: unknown }).stack;
  if (typeof stack !== "string") return false;
  const frames = stack
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.startsWith("at ") || line.includes("@http"));
  return frames.length > 0 && frames.every((line) => WAILS_RUNTIME_FRAME_RE.test(line));
}
