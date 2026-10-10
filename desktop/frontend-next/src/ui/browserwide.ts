import { useCallback, useEffect, useState } from "react";
import { pressedChord } from "./keys";
import { listenAction } from "./listen";
import "../styles/browserwide.css";

export function useBrowserWide(docked: boolean, active: boolean) {
  const [size, setSize] = useState<"split" | "wide">("split");
  const toggle = useCallback(() => setSize((size) => size === "split" ? "wide" : "split"), []);
  useEffect(() => {
    if (!docked) setSize("split");
  }, [docked]);
  useEffect(() => {
    if (!docked || !active) return;
    const key = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing || e.repeat) return;
      if (pressedChord(e, "b", true)) {
        e.preventDefault();
        toggle();
      } else if (e.key === "Escape" && size === "wide") {
        e.preventDefault();
        e.stopPropagation();
        setSize("split");
      }
    };
    // Document bubbling follows controls and dismiss layers, but precedes the
    // window's Escape handler that closes the browser or stops the turn.
    return listenAction(document, "keydown", { action: "browser.wide", listener: key as EventListener });
  }, [docked, active, size, toggle]);
  return { size: docked ? size : "split", toggle };
}
