import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { ModelsSection } from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import { baseSettings, installCanvasMock } from "../test-support/settingsTestFixtures";
const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
// React's legacy input-event fallback expects these IE hooks when JSDOM does
// not expose native input event support. The custom threshold editor focuses
// its input on open, so keep that production behavior testable without noise.
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
installCanvasMock(dom.window as unknown as Window);
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.sessionStorage = dom.window.sessionStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
window.scrollTo = () => {};
window.matchMedia = () => ({ matches: true, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList;
Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", { configurable: true, value: () => ({x:10,y:10,top:10,left:10,right:310,bottom:54,width:300,height:44,toJSON(){return {};}}) });
localStorage.clear();


let selected = "";
window.go = {main:{App:{ApplyModelSettings:async(change)=>{
  assert.equal(change.kind,"preference");
  if(change.kind === "preference" && change.field === "search") selected=change.ref;
  return {requestId:change.requestId,persisted:true,revision:"saved",application:"not_required",targets:[],issues:[],appliedCatalogs:[]};
}} as Partial<AppBindings> as AppBindings}};
const host=document.getElementById("root")!;
const root=createRoot(host);
const settings=baseSettings();
settings.webSearchModel="auto";
settings.webSearchModels=[];
const render=async()=>{await act(async()=>{root.render(<LocaleProvider><ModelsSection s={{...settings}} busy={false} subtab="usage" apply={async fn=>{await fn();return true;}} /></LocaleProvider>);});};
await render();
const picker=()=>host.querySelector<HTMLButtonElement>('button[aria-label="Web search"],button[aria-label="网页搜索"]')!;
assert.ok(picker(),"search picker rendered");
assert.equal(picker().disabled,false,"auto remains selectable without candidates");
settings.webSearchModel="removed/org/model";
settings.webSearchModelStatus="invalid";
settings.webSearchModelReason="Search connection is not added";
await render();
assert.ok(picker().textContent?.includes("removed/org/model"),"invalid ref retained");
assert.ok(host.querySelector('[role="status"]'),"invalid assignment explained");
await act(async()=>picker().click());
const auto=Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(el=>/auto|自动/i.test(el.textContent??""));
assert.ok(auto,"automatic option available");
await act(async()=>auto.click());
assert.equal(selected,"auto","automatic choice reaches binding");
settings.webSearchModelOverridden=true;
settings.effectiveWebSearchModel="project/m";
settings.webSearchModelStatus="ready";
await render();
assert.ok(host.textContent?.includes("project/m"),"project override visible");
assert.ok(host.querySelector('[role="status"]'),"stale global assignment remains visible under a valid project override");
const helpButtons = host.querySelectorAll<HTMLButtonElement>(".model-setting-help");
assert.equal(helpButtons.length, 6, "all model assignments and subagent effort have help");
assert.equal(document.querySelector('[role="tooltip"]'), null, "help starts hidden");
const searchHelp = Array.from(helpButtons).find(button => /Web search|网页搜索/.test(button.getAttribute("aria-label") ?? ""))!;
await act(async () => searchHelp.focus());
assert.equal(searchHelp.getAttribute("aria-expanded"), "true", "keyboard focus opens help");
assert.ok(document.querySelector('[role="tooltip"]')?.textContent?.includes("Running tasks"), "search activation explained in help");
await act(async () => window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" })));
assert.equal(searchHelp.getAttribute("aria-expanded"), "false", "Escape closes help");
await act(async () => searchHelp.click());
assert.equal(searchHelp.getAttribute("aria-expanded"), "true", "click pins help");
await act(async () => document.body.click());
assert.equal(searchHelp.getAttribute("aria-expanded"), "false", "outside click closes help");
await act(async()=>root.unmount());
console.log("web search model settings passed");
