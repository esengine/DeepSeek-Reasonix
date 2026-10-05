// What jsdom does not implement and the components under test really call.
// Installed on import, before any render: a component that measures itself
// during mount cannot be given the stub afterwards.
//
// This is the interaction layer's floor, not a place to fake product
// behaviour — anything here is a browser API, and anything a guard has to
// pretend about the product belongs in the guard where a reader can see it.

class StubResizeObserver implements ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

// Overflow watches every clipped-or-not question through one shared observer,
// so a row that renders at all constructs this.
globalThis.ResizeObserver ??= StubResizeObserver;

// jsdom reports no layout, so nothing is ever clipped — which is the right
// answer here: these guards are about what a control does, and whether a
// string is too long for its box is what the browser guards measure.
globalThis.matchMedia ??= ((query: string) =>
  ({
    matches: false,
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent: () => false,
  }) as MediaQueryList) as typeof globalThis.matchMedia;

// jsdom has no scrolling, so it implements none of the methods that ask for
// it. Left out, a tab strip keeping the selected tab in view throws inside a
// requestAnimationFrame — outside React, where it is reported as an unhandled
// error the file still passes with, which is exactly how a real one would go
// unnoticed later.
Element.prototype.scrollIntoView ??= function scrollIntoView() {};

// jsdom has no viewport, so it has no intersections to report. The transcript
// mounts a block when one is near, and without this it asks an observer that
// does not exist. Reporting everything as near is the right answer here: these
// guards are about what a block does once it is on screen, and which blocks
// are on screen is what the browser guards measure.
class StubIntersectionObserver implements IntersectionObserver {
  readonly root = null;
  readonly rootMargin = "";
  readonly thresholds: readonly number[] = [];
  readonly scrollMargin = "";
  constructor(private readonly fn: IntersectionObserverCallback) {}
  observe(el: Element) {
    this.fn([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this);
  }
  unobserve() {}
  disconnect() {}
  takeRecords(): IntersectionObserverEntry[] { return []; }
}
globalThis.IntersectionObserver ??= StubIntersectionObserver;

// jsdom lays nothing out, so a Range has no box. Zero is the honest answer:
// where a found occurrence lands on screen is what the browser guards measure.
Range.prototype.getBoundingClientRect ??= function getBoundingClientRect() {
  return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, toJSON() {} } as DOMRect;
};
