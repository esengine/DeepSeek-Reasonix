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
