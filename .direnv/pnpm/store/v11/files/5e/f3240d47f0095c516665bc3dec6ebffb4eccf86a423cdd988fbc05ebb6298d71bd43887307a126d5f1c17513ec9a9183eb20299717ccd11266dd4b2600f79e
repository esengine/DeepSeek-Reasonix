"use strict";
Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
const React = require("react");
const reactDom = require("react-dom");
const virtualCore = require("@tanstack/virtual-core");
function _interopNamespaceDefault(e) {
  const n = Object.create(null, { [Symbol.toStringTag]: { value: "Module" } });
  if (e) {
    for (const k in e) {
      if (k !== "default") {
        const d = Object.getOwnPropertyDescriptor(e, k);
        Object.defineProperty(n, k, d.get ? d : {
          enumerable: true,
          get: () => e[k]
        });
      }
    }
  }
  n.default = e;
  return Object.freeze(n);
}
const React__namespace = /* @__PURE__ */ _interopNamespaceDefault(React);
const useIsomorphicLayoutEffect = typeof document !== "undefined" ? React__namespace.useLayoutEffect : React__namespace.useEffect;
function useVirtualizerBase({
  useFlushSync = true,
  directDomUpdates = false,
  directDomUpdatesMode = "transform",
  ...options
}) {
  const rerender = React__namespace.useReducer((x) => x + 1, 0)[1];
  const directRef = React__namespace.useRef({
    enabled: directDomUpdates,
    mode: directDomUpdatesMode,
    container: null,
    lastSize: null,
    // Keyed by the element itself so a remounted node (same key, new DOM
    // node — e.g. when `enabled` is toggled off then on) is treated as fresh
    // and gets its style written.
    lastPositions: /* @__PURE__ */ new WeakMap(),
    prevRange: null
  });
  directRef.current.enabled = directDomUpdates;
  directRef.current.mode = directDomUpdatesMode;
  const applyDirectStyles = (instance2) => {
    const state = directRef.current;
    if (!state.enabled || !state.container) return;
    const totalSize = instance2.getTotalSize();
    if (totalSize !== state.lastSize) {
      state.lastSize = totalSize;
      const sizeAxis = instance2.options.horizontal ? "width" : "height";
      state.container.style[sizeAxis] = `${totalSize}px`;
    }
    const horizontal = !!instance2.options.horizontal;
    const useTransform = state.mode === "transform";
    const posAxis = horizontal ? "left" : "top";
    const scrollMargin = instance2.options.scrollMargin;
    const items = instance2.getVirtualItems();
    for (const item of items) {
      const next = item.start - scrollMargin;
      const el = instance2.elementsCache.get(item.key);
      if (!el) continue;
      if (state.lastPositions.get(el) === next) continue;
      state.lastPositions.set(el, next);
      if (useTransform) {
        el.style.transform = horizontal ? `translate3d(${next}px, 0, 0)` : `translate3d(0, ${next}px, 0)`;
      } else {
        el.style[posAxis] = `${next}px`;
      }
    }
  };
  const resolvedOptions = {
    ...options,
    onChange: (instance2, sync) => {
      var _a;
      const state = directRef.current;
      let shouldRerender = true;
      if (state.enabled) {
        applyDirectStyles(instance2);
        const range = instance2.range;
        const prev = state.prevRange;
        shouldRerender = !prev || prev.isScrolling !== instance2.isScrolling || prev.startIndex !== (range == null ? void 0 : range.startIndex) || prev.endIndex !== (range == null ? void 0 : range.endIndex);
        if (shouldRerender) {
          state.prevRange = range ? {
            startIndex: range.startIndex,
            endIndex: range.endIndex,
            isScrolling: instance2.isScrolling
          } : null;
        }
      }
      if (shouldRerender) {
        if (useFlushSync && sync) {
          reactDom.flushSync(rerender);
        } else {
          rerender();
        }
      }
      (_a = options.onChange) == null ? void 0 : _a.call(options, instance2, sync);
    }
  };
  const [instance] = React__namespace.useState(() => {
    const v = new virtualCore.Virtualizer(resolvedOptions);
    return Object.assign(v, {
      containerRef: (node) => {
        const state = directRef.current;
        state.container = node;
        state.lastSize = null;
        if (node && state.enabled) {
          const total = v.getTotalSize();
          state.lastSize = total;
          const axis = v.options.horizontal ? "width" : "height";
          node.style[axis] = `${total}px`;
        }
      }
    });
  });
  instance.setOptions(resolvedOptions);
  useIsomorphicLayoutEffect(() => {
    return instance._didMount();
  }, []);
  useIsomorphicLayoutEffect(() => {
    return instance._willUpdate();
  });
  useIsomorphicLayoutEffect(() => {
    applyDirectStyles(instance);
  });
  return instance;
}
function useVirtualizer(options) {
  return useVirtualizerBase({
    observeElementRect: virtualCore.observeElementRect,
    observeElementOffset: virtualCore.observeElementOffset,
    scrollToFn: virtualCore.elementScroll,
    ...options
  });
}
function useWindowVirtualizer(options) {
  return useVirtualizerBase({
    getScrollElement: () => typeof document !== "undefined" ? window : null,
    observeElementRect: virtualCore.observeWindowRect,
    observeElementOffset: virtualCore.observeWindowOffset,
    scrollToFn: virtualCore.windowScroll,
    initialOffset: () => typeof document !== "undefined" ? window.scrollY : 0,
    ...options
  });
}
exports.useVirtualizer = useVirtualizer;
exports.useWindowVirtualizer = useWindowVirtualizer;
Object.keys(virtualCore).forEach((k) => {
  if (k !== "default" && !Object.prototype.hasOwnProperty.call(exports, k)) Object.defineProperty(exports, k, {
    enumerable: true,
    get: () => virtualCore[k]
  });
});
//# sourceMappingURL=index.cjs.map
