import { useEffect, useState } from "react";
// A name from outside the scanned tree: its body is not here, so a call to it
// is an open edge rather than a proven one. That is what these fixtures need —
// an axis that answers "unknown" without answering "mutation".
import { helper } from "outside-the-tree";
import { mutation } from "./sink";

// J1 — the dependency axis cannot close, and the lifecycle axis has a witness.
// A proven mutation is not erased by another axis not knowing.
function J1Child() {
  useEffect(() => {
    mutation();
  }, []);
  return <i>j1</i>;
}
export function J1() {
  const [show, setShow] = useState(false);
  useEffect(() => {
    helper();
  }, [show]);
  return (
    <div>
      <button onClick={() => setShow(true)}>j1</button>
      {show ? <J1Child /> : null}
    </div>
  );
}

// J2 — one axis is proven clean and the other does not know. One vote of
// read-only and one of unknown is unknown, never read-only.
function J2Child() {
  useEffect(() => {
    helper();
  }, []);
  return <i>j2</i>;
}
export function J2() {
  const [show, setShow] = useState(false);
  return (
    <div>
      <button onClick={() => setShow(true)}>j2</button>
      {show ? <J2Child /> : null}
    </div>
  );
}

// J3 — every reachable axis proven, nothing left open. Only this is read-only.
function J3Child() {
  useEffect(() => {
    /* nothing reaching a host */
  }, []);
  return <i>j3</i>;
}
export function J3() {
  const [show, setShow] = useState(false);
  useEffect(() => {
    if (show) return;
  }, [show]);
  return (
    <div>
      <button onClick={() => setShow(true)}>j3</button>
      {show ? <J3Child /> : null}
    </div>
  );
}
