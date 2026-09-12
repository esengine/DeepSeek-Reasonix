import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { ChatScrollController } from "../lib/chatScrollController";

const dom = new JSDOM('<div id="scroll"><div id="content"></div></div>', { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement });
let nextFrame = 0;
const frames = new Map<number, FrameRequestCallback>();
globalThis.requestAnimationFrame = callback => { frames.set(++nextFrame, callback); return nextFrame; };
globalThis.cancelAnimationFrame = id => { frames.delete(id); };
const callbacks: ResizeObserverCallback[] = [];
globalThis.ResizeObserver = class {
  constructor(callback: ResizeObserverCallback) { callbacks.push(callback); }
  observe() {} unobserve() {} disconnect() {}
} as unknown as typeof ResizeObserver;
const el = document.getElementById("scroll")!;
const content = document.getElementById("content")!;
let offset = 0, writes = 0;
Object.defineProperties(el, {
  clientHeight: { get: () => 200 }, scrollHeight: { get: () => content.children.length * 100 },
  scrollTop: { get: () => offset, set: value => { writes++; offset = Math.max(0, Math.min(value, content.children.length * 100 - 200)); } },
});
el.getBoundingClientRect = () => ({ top: 0, bottom: 200 }) as DOMRect;
function row(key: string) {
  const node = document.createElement("div"); node.dataset.chatAnchorKey = key; node.dataset.chatTurn = key; node.textContent = key;
  node.getBoundingClientRect = () => { const top = Array.from(content.children).indexOf(node) * 100 - offset; return { top, bottom: top + 100 } as DOMRect; };
  return node;
}
for (let index = 0; index < 10; index++) content.append(row(`row${index}`));
const controller = new ChatScrollController("scroll-test");
controller.attach(el, content); controller.ready();
assert.equal(offset, 800);
const unchanged = writes; controller.layout(); assert.equal(writes, unchanged, "no redundant physical write");
el.dispatchEvent(new dom.window.WheelEvent("wheel", { deltaY: -5 }));
offset = 795; el.dispatchEvent(new dom.window.Event("scroll"));
controller.layout();
assert.equal(offset, 795, "small upward movement inside 24px never snaps back");
assert.equal(controller.getSnapshot().following, false);
offset = 450; el.dispatchEvent(new dom.window.Event("scroll")); controller.beforeChange();
const held = content.children[4].getBoundingClientRect().top;
content.prepend(row("older")); controller.layout();
assert.equal(content.children[5].getBoundingClientRect().top, held, "prepend preserves row-relative position");
const reading = offset; content.append(row("tail")); controller.layout();
assert.equal(offset, reading, "tail growth does not move a reader");
controller.jump("row2"); assert.equal(document.querySelector('[data-chat-anchor-key="row2"]')!.getBoundingClientRect().top, 0);
controller.toBottom(); assert.equal(offset, 1000);
const callback = callbacks[0];
controller.dispose();
const afterDispose = writes;
callback([], {} as ResizeObserver);
frames.forEach(callback => callback(16));
assert.equal(writes, afterDispose, "late observers and frames cannot write after disposal");
dom.window.close();
console.log("chat scroll: reader intent, prepend, growth, jump, no-op and stale observer tests passed");
