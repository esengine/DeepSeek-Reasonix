import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { pinToViewport } from "./place";

// Gap between the text and the bubble, in the space a rect reports. It lives
// here rather than in CSS for the reason Context's does: a fixed element is
// placed by measurement, so no rule owns the offset any more.
const GAP = 8;
// Long enough that reading down a list does not open one bubble per row.
const OPEN_MS = 420;
// The pointer has to be able to cross the gap into the bubble to select from it.
const SHUT_MS = 140;

// One observer for every clipped-or-not question, not one per row: a queue at
// its ceiling is 64 of these on screen at once, and the transcript already
// settled that a shared observer is what the API is for.
const asked = new Map<Element, (clipped: boolean) => void>();
let lens: ResizeObserver | null = null;

function watch(el: Element, cb: (clipped: boolean) => void) {
  asked.set(el, cb);
  lens ??= new ResizeObserver((entries) => {
    for (const e of entries) asked.get(e.target)?.(clipped(e.target));
  });
  lens.observe(el);
  cb(clipped(el));
  return () => {
    asked.delete(el);
    lens?.unobserve(el);
  };
}

// Rounded up on both sides: sub-pixel layout reports a one-pixel difference on
// text that is not actually cut, and a bubble offering to show you what you can
// already read is worse than none.
const clipped = (el: Element) => el.scrollWidth > el.clientWidth + 1;

/** Overflow shows the whole of a single-line, ellipsised string — and only when
 *  there is more of it than fits. It replaces the native title attribute, whose
 *  delay, placement, size and scrollability all belong to the browser: a long
 *  one is an uncontrollable sheet over the page. This one is bounded, scrolls
 *  inside itself, and lets the text be selected. */
export function Overflow({ text, className }: { text: string; className?: string }) {
  const anchor = useRef<HTMLSpanElement>(null);
  const [cut, setCut] = useState(false);
  const [open, setOpen] = useState(false);
  const timer = useRef(0);
  // Set while the pointer is over the bubble, so leaving the text does not shut
  // the thing the pointer is on its way to.
  const held = useRef(false);

  useEffect(() => (anchor.current ? watch(anchor.current, setCut) : undefined), [text]);

  const after = useCallback((ms: number, to: boolean) => {
    clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      if (to && !clipped(anchor.current!)) return;
      setOpen(to);
    }, ms);
  }, []);

  useEffect(() => () => clearTimeout(timer.current), []);

  // Placed once, and stale the moment anything moves it. There is nothing
  // useful to show mid-scroll, so it closes rather than chasing.
  useEffect(() => {
    if (!open) return;
    const shut = () => setOpen(false);
    // Capture, and stopped there — the convention useDismiss set. A transient
    // surface owns Escape while it is up, and the window's own handler (which
    // stops a turn, or leaves focus) must not also act on the same press.
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      shut();
    };
    addEventListener("scroll", shut, true);
    addEventListener("resize", shut);
    addEventListener("keydown", key, true);
    return () => {
      removeEventListener("scroll", shut, true);
      removeEventListener("resize", shut);
      removeEventListener("keydown", key, true);
    };
  }, [open]);

  const place = (el: HTMLDivElement | null) => {
    if (!el || !anchor.current) return;
    const to = anchor.current.getBoundingClientRect();
    const box = el.getBoundingClientRect();
    const above = to.top - box.height - GAP;
    pinToViewport(el, to.left, above >= 6 ? above : to.bottom + GAP);
  };

  return (
    <>
      <span
        ref={anchor}
        className={className}
        // A tab stop only where there is something behind it. Sixty-four rows
        // that all take focus and none of which has more to say is a keyboard
        // path through nothing.
        tabIndex={cut ? 0 : undefined}
        data-cut={cut ? "" : undefined}
        onPointerEnter={() => after(OPEN_MS, true)}
        onPointerLeave={() => !held.current && after(SHUT_MS, false)}
        onFocus={() => after(OPEN_MS, true)}
        onBlur={() => after(0, false)}
      >
        {text}
      </span>
      {open &&
        // To body, the way the rewind menu goes. A finished entry animation with
        // fill-mode: both leaves transform as the identity matrix rather than
        // none, and an identity matrix is still a containing block for a fixed
        // child — measured here as a bubble whose inline left/top were right and
        // which rendered 675px below the window. Every row in this queue carries
        // one, so nothing anchored inside a card may position against the
        // viewport from where it sits.
        createPortal(
          <div
            className="ovf"
            role="tooltip"
            ref={place}
            onPointerEnter={() => {
              held.current = true;
              clearTimeout(timer.current);
            }}
            onPointerLeave={() => {
              held.current = false;
              after(SHUT_MS, false);
            }}
          >
            {text}
          </div>,
          document.body,
        )}
    </>
  );
}
