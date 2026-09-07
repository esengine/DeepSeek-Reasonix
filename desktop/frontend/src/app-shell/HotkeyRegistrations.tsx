import { useShellExpand } from "../lib/shellExpand";
import { useGlobalShortcut } from "../lib/keyboardShortcuts";
import { applyTextSize, DEFAULT_TEXT_SIZE, getTextSize, nextTextSize } from "../lib/textSize";

/** Global hotkey handler for shell-expand toggle (Ctrl/Cmd+B). */
export function ShellHotkeys() {
  const shellExpand = useShellExpand();
  useGlobalShortcut("shell.toggle", () => shellExpand?.toggleLast(), [shellExpand], Boolean(shellExpand));
  return null;
}

/** Global hotkey handler for text-size shortcuts (Ctrl/Cmd + Plus/Minus/0). */
export function TextSizeHotkeys() {
  useGlobalShortcut("textSize.increase", () => applyTextSize(nextTextSize(getTextSize(), 1)));
  useGlobalShortcut("textSize.decrease", () => applyTextSize(nextTextSize(getTextSize(), -1)));
  useGlobalShortcut("textSize.reset", () => applyTextSize(DEFAULT_TEXT_SIZE));
  return null;
}
