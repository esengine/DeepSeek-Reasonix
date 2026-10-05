import { useMemo, useState } from "react";
import { mutation } from "./sink";
import { useEffect } from "react";

// N — the dependency is a prop, and the prop is the parent's state.
function NChild({ value }: { value: boolean }) {
  useEffect(() => {
    mutation();
  }, [value]);
  return <i>n</i>;
}
export function N() {
  const [x, setX] = useState(false);
  return (
    <div>
      <button onClick={() => setX(!x)}>n</button>
      <NChild value={x} />
    </div>
  );
}

// O — two hops of forwarding. The edge has to be carried by the actual/formal
// pair at each site, not by the prop happening to keep the same name.
function OChild({ value }: { value: boolean }) {
  useEffect(() => {
    mutation();
  }, [value]);
  return <i>o</i>;
}
function OMiddle({ value }: { value: boolean }) {
  return <OChild value={value} />;
}
export function O() {
  const [x, setX] = useState(false);
  return (
    <div>
      <button onClick={() => setX(!x)}>o</button>
      <OMiddle value={x} />
    </div>
  );
}

// P — the prop's contents have nothing to do with x, and the effect still
// reruns: a write to x rerenders the parent, the literal is built again, and
// Object.is says the dependency changed. This is what separates identity
// instability from data provenance.
function PChild({ config }: { config: { fixed: boolean } }) {
  useEffect(() => {
    mutation();
  }, [config]);
  return <i>p</i>;
}
export function P() {
  const [x, setX] = useState(false);
  return (
    <div>
      <button onClick={() => setX(!x)}>p</button>
      <PChild config={{ fixed: true }} />
    </div>
  );
}

// Q — the same literal, memoised with an empty dependency list. Its identity
// survives the parent's renders, so x reaches nothing. Negative control for P.
function QChild({ config }: { config: { fixed: boolean } }) {
  useEffect(() => {
    mutation();
  }, [config]);
  return <i>q</i>;
}
export function Q() {
  const [x, setX] = useState(false);
  const config = useMemo(() => ({ fixed: true }), []);
  return (
    <div>
      <button onClick={() => setX(!x)}>q</button>
      <QChild config={config} />
    </div>
  );
}

// R — memoised on x. The identity changes exactly when x does, which is the
// cross-component form of the useCallback contract already in use.
function RChild({ config }: { config: { value: boolean } }) {
  useEffect(() => {
    mutation();
  }, [config]);
  return <i>r</i>;
}
export function R() {
  const [x, setX] = useState(false);
  const config = useMemo(() => ({ value: x }), [x]);
  return (
    <div>
      <button onClick={() => setX(!x)}>r</button>
      <RChild config={config} />
    </div>
  );
}
