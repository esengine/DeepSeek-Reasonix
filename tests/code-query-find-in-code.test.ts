import { describe, expect, it } from "vitest";
import { findInCode } from "../src/code-query/find-in-code.js";

describe("findInCode", () => {
  it("finds an identifier across definition, references, and call sites", async () => {
    const source = `
function fetchUser(id: string) {
  return fetchUser.cache?.[id];
}
const u = fetchUser("a");
const v = fetchUser("b");
`;
    const matches = await findInCode("a.ts", source, "fetchUser");
    const kinds = matches.map((m) => m.kind).sort();
    expect(kinds).toContain("definition");
    expect(kinds.filter((k) => k === "call").length).toBe(2);
    expect(kinds.filter((k) => k === "reference").length).toBeGreaterThanOrEqual(1);
  });

  it("does not match identifiers inside string literals or comments", async () => {
    const source = `
// fetchUser is a function defined elsewhere
function fetchUser() {}
const note = "call fetchUser here";
fetchUser();
`;
    const matches = await findInCode("a.ts", source, "fetchUser");
    expect(matches.length).toBe(2);
    const kinds = matches.map((m) => m.kind).sort();
    expect(kinds).toEqual(["call", "definition"]);
  });

  it("filters by kind=call", async () => {
    const source = `
function helper() {}
helper();
const r = helper;
helper();
`;
    const matches = await findInCode("a.ts", source, "helper", { kind: "call" });
    expect(matches.length).toBe(2);
    expect(matches.every((m) => m.kind === "call")).toBe(true);
  });

  it("filters by kind=definition", async () => {
    const source = `
interface User { id: string; }
type User = string;
const User = 1;
`;
    const matches = await findInCode("a.ts", source, "User", { kind: "definition" });
    expect(matches.length).toBeGreaterThanOrEqual(2);
    expect(matches.every((m) => m.kind === "definition")).toBe(true);
  });

  it("returns 1-based line/column and the source-line snippet", async () => {
    const source = "const x = 1;\nconst y = 2;\nconsole.log(y);\n";
    const matches = await findInCode("a.ts", source, "y");
    expect(matches.length).toBeGreaterThanOrEqual(1);
    const first = matches[0]!;
    expect(first.line).toBe(2);
    expect(first.column).toBe(7);
    expect(first.snippet).toBe("const y = 2;");
  });

  it("returns empty for unsupported language", async () => {
    expect(await findInCode("a.cpp", "int x = 1;\n", "x")).toEqual([]);
  });

  it("classifies Python calls (foo() and obj.foo())", async () => {
    const source = "def foo():\n    pass\nfoo()\nobj.foo()\n";
    const matches = await findInCode("a.py", source, "foo");
    const kinds = matches.map((m) => m.kind).sort();
    expect(kinds).toEqual(["call", "call", "definition"]);
  });

  it("classifies Go calls and method invocations", async () => {
    const source = `package main
func hello() {}
func main() {
  hello()
  m.hello()
}
`;
    const calls = await findInCode("a.go", source, "hello", { kind: "call" });
    expect(calls.length).toBe(2);
  });

  it("classifies Rust function calls and method calls", async () => {
    const source = `fn helper() {}
fn main() {
  helper();
  obj.helper();
}
`;
    const calls = await findInCode("a.rs", source, "helper", { kind: "call" });
    expect(calls.length).toBe(2);
  });

  it("classifies Java method invocations", async () => {
    const source = `class C {
  void run() { obj.run(); other.run(); }
}
`;
    const calls = await findInCode("C.java", source, "run", { kind: "call" });
    expect(calls.length).toBe(2);
  });

  it("returns empty when name is empty", async () => {
    expect(await findInCode("a.ts", "function foo(){}", "")).toEqual([]);
  });

  it("classifies obj.foo() as a call on foo", async () => {
    const source = "obj.foo(); function foo(){}";
    const matches = await findInCode("a.ts", source, "foo");
    const kinds = matches.map((m) => m.kind).sort();
    expect(kinds).toEqual(["call", "definition"]);
  });

  it("classifies bare access obj.foo (no call) as a reference", async () => {
    const source = "const r = obj.foo;\nfunction foo(){}";
    const matches = await findInCode("a.ts", source, "foo");
    const kinds = matches.map((m) => m.kind).sort();
    expect(kinds).toEqual(["definition", "reference"]);
  });

  it("classifies `new Foo()` and `new obj.Foo()` as calls on Foo", async () => {
    const source = "class Foo {}\nconst a = new Foo();\nconst b = new ns.Foo();";
    const matches = await findInCode("a.ts", source, "Foo");
    const kinds = matches.map((m) => m.kind).sort();
    expect(kinds).toEqual(["call", "call", "definition"]);
  });
});
