// Run: tsx src/__tests__/transcript-geometry-environment.test.ts

import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  applyTypographyPreferences,
  createDefaultTypographyPreferences,
  onTypographyPreferencesChange,
} from "../lib/typographyPreferences";
import { readTranscriptGeometryEnvironment } from "../lib/transcriptGeometryEnvironment";

const dom = new JSDOM("<!doctype html><html><body><div class='transcript'></div></body></html>", { url: "http://localhost" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window) as typeof getComputedStyle;
Object.defineProperty(globalThis, "localStorage", { configurable: true, value: dom.window.localStorage });

const transcript = document.querySelector<HTMLElement>(".transcript")!;
Object.defineProperty(transcript, "clientWidth", { configurable: true, value: 800 });
document.documentElement.style.setProperty("--maxw", "960px");
document.documentElement.style.setProperty("--transcript-inline-pad", "32px");
transcript.style.fontFamily = "Test Conversation";
transcript.style.fontSize = "14px";
transcript.style.lineHeight = "22px";

const environment = readTranscriptGeometryEnvironment(transcript);
assert.equal(environment.contentWidth, 736, "geometry uses the readable column after transcript padding");
assert.match(environment.typographySignature, /Test Conversation/);

const baseline = createDefaultTypographyPreferences();
applyTypographyPreferences(baseline);
let changes = 0;
const unsubscribe = onTypographyPreferencesChange(() => { changes += 1; });
applyTypographyPreferences(baseline);
assert.equal(changes, 0, "reapplying identical typography publishes no geometry churn");
const changed = createDefaultTypographyPreferences();
changed.conversation = { ...changed.conversation, followGlobal: false, fontSize: 18 };
applyTypographyPreferences(changed);
assert.equal(changes, 1, "a real typography change publishes one invalidation");
unsubscribe();

console.log("transcript geometry environment tests passed");
