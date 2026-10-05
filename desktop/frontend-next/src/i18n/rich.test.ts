import { describe, expect, it } from "vitest";
import { tx } from "./rich";

describe("tx", () => {
  it("keeps the sentence whole and fills its placeholders in order", () => {
    const parts = tx("本段的 {n} 处改动均已写入简报", { n: 3 });
    const text = parts.map((p) => (typeof p === "string" ? p : "<node>")).join("");
    expect(text).toContain("<node>");
    expect(text.startsWith("<node>")).toBe(false);
  });

  it("shows an unfilled placeholder rather than dropping it", () => {
    const parts = tx("还有 {missing} 处", {});
    expect(parts.join("")).toContain("{missing}");
  });
});
