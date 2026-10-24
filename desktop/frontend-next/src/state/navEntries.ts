export type NavLayout = { order: string[]; hidden: string[] };

const KEY = "rx-nav-entries";
const DEFAULT: NavLayout = { order: [], hidden: [] };
const listeners = new Set<() => void>();

let seen: string | null = null;
let held: NavLayout = DEFAULT;

const names = (v: unknown): string[] | null => (Array.isArray(v) && v.every((x) => typeof x === "string") ? (v as string[]) : null);

function read(): NavLayout {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(KEY);
  } catch {
    return DEFAULT;
  }
  if (raw === seen) return held;
  seen = raw;
  held = DEFAULT;
  if (raw) {
    try {
      const v = JSON.parse(raw) as { order?: unknown; hidden?: unknown } | null;
      const order = names(v?.order);
      const hidden = names(v?.hidden);
      if (order && hidden) held = { order, hidden };
    } catch {
      /* unreadable reads as the default */
    }
  }
  return held;
}

export const navLayout = read;

export function onNavLayoutChange(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

export function setNavLayout(layout: NavLayout): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(layout));
  } catch {
    /* a private window keeps the default, which is the same answer it gives */
  }
  listeners.forEach((fn) => fn());
}

export function resetNavLayout(): void {
  try {
    localStorage.removeItem(KEY);
  } catch {
    /* same as above */
  }
  listeners.forEach((fn) => fn());
}

/** Every destination in the order this machine chose, hidden ones included.
 *  A name the list does not have is ignored; one the choice does not name keeps
 *  its place after the named ones. */
export function ordered(all: readonly string[], layout: NavLayout): string[] {
  const known = new Set(all);
  const named = layout.order.filter((k, i) => known.has(k) && layout.order.indexOf(k) === i);
  return [...named, ...all.filter((k) => !named.includes(k))];
}

/** What is drawn: the chosen order without the hidden ones. */
export function arranged(all: readonly string[], layout: NavLayout): string[] {
  const hidden = new Set(layout.hidden);
  return ordered(all, layout).filter((k) => !hidden.has(k));
}
