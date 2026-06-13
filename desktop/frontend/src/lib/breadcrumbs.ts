// A small ring buffer of recent app events, attached to crash reports so a stack
// trace arrives with the steps that led to it. Self-contained (no app imports) so
// the crash overlay can read it even when the rest of the app is broken.

type Breadcrumb = { t: number; cat: string; msg: string };

const MAX = 30;
const ring: Breadcrumb[] = [];

export function addBreadcrumb(cat: string, msg: string): void {
  ring.push({ t: Date.now(), cat, msg: msg.length > 200 ? `${msg.slice(0, 200)}…` : msg });
  if (ring.length > MAX) ring.shift();
}

export function dumpBreadcrumbs(): string {
  if (!ring.length) return "";
  const now = Date.now();
  return ring.map((b) => `-${((now - b.t) / 1000).toFixed(1)}s [${b.cat}] ${b.msg}`).join("\n");
}

function stringifyArg(a: unknown): string {
  if (typeof a === "string") return a;
  if (a instanceof Error) return a.message;
  try {
    return JSON.stringify(a);
  } catch {
    return String(a);
  }
}

export function installBreadcrumbConsoleHook(): void {
  for (const level of ["error", "warn"] as const) {
    const orig = console[level].bind(console);
    console[level] = (...args: unknown[]) => {
      addBreadcrumb(`console.${level}`, args.map(stringifyArg).join(" "));
      orig(...args);
    };
  }
}
