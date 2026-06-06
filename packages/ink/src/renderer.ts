import { logForDebugging } from './_internal/debug.js';
import { type DOMElement, markDirty } from './dom.js';
import type { Frame } from './frame.js';
import { consumeAbsoluteRemovedFlag } from './node-cache.js';
import Output from './output.js';
import renderNodeToOutput, {
  getScrollDrainNode,
  getScrollHint,
  resetLayoutShifted,
  resetScrollDrainNode,
  resetScrollHint,
} from './render-node-to-output.js';
import { createScreen, type StylePool } from './screen.js';

export type RenderOptions = {
  frontFrame: Frame;
  backFrame: Frame;
  isTTY: boolean;
  terminalWidth: number;
  terminalRows: number;
  altScreen: boolean;
  prevFrameContaminated: boolean;
};

export type Renderer = (options: RenderOptions) => Frame;

export default function createRenderer(
  node: DOMElement,
  stylePool: StylePool,
): Renderer {
  // The Output instance lives across frames so its char cache (tokenize +
  // grapheme clustering) accumulates — the same lines tend to come back
  // unchanged frame after frame, and the cache turns those into pointer
  // comparisons.
  let output: Output | undefined;
  return options => {
    const { frontFrame, backFrame, isTTY, terminalWidth, terminalRows } =
      options;
    const prevScreen = frontFrame.screen;
    const backScreen = backFrame.screen;
    // Pools are read off the back buffer rather than captured in the
    // closure because a generational reset may have swapped them out
    // between frames.
    const charPool = backScreen.charPool;
    const hyperlinkPool = backScreen.hyperlinkPool;

    // Bail out cleanly when the yoga node is missing or hasn't laid out.
    // getComputedHeight() returns NaN before calculateLayout() runs, and
    // we also defend against the negative/Infinity cases that would
    // throw RangeError when allocating cell arrays.
    const computedHeight = node.yogaNode?.getComputedHeight();
    const computedWidth = node.yogaNode?.getComputedWidth();
    const hasInvalidHeight =
      computedHeight === undefined ||
      !Number.isFinite(computedHeight) ||
      computedHeight < 0;
    const hasInvalidWidth =
      computedWidth === undefined ||
      !Number.isFinite(computedWidth) ||
      computedWidth < 0;

    if (!node.yogaNode || hasInvalidHeight || hasInvalidWidth) {
      // Log when there *is* a yoga node but its dimensions are nonsense —
      // that case shouldn't occur and the diagnostic helps trace it.
      if (node.yogaNode && (hasInvalidHeight || hasInvalidWidth)) {
        logForDebugging(
          `Invalid yoga dimensions: width=${computedWidth}, height=${computedHeight}, ` +
            `childNodes=${node.childNodes.length}, terminalWidth=${terminalWidth}, terminalRows=${terminalRows}`,
        );
      }
      return {
        screen: createScreen(
          terminalWidth,
          0,
          stylePool,
          charPool,
          hyperlinkPool,
        ),
        viewport: { width: terminalWidth, height: terminalRows },
        cursor: { x: 0, y: 0, visible: true },
      };
    }

    const width = Math.floor(node.yogaNode.getComputedWidth());
    const yogaHeight = Math.floor(node.yogaNode.getComputedHeight());
    // Alt-screen invariant: the screen buffer IS the alt buffer and must
    // be exactly terminalRows tall. The expected shape is a top-level
    // <Box height={rows} flexShrink={0}>, in which case yogaHeight equals
    // terminalRows naturally. When a sibling escapes that box (a layout
    // bug we've actually shipped — a stray component rendered next to the
    // fullscreen layout instead of inside it), yogaHeight exceeds
    // terminalRows and several downstream invariants collapse: the
    // viewport +1 trick, the cursor.y clamp, log-update's heightDelta===0
    // fast path. Clamping here makes the offending sibling invisible
    // (overflow cells land past screen.height and setCellAt drops them)
    // instead of corrupting the whole terminal — and we log so the
    // sibling is easy to find.
    const height = options.altScreen ? terminalRows : yogaHeight;
    if (options.altScreen && yogaHeight > terminalRows) {
      logForDebugging(
        `alt-screen: yoga height ${yogaHeight} > terminalRows ${terminalRows} — ` +
          `something is rendering outside <AlternateScreen>. Overflow clipped.`,
        { level: 'warn' },
      );
    }
    const screen =
      backScreen ??
      createScreen(width, height, stylePool, charPool, hyperlinkPool);
    if (output) {
      output.reset(width, height, screen);
    } else {
      output = new Output({ width, height, stylePool, screen });
    }

    resetLayoutShifted();
    resetScrollHint();
    resetScrollDrainNode();

    // Decide whether the blit fast path can use prevScreen. Two
    // independent contamination sources to consider:
    //   - prevFrameContaminated (passed in): selection overlay, alt-screen
    //     reset, or forceRedraw mutated the screen after we returned it.
    //   - absoluteRemoved (consumed here): an absolute-positioned node
    //     was removed since last frame. Its painted cells may have
    //     covered non-siblings (e.g. an overlay over an unrelated
    //     ScrollBox earlier in tree order), so blitting from prevScreen
    //     would restore those pixels. hasRemovedChild only protects
    //     direct siblings; absolute removals reach across subtrees.
    // Normal-flow removals don't paint outside their subtree and are safe.
    const absoluteRemoved = consumeAbsoluteRemovedFlag(node);
    renderNodeToOutput(node, output, {
      prevScreen:
        absoluteRemoved || options.prevFrameContaminated
          ? undefined
          : prevScreen,
    });

    const renderedScreen = output.get();

    // Drain continuation: when a ScrollBox is in the middle of a multi-
    // frame scroll drain, the render that just finished cleared its
    // dirty flag, which would make the root blit skip the subtree next
    // frame. Re-mark it so the next frame walks back in. This has to run
    // AFTER renderNodeToOutput; otherwise the end-of-render dirty clear
    // would overwrite the mark.
    const drainNode = getScrollDrainNode();
    if (drainNode) markDirty(drainNode);

    return {
      scrollHint: options.altScreen ? getScrollHint() : null,
      scrollDrainPending: drainNode !== null,
      screen: renderedScreen,
      viewport: {
        width: terminalWidth,
        // Alt-screen viewport height is reported as `rows + 1` so the
        // diff layer's `screen.height >= viewport.height` predicate
        // (which would otherwise treat exactly-filling content as
        // overflowing into scrollback) never fires. Alt-screen content
        // is always exactly `rows` tall via the wrapping <Box>, and the
        // cursor never actually moves because we clamp it below — the
        // fake +1 just keeps log-update from issuing a full-reset.
        height: options.altScreen ? terminalRows + 1 : terminalRows,
      },
      cursor: {
        x: 0,
        // Keep the cursor inside the visible viewport in alt-screen mode.
        // If we let cursor.y = screen.height when screen.height ===
        // terminalRows exactly (the steady state when content fills the
        // alt buffer), log-update's cursor-restore emits an LF on the
        // last row, scrolls one row off the top of the alt buffer, and
        // desyncs our diff model. The cursor itself is hidden in this
        // mode so the y value only matters for the diff coords.
        y: options.altScreen
          ? Math.max(0, Math.min(screen.height, terminalRows) - 1)
          : screen.height,
        // Hide the cursor while there's dynamic content to repaint;
        // leaving it visible would smear it across the diff path. In
        // non-TTY mode we keep it visible since nothing animates anyway.
        visible: !isTTY || screen.height === 0,
      },
    };
  };
}
