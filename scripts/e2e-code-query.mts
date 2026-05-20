import { resolve } from "node:path";
import { ToolRegistry } from "../src/tools.js";
import { registerCodeQueryTools } from "../src/tools/code-query.js";

const rootDir = resolve(import.meta.dirname, "..");
const reg = new ToolRegistry();
registerCodeQueryTools(reg, { rootDir });

async function dispatchPretty(name: string, args: object): Promise<void> {
  const raw = await reg.dispatch(name, JSON.stringify(args));
  console.log(`\n=== ${name} ${JSON.stringify(args)} ===`);
  try {
    console.log(JSON.stringify(JSON.parse(raw), null, 2).slice(0, 1500));
  } catch {
    console.log(raw.slice(0, 1500));
  }
}

const cases: Array<[string, object]> = [
  ["get_symbols", { path: "src/tools.ts" }],
  ["get_symbols", { path: "src/code/setup.ts" }],
  ["get_symbols", { path: "src/code-query/parser.ts" }],
  ["get_symbols", { path: "tests/fixtures/code-query/sample.py" }],
  ["get_symbols", { path: "tests/fixtures/code-query/sample.go" }],
  ["get_symbols", { path: "tests/fixtures/code-query/sample.rs" }],
  ["get_symbols", { path: "tests/fixtures/code-query/Sample.java" }],
  ["get_symbols", { path: "package.json" }],
  ["find_in_code", { path: "src/code/setup.ts", name: "registerCodeQueryTools" }],
  ["find_in_code", { path: "src/tools/code-query.ts", name: "register", kind: "call" }],
  ["find_in_code", { path: "src/code-query/parser.ts", name: "Parser", kind: "call" }],
  ["find_in_code", { path: "tests/fixtures/code-query/sample.py", name: "greet", kind: "call" }],
  ["find_in_code", { path: "tests/fixtures/code-query/sample.go", name: "Hello", kind: "call" }],
  ["find_in_code", { path: "tests/fixtures/code-query/sample.rs", name: "hello", kind: "call" }],
  ["find_in_code", { path: "tests/fixtures/code-query/Sample.java", name: "greet", kind: "call" }],
];

for (const [name, args] of cases) {
  await dispatchPretty(name, args);
}
