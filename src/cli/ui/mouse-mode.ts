// Reasonix is append-only now: the terminal owns scrollback, copy, and the
// mouse wheel. We never want to capture the wheel, and on startup we emit
// disables for every common mouse-capture mode so stale state from a prior
// crashed TUI in the same terminal can't keep eating wheel events. The
// REASONIX_MOUSE_MODE env var stays as an escape hatch.

type Mode = "alternate-scroll" | "sgr" | "off";

function readMode(): Mode {
  const raw = (process.env.REASONIX_MOUSE_MODE ?? "").toLowerCase();
  if (raw === "sgr") return "sgr";
  if (raw === "alternate-scroll") return "alternate-scroll";
  return "off";
}

const RESET_ALL =
  "\u001b[?9l\u001b[?1000l\u001b[?1002l\u001b[?1003l\u001b[?1005l\u001b[?1006l\u001b[?1007l\u001b[?1015l";

const SEQUENCES: Record<Mode, { enable: string; disable: string }> = {
  "alternate-scroll": { enable: "\u001b[?1007h", disable: "\u001b[?1007l" },
  sgr: { enable: "\u001b[?1000h\u001b[?1006h", disable: "\u001b[?1006l\u001b[?1000l" },
  off: { enable: RESET_ALL, disable: "" },
};

let active = false;
let activeMode: Mode = "off";

export function enableMouseMode(): void {
  if (active) return;
  if (!process.stdout.isTTY) return;
  activeMode = readMode();
  const seq = SEQUENCES[activeMode].enable;
  if (seq) process.stdout.write(seq);
  active = true;
}

export function disableMouseMode(): void {
  if (!active) return;
  const seq = SEQUENCES[activeMode].disable;
  if (seq) process.stdout.write(seq);
  active = false;
}
