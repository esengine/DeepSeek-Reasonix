
/** `bidi-js` ships without types. */
declare module 'bidi-js' {
  type BidiFactory = (
    text: string,
    explicitDirection?: string,
  ) => {
    paragraphs: Array<{ start: number; end: number; level: number }>;
    levels: Uint8Array;
    getReorderSegments(start: number, end: number, levels: Uint8Array): number[][];
  };
  const bidiFactory: () => BidiFactory;
  export default bidiFactory;
}

declare module 'react-reconciler' {
  interface Reconciler<C, _I, _T, _R, _P> {
    updateContainerSync: (
      element: unknown,
      container: C,
      parent: unknown,
      callback: unknown,
    ) => unknown;
    flushSyncWork: () => void;
    flushSyncFromReconciler: <R>(fn: () => R) => R;
  }
}

/** Bun exposes process-level globals that don't exist under Node. */
// biome-ignore lint/suspicious/noExplicitAny: Bun globals tolerate any payload
declare const Bun:
  | undefined
  | {
      version?: string;
      argv?: readonly string[];
      stringWidth?: (s: string, opts?: unknown) => number;
      wrapAnsi?: (s: string, cols: number, opts?: unknown) => string;
    };

declare namespace JSX {
  interface IntrinsicElements {
    'ink-box': Record<string, unknown>;
    'ink-text': Record<string, unknown>;
    'ink-link': Record<string, unknown>;
    'ink-raw-ansi': Record<string, unknown>;
  }
}
