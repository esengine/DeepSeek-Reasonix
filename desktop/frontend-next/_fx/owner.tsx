import { useEffect, useState } from "react";
import { mutation } from "./sink";

// Any call can hold a component body; what names the scope is the function
// expression's own id, not the binding it happens to be assigned to.
const wrapper = <T,>(f: T) => f;

// S — the body is a named function expression inside a wrapper, under the same
// binding name. The prop chain has to continue through it.
const SChild = wrapper(function SChild({ value }: { value: boolean }) {
  useEffect(() => {
    mutation();
  }, [value]);
  return <i>s</i>;
});
export function S() {
  const [x, setX] = useState(false);
  return (
    <div>
      <button onClick={() => setX(!x)}>s</button>
      <SChild value={x} />
    </div>
  );
}

// T — the wrapper renames it: the site says TView, the body says TInner. The
// two have to be the same component or the whole subtree loses its presence.
const TView = wrapper(function TInner() {
  useEffect(() => {
    mutation();
  }, []);
  return <i>t</i>;
});
export function T() {
  const [show, setShow] = useState(false);
  return (
    <div>
      <button onClick={() => setShow(true)}>t</button>
      {show ? <TView /> : null}
    </div>
  );
}

// U — negative control. Naming a function expression names a scope; it does not
// make it a component. Nothing renders this one, so what it would do on mount
// attaches to nobody's state, and U.x stays read-only.
const unrendered = wrapper(function helper() {
  useEffect(() => {
    mutation();
  }, []);
  return <i>u</i>;
});
export const kept = unrendered;
export function U() {
  const [x, setX] = useState(false);
  return (
    <div>
      <button onClick={() => setX(!x)}>u</button>
    </div>
  );
}
