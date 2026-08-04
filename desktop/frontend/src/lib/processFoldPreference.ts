export type ProcessFoldPreference = "auto" | "collapsed" | "expanded";

const PROCESS_FOLD_KEY = "reasonix-process-fold";
const PROCESS_FOLD_EVENT = "reasonix:process-fold";

export function getProcessFoldPreference(): ProcessFoldPreference {
  if (typeof localStorage === "undefined") return "auto";
  const stored = localStorage.getItem(PROCESS_FOLD_KEY);
  return stored === "collapsed" || stored === "expanded" ? stored : "auto";
}

export function setProcessFoldPreference(pref: ProcessFoldPreference): void {
  localStorage.setItem(PROCESS_FOLD_KEY, pref);
  window.dispatchEvent(new CustomEvent(PROCESS_FOLD_EVENT, { detail: pref }));
}

export function onProcessFoldPreferenceChange(cb: (pref: ProcessFoldPreference) => void): () => void {
  const handler = (e: Event) => {
    const pref = (e as CustomEvent).detail;
    if (pref === "auto" || pref === "collapsed" || pref === "expanded") cb(pref);
  };
  window.addEventListener(PROCESS_FOLD_EVENT, handler);
  return () => window.removeEventListener(PROCESS_FOLD_EVENT, handler);
}
