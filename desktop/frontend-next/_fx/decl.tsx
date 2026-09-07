import { useEffect } from "react";
import { listenAction } from "./listen";
import { mutation } from "./sink";

// Y — a listener says which action it belongs to, and joins the button that
// says the same thing. Two input kinds, one action, two roots.
export function Y() {
  useEffect(
    () =>
      listenAction(window, "keydown", {
        action: "fx.dismiss",
        value: "escape",
        listener: () => mutation(),
      }),
    [],
  );
  return (
    <button data-action="fx.dismiss" data-value="click" onClick={() => mutation()}>
      y
    </button>
  );
}

// Z — the same declaration on something a person does not do. Resizing is the
// environment's, and writing an action on it must be an error rather than a
// promotion: metadata says which action, never whether this is one at all.
export function Z() {
  useEffect(
    () =>
      listenAction(window, "resize", {
        action: "fx.not-an-input",
        listener: () => mutation(),
      }),
    [],
  );
  return <i>z</i>;
}
