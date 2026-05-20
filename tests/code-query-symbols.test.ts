import { describe, expect, it } from "vitest";
import { extractSymbols } from "../src/code-query/symbols.js";

describe("extractSymbols", () => {
  it("finds top-level function and class declarations in TS", async () => {
    const source = `
export function add(a: number, b: number): number {
  return a + b;
}

export class Calculator {
  total = 0;
  add(n: number): void {
    this.total += n;
  }
}
`;
    const symbols = await extractSymbols("calc.ts", source);
    const names = symbols.map((s) => `${s.kind}:${s.name}${s.parent ? `@${s.parent}` : ""}`);
    expect(names).toContain("function:add");
    expect(names).toContain("class:Calculator");
    expect(names).toContain("property:total@Calculator");
    expect(names).toContain("method:add@Calculator");
  });

  it("extracts interfaces, type aliases, and enums", async () => {
    const source = `
interface User { id: string; }
type ID = string | number;
enum Color { Red, Green, Blue }
`;
    const symbols = await extractSymbols("types.ts", source);
    const byKind = (k: string) => symbols.filter((s) => s.kind === k).map((s) => s.name);
    expect(byKind("interface")).toEqual(["User"]);
    expect(byKind("type")).toEqual(["ID"]);
    expect(byKind("enum")).toEqual(["Color"]);
  });

  it("treats arrow-function variables as functions", async () => {
    const source = `
const greet = (name: string): string => "hi " + name;
const noop = function () {};
const value = 42;
`;
    const symbols = await extractSymbols("vars.ts", source);
    const names = symbols.filter((s) => s.kind === "function").map((s) => s.name);
    expect(names).toContain("greet");
    expect(names).toContain("noop");
    expect(names).not.toContain("value");
  });

  it("returns symbols in source order with 1-based positions", async () => {
    const source = "function a(){}\nfunction b(){}\nfunction c(){}\n";
    const symbols = await extractSymbols("order.ts", source);
    expect(symbols.map((s) => s.name)).toEqual(["a", "b", "c"]);
    expect(symbols[0]!.line).toBe(1);
    expect(symbols[1]!.line).toBe(2);
    expect(symbols[2]!.line).toBe(3);
    expect(symbols.every((s) => s.column === 1)).toBe(true);
  });

  it("handles TSX with class + JSX method body", async () => {
    const source = `
class App {
  render() {
    return <div>hi</div>;
  }
}
`;
    const symbols = await extractSymbols("App.tsx", source);
    const names = symbols.map((s) => `${s.kind}:${s.name}`);
    expect(names).toContain("class:App");
    expect(names).toContain("method:render");
  });

  it("handles JS without interfaces or types", async () => {
    const source = `
function hello() {}
class Foo {
  bar() {}
}
const g = () => 1;
`;
    const symbols = await extractSymbols("a.js", source);
    const names = symbols.map((s) => `${s.kind}:${s.name}`);
    expect(names).toContain("function:hello");
    expect(names).toContain("class:Foo");
    expect(names).toContain("method:bar");
    expect(names).toContain("function:g");
  });

  it("returns empty for unsupported language", async () => {
    expect(await extractSymbols("file.cpp", "int main(){}")).toEqual([]);
  });

  it("extracts Python functions and classes; nested functions become methods", async () => {
    const source = `
def hello():
    return 1

class Greeter:
    def greet(self):
        return "hi"

    def __init__(self):
        self.n = 0
`;
    const symbols = await extractSymbols("a.py", source);
    const named = symbols.map((s) => `${s.kind}:${s.name}${s.parent ? `@${s.parent}` : ""}`);
    expect(named).toContain("function:hello");
    expect(named).toContain("class:Greeter");
    expect(named).toContain("method:greet@Greeter");
    expect(named).toContain("method:__init__@Greeter");
  });

  it("extracts Go funcs, methods, structs, and interfaces", async () => {
    const source = `package main

type User struct {
  Name string
}

type Greeter interface {
  Greet() string
}

func Hello() string { return "hi" }
func (u *User) Greet() string { return "hi " + u.Name }
`;
    const symbols = await extractSymbols("a.go", source);
    const named = symbols.map((s) => `${s.kind}:${s.name}`);
    expect(named).toContain("class:User");
    expect(named).toContain("interface:Greeter");
    expect(named).toContain("function:Hello");
    expect(named).toContain("method:Greet");
  });

  it("extracts Rust functions, structs, traits, and impl methods", async () => {
    const source = `pub struct User { pub name: String }
pub trait Greet { fn greet(&self) -> String; }
pub fn hello() -> i32 { 1 }
impl User {
  pub fn name_len(&self) -> usize { self.name.len() }
}
`;
    const symbols = await extractSymbols("a.rs", source);
    const named = symbols.map((s) => `${s.kind}:${s.name}${s.parent ? `@${s.parent}` : ""}`);
    expect(named).toContain("class:User");
    expect(named).toContain("interface:Greet");
    expect(named).toContain("function:hello");
    expect(named).toContain("method:name_len@User");
  });

  it("extracts Java classes, methods, and fields", async () => {
    const source = `public class Calc {
  int total = 0;
  public int add(int n) { return total + n; }
  public Calc() {}
}
`;
    const symbols = await extractSymbols("Calc.java", source);
    const named = symbols.map((s) => `${s.kind}:${s.name}${s.parent ? `@${s.parent}` : ""}`);
    expect(named).toContain("class:Calc");
    expect(named).toContain("method:add@Calc");
    expect(named).toContain("method:Calc@Calc");
    expect(named).toContain("property:total@Calc");
  });

  it("returns empty for empty source", async () => {
    expect(await extractSymbols("empty.ts", "")).toEqual([]);
  });

  it("survives parse errors and still extracts what came before", async () => {
    const source = "function ok() {}\nconst broken = ;\n";
    const symbols = await extractSymbols("partial.ts", source);
    expect(symbols.map((s) => s.name)).toContain("ok");
  });
});
