// T1–T8: where a request's endpoint identity lives.
//
// The verb is on the fetch; the endpoint is usually the caller's. These pin
// that the adjudication belongs to the endpoint site and not to the wrapper —
// and, most of all, that a wrapper ceasing to be a sink never turns an
// unreadable endpoint into a clean one.
const base = "";

// The wrapper: a mutating fetch whose path is its own parameter.
function wrap(path: string, body?: unknown) {
  return fetch(base + path, { method: "POST", body: JSON.stringify(body) });
}
function del(path: string) {
  return fetch(base + path, { method: "DELETE" });
}
// One more hop, to keep the fixpoint honest: no hop limit anywhere.
function outer(path: string) {
  return wrap(path);
}
function runtimePath(): string {
  return String(Date.now());
}

/** T1 — a literal through a wrapper is an endpoint the census can see. */
export const t1 = () => wrap("/write");
/** T2 — the same wrapper, an endpoint declared non-mutating. */
export const t2 = () => wrap("/probe");
/** T3 — an endpoint nothing here can read. Must be unresolved, never clean. */
export const t3 = () => wrap(runtimePath());
/** T4 — two hops. Ownership still reaches this call site. */
export const t4 = () => outer("/nested");
/** T5 — a fixed endpoint written here. Having other parameters does not make
 *  this a wrapper; it supplies its own endpoint. */
export function t5(body: unknown) {
  return fetch(base + "/inline", { method: "POST", body: JSON.stringify(body) });
}
/** T6 — a local function that happens to be spelled like the transport's. It
 *  reaches no fetch, so it carries no endpoint and declares nothing. */
function post0(path: string) {
  return path.length;
}
export const t6 = () => post0("/not-a-transport");
/** T7 — one wrapper, two endpoints, one of them declared. The declaration must
 *  reach the endpoint and not the wrapper. */
export const t7a = () => wrap("/write2");
export const t7b = () => wrap("/probe2");
/** T8 — one path, two verbs, one of them declared. An EndpointKey is both. */
export const t8a = () => wrap("/same");
export const t8b = () => del("/same");

/** T9 — one function, two endpoints, one declared. A capability that can reach
 *  a writing endpoint is writing, whatever else it can also reach. */
export const t9 = (flag: boolean) => wrap(flag ? "/write" : "/probe");
/** T10 — one declared endpoint and one nothing can read. Not a mutation, and
 *  not clean either: the verb is proven and the operation is not. */
export const t10 = (flag: boolean) => (flag ? wrap("/probe") : wrap(runtimePath()));
