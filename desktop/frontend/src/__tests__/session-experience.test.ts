import { JSDOM } from "jsdom";
import {
  applySessionExperience,
  getSessionExperience,
  hydrateSessionExperience,
  resolveWorkProcessPresentation,
} from "../lib/sessionExperience";

const dom = new JSDOM("<!doctype html><html><body></body></html>", {
  url: "http://localhost/",
});
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.localStorage = dom.window.localStorage;
globalThis.CustomEvent = dom.window.CustomEvent;

let passed = 0;
let failed = 0;
function check(value: boolean, message: string): void {
  if (value) {
    passed += 1;
    console.log(`  PASS  ${message}`);
  } else {
    failed += 1;
    console.log(`  FAIL  ${message}`);
  }
}

console.log("\nsession experience");
localStorage.clear();
localStorage.setItem("reasonix-session-experience", "deep");
check(getSessionExperience() === "standard", "startup ignores stale localStorage before the backend snapshot");
hydrateSessionExperience("invalid");
check(getSessionExperience() === "standard", "invalid startup values normalize to standard");
check(resolveWorkProcessPresentation("standard").keepExpandedAfterCompletion === false, "standard collapses completed work");
check(resolveWorkProcessPresentation("deep").showWhileRunning === true, "deep shows work while running");
check(resolveWorkProcessPresentation("deep").keepExpandedAfterCompletion === true, "deep keeps completed work expanded");

applySessionExperience("deep");
check(getSessionExperience() === "deep", "apply persists deep");
check(localStorage.getItem("reasonix-session-experience") === "deep", "canonical localStorage key stores deep");
check(localStorage.getItem("reasonix-display-mode") === "standard", "compatibility density mirror stays standard");
check(localStorage.getItem("reasonix-process-fold") === "expanded", "deep mirrors the old expanded fold value");

// An authoritative startup snapshot must win over a stale local optimistic value.
hydrateSessionExperience("standard");
check(getSessionExperience() === "standard", "authoritative hydrate wins over stale localStorage");
check(localStorage.getItem("reasonix-session-experience") === "standard", "hydrate rewrites the canonical localStorage value");

if (failed > 0) {
  throw new Error(`${failed} session experience checks failed`);
}
console.log(`  ${passed} checks passed`);
