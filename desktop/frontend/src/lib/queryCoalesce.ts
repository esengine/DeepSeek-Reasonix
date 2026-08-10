/**
 * Coalesces identical read-only backend queries that a single UI transition
 * fires more than once.
 *
 * Switching a session fans out ~20-28 bridge calls, and measurement showed a
 * third of them are duplicates within the same transition: independent hooks
 * each ask for the context panel, the project tree, the tab meta. Deduping at
 * the call sites would mean threading a cache through a dozen hooks; deduping
 * at the bridge is one place and cannot be forgotten by the next hook.
 *
 * Only queries on the allowlist are coalesced, and only for the few
 * milliseconds a transition takes: a stale answer is worse than a slow one, so
 * nothing here outlives the burst it exists to collapse.
 */

/** How long an in-flight or just-settled answer may be shared. */
const WINDOW_MS = 200;

/**
 * COALESCED lists read-only queries whose answer cannot change within one UI
 * transition. Anything that mutates, starts work, or reads a value the user is
 * actively editing stays off this list.
 */
const COALESCED = new Set([
  "ListProjectTree",
  "ListTabs",
  "ContextPanel",
  "MetaForTab",
  "EffortForTab",
  "JobsForTab",
  "BackgroundRuntimes",
  "BalanceForTab",
]);

// Any command that can change backend-visible state starts a new query epoch.
// Prefixes match the generated binding names while leaving ordinary reads
// (List/Get/Meta/History/Context/Jobs/Checkpoints) available for coalescing.
const MUTATION_PREFIX = /^(Activate|Add|Answer|Apply|Approve|Cancel|Clear|Close|Connect|Create|Delete|Ensure|Fetch|Install|New|Open|Refresh|Remove|Rename|Reorder|Replay|Resolve|Resume|Run|Save|Send|Set|Start|Steer|Stop|Switch|Trash|Try|Update|Upgrade)/;

type Entry = { promise: Promise<unknown>; at: number };

const inflight = new Map<string, Entry>();

function now(): number {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

export function coalescesQuery(method: string): boolean {
  return COALESCED.has(method);
}

export function invalidatesQueryCoalescing(method: string): boolean {
  return MUTATION_PREFIX.test(method);
}

/**
 * maybeShare is the bridge's single entry point: it decides whether a call is
 * shareable and shares it, so the proxy never has to know the allowlist.
 */
export function maybeShare(method: string, args: unknown[], run: () => unknown): unknown {
  if (!coalescesQuery(method)) {
    if (invalidatesQueryCoalescing(method)) inflight.clear();
    return run();
  }
  return shareQuery(method, args, async () => run());
}

/**
 * shareQuery returns the in-flight answer for an identical call made inside the
 * window, or runs it. A rejection is never shared beyond its own settlement:
 * the next caller retries rather than inheriting a stale failure. Callers that
 * cross an external state boundary may explicitly invalidate that query, while
 * bridge mutations invalidate the whole burst automatically.
 */
export function shareQuery<T>(method: string, args: unknown[], run: () => Promise<T>): Promise<T> {
  const key = `${method}|${safeKey(args)}`;
  const at = now();
  const hit = inflight.get(key);
  if (hit && at - hit.at < WINDOW_MS) return hit.promise as Promise<T>;
  const promise = run();
  inflight.set(key, { promise, at });
  void promise.then(
    () => {
      if (method === "ListTabs") {
        forget(key, promise);
        return;
      }
      setTimeout(() => forget(key, promise), WINDOW_MS);
    },
    () => forget(key, promise),
  );
  return promise;
}

function forget(key: string, promise: Promise<unknown>): void {
  if (inflight.get(key)?.promise === promise) inflight.delete(key);
}

/** Arguments are identifiers and flags; anything unserialisable disables the share. */
function safeKey(args: unknown[]): string {
  try {
    return JSON.stringify(args);
  } catch {
    return `\u0000${Math.random()}`;
  }
}

/** Drop a settled burst answer at an external state boundary (for example turn_done). */
export function invalidateSharedQuery(method: string, args: unknown[]): void {
  inflight.delete(`${method}|${safeKey(args)}`);
}

/** Test seam: forget everything remembered so far. */
export function resetQueryCoalescing(): void {
  inflight.clear();
}
