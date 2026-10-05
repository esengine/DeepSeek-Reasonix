import type { KeyboardEvent } from "react";
import { mutation } from "./sink";

// EP2's helper lives at module scope on purpose: it is an ordinary function
// whose parameter happens to be spelled like an event, and nothing about that
// spelling may reach the event contract.
function helper(e: { preventDefault: () => void }) {
  e.preventDefault();
}

// EP1 — the event is the binding, not the name. This handler's parameter is
// what the certified keydown delivers, whatever it is called, so the control
// operation on it is proven and leaves nothing open.
export function EP1() {
  const onKey = (whatever: KeyboardEvent) => {
    whatever.preventDefault();
  };
  return <input onKeyDown={onKey} />;
}

// EP2 — the same spelling on a function no event reaches. `helper` is called
// by a handler; it is not the handler, and its parameter is its own.
export function EP2() {
  return <input onKeyDown={() => helper({ preventDefault: () => {} })} />;
}

// EP3 — parameter zero is what the source delivers, and only that one. Both
// calls are written identically; the first is on the event this handler
// receives and the second is on an argument its caller supplies.
export function EP3() {
  const handler = (x: KeyboardEvent, extra?: { preventDefault: () => void }) => {
    x.preventDefault();
    if (extra) extra.preventDefault();
  };
  return <input onKeyDown={handler} />;
}

// EP4 — a member of the event that no contract covers. The receiver being
// proven says what the receiver is, never what everything reachable on it does.
export function EP4() {
  return (
    <input
      onKeyDown={(e) => {
        if (e.getModifierState("Shift")) mutation();
      }}
    />
  );
}

// EP5 — an inner function takes the name back. From there down the binding is
// its own, and the event's contract stops at the scope that owns the name.
export function EP5() {
  return (
    <input
      onKeyDown={(e) => {
        function inner(e: { preventDefault: () => void }) {
          e.preventDefault();
        }
        void e;
        inner({ preventDefault: () => {} });
      }}
    />
  );
}

// EP6 — through the event is not the event. What currentTarget is has its own
// provenance, and it is not this fact's to answer.
export function EP6() {
  return <input onKeyDown={(e) => e.currentTarget.blur()} />;
}
