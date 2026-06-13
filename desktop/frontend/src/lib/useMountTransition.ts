import { useEffect, useRef, useState } from "react";

// useMountTransition keeps a conditionally-rendered overlay mounted long enough
// to play an exit animation. Callers flip `open`; the hook returns whether the
// node should still be in the DOM (`mounted`) and its transition `status`,
// which the component maps to a `data-state` attribute so CSS can drive both
// the enter and the exit keyframes.
//
//   open  true  → mounted=true,  status "open"     (enter animation runs)
//   open  false → mounted=true,  status "closing"  (exit animation runs)
//   …after `duration` ms → mounted=false           (node unmounts)
//
// The exit timer is cancelled if `open` flips back to true mid-exit, so a
// rapid close→open reuses the live node instead of unmounting it. Honors
// prefers-reduced-motion by collapsing the exit delay to ~0, matching the
// global reduced-motion rule that zeroes every animation.
export type MountStatus = "open" | "closing";

function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function useMountTransition(
  open: boolean,
  duration: number,
): { mounted: boolean; status: MountStatus } {
  const [mounted, setMounted] = useState(open);
  const [status, setStatus] = useState<MountStatus>(open ? "open" : "closing");
  const timerRef = useRef<number | null>(null);

  const clearTimer = () => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  useEffect(() => {
    if (open) {
      clearTimer();
      setMounted(true);
      // Defer the "open" status by a frame so the node mounts in its
      // enter-from state before the enter animation is applied.
      const raf = requestAnimationFrame(() => setStatus("open"));
      return () => cancelAnimationFrame(raf);
    }
    if (!mounted) return;
    setStatus("closing");
    const wait = prefersReducedMotion() ? 0 : duration;
    clearTimer();
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      setMounted(false);
    }, wait);
    return undefined;
  }, [open, mounted, duration]);

  useEffect(() => () => clearTimer(), []);

  return { mounted, status };
}

// useDeferredClose suits overlays whose mount is owned by a parent (rendered as
// `{cond && <Panel onClose={...} />}`). The panel can't keep itself mounted, so
// instead it defers the parent's unmount: `requestClose` flips `closing` true
// (CSS plays the exit animation), then fires the real `onClose` after
// `duration`. `status` maps to data-state exactly like useMountTransition, so
// the same enter/exit CSS applies. Reduced-motion collapses the delay to ~0.
export function useDeferredClose(
  onClose: () => void,
  duration: number,
): { status: MountStatus; requestClose: () => void } {
  const [closing, setClosing] = useState(false);
  const timerRef = useRef<number | null>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  const requestClose = () => {
    if (timerRef.current !== null) return; // already closing
    setClosing(true);
    const wait = prefersReducedMotion() ? 0 : duration;
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      onCloseRef.current();
    }, wait);
  };

  useEffect(
    () => () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    },
    [],
  );

  return { status: closing ? "closing" : "open", requestClose };
}
