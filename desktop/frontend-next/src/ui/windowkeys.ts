import { useEffect } from "react";
import { listenAction } from "./listen";
import { pressedChord, typingElsewhere } from "./keys";

/** One window shortcut, named by the action it performs. `fields` says the
 *  chord is also taken from a text field that did not hand it through. */
export interface Shortcut {
  chord: string;
  shift?: boolean;
  fields?: boolean;
  action: string;
  run: () => void;
}

/** The window's keyboard: its shortcut table, and Escape closing the browser
 *  or stopping the turn on screen. */
export function useWindowKeys(
  shortcuts: Shortcut[],
  escape: { browser: boolean; settings: boolean; running: boolean; closeBrowser: () => void; stop: () => void },
) {
  const { browser, settings, running, closeBrowser, stop } = escape;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // A control that answered the press itself — the code editor's own find
      // on Ctrl+F, or its Escape closing that — has spent it.
      if (e.defaultPrevented) return;
      const hit = shortcuts.find((s) => pressedChord(e, s.chord, !!s.shift));
      if (hit && (hit.fields || !typingElsewhere(e.target))) {
        e.preventDefault();
        hit.run();
      }
      // Escape stops only the turn on screen, and a pane over it closes first.
      // Anything transient takes the press in the capture phase (useDismiss), so
      // one that reaches here is a press nothing else wanted.
      if (e.key === "Escape" && browser) {
        closeBrowser();
      } else if (e.key === "Escape" && !settings && running) {
        stop();
      }
    };
    return listenAction(window, "keydown", {
      action: browser ? "browser.open" : "session.stop",
      listener: onKey as EventListener,
    });
  }, [browser, settings, running, closeBrowser, stop, shortcuts]);
}
