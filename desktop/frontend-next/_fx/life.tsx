import { useEffect, useState } from "react";
import { mutation } from "./sink";

// H — mount positive. show decides whether MountChild exists at all, and its
// setup mutates the first time it does.
function MountChild() {
  useEffect(() => {
    mutation();
  }, []);
  return <i>h</i>;
}
export function H() {
  const [show, setShow] = useState(false);
  return (
    <div>
      <button onClick={() => setShow(true)}>h</button>
      {show ? <MountChild /> : null}
    </div>
  );
}

// I — stable-presence negative. StableChild is rendered unconditionally, so a
// write to x rerenders it and does not mount it again. This is the case that
// separates "an ancestor rendered" from "this component came into existence";
// a pass that cannot tell them apart turns every ancestor's state into a write.
function StableChild() {
  useEffect(() => {
    mutation();
  }, []);
  return <i>i</i>;
}
export function I() {
  const [x, setX] = useState(false);
  return (
    <div>
      <button onClick={() => setX(!x)}>i</button>
      <StableChild />
    </div>
  );
}

// J — unmount positive. The cleanup runs when show goes false and the component
// leaves, which is a different trigger from a dependency change.
function UnmountChild() {
  useEffect(() => () => {
    mutation();
  }, []);
  return <i>j</i>;
}
export function J() {
  const [show, setShow] = useState(true);
  return (
    <div>
      <button onClick={() => setShow(false)}>j</button>
      {show ? <UnmountChild /> : null}
    </div>
  );
}

// L — register boundary. Mounting installs a listener; performing it is the
// click that comes later. The state that mounts this is not the writer.
function RegisterChild() {
  useEffect(() => {
    const onClick = () => {
      mutation();
    };
    window.addEventListener("click", onClick);
    return () => window.removeEventListener("click", onClick);
  }, []);
  return <i>l</i>;
}
export function L() {
  const [show, setShow] = useState(false);
  return (
    <div>
      <button onClick={() => setShow(true)}>l</button>
      {show ? <RegisterChild /> : null}
    </div>
  );
}

// M — identity-changing remount. Presence never changes; the key does, and a
// new key is a new component instance: the old one unmounts and a new one
// mounts, setup and all.
function KeyChild() {
  useEffect(() => {
    mutation();
  }, []);
  return <i>m</i>;
}
export function M() {
  const [sel, setSel] = useState("a");
  return (
    <div>
      <button onClick={() => setSel("b")}>m</button>
      <KeyChild key={sel} />
    </div>
  );
}
