/**
 * KeystrokeContext — React surface in front of the raw stdin reader.
 *
 * Replaces Ink's `useInput` chain. Reasonix's components no longer
 * import `useInput` from "ink"; they call `useKeystroke(handler,
 * isActive)` from this module. The provider mounted once at App
 * level owns a `StdinReader`, subscribes a single fan-out function
 * to it, and dispatches each parsed `KeyEvent` to every active
 * consumer.
 *
 * Why a Context instead of a singleton import: the provider can be
 * disabled in tests / replay mode without touching the components,
 * and the lifecycle (start/stop on mount/unmount) is tied to the
 * React tree rather than a global side effect.
 *
 * Why not just keep Ink's useInput: Ink's parse-keypress uses a
 * 100 ms intra-CSI timeout that's too short for Windows ConPTY,
 * leaking arrow-key bytes / paste markers into the buffer. Our
 * reader uses 250 ms and recognises the ESC-stripped variants too
 * — see `stdin-reader.ts`.
 */

import { useInput } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React as a runtime value
import React, { createContext, useContext, useEffect, useRef } from "react";
import { type KeyEvent, type StdinReader, getStdinReader } from "./stdin-reader.js";

interface KeystrokeBus {
  /** Subscribe — returns an unsubscribe function. */
  subscribe(handler: KeystrokeHandler): () => void;
}

export type KeystrokeHandler = (ev: KeyEvent) => void;

/** Minimum surface KeystrokeProvider needs from a key source. StdinReader implements this; the Rust input adapter does too. */
export interface KeystrokeReader {
  start(): void;
  subscribe(handler: KeystrokeHandler): () => void;
}

const KeystrokeContext = createContext<KeystrokeBus | null>(null);

export interface KeystrokeProviderProps {
  children: React.ReactNode;
  /**
   * Optional reader override. Tests inject a synthetic reader so
   * they can `feed()` chunks instead of touching real stdin; the
   * Rust input adapter (when `REASONIX_RENDERER=rust`) injects one
   * that receives events from a spawned `reasonix-render --emit-input`
   * child. Production stdin callers leave this unset and get the
   * singleton.
   */
  reader?: KeystrokeReader;
}

export function KeystrokeProvider({
  children,
  reader: providedReader,
}: KeystrokeProviderProps): React.ReactElement {
  const handlersRef = useRef<Set<KeystrokeHandler>>(new Set());
  // Ref so the bus value's identity is stable across re-renders —
  // consumers don't accidentally re-subscribe every render.
  const busRef = useRef<KeystrokeBus | null>(null);
  if (busRef.current === null) {
    busRef.current = {
      subscribe(handler) {
        handlersRef.current.add(handler);
        return () => {
          handlersRef.current.delete(handler);
        };
      },
    };
  }

  useEffect(() => {
    const reader = providedReader ?? getStdinReader();
    reader.start();
    const unsubscribe = reader.subscribe((ev) => {
      // Snapshot the handler set so handlers added/removed during
      // dispatch don't perturb iteration. Cheap — typical N=1-3.
      for (const fn of [...handlersRef.current]) fn(ev);
    });
    return () => {
      unsubscribe();
      // Don't `stop()` the singleton on every unmount — multiple
      // mounts (test reruns, hot-reload) must not tear down stdin.
      // The singleton's own start() is idempotent; stop() is the
      // process-exit handler's job.
    };
  }, [providedReader]);

  return <KeystrokeContext.Provider value={busRef.current}>{children}</KeystrokeContext.Provider>;
}

/** Subscribe to keystroke events; falls back to Ink's useInput when no KeystrokeProvider is mounted. */
export function useKeystroke(handler: KeystrokeHandler, isActive = true): void {
  const bus = useContext(KeystrokeContext);
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    if (!bus || !isActive) return undefined;
    return bus.subscribe((ev) => handlerRef.current(ev));
  }, [bus, isActive]);

  useInput(
    (input, key) => {
      if (bus) return;
      handlerRef.current({
        input,
        upArrow: key.upArrow,
        downArrow: key.downArrow,
        leftArrow: key.leftArrow,
        rightArrow: key.rightArrow,
        return: key.return,
        escape: key.escape,
        backspace: key.backspace,
        delete: key.delete,
        tab: key.tab,
        shift: key.shift,
        ctrl: key.ctrl,
        meta: key.meta,
        pageUp: key.pageUp,
        pageDown: key.pageDown,
      });
    },
    { isActive: !bus && isActive },
  );
}

/**
 * Lower-level hook for components that need a stable subscription
 * across the lifetime of the consumer (typically StdinReader-aware
 * unit tests).
 */
export function useKeystrokeBus(): KeystrokeBus | null {
  return useContext(KeystrokeContext);
}

/** Test helper — assemble a KeyEvent with sensible defaults. */
export function makeKeyEvent(overrides: Partial<KeyEvent> = {}): KeyEvent {
  return { input: "", ...overrides };
}
