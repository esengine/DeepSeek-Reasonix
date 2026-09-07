import { mutation } from "./sink";

const clean = () => {};
declare const opaque: { onGo?: () => void; other?: number };
declare const flag: boolean;

type P = { onGo?: () => void; other?: number };

// S1 — the explicit attribute is written after the spread, so it is the one that
// provides the prop whatever the spread carries.
function A1({ onGo }: P) {
  return <button onClick={() => onGo?.()}>s1</button>;
}
export function S1() {
  return <A1 {...opaque} onGo={clean} />;
}

// S2 — the same two attributes the other way round. The spread may replace the
// explicit one and nothing here can read it, so the answer is not `clean`: an
// attribute a later spread might overwrite is not the source.
function A2({ onGo }: P) {
  return <button onClick={() => onGo?.()}>s2</button>;
}
export function S2() {
  return <A2 onGo={clean} {...opaque} />;
}

// S3 — a spread whose object is written out. Every key is a plain name, so what it
// carries for this slot is exact.
function A3({ onGo }: P) {
  return <button onClick={() => onGo?.()}>s3</button>;
}
export function S3() {
  return <A3 {...{ onGo: mutation }} />;
}

// S4 — a readable spread that proves it does not carry the slot, to the left.
function A4({ onGo }: P) {
  return <button onClick={() => onGo?.()}>s4</button>;
}
export function S4() {
  return <A4 {...{ other: 1 }} onGo={clean} />;
}

// S5 — the same proof of absence, to the right.
function A5({ onGo }: P) {
  return <button onClick={() => onGo?.()}>s5</button>;
}
export function S5() {
  return <A5 onGo={clean} {...{ other: 1 }} />;
}

// S6 — a readable spread, an explicit attribute, and an unreadable spread last. The
// rightmost one decides and cannot be read, so the attribute in the middle is
// not the answer either.
function A6({ onGo }: P) {
  return <button onClick={() => onGo?.()}>s6</button>;
}
export function S6() {
  return <A6 {...{ other: 1 }} onGo={clean} {...opaque} />;
}

// S7 — a conditional object. Answering it means evaluating the branch, which this
// pass does not do; pinned as the boundary rather than given an evaluator to
// make a fixture green.
function A7({ onGo }: P) {
  return <button onClick={() => onGo?.()}>s7</button>;
}
export function S7() {
  return <A7 {...(flag ? { onGo: mutation } : { onGo: clean })} />;
}

