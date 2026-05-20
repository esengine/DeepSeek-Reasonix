import { resolve } from "node:path";
import { setGrammarDir } from "../src/code-query/parser.js";
import { extractSymbols } from "../src/code-query/symbols.js";

setGrammarDir(resolve(import.meta.dirname, "..", "dist", "grammars"));

const code = "export function hello(): void {}\nexport class Greeter { greet(){} }";
const symbols = await extractSymbols("hello.ts", code);
const kinds = symbols.map((s) => `${s.kind}:${s.name}`);
console.log("dist/grammars symbols:", JSON.stringify(kinds));
if (kinds.length !== 3) {
  console.error(`FAIL — expected 3 symbols, got ${kinds.length}`);
  process.exit(1);
}
console.log("OK — dist/grammars wasms load and parse correctly");
