interface StyleTarget {
  style: {
    setProperty(name: string, value: string): void;
  };
}

interface AriaTarget {
  setAttribute(name: string, value: string): void;
}

interface RafResizeUpdaterOptions {
  target: StyleTarget;
  separator?: AriaTarget | null;
  cssVar: string;
  onApply?: (value: number) => void;
}

export interface RafResizeUpdater {
  schedule(value: number): void;
  flush(): void;
  cancel(): void;
}

export interface PointerResizeLifecycle {
  /** Finish once, remove every listener, and release pointer capture. */
  finish(): void;
}

/**
 * Own one pointer-resize gesture across capture loss, cancellation, blur, and
 * component cleanup. Every terminal path calls onFinish exactly once.
 */
export function createPointerResizeLifecycle({
  separator,
  pointerId,
  onMove,
  onFinish,
}: {
  separator: HTMLElement;
  pointerId: number;
  onMove: (event: PointerEvent) => void;
  onFinish: () => void;
}): PointerResizeLifecycle {
  let active = true;

  function removeListeners() {
    window.removeEventListener("pointermove", handleMove);
    window.removeEventListener("pointerup", handlePointerDone);
    window.removeEventListener("pointercancel", handlePointerDone);
    window.removeEventListener("blur", finish);
    separator.removeEventListener("lostpointercapture", handlePointerDone);
  }

  function finish() {
    if (!active) return;
    active = false;
    removeListeners();
    try {
      if (separator.hasPointerCapture(pointerId)) separator.releasePointerCapture(pointerId);
    } catch {
      // WebView2 can release capture before the terminal pointer event.
    }
    onFinish();
  }

  function handleMove(event: PointerEvent) {
    if (event.pointerId === pointerId) onMove(event);
  }

  function handlePointerDone(event: PointerEvent) {
    if (event.pointerId === pointerId) finish();
  }

  try {
    separator.setPointerCapture(pointerId);
  } catch {
    // Pointer capture is optional in older embedded browser runtimes.
  }
  window.addEventListener("pointermove", handleMove);
  window.addEventListener("pointerup", handlePointerDone);
  window.addEventListener("pointercancel", handlePointerDone);
  window.addEventListener("blur", finish);
  separator.addEventListener("lostpointercapture", handlePointerDone);

  return { finish };
}

function roundedPixel(value: number): number {
  return Math.round(value);
}

export function createRafResizeUpdater({ target, separator, cssVar, onApply }: RafResizeUpdaterOptions): RafResizeUpdater {
  let frame: number | null = null;
  let latest: number | null = null;

  const apply = () => {
    frame = null;
    if (latest === null) return;
    const rounded = roundedPixel(latest);
    target.style.setProperty(cssVar, `${rounded}px`);
    separator?.setAttribute("aria-valuenow", String(rounded));
    onApply?.(rounded);
  };

  return {
    schedule(value: number) {
      latest = value;
      if (frame !== null) return;
      frame = requestAnimationFrame(apply);
    },
    flush() {
      if (frame !== null) {
        cancelAnimationFrame(frame);
        frame = null;
      }
      apply();
    },
    cancel() {
      if (frame === null) return;
      cancelAnimationFrame(frame);
      frame = null;
    },
  };
}
