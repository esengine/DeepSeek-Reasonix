import assert from "node:assert/strict";
import { boundedDiagnostics } from "../lib/processDiagnostics";

assert.equal(await boundedDiagnostics(async () => 42), 42);
assert.equal(await boundedDiagnostics(async () => { throw Error("old shell"); }), undefined);
assert.equal(await boundedDiagnostics(() => new Promise(() => {}), 1), undefined);
console.log("process diagnostics fallback tests passed");
