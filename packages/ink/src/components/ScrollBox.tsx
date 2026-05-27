import React, { type PropsWithChildren, type Ref, useImperativeHandle, useRef, useState } from 'react';
import type { Except } from 'type-fest';
import { markScrollActivity } from '../_internal/state.js';
import type { DOMElement } from '../dom.js';
import { markDirty, scheduleRenderFrom } from '../dom.js';
import { markCommitStart } from '../reconciler.js';
import type { Styles } from '../styles.js';
import Box from './Box.js';

export type ScrollBoxHandle = {
  /** Set absolute scrollTop in lines. Breaks stickiness. */
  scrollTo: (y: number) => void;
  /** Adjust scrollTop by `dy` lines. Breaks stickiness. */
  scrollBy: (dy: number) => void;
  scrollToElement: (el: DOMElement, offset?: number) => void;
  scrollToBottom: () => void;
  getScrollTop: () => number;
  getPendingDelta: () => number;
  getScrollHeight: () => number;
  getFreshScrollHeight: () => number;
  getViewportHeight: () => number;
  getViewportTop: () => number;
  /** `true` when scroll is pinned to the bottom. */
  isSticky: () => boolean;
  subscribe: (listener: () => void) => () => void;
  /** Restrict render-time scrollTop to `[min, max]`. */
  setClampBounds: (min: number | undefined, max: number | undefined) => void;
};

export type ScrollBoxProps = Except<Styles, 'textWrap' | 'overflow' | 'overflowX' | 'overflowY'> & {
  ref?: Ref<ScrollBoxHandle>;
  stickyScroll?: boolean;
};

/** A Box with `overflow: scroll` and an imperative scroll API. */
function ScrollBox({
  children,
  ref,
  stickyScroll,
  ...style
}: PropsWithChildren<ScrollBoxProps>): React.ReactNode {
  const domRef = useRef<DOMElement>(null);
  // The wheel-event hot path skips React entirely: `scrollTo`/`scrollBy`
  // mutate `scrollTop` on the DOM node, mark it dirty, and invoke the
  // throttled `scheduleRender` directly. The renderer reads `scrollTop`
  // off the node at render time, so no React state and no reconciler
  // pass is needed per wheel event. A microtask defer coalesces all
  // `scrollBy` calls inside one input batch (discreteUpdates) into a
  // single render — without it, `scheduleRender`'s leading edge would
  // fire on the first event with the pre-mutation `scrollTop` and lose
  // every subsequent delta in that batch. `scrollToBottom` still goes
  // through a React render because stickiness is observed via attribute
  // and there's no DOM-only sticky path.
  const [, forceRender] = useState(0);
  const listenersRef = useRef(new Set<() => void>());
  const renderQueuedRef = useRef(false);
  const notify = () => {
    for (const l of listenersRef.current) l();
  };
  function scrollMutated(el: DOMElement): void {
    // Tell background intervals (IDE poll, LSP poll, notification
    // dampening, ...) that the user is actively driving the UI so they
    // skip their next idle-gated tick. Without this, a long scroll
    // drain competes with those polls for the event loop and produces
    // visible frame gaps in the hundreds of milliseconds.
    markScrollActivity();
    markDirty(el);
    markCommitStart();
    notify();
    if (renderQueuedRef.current) return;
    renderQueuedRef.current = true;
    queueMicrotask(() => {
      renderQueuedRef.current = false;
      scheduleRenderFrom(el);
    });
  }
  useImperativeHandle(ref, (): ScrollBoxHandle => ({
    scrollTo(y: number) {
      const el = domRef.current;
      if (!el) return;
      // Explicit `false` here (rather than `undefined`) overrides the
      // DOM attribute so manual scroll breaks stickiness. The render
      // code reads `el.stickyScroll ?? attributes.stickyScroll`, so a
      // missing value would still inherit the attribute.
      el.stickyScroll = false;
      el.pendingScrollDelta = undefined;
      el.scrollAnchor = undefined;
      el.scrollTop = Math.max(0, Math.floor(y));
      scrollMutated(el);
    },
    scrollToElement(el: DOMElement, offset = 0) {
      const box = domRef.current;
      if (!box) return;
      box.stickyScroll = false;
      box.pendingScrollDelta = undefined;
      box.scrollAnchor = {
        el,
        offset
      };
      scrollMutated(box);
    },
    scrollBy(dy: number) {
      const el = domRef.current;
      if (!el) return;
      el.stickyScroll = false;
      // User-driven wheel input takes precedence over any in-flight
      // `scrollToElement` anchor seek — cancel it.
      el.scrollAnchor = undefined;
      // Accumulate into `pendingScrollDelta`; the renderer drains it at
      // a capped rate so a fast flick yields a few intermediate frames
      // rather than a single jump. Because this is a pure accumulator,
      // an up-then-down nudge naturally cancels itself.
      el.pendingScrollDelta = (el.pendingScrollDelta ?? 0) + Math.floor(dy);
      scrollMutated(el);
    },
    scrollToBottom() {
      const el = domRef.current;
      if (!el) return;
      el.pendingScrollDelta = undefined;
      el.stickyScroll = true;
      markDirty(el);
      notify();
      forceRender(n => n + 1);
    },
    getScrollTop() {
      return domRef.current?.scrollTop ?? 0;
    },
    getPendingDelta() {
      // The delta accumulated by `scrollBy` but not yet drained.
      // `useVirtualScroll` reads this so it can mount the union of the
      // committed range and the pending one — otherwise the drain
      // frames in between would find no children mounted at their
      // target offsets and render a blank slice.
      return domRef.current?.pendingScrollDelta ?? 0;
    },
    getScrollHeight() {
      return domRef.current?.scrollHeight ?? 0;
    },
    getFreshScrollHeight() {
      const content = domRef.current?.childNodes[0] as DOMElement | undefined;
      return content?.yogaNode?.getComputedHeight() ?? domRef.current?.scrollHeight ?? 0;
    },
    getViewportHeight() {
      return domRef.current?.scrollViewportHeight ?? 0;
    },
    getViewportTop() {
      return domRef.current?.scrollViewportTop ?? 0;
    },
    isSticky() {
      const el = domRef.current;
      if (!el) return false;
      return el.stickyScroll ?? Boolean(el.attributes['stickyScroll']);
    },
    subscribe(listener: () => void) {
      listenersRef.current.add(listener);
      return () => listenersRef.current.delete(listener);
    },
    setClampBounds(min, max) {
      const el = domRef.current;
      if (!el) return;
      el.scrollClampMin = min;
      el.scrollClampMax = max;
    }
  }),
  // `notify` and `scrollMutated` are inline (no `useCallback`) but only
  // close over refs and module-level imports, so they're stable across
  // renders. Empty deps prevents rebuilding the imperative handle on
  // every render — re-registering would churn the parent's ref and
  // cause subscribers to detach/reattach for no reason.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  []);

  // Layout structure:
  //   outer ink-box (overflow:scroll, constrained height)
  //     -> inner Box (flexGrow:1, flexShrink:0)
  //
  // The inner box fills at least the viewport (flexGrow:1) but is free
  // to grow taller for long content (flexShrink:0). flexGrow:1 also
  // lets children use spacers to pin elements to the bottom of the
  // scroll area. Yoga's Overflow.Scroll prevents the outer viewport
  // from growing to fit the inner content, which is what makes the
  // overflow happen in the first place. The renderer reads
  // `scrollHeight` from the inner content box and culls its children
  // against `scrollTop`.
  //
  // `stickyScroll` is set directly on `ink-box` (not via the ref) so
  // it's available on the very first render. Ref callbacks only run
  // after commit, which is too late for the first frame — we'd render
  // one non-sticky frame before catching up.
  return <ink-box ref={(el: DOMElement | null) => {
    domRef.current = el;
    if (el) el.scrollTop ??= 0;
  }} style={{
    flexWrap: 'nowrap',
    flexDirection: style.flexDirection ?? 'row',
    flexGrow: style.flexGrow ?? 0,
    flexShrink: style.flexShrink ?? 1,
    ...style,
    overflowX: 'scroll',
    overflowY: 'scroll'
  }} {...stickyScroll ? {
    stickyScroll: true
  } : {}}>
      <Box flexDirection="column" flexGrow={1} flexShrink={0} width="100%">
        {children}
      </Box>
    </ink-box>;
}
export default ScrollBox;
