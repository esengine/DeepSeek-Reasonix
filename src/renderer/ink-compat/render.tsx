/** Ink-shaped render() that mounts a React tree on the cell-diff renderer. */

import type { ReactNode } from "react";
import { CharPool } from "../pools/char-pool.js";
import { HyperlinkPool } from "../pools/hyperlink-pool.js";
import { StylePool } from "../pools/style-pool.js";
import { type Handle, mount } from "../reconciler/mount.js";

export interface InkLikeRenderOptions {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
  readonly stderr?: NodeJS.WriteStream;
  /** Default true. Listens for Ctrl+C on stdin and unmounts. */
  readonly exitOnCtrlC?: boolean;
  /** Ink had this; we don't patch console, so this option is accepted but ignored. */
  readonly patchConsole?: boolean;
}

export interface InkLikeInstance {
  rerender(element: ReactNode): void;
  unmount(): void;
  waitUntilExit(): Promise<void>;
  cleanup(): void;
  clear(): void;
}

export function render(element: ReactNode, opts: InkLikeRenderOptions = {}): InkLikeInstance {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;
  const exitOnCtrlC = opts.exitOnCtrlC !== false;

  const pools = {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };

  let resolveExit: () => void = () => {};
  let exitError: Error | undefined;
  const exitPromise = new Promise<void>((resolve, reject) => {
    resolveExit = () => {
      if (exitError) reject(exitError);
      else resolve();
    };
  });

  let destroyed = false;
  let handle: Handle | null = null;

  const onResize = (): void => {
    if (destroyed || !handle) return;
    handle.resize(stdout.columns ?? 80, stdout.rows ?? 24);
  };

  const onCtrlCData = (chunk: Buffer | string): void => {
    if (destroyed || !exitOnCtrlC) return;
    const text = typeof chunk === "string" ? chunk : chunk.toString("utf8");
    // Ctrl+C in raw mode is a literal \x03; outside raw mode the terminal turns
    // it into SIGINT and we never see it here. Either path lands in unmount.
    if (text.includes("\x03")) instance.unmount();
  };

  handle = mount(element, {
    viewportWidth: stdout.columns ?? 80,
    viewportHeight: stdout.rows ?? 24,
    pools,
    write: (bytes) => stdout.write(bytes),
    stdin,
    onExit: (err) => {
      if (destroyed) return;
      if (err) exitError = err;
      // Tear down on app-driven exit (useApp().exit()).
      instance.unmount();
    },
  });

  stdout.on("resize", onResize);
  stdin.on("data", onCtrlCData);

  const sigintListener = (): void => {
    if (!destroyed) instance.unmount();
  };
  if (exitOnCtrlC) process.on("SIGINT", sigintListener);

  const instance: InkLikeInstance = {
    rerender(next) {
      if (destroyed || !handle) return;
      handle.update(next);
    },
    unmount() {
      if (destroyed) return;
      destroyed = true;
      stdout.off("resize", onResize);
      stdin.off("data", onCtrlCData);
      if (exitOnCtrlC) process.off("SIGINT", sigintListener);
      handle?.destroy();
      handle = null;
      resolveExit();
    },
    waitUntilExit() {
      return exitPromise;
    },
    cleanup() {
      // Ink uses cleanup() to clear listeners without unmounting; for our renderer
      // the listeners are bound to the handle's lifetime, so this is a no-op
      // unless explicitly unmounting.
    },
    clear() {
      if (destroyed) return;
      stdout.write("\x1b[2J\x1b[H");
    },
  };

  return instance;
}
