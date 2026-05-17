/** tau-bench in-memory DB helpers — pure functions over a nested-table WorldState. */
import type { WorldState } from "./types.js";

/** Deep-clone a WorldState — `structuredClone` is enough since the type is JSON-shaped by contract. Idempotent: calling cloneDb N times on the same input yields identical copies. */
export function cloneDb(db: WorldState): WorldState {
  return structuredClone(db);
}

/** Get a row by table name and id. Lookup is `db[table][id]` — two-level nested dictionary. Returns the row object or `undefined` if not found. */
export function getRow(
  db: WorldState,
  table: string,
  id: string,
): Record<string, unknown> | undefined {
  return db[table]?.[id];
}

/** Set a single field on a row. Mutates `db` in-place — caller should `cloneDb` first if isolation is needed. Returns `true` if the row was found and updated, `false` if not. */
export function setField(
  db: WorldState,
  table: string,
  id: string,
  field: string,
  value: unknown,
): boolean {
  const row = db[table]?.[id];
  if (!row) return false;
  row[field] = value;
  return true;
}
