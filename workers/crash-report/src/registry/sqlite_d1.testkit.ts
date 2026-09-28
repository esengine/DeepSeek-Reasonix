// @ts-expect-error Node types are intentionally not part of the Worker build.
import { DatabaseSync } from "node:sqlite";

// An in-memory SQLite database behind the slice of the D1 API the registry
// uses. batch() runs inside one transaction, as D1 does, and yields to the
// event loop first so concurrent callers really interleave between batches.
export function sqliteD1(schema: string): { db: D1Database; sqlite: any; close: () => void } {
  const sqlite = new DatabaseSync(":memory:");
  sqlite.exec(schema);
  const returnsRows = (sql: string) => /^\s*(SELECT|WITH)\b/i.test(sql) || /\bRETURNING\b/i.test(sql);
  const prepare = (sql: string) => {
    const statement = sqlite.prepare(sql);
    const wrapper: any = {
      values: [] as unknown[],
      bind(...values: unknown[]) {
        wrapper.values = values;
        return wrapper;
      },
      async first() {
        return statement.get(...wrapper.values) ?? null;
      },
      async all() {
        return { results: statement.all(...wrapper.values) };
      },
      async run() {
        return { meta: { changes: Number(statement.run(...wrapper.values).changes) } };
      },
      exec() {
        if (returnsRows(sql)) return { results: statement.all(...wrapper.values) };
        return { results: [], meta: { changes: Number(statement.run(...wrapper.values).changes) } };
      },
    };
    return wrapper;
  };
  const db = {
    prepare,
    async batch(statements: Array<{ exec: () => unknown }>) {
      await new Promise((resolve) => setTimeout(resolve, 0));
      sqlite.exec("BEGIN");
      try {
        const results = statements.map((s) => s.exec());
        sqlite.exec("COMMIT");
        return results;
      } catch (err) {
        sqlite.exec("ROLLBACK");
        throw err;
      }
    },
  } as unknown as D1Database;
  return { db, sqlite, close: () => sqlite.close() };
}
