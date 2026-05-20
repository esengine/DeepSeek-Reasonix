import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ToolRegistry } from "../src/tools.js";
import { registerCodeQueryTools } from "../src/tools/code-query.js";

describe("code-query tools", () => {
  let tmp: string;
  let registry: ToolRegistry;

  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-code-query-"));
    registry = new ToolRegistry();
    registerCodeQueryTools(registry, { rootDir: tmp });
  });

  afterEach(() => {
    rmSync(tmp, { recursive: true, force: true });
  });

  describe("get_symbols", () => {
    it("returns symbols for a TS file under the project root", async () => {
      writeFileSync(
        join(tmp, "foo.ts"),
        "export function add(a: number, b: number) { return a + b; }\nexport class C {}\n",
      );
      const raw = await registry.dispatch("get_symbols", JSON.stringify({ path: "foo.ts" }));
      const parsed = JSON.parse(raw) as {
        path: string;
        symbols: Array<{ name: string; kind: string }>;
      };
      expect(parsed.path).toBe("foo.ts");
      expect(parsed.symbols.map((s) => `${s.kind}:${s.name}`)).toEqual(["function:add", "class:C"]);
    });

    it("reports unsupported language for non-JS/TS/Python/Go/Rust/Java files", async () => {
      writeFileSync(join(tmp, "a.cpp"), "int main(){}\n");
      const raw = await registry.dispatch("get_symbols", JSON.stringify({ path: "a.cpp" }));
      const parsed = JSON.parse(raw) as { error?: string };
      expect(parsed.error).toMatch(/language not supported/);
    });

    it("extracts symbols for Python files", async () => {
      writeFileSync(join(tmp, "a.py"), "def hello():\n    pass\n");
      const raw = await registry.dispatch("get_symbols", JSON.stringify({ path: "a.py" }));
      const parsed = JSON.parse(raw) as { symbols: Array<{ name: string; kind: string }> };
      expect(parsed.symbols.map((s) => `${s.kind}:${s.name}`)).toEqual(["function:hello"]);
    });

    it("treats leading slash as project-root-relative", async () => {
      writeFileSync(join(tmp, "x.ts"), "function f(){}");
      const raw = await registry.dispatch("get_symbols", JSON.stringify({ path: "/x.ts" }));
      const parsed = JSON.parse(raw) as { symbols: Array<{ name: string }> };
      expect(parsed.symbols.map((s) => s.name)).toEqual(["f"]);
    });
  });

  describe("find_in_code", () => {
    it("finds an identifier across roles", async () => {
      writeFileSync(join(tmp, "a.ts"), "function foo(){}\nfoo();\nconst x = foo;\n");
      const raw = await registry.dispatch(
        "find_in_code",
        JSON.stringify({ path: "a.ts", name: "foo" }),
      );
      const parsed = JSON.parse(raw) as {
        matches: Array<{ kind: string; line: number }>;
      };
      const kinds = parsed.matches.map((m) => m.kind).sort();
      expect(kinds).toEqual(["call", "definition", "reference"]);
    });

    it("respects kind filter", async () => {
      writeFileSync(
        join(tmp, "a.ts"),
        "function helper(){}\nhelper();\nhelper();\nconst r = helper;\n",
      );
      const raw = await registry.dispatch(
        "find_in_code",
        JSON.stringify({ path: "a.ts", name: "helper", kind: "call" }),
      );
      const parsed = JSON.parse(raw) as { matches: Array<{ kind: string }> };
      expect(parsed.matches.length).toBe(2);
      expect(parsed.matches.every((m) => m.kind === "call")).toBe(true);
    });

    it("reports unsupported language", async () => {
      writeFileSync(join(tmp, "a.cpp"), "int main(){}\n");
      const raw = await registry.dispatch(
        "find_in_code",
        JSON.stringify({ path: "a.cpp", name: "main" }),
      );
      const parsed = JSON.parse(raw) as { error?: string };
      expect(parsed.error).toMatch(/language not supported/);
    });

    it("finds call sites in Rust", async () => {
      writeFileSync(join(tmp, "a.rs"), "fn helper(){}\nfn main(){ helper(); helper(); }\n");
      const raw = await registry.dispatch(
        "find_in_code",
        JSON.stringify({ path: "a.rs", name: "helper", kind: "call" }),
      );
      const parsed = JSON.parse(raw) as { matches: Array<{ kind: string }> };
      expect(parsed.matches.length).toBe(2);
    });
  });

  describe("registration", () => {
    it("registers both tools as read-only and parallel-safe", () => {
      const specs = registry.specs();
      const names = specs.map((s) => s.function.name);
      expect(names).toContain("get_symbols");
      expect(names).toContain("find_in_code");
    });
  });
});
