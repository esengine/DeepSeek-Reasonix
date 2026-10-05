// A touch-first device has no hardware Enter key to reach for, so Enter belongs
// to the textarea and the visible button owns submission. A mouse-first hybrid
// still gets the desktop shortcut even if it also has a touchscreen.
export function touchKeyboard(): boolean {
  return typeof window !== "undefined" && window.matchMedia?.("(pointer: coarse)").matches === true;
}
