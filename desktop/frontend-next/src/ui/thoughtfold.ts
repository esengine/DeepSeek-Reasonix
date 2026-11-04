import { createContext, useContext, useMemo, useRef } from "react";
import type { Item } from "../state/session";

// Whether the reader opened or closed a thought, kept per conversation so a
// reload draws it the way they left it. A card's id is a per-load counter, so a
// thought is named by its turn's user message and its place among that turn's
// thoughts, which a rebuilt transcript gives back.
const KEY = "rx-thought-folds";
const KEEP = 400;

type Held = Record<string, boolean>;

function read(scope: string): Held {
  try {
    const raw = localStorage.getItem(`${KEY}:${scope}`);
    const got = raw ? (JSON.parse(raw) as unknown) : {};
    return got && typeof got === "object" && !Array.isArray(got) ? (got as Held) : {};
  } catch {
    return {};
  }
}

function write(scope: string, held: Held) {
  const keys = Object.keys(held);
  const kept = keys.length > KEEP ? Object.fromEntries(keys.slice(-KEEP).map((k) => [k, held[k]])) : held;
  try {
    localStorage.setItem(`${KEY}:${scope}`, JSON.stringify(kept));
  } catch {
    // A full or blocked store keeps the choice for this load only.
  }
}

/** Each thought's name across reloads, by card id; a thought before any user
 *  message with a logged index has none. */
export function thoughtKeys(items: readonly Item[]): Map<string, string> {
  const out = new Map<string, string>();
  let turn: number | undefined;
  let nth = 0;
  for (const it of items) {
    if (it.t === "user" && !it.pending) {
      turn = it.msgIndex;
      nth = 0;
    } else if (it.t === "say" && it.reasoning && turn !== undefined) {
      out.set(it.id, `${turn}.${nth++}`);
    }
  }
  return out;
}

interface Folds {
  get: (id: string) => boolean | undefined;
  set: (id: string, open: boolean) => void;
}

const NONE: Folds = { get: () => undefined, set: () => {} };

export const ThoughtFolds = createContext<Folds>(NONE);

/** The pane's memory of opened and closed thoughts. Its identity changes only
 *  with the conversation, so streaming a token re-renders no card through it. */
export function useThoughtFolds(items: readonly Item[], scope: string): Folds {
  const latest = useRef(items);
  latest.current = items;
  return useMemo(() => {
    if (!scope) return NONE;
    let held: Held | null = null;
    let seen: readonly Item[] | null = null;
    let keys = new Map<string, string>();
    const keyOf = (id: string) => {
      if (seen !== latest.current) {
        seen = latest.current;
        keys = thoughtKeys(seen);
      }
      return keys.get(id);
    };
    return {
      get: (id) => {
        const k = keyOf(id);
        held ??= read(scope);
        return k === undefined ? undefined : held[k];
      },
      set: (id, open) => {
        const k = keyOf(id);
        if (k === undefined) return;
        held ??= read(scope);
        delete held[k];
        held[k] = open;
        write(scope, held);
      },
    };
  }, [scope]);
}

export const useThoughtFold = () => useContext(ThoughtFolds);
