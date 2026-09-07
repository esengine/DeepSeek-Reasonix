import { mutation } from "./sink";

// H1 — a formal prop that is called, whose name is not shaped `onX`. Which
// props are followed is decided by that spelling, so this actual is never
// reached and the call is reported as a name the walk could not place. The
// prop's binding says what it is; its spelling says nothing.
//
// Declared as a break rather than fixed: twelve such calls exist in the product
// and none of them reaches a write, so the heuristic costs precision and not
// soundness. This fixture is what proves the tripwire that watches for the day
// that stops being true actually fires.
function Runner({ go }: { go: () => void }) {
  return <button onClick={() => go()}>h1</button>;
}
export function H1() {
  return <Runner go={mutation} />;
}
