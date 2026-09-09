import type { RefObject } from "react";

/** Ref attachment occurs before command publication and is never an async command. */
export function composeDomRef<Node>(attach: (node: Node | null) => void, ref: RefObject<Node | null>) {
  return (node: Node | null) => {
    attach(node);
    ref.current = node;
  };
}
