import { useEffect, useState } from "react";
import { mutation } from "./sink";

// V — modality independence. A button and a shortcut are two entry points and
// one intent, and only the source saying so makes them one: the button carries
// the id as an attribute, the shortcut table carries it as a field.
export function V() {
  useEffect(() => {
    const table = [{ chord: "v", action: "fx.open", run: () => mutation() }];
    const onKey = (e: KeyboardEvent) => table.find((s) => s.chord === e.key)?.run();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return (
    <button data-action="fx.open" onClick={() => mutation()}>
      v
    </button>
  );
}

// W — location independence. The same intent rendered in two places stays one
// action, and the two roots stay two roots.
export function W1() {
  return (
    <button data-action="fx.close" onClick={() => mutation()}>
      w1
    </button>
  );
}
// X — staged. Opening the confirmation writes nothing, dismissing it writes
// nothing, confirming writes. The action is mutating because one member is, and
// the click that opens it keeps its own read-only verdict — the join never
// reaches back down.
export function X() {
  const [confirming, setConfirming] = useState(false);
  return (
    <div>
      <button data-action="fx.remove" onClick={() => setConfirming(true)}>
        x
      </button>
      {confirming && (
        <>
          <button data-action="fx.remove" data-value="cancel" onClick={() => setConfirming(false)}>
            no
          </button>
          <button data-action="fx.remove" data-value="confirm" onClick={() => mutation()}>
            yes
          </button>
        </>
      )}
    </div>
  );
}
