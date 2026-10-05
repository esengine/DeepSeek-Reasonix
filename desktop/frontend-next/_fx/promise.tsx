import { mutation } from "./sink";

// The declaration every proof below bottoms out in: a return type the source
// states, on a member of a type a receiver is annotated with. `hold` is the
// control that matters most — same receiver, same member spelling downstream,
// and a declared return type that is not a Promise.
type Thenable = { then: (cb: () => void) => void };
interface Api {
  load(): Promise<string>;
  hold(): Thenable;
}
declare const api: Api;
// Three receivers nothing here proves: a call no type covers, a binding the
// source types Promise, and an object whose member is spelled the same way.
declare const mystery: () => Thenable;
declare const loading: Promise<void>;
declare const weird: Thenable;
declare const pickHandler: () => () => void;

const clean = () => {};

// PC1 — the certified return. The callback runs later and may run, so the write
// is reached and the edge carrying it is deferred.
export function PC1() {
  return <button onClick={() => void api.load().then(() => mutation())}>pc1</button>;
}

// PC2 — the fixpoint. `.catch` is a continuation on the result of `.then`,
// which is a continuation on the certified call: three receivers deep, and
// nothing about the depth is bounded.
export function PC2() {
  return (
    <button
      onClick={() =>
        void api
          .load()
          .then(clean)
          .catch(() => mutation())
          .finally(clean)
      }
    >
      pc2
    </button>
  );
}

// PC3 — the second slot of `.then` is the rejection handler, which is the edge
// `.catch` carries. Both branches may run.
export function PC3() {
  return <button onClick={() => void api.load().then(clean, () => mutation())}>pc3</button>;
}

// PC4 — an opaque call result. The member is spelled the same and proves
// nothing, so the write behind it stays unreachable rather than clean.
export function PC4() {
  return <button onClick={() => mystery().then(() => mutation())}>pc4</button>;
}

// PC5 — a binding the source types Promise. Its own source, measured and
// deliberately not taken: this cut certifies two, and this is not one of them.
export function PC5() {
  return <button onClick={() => void loading.then(() => mutation())}>pc5</button>;
}

// PC6 — the receiver is the proven type from PC1 and the member is spelled
// then, and the declared return type is the only difference. If a spelling ever
// buys the edge again, this is the case that turns.
export function PC6() {
  return <button onClick={() => api.hold().then(() => mutation())}>pc6</button>;
}

// PC7 — optional shapes are calls. Reaching the write through the same fact,
// with the optionality kept on the evidence rather than in the conclusion.
export function PC7() {
  return <button onClick={() => void api.load()?.then?.(() => mutation())}>pc7</button>;
}

// PC8 — a member taken off the object is an ordinary call. A proven Promise
// standing next to it is not a proof about it.
export function PC8() {
  const p = api.load();
  void p;
  const { then } = weird;
  return <button onClick={() => then(() => mutation())}>pc8</button>;
}

// PC9 — a proven continuation handed something that is neither a function nor a
// name. The continuation is proven and the callback is not, and those are two
// answers rather than one clean one.
export function PC9() {
  return <button onClick={() => void api.load().then(pickHandler())}>pc9</button>;
}
