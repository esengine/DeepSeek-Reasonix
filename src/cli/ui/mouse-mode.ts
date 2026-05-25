// Reasonix is append-only now: the terminal owns scrollback, copy, and the
// mouse wheel. On most terminals, startup emits disables for common
// mouse-capture modes so stale state from a prior crashed TUI can't keep
// eating wheel events. Apple Terminal has had native crashes in its renderer
// after receiving these private mouse-mode toggles, so its default is silent.
// REASONIX_MOUSE_MODE remains an escape hatch.

type Mode = "alternate-scroll" | "sgr" | "off" | "apple-terminal-off";
export type MouseHistoryMode = "native" | "app";

function readMode(historyMode: MouseHistoryMode): Mode {
  const raw = (process.env.REASONIX_MOUSE_MODE ?? "").toLowerCase();
  if (raw === "sgr") return "sgr";
  if (raw === "alternate-scroll") return "alternate-scroll";
  if (raw === "off") return "off";
  if (process.env.TERM_PROGRAM === "Apple_Terminal" && raw === "") return "apple-terminal-off";
  return historyMode === "app" ? "sgr" : "off";
}

const RESET_ALL =
  "\u001b[?9l\u001b[?1000l\u001b[?1002l\u001b[?1003l\u001b[?1005l\u001b[?1006l\u001b[?1007l\u001b[?1015l";

const SEQUENCES: Record<Mode, { enable: string; disable: string }> = {
  "alternate-scroll": { enable: "\u001b[?1007h", disable: "\u001b[?1007l" },
  sgr: { enable: "\u001b[?1000h\u001b[?1006h", disable: "\u001b[?1006l\u001b[?1000l" },
  off: { enable: RESET_ALL, disable: "" },
  "apple-terminal-off": { enable: "", disable: "" },
};

let active = false;
let activeMode: Mode = "off";

export function enableMouseMode(historyMode: MouseHistoryMode = "native"): void {
  if (active) return;
  if (!process.stdout.isTTY) return;
  activeMode = readMode(historyMode);
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
