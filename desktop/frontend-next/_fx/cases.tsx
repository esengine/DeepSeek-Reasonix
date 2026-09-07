import { useCallback, useEffect, useState } from "react";
import { mutation } from "./sink";

// A: the state value is the dependency, and the setup mutates.
export function A() {
  const [x, setX] = useState(false);
  useEffect(() => {
    mutation();
  }, [x]);
  return <button onClick={() => setX(true)}>a</button>;
}

// B: mount only. No dependency change exists to retrigger it.
export function B() {
  const [x, setX] = useState(false);
  useEffect(() => {
    mutation();
  }, []);
  return <button onClick={() => setX(true)}>b</button>;
}

// C: no dependency list — every commit runs it, so every state in the component
// retriggers it without naming any of them.
export function C() {
  const [x, setX] = useState(false);
  useEffect(() => {
    mutation();
  });
  return <button onClick={() => setX(true)}>c</button>;
}

// D: the mutation is in the cleanup. A dependency change runs cleanup before
// the next setup, so the state reaches it exactly as A's does.
export function D() {
  const [x, setX] = useState(false);
  useEffect(
    () => () => {
      mutation();
    },
    [x],
  );
  return <button onClick={() => setX(true)}>d</button>;
}

// E: the setup installs a listener that mutates. Installing is not performing,
// so the state retriggers a registration and nothing else.
export function E() {
  const [x, setX] = useState(false);
  useEffect(() => {
    const onKey = () => {
      mutation();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [x]);
  return <button onClick={() => setX(true)}>e</button>;
}

// F: the dependency is a memoised callable and the state is in that callable's
// own dependency list. Writing x changes the callable's identity, which changes
// the effect's dependency, which reruns the effect.
export function F() {
  const [x, setX] = useState(false);
  const cb = useCallback(() => {}, [x]);
  useEffect(() => {
    mutation();
  }, [cb]);
  return <button onClick={() => setX(true)}>f</button>;
}

// G: F with an empty memo dependency list. The callable's identity never
// changes, so x reaches nothing. Negative control — this is what tells a test
// of dependency invalidation from a test of "is x spelled in the array".
export function G() {
  const [x, setX] = useState(false);
  const cb = useCallback(() => {}, []);
  useEffect(() => {
    mutation();
  }, [cb]);
  return <button onClick={() => setX(true)}>g</button>;
}
