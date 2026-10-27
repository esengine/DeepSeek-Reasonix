import { useCallback, useEffect, useState } from "react";
import { pressedChord } from "./keys";
import { listenAction } from "./listen";
import "../styles/browserfull.css";

type BrowserSize = "split" | "full";

export function useBrowserFull(docked: boolean, active: boolean, blocked = false) {
  const [size, setSize] = useState<BrowserSize>("split");
  const toggleFull = useCallback(() => setSize((size) => size === "full" ? "split" : "full"), []);
  useEffect(() => {
    if (!docked) setSize("split");
  }, [docked]);
  useEffect(() => {
    if (blocked) setSize((size) => size === "full" ? "split" : size);
  }, [blocked]);
  useEffect(() => {
    if (!docked || !active) return;
    const key = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing || e.repeat) return;
      if (pressedChord(e, "f", true)) {
        e.preventDefault();
        toggleFull();
      } else if (e.key === "Escape" && size !== "split") {
        e.preventDefault();
        e.stopPropagation();
        setSize("split");
      }
    };
    // Document bubbling follows controls and dismiss layers, but precedes the
    // window's Escape handler that closes the browser or stops the turn.
    return listenAction(document, "keydown", { action: "browser.full", listener: key as EventListener });
  }, [docked, active, size, toggleFull]);
  return { size: docked ? size : "split", toggleFull };
}
