import { memo } from "react";
import { mutation } from "./sink";

const clean = () => {};

// M1 — no wrapper. The render target and the body are one name, which is the
// case every reader already handled.
function DirectBody({ onGo }: { onGo: () => void }) {
  return <button onClick={() => onGo()}>m1</button>;
}
export function M1() {
  return <DirectBody onGo={mutation} />;
}

// M2 — the certified React wrapper, and the only way this body is rendered.
// `Public` is a render target of `WrappedBody`, so a prop passed at <Public> is
// an actual of WrappedBody's formal. Without the alias the body has no render
// site at all and its prop resolves to nothing.
function WrappedBody({ onGo }: { onGo: () => void }) {
  return <button onClick={() => onGo()}>m2</button>;
}
const Public = memo(WrappedBody);
export function M2() {
  return <Public onGo={mutation} />;
}

// M4 — two bodies behind two wrappers. Identity is per body, so the writing
// prop at one may not reach the other.
function AView({ onGo }: { onGo: () => void }) {
  return <button onClick={() => onGo()}>a</button>;
}
function BView({ onGo }: { onGo: () => void }) {
  return <button onClick={() => onGo()}>b</button>;
}
const A = memo(AView);
const B = memo(BView);
export function M4() {
  return (
    <>
      <A onGo={mutation} />
      <B onGo={clean} />
    </>
  );
}

// M5 — a plain rebinding of an alias. This resolver records `const X = w(inner)`
// and nothing else, so `const Twice = Once` is not a hop it knows; the prop
// stays unresolved rather than being followed. Pinned as the boundary it is:
// the fixture says what the pass does, and inventing the hop to make a fixture
// green is how a resolver acquires behaviour nobody decided to give it.
function ChainBody({ onGo }: { onGo: () => void }) {
  return <button onClick={() => onGo()}>m5</button>;
}
const Once = memo(ChainBody);
const Twice = Once;
export function M5() {
  return <Twice onGo={mutation} />;
}
