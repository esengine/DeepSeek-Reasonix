// The UI census. Compute is pure and lives in the modules report.mjs imports;
// report.mjs holds every line the analyzer prints, in the order it prints them,
// so that splitting the analyzer cannot move a byte of its output.
//
// PROBE and SHOW select a report. CENSUS_SRC and CENSUS_ROOTS point it at a
// tree — _fx is the fixture corpus the rules are exercised against.
//
// Reporting always runs; gate.mjs then fails the process on any invariant the
// tree no longer holds, so a green exit is the claim and the report is how one
// reads what stands behind it.
import "./report.mjs";
import { enforce } from "./gate.mjs";

enforce();
