import { useEffect, useState } from "react";

declare const outside: { one: () => void; two: () => void };

// C1 — two independent causes on one root, and both have to survive. The write
// reaches an effect whose setup cannot be read (an unknown callee) and, through
// a dependency list this walk cannot read, a second one. Recording whichever
// was met first made the traversal's order the owner of what the debt is.
export function C1() {
  const [n, setN] = useState(0);
  const [m, setM] = useState(0);
  useEffect(() => {
    outside.one();
  }, [n]);
  useEffect(() => {
    outside.two();
  }, [n > 0 ? m : n]);
  return <button onClick={() => setN(1)}>c1</button>;
}

// C2 — the same two causes, written the other way round. Discovery order is not
// a fact about the code, so C1 and C2 must produce the same cause set. A pass
// that reads the first one it meets passes on one of these and fails the other.
export function C2() {
  const [n, setN] = useState(0);
  const [m, setM] = useState(0);
  useEffect(() => {
    outside.two();
  }, [n > 0 ? m : n]);
  useEffect(() => {
    outside.one();
  }, [n]);
  return <button onClick={() => setN(1)}>c2</button>;
}

// C3 — one source cause under two projections. The write reaches the same
// unreadable effect twice, once directly and once through a second cell, so the
// axes report two open edges; the thing anyone could go and fix is one.
export function C3() {
  const [n, setN] = useState(0);
  const via = n + 1;
  useEffect(() => {
    outside.one();
  }, [n, via]);
  return <button onClick={() => setN(1)}>c3</button>;
}

// C4 — two source causes that produce the same verdict must not collapse into
// one. Reaching UNRESOLVED by two roads is still two roads, and a ranking that
// merged them would offer a cut that closes neither.
export function C4({ hidden }: { hidden: boolean }) {
  const [n, setN] = useState(0);
  useEffect(() => {
    outside.one();
  }, [n]);
  useEffect(() => {
    outside.two();
  }, [n > 0 ? hidden : n]);
  return <button onClick={() => setN(1)}>c4</button>;
}

// C5 — the same cause found twice is one cause. Two writes in one handler reach
// the same unreadable effect; counting the ways it was found would let a path
// count stand in for how much code there is to fix.
export function C5() {
  const [a, setA] = useState(0);
  const [b, setB] = useState(0);
  useEffect(() => {
    outside.one();
  }, [a, b]);
  return (
    <button
      onClick={() => {
        setA(1);
        setB(1);
      }}
    >
      c5
    </button>
  );
}
