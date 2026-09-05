import { JSDOM } from "jsdom";
export function managementDom() {
  const dom = new JSDOM('<!doctype html><div id="root"></div>', { url: "http://localhost", pretendToBeVisual: true });
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement,
    HTMLInputElement: dom.window.HTMLInputElement, HTMLTextAreaElement: dom.window.HTMLTextAreaElement,
    Element: dom.window.Element, Node: dom.window.Node, Event: dom.window.Event, MouseEvent: dom.window.MouseEvent,
    localStorage: dom.window.localStorage, sessionStorage: dom.window.sessionStorage, IS_REACT_ACT_ENVIRONMENT: true,
    ResizeObserver: class { observe() {} unobserve() {} disconnect() {} } });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  window.matchMedia = () => ({ matches: true, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList;
  return dom;
}
