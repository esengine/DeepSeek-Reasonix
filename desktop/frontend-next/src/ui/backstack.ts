import { useEffect, useRef } from "react";
import { host } from "../port/host";

/**
 * What a back gesture should close, innermost last.
 *
 * The system back gesture belongs to the shell — the page never sees it — but
 * only the page knows what is open. So the page keeps this stack, tells the
 * shell whether it is empty, and closes the top itself when asked. The shell
 * keeps the decision about what back *means* when nothing is open (leave the
 * screen, keep the app).
 *
 * A stack rather than a count because closing must hit the innermost layer: a
 * menu over the settings sheet has to go before the sheet does, which is the
 * same order `dismiss.ts` gives Escape by claiming it in the capture phase.
 *
 * Registered by the layers that Escape already closes, so the two gestures agree
 * by construction rather than by two lists that drift.
 */
const stack: Array<() => void> = [];

/** What the shell was last told, so a change is reported once rather than on
 *  every push and pop. */
let reported = false;

function publish() {
  const open = stack.length > 0;
  if (open === reported) return;
  reported = open;
  host().reportLayerOpen?.(open);
}

export function pushBackLayer(close: () => void): () => void {
  stack.push(close);
  publish();
  return () => {
    const at = stack.lastIndexOf(close);
    if (at >= 0) stack.splice(at, 1);
    publish();
  };
}

/** Close the innermost layer, answering whether there was one to close. */
export function closeTopLayer(): boolean {
  const top = stack.pop();
  if (!top) return false;
  publish();
  top();
  return true;
}

/**
 * Register a layer for the back gesture while it is open.
 *
 * The close callback is held in a ref: callers pass a fresh arrow every render,
 * and re-registering on each one would repeatedly move this layer to the top of
 * the stack — which is where the *innermost* layer belongs, not the most
 * recently rendered.
 */
export function useBackLayer(open: boolean, close: () => void) {
  const latest = useRef(close);
  latest.current = close;
  useEffect(() => {
    if (!open) return;
    return pushBackLayer(() => latest.current());
  }, [open]);
}

// The shell's way in. On `window` rather than behind a bridge method because the
// shell's back handler runs on the main thread and must not wait for a JS round
// trip to learn whether anything closed.
//
// Guarded because this module is imported for its hooks by components that are
// also rendered outside a browser — a test running under node imports the chain
// this file sits in, and an unguarded assignment here throws
// `window is not defined` at import time, taking down every test that touches
// the component rather than failing one assertion.
declare global {
  interface Window {
    __reasonixBack?: () => boolean;
  }
}

if (typeof window !== "undefined") {
  window.__reasonixBack = closeTopLayer;
}
