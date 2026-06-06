// Placeholder hook for a future "capture stdin before React mounts" feature.
// Some host CLIs need to buffer keystrokes typed during boot so they aren't
// dropped before the input reader is wired up. We keep the export so call
// sites compile, but right now there is nothing to stop.
export function stopCapturingEarlyInput(): void {
  /* intentionally empty until early-input buffering is implemented */
}
