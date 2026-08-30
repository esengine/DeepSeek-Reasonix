import { readFileSync } from "node:fs";
import { join } from "node:path";
import assert from "node:assert/strict";

const root = join(import.meta.dirname, "..", "..");
const arbiter = readFileSync(join(root, "src/lib/useTranscriptScrollArbiter.ts"), "utf8");
const styles = readFileSync(join(root, "src/styles.css"), "utf8");

assert.equal(arbiter.includes("manualMeasurementFreezeRef"), false, "manual transcript scrolling has no measurement freeze state");
assert.equal(arbiter.includes("upwardManualGesture"), false, "wheel/touch reading does not freeze recycled rows");
assert.equal(
  styles.includes("\n  height: max(0px, calc(var(--transcript-row-estimate) - 32px))"),
  true,
  "pending Markdown uses only its bounded parser fallback height",
);
process.stdout.write("transcript measurement contract passed\n");
