// SGR mouse tracking on/off. Enables the wheel to reach the TUI in every
// terminal (Windows Terminal in particular doesn't translate wheel→arrow
// when alt-screen is active for some users). The trade-off — native
// drag-select stops working — was the reason #514 dropped this; users
// asked for the wheel back because the native select only sees the
// current screen anyway. Shift+drag still selects in most terminals.

const ENABLE = "\u001b[?1000h\u001b[?1006h";
const DISABLE = "\u001b[?1006l\u001b[?1000l";

let active = false;

export function enableMouseMode(): void {
  if (active) return;
  if (!process.stdout.isTTY) return;
  process.stdout.write(ENABLE);
  active = true;
}

export function disableMouseMode(): void {
  if (!active) return;
  process.stdout.write(DISABLE);
  active = false;
}
