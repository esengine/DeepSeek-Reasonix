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
