// Clipboard writes for the desktop shell: the async Clipboard API when the
// webview grants it, the Wails runtime bridge when it does not, and a hidden
// textarea + execCommand as the last resort.

export async function writeClipboardText(value: string): Promise<boolean> {
  try {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return true;
    }
  } catch {
    // Permission denied or unavailable — try the Wails bridge.
  }
  try {
    if (typeof window !== "undefined" && (await window.runtime?.ClipboardSetText?.(value))) {
      return true;
    }
  } catch {
    // Bridge missing or failed — fall through to execCommand.
  }
  return fallbackCopyText(value);
}

// execCommand("copy") needs a selected editable element, so this selects a
// hidden textarea and must hand the user's selection and focus back afterwards.
export function fallbackCopyText(value: string): boolean {
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
  } catch {
    // Some WebViews reject execCommand("copy") with NotAllowedError instead of
    // returning false; treat that as a failed copy, never a thrown rejection, so
    // callers (and writeClipboardText's Promise<boolean> contract) stay honored.
    ok = false;
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
