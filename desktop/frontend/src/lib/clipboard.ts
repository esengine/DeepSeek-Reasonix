// clipboard provides a cross-platform clipboard write with three fallback layers:
//   1. navigator.clipboard.writeText (modern async API)
//   2. window.runtime.ClipboardSetText (Wails desktop runtime)
//   3. document.execCommand("copy") via a hidden textarea (legacy webview fallback)

function fallbackCopyText(value: string): boolean {
  const activeElement = document.activeElement;
  const selection = document.getSelection();
  const ranges: Range[] = [];
  if (selection) {
    for (let index = 0; index < selection.rangeCount; index += 1) {
      ranges.push(selection.getRangeAt(index));
    }
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.inset = "0 auto auto 0";
  textarea.style.width = "1px";
  textarea.style.height = "1px";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } finally {
    textarea.remove();
    if (selection) {
      selection.removeAllRanges();
      for (const range of ranges) selection.addRange(range);
    }
    if (activeElement instanceof HTMLElement) activeElement.focus();
  }
  return ok;
}

/**
 * Write text to the clipboard. Tries three strategies in order:
 * 1. The modern `navigator.clipboard.writeText()` async API
 * 2. The Wails desktop runtime `ClipboardSetText` binding
 * 3. A legacy textarea + `document.execCommand("copy")` fallback
 *
 * Resolves silently (returns void) — callers should treat failures as
 * best-effort. The function never throws.
 */
export async function writeClipboardText(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
    return;
  } catch {
    /* try the desktop runtime below */
  }
  try {
    if (typeof window !== "undefined" && (await window.runtime?.ClipboardSetText?.(value))) return;
  } catch {
    /* runtime unavailable in browser dev */
  }
  try {
    fallbackCopyText(value);
  } catch {
    /* all strategies exhausted */
  }
}
