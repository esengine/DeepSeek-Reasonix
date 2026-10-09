// Whether anyone can be looking at this window: it is on screen and has the
// keyboard. Decoration that only matters to a watching reader listens here
// instead of keeping its own visibility and focus listeners.

const subs = new Set<(present: boolean) => void>();
let last = true;

export function present(): boolean {
  return !document.hidden && document.hasFocus();
}

function settle() {
  const now = present();
  if (now === last) return;
  last = now;
  subs.forEach((fn) => fn(now));
}

let wired = false;
function wire() {
  if (wired) return;
  wired = true;
  last = present();
  document.addEventListener("visibilitychange", settle);
  window.addEventListener("focus", settle);
  window.addEventListener("blur", settle);
}

/** onPresence calls back when the window gains or loses its audience. */
export function onPresence(fn: (present: boolean) => void): () => void {
  wire();
  subs.add(fn);
  return () => subs.delete(fn);
}
