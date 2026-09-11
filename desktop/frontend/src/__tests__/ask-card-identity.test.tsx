import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import type { QuestionAnswer, WireAsk } from "../lib/types";

const dom = new JSDOM('<div id="root"></div>', { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, {
  window: dom.window, document: dom.window.document, Element: dom.window.Element,
  HTMLElement: dom.window.HTMLElement, Node: dom.window.Node, localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true,
});
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
const { default: React, act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { AskCard } = await import("../components/AskCard");
const { LocaleProvider } = await import("../lib/i18n");
const root = createRoot(document.getElementById("root")!);
const answers: QuestionAnswer[][] = [];
const ask: WireAsk = {
  id: "1", runtimeEpoch: "runtime-a", turnId: "turn-a",
  questions: [{ id: "q1", prompt: "Choose", options: [{ label: "Option A" }, { label: "Option B" }] }],
};
async function render(next: WireAsk | null, scope = "tab-a") {
  await act(async () => root.render(<LocaleProvider>{next && <AskCard ask={next} draftScope={scope}
    onAnswer={(_id, value) => { answers.push(value); }} onStop={() => {}} />}</LocaleProvider>));
}
function input() { return document.querySelector<HTMLInputElement>(".ask-shelf__custom")!; }
async function type(text: string) {
  await act(async () => input().focus()); // Keyboard focus does not click the custom row.
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")!.set!.call(input(), text);
    input().dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  });
}

await render(ask);
await type("Keyboard answer");
await act(async () => input().dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
assert.deepEqual(answers, [[{ questionId: "q1", selected: ["Keyboard answer"] }]]);
console.log("  PASS  keyboard focus and Enter submit the custom answer exactly once");

await render(null);
await render(ask);
assert.equal(input().value, "", "successful submission clears its draft");
await type("Keep this draft");
await render(null);
await render({ ...ask });
assert.equal(input().value, "Keep this draft", "same prompt survives unmount and replay");

await render({ ...ask, runtimeEpoch: "runtime-b" });
assert.equal(input().value, "", "runtime replacement resets a mounted card even with the same prompt and turn IDs");
await type("Runtime B draft");
await render({ ...ask, runtimeEpoch: "runtime-b", turnId: "turn-b" });
assert.equal(input().value, "", "new turn cannot inherit the previous turn's draft");
await render(null);
await render({ ...ask, runtimeEpoch: "runtime-c" });
assert.equal(input().value, "", "a replacement runtime cannot inherit an unmounted card's draft");
await render(ask, "tab-b");
assert.equal(input().value, "", "tabs have separate drafts");
await render(ask);
assert.equal(input().value, "Keep this draft", "switching back restores only the original prompt's draft");
console.log("  PASS  drafts follow tab, runtime, turn, and prompt identity across replay and replacement");
await act(async () => root.unmount());
dom.window.close();
