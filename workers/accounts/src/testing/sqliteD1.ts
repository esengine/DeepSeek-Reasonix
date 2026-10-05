import { readdirSync, readFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { fileURLToPath } from "node:url";

type Params = unknown[];

// A D1Database over an in-memory SQLite, so tests exercise the real SQL
// (constraints, ON CONFLICT, RETURNING) instead of recording statements.
// `fail` lets a test make selected statements throw.
export interface SqliteD1 {
  db: D1Database;
  raw: DatabaseSync;
  statements: string[];
}

class Statement {
  private params: Params = [];
  constructor(
    private readonly raw: DatabaseSync,
    readonly sql: string,
    private readonly log: string[],
    private readonly fail: (sql: string) => boolean,
  ) {}

  bind(...values: Params): this {
    this.params = values;
    return this;
  }

  private guard(): void {
    this.log.push(this.sql);
    if (this.fail(this.sql)) throw new Error("injected database failure");
  }

  async first<T>(): Promise<T | null> {
    this.guard();
    return (this.raw.prepare(this.sql).get(...this.params) as T | undefined) ?? null;
  }

  async all<T>(): Promise<{ results: T[]; meta: { changes: number } }> {
    this.guard();
    const results = this.raw.prepare(this.sql).all(...this.params) as T[];
    return { results, meta: { changes: results.length } };
  }

  async run(): Promise<{ meta: { changes: number; last_row_id: number } }> {
    this.guard();
    const info = this.raw.prepare(this.sql).run(...this.params);
    return { meta: { changes: Number(info.changes), last_row_id: Number(info.lastInsertRowid) } };
  }
}

export function migrationsDir(): string {
  return fileURLToPath(new URL("../../migrations/", import.meta.url));
}

export function applyMigrations(raw: DatabaseSync, upTo?: string): void {
  const files = readdirSync(migrationsDir()).filter((name) => name.endsWith(".sql")).sort();
  for (const file of files) {
    if (upTo && file > upTo) break;
    raw.exec(readFileSync(migrationsDir() + file, "utf8"));
  }
}

export function sqliteD1(options: { fail?: (sql: string) => boolean; migrate?: boolean } = {}): SqliteD1 {
  const raw = new DatabaseSync(":memory:");
  if (options.migrate !== false) applyMigrations(raw);
  const statements: string[] = [];
  const fail = options.fail ?? (() => false);
  const db = {
    prepare: (sql: string) => new Statement(raw, sql, statements, fail),
    async batch(batch: Statement[]) {
      raw.exec("BEGIN");
      try {
        const out = [];
        for (const statement of batch) out.push(await statement.all());
        raw.exec("COMMIT");
        return out;
      } catch (error) {
        raw.exec("ROLLBACK");
        throw error;
      }
    },
  } as unknown as D1Database;
  return { db, raw, statements };
}
