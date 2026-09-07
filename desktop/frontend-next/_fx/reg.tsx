import { useEffect } from "react";
import { mutation } from "./sink";

// E1 — the platform's own registration, written bare. Nothing nearer owns the
// name, so the target is the implicit global and the call is an install, not a
// name the walk failed to place. The listener runs when the event happens and
// not when this call does: E1 must be a root, and the effect that installs it
// must not be credited with what it does.
export function E1() {
  useEffect(() => {
    const onKey = () => mutation();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);
  return <i>e1</i>;
}

// E2 — the same spelling as E1, owned by a prop this component destructures.
// A binding wins its own name; the platform's identity is not something a
// spelling confers. This must not be certified as a registration, must not
// become a root, and must reach the ordinary callee flow — an unknown call
// whose callback nothing proves is executed.
export function E2({ addEventListener }: { addEventListener: (t: string, f: () => void) => void }) {
  useEffect(() => {
    addEventListener("keydown", () => mutation());
  }, [addEventListener]);
  return <i>e2</i>;
}

// E3 — a member call on a receiver nothing proves to be an EventTarget.
// `.addEventListener` is a member name, and a member name is not a receiver
// type: this is an unresolved receiver, never a platform registration.
export function E3({ thing }: { thing: { addEventListener: (t: string, f: () => void) => void } }) {
  useEffect(() => {
    thing.addEventListener("keydown", () => mutation());
  }, [thing]);
  return <i>e3</i>;
}

// E4 — removing a listener is not installing one. It takes an entry point away,
// so it may never add a root; and like an install it does not run the listener
// it names.
export function E4() {
  useEffect(() => {
    const onClick = () => mutation();
    removeEventListener("click", onClick);
  }, []);
  return <i>e4</i>;
}
