import { describe, expect, it } from "vitest";
import { UserRepo } from "./users";

describe("UserRepo.updateProfile", () => {
  it("claims a new handle atomically and reports a conflict without a read-then-write race", async () => {
    let sql = "";
    let binds: unknown[] = [];
    const statement = {
      bind(...values: unknown[]) { binds = values; return this; },
      async run() { return { meta: { changes: 0 } }; },
    };
    const db = {
      prepare(query: string) {
        sql = query;
        return statement;
      },
    } as unknown as D1Database;

    const row = await new UserRepo(db).updateProfile(7, { handle: "claimed" });
    expect(row).toBeNull();
    expect(sql).toContain("NOT EXISTS");
    expect(sql).toContain("other.id !=");
    expect(binds).toContain("claimed");
    expect(binds).toContain(7);
  });
});
