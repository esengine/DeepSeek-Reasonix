import { mutation } from "./sink";

declare const nope: (() => void) | undefined;
declare const t1: { go?: () => void };
declare const t2: { go: () => void } | undefined;
declare const t3: { go?: () => void } | undefined;
let saved: (() => void) | null = null;

// A function that runs the callback it is handed, through an optional call.
function invoke(cb?: () => void) {
  cb?.();
}
// One that only keeps it. Holding a callback is not calling it, and an optional
// call must not make that difference disappear either.
function store(cb: () => void) {
  saved = cb;
  void saved;
}

// O1/O2 — the golden pair. One character apart. The question is whether an
// execution can reach the write, and a call that may be skipped may happen, so
// both are mutations; only the evidence differs.
export function O1() {
  return <button onClick={() => mutation()}>o1</button>;
}
export function O2() {
  return <button onClick={() => mutation?.()}>o2</button>;
}

// O3 — an optional call on a name nothing places is unknown. Never clean: the
// direction that reads "it might not run, so nothing happens" is the defect.
export function O3() {
  return <button onClick={() => nope?.()}>o3</button>;
}

// O4/O5 — the parameter a function executes, reached optionally, and the
// control that only stores it.
export function O4() {
  return <button onClick={() => invoke(() => mutation())}>o4</button>;
}
export function O5() {
  return <button onClick={() => store(() => mutation())}>o5</button>;
}

// O6/O7/O8 — the three shapes Babel gives an optional member call. None of them
// is a proof about the receiver: an optional chain says the access may be
// skipped, never that what it reaches is known.
export function O6() {
  return <button onClick={() => t1.go?.()}>o6</button>;
}
export function O7() {
  return <button onClick={() => t2?.go()}>o7</button>;
}
export function O8() {
  return <button onClick={() => t3?.go?.()}>o8</button>;
}
