import { mutation } from "./sink";

// The other half of W: same declared action, a different file. Location is not
// part of an action's identity, and the two roots stay two roots.
export function W2() {
  return (
    <button data-action="fx.close" onClick={() => mutation()}>
      w2
    </button>
  );
}
