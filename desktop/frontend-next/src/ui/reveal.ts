import { useEffect, useRef, useState } from "react";

// The kernel pushes 2-3 characters per frame, but in bursts: measured against a
// live turn, the median gap between frames is 0ms and the 90th is 32ms. Drawing
// each arrival reproduces that jitter as visible stutter. Revealing on the frame
// clock instead turns the same bytes into steady typing.
//
// The backlog drains proportionally rather than at a fixed rate, so a burst is
// absorbed within a few frames and the text never falls meaningfully behind.
const DRAIN = 5;

export function useRevealed(text: string, streaming?: boolean): string {
  const [shown, setShown] = useState(text.length);
  const at = useRef(text.length);
  const want = useRef(text.length);
  want.current = text.length;

  useEffect(() => {
    // A settled message, a reload or a new turn has no backlog to pace: whatever
    // is there is already final.
    if (!streaming || matchMedia("(prefers-reduced-motion: reduce)").matches) {
      at.current = want.current;
      setShown(want.current);
      return;
    }
    if (at.current >= want.current) return;
    let raf = 0;
    const step = () => {
      const target = want.current;
      // Replaced rather than appended (session switch, history restore).
      if (target < at.current) {
        at.current = target;
        setShown(target);
        return;
      }
      if (at.current >= target) return;
      at.current = Math.min(target, at.current + Math.max(1, Math.ceil((target - at.current) / DRAIN)));
      setShown(at.current);
      if (at.current < target) raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [streaming, text.length]);

  return streaming ? text.slice(0, shown) : text;
}
