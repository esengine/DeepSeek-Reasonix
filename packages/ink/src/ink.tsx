// @ts-nocheck — react-reconciler internals (FiberRoot, ConcurrentRoot) lag the runtime types.

import autoBind from 'auto-bind';
import { closeSync, constants as fsConstants, openSync, readSync, writeSync } from 'fs';
import noop from 'lodash-es/noop.js';
import throttle from 'lodash-es/throttle.js';
import React, { type ReactNode } from 'react';
import type { FiberRoot } from 'react-reconciler';
import { ConcurrentRoot } from 'react-reconciler/constants.js';
import { onExit } from 'signal-exit';
import { flushInteractionTime } from './_internal/state.js';
import { getYogaCounters } from './_internal/yoga-layout.js';
import { logForDebugging } from './_internal/debug.js';
import { logError } from './_internal/log.js';
import { format } from 'util';
import { colorize } from './colorize.js';
import App from './components/App.js';
import type { CursorDeclaration, CursorDeclarationSetter } from './components/CursorDeclarationContext.js';
import { FRAME_INTERVAL_MS } from './constants.js';
import * as dom from './dom.js';
import { KeyboardEvent } from './events/keyboard-event.js';
import { FocusManager } from './focus.js';
import { emptyFrame, type Frame, type FrameEvent } from './frame.js';
import { dispatchClick, dispatchHover } from './hit-test.js';
import instances from './instances.js';
import { LogUpdate } from './log-update.js';
import { nodeCache } from './node-cache.js';
import { optimize } from './optimizer.js';
import Output from './output.js';
import type { ParsedKey } from './parse-keypress.js';
import reconciler, { dispatcher, getLastCommitMs, getLastYogaMs, isDebugRepaintsEnabled, recordYogaMs, resetProfileCounters } from './reconciler.js';
import renderNodeToOutput, { consumeFollowScroll, didLayoutShift } from './render-node-to-output.js';
import { applyPositionedHighlight, type MatchPosition, scanPositions } from './render-to-screen.js';
import createRenderer, { type Renderer } from './renderer.js';
import { CellWidth, CharPool, cellAt, createScreen, HyperlinkPool, isEmptyCellAt, migrateScreenPools, StylePool } from './screen.js';
import { applySearchHighlight } from './searchHighlight.js';
import { applySelectionOverlay, captureScrolledRows, clearSelection, createSelectionState, extendSelection, type FocusMove, findPlainTextUrlAt, getSelectedText, hasSelection, moveFocus, type SelectionState, selectLineAt, selectWordAt, shiftAnchor, shiftSelection, shiftSelectionForFollow, startSelection, updateSelection } from './selection.js';
import { SYNC_OUTPUT_SUPPORTED, supportsExtendedKeys, type Terminal, writeDiffToTerminal } from './terminal.js';
import { CURSOR_HOME, cursorMove, cursorPosition, DISABLE_KITTY_KEYBOARD, DISABLE_MODIFY_OTHER_KEYS, ENABLE_KITTY_KEYBOARD, ENABLE_MODIFY_OTHER_KEYS, ERASE_SCREEN } from './termio/csi.js';
import { DBP, DFE, DISABLE_MOUSE_TRACKING, ENABLE_MOUSE_TRACKING, ENTER_ALT_SCREEN, EXIT_ALT_SCREEN, SHOW_CURSOR } from './termio/dec.js';
import { CLEAR_ITERM2_PROGRESS, CLEAR_TAB_STATUS, setClipboard, supportsTabStatus, wrapForMultiplexer } from './termio/osc.js';
import { TerminalWriteProvider } from './useTerminalNotification.js';

// In alt-screen mode the cursor is always hidden: `renderer.ts` derives
// `cursor.visible = !isTTY || screen.height === 0`, both of which are
// false here (TTY plus content that fills the screen). Freezing a
// single shared object instead of building one each frame removes one
// allocation from the hot path.
const ALT_SCREEN_ANCHOR_CURSOR = Object.freeze({
  x: 0,
  y: 0,
  visible: false
});
const CURSOR_HOME_PATCH = Object.freeze({
  type: 'stdout' as const,
  content: CURSOR_HOME
});
const ERASE_THEN_HOME_PATCH = Object.freeze({
  type: 'stdout' as const,
  content: ERASE_SCREEN + CURSOR_HOME
});

// Park-cursor escape cached per `Ink` instance and invalidated whenever
// the terminal resizes. In alt-screen mode `frame.cursor.y` is always
// `terminalRows - 1` (see `renderer.ts`), so a single CSI move suffices.
function makeAltScreenParkPatch(terminalRows: number) {
  return Object.freeze({
    type: 'stdout' as const,
    content: cursorPosition(terminalRows, 1)
  });
}
export type Options = {
  stdout: NodeJS.WriteStream;
  stdin: NodeJS.ReadStream;
  stderr: NodeJS.WriteStream;
  exitOnCtrlC: boolean;
  patchConsole: boolean;
  waitUntilExit?: () => Promise<void>;
  onFrame?: (event: FrameEvent) => void;
};
export default class Ink {
  private readonly log: LogUpdate;
  private readonly terminal: Terminal;
  private scheduleRender: (() => void) & {
    cancel?: () => void;
  };
  // Ignore last render after unmounting a tree to prevent empty output before exit
  private isUnmounted = false;
  private isPaused = false;
  private readonly container: FiberRoot;
  private rootNode: dom.DOMElement;
  readonly focusManager: FocusManager;
  private renderer: Renderer;
  private readonly stylePool: StylePool;
  private charPool: CharPool;
  private hyperlinkPool: HyperlinkPool;
  private exitPromise?: Promise<void>;
  private restoreConsole?: () => void;
  private restoreStderr?: () => void;
  private readonly unsubscribeTTYHandlers?: () => void;
  private terminalColumns: number;
  private terminalRows: number;
  private currentNode: ReactNode = null;
  private frontFrame: Frame;
  private backFrame: Frame;
  private lastPoolResetTime = performance.now();
  private drainTimer: ReturnType<typeof setTimeout> | null = null;
  private lastYogaCounters: {
    ms: number;
    visited: number;
    measured: number;
    cacheHits: number;
    live: number;
  } = {
    ms: 0,
    visited: 0,
    measured: 0,
    cacheHits: 0,
    live: 0
  };
  private altScreenParkPatch: Readonly<{
    type: 'stdout';
    content: string;
  }>;
  // Text-selection state. Only relevant in alt-screen mode. Lives on
  // the Ink instance so the selection-overlay pass in `onRender` can
  // read it and `App.tsx` can mutate it in response to mouse events.
  // Made public so consumers reached via `instances.get(...)` can
  // read selection state without re-implementing it.
  readonly selection: SelectionState = createSelectionState();
  // Search highlight query (alt-screen only). Setter below triggers
  // scheduleRender; applySearchHighlight in onRender inverts matching cells.
  private searchHighlightQuery = '';
  // Position-based highlight (used by virtual-message-list search).
  // The owning component scans match positions exactly once via
  // `scanElementSubtree` when the target message is mounted, stores
  // them message-relative, and updates this struct on every frame so
  // `applyPositionedHighlight` can paint them.
  //   - `rowOffset` is the message's current screen-top, used to
  //     translate stored positions into absolute screen coordinates.
  //   - `currentIdx` selects the currently focused match (rendered in
  //     yellow); any other match uses the plain inverse overlay.
  //   - `null` clears the highlight entirely.
  // Navigation between matches is plain index arithmetic — there is no
  // re-scan, so there is no risk of a feedback loop with the renderer.
  private searchPositions: {
    positions: MatchPosition[];
    rowOffset: number;
    currentIdx: number;
  } | null = null;
  // React-land subscribers for selection state changes (useHasSelection).
  // Fired alongside the terminal repaint whenever the selection mutates
  // so UI (e.g. footer hints) can react to selection appearing/clearing.
  private readonly selectionListeners = new Set<() => void>();
  // DOM nodes currently under the pointer (mode-1003 motion). Held here
  // so App.tsx's handleMouseEvent is stateless — dispatchHover diffs
  // against this set and mutates it in place.
  private readonly hoveredNodes = new Set<dom.DOMElement>();
  // Set by <AlternateScreen> via setAltScreenActive(). Controls the
  // renderer's cursor.y clamping (keeps cursor in-viewport to avoid
  // LF-induced scroll when screen.height === terminalRows) and gates
  // alt-screen-aware SIGCONT/resize/unmount handling.
  private altScreenActive = false;
  // Set alongside altScreenActive so SIGCONT resume knows whether to
  // re-enable mouse tracking (not all <AlternateScreen> uses want it).
  private altScreenMouseTracking = false;
  // True when the previous frame's screen buffer cannot be trusted for
  // blit — selection overlay mutated it, resetFramesForAltScreen()
  // replaced it with blanks, or forceRedraw() reset it to 0×0. Forces
  // one full-render frame; steady-state frames after clear it and regain
  // the blit + narrow-damage fast path.
  private prevFrameContaminated = false;
  // Set by handleResize: prepend ERASE_SCREEN to the next onRender's patches
  // INSIDE the BSU/ESU block so clear+paint is atomic. Writing ERASE_SCREEN
  // synchronously in handleResize would leave the screen blank for the ~80ms
  // render() takes; deferring into the atomic block means old content stays
  // visible until the new frame is fully ready.
  private needsEraseBeforePaint = false;
  // Native cursor positioning: a component (via useDeclaredCursor) declares
  // where the terminal cursor should be parked after each frame. Terminal
  // emulators render IME preedit text at the physical cursor position, and
  // screen readers / screen magnifiers track it — so parking at the text
  // input's caret makes CJK input appear inline and lets a11y tools follow.
  private cursorDeclaration: CursorDeclaration | null = null;
  // Main-screen: physical cursor position after the declared-cursor move,
  // tracked separately from frame.cursor (which must stay at content-bottom
  // for log-update's relative-move invariants). Alt-screen doesn't need
  // this — every frame begins with CSI H. null = no move emitted last frame.
  private displayCursor: {
    x: number;
    y: number;
  } | null = null;
  constructor(private readonly options: Options) {
    autoBind(this);
    if (this.options.patchConsole) {
      this.restoreConsole = this.patchConsole();
      this.restoreStderr = this.patchStderr();
    }
    this.terminal = {
      stdout: options.stdout,
      stderr: options.stderr
    };
    this.terminalColumns = options.stdout.columns || 80;
    this.terminalRows = options.stdout.rows || 24;
    this.altScreenParkPatch = makeAltScreenParkPatch(this.terminalRows);
    this.stylePool = new StylePool();
    this.charPool = new CharPool();
    this.hyperlinkPool = new HyperlinkPool();
    this.frontFrame = emptyFrame(this.terminalRows, this.terminalColumns, this.stylePool, this.charPool, this.hyperlinkPool);
    this.backFrame = emptyFrame(this.terminalRows, this.terminalColumns, this.stylePool, this.charPool, this.hyperlinkPool);
    this.log = new LogUpdate({
      isTTY: options.stdout.isTTY as boolean | undefined || false,
      stylePool: this.stylePool
    });

    // scheduleRender is called from the reconciler's resetAfterCommit, which
    // runs BEFORE React's layout phase (ref attach + useLayoutEffect). Any
    // state set in layout effects — notably the cursorDeclaration from
    // useDeclaredCursor — would lag one commit behind if we rendered
    // synchronously. Deferring to a microtask runs onRender after layout
    // effects have committed, so the native cursor tracks the caret without
    // a one-keystroke lag. Same event-loop tick, so throughput is unchanged.
    // Test env uses onImmediateRender (direct onRender, no throttle) so
    // existing synchronous lastFrame() tests are unaffected.
    const deferredRender = (): void => queueMicrotask(this.onRender);
    this.scheduleRender = throttle(deferredRender, FRAME_INTERVAL_MS, {
      leading: true,
      trailing: true
    });

    // Ignore last render after unmounting a tree to prevent empty output before exit
    this.isUnmounted = false;

    // Unmount when process exits
    this.unsubscribeExit = onExit(this.unmount, {
      alwaysLast: false
    });
    if (options.stdout.isTTY) {
      options.stdout.on('resize', this.handleResize);
      process.on('SIGCONT', this.handleResume);
      this.unsubscribeTTYHandlers = () => {
        options.stdout.off('resize', this.handleResize);
        process.off('SIGCONT', this.handleResume);
      };
    }
    this.rootNode = dom.createNode('ink-root');
    this.focusManager = new FocusManager((target, event) => dispatcher.dispatchDiscrete(target, event));
    this.rootNode.focusManager = this.focusManager;
    this.renderer = createRenderer(this.rootNode, this.stylePool);
    this.rootNode.onRender = this.scheduleRender;
    this.rootNode.onImmediateRender = this.onRender;
    this.rootNode.onComputeLayout = () => {
      // Calculate layout during React's commit phase so useLayoutEffect hooks
      // have access to fresh layout data
      // Guard against accessing freed Yoga nodes after unmount
      if (this.isUnmounted) {
        return;
      }
      if (this.rootNode.yogaNode) {
        const t0 = performance.now();
        this.rootNode.yogaNode.setWidth(this.terminalColumns);
        this.rootNode.yogaNode.calculateLayout(this.terminalColumns);
        const ms = performance.now() - t0;
        recordYogaMs(ms);
        const c = getYogaCounters();
        this.lastYogaCounters = {
          ms,
          ...c
        };
      }
    };

    this.container = reconciler.createContainer(this.rootNode, ConcurrentRoot, null, false, null, 'id', noop,
    // onUncaughtError
    noop,
    // onCaughtError
    noop,
    // onRecoverableError
    noop // onDefaultTransitionIndicator
    );
    if (("production" as string) === 'development') {
      reconciler.injectIntoDevTools({
        bundleType: 0,
        // The `version` field here must report the React version we
        // were *built against*, not our own package version. React
        // DevTools uses it to pick the matching internal API surface;
        // reporting our own version causes it to fall back to a
        // generic renderer view with no component tree.
        version: '16.13.1',
        rendererPackageName: 'ink'
      });
    }
  }
  private handleResume = () => {
    if (!this.options.stdout.isTTY) {
      return;
    }

    // Alt screen: after SIGCONT, content is stale (shell may have written
    // to main screen, switching focus away) and mouse tracking was
    // disabled by handleSuspend.
    if (this.altScreenActive) {
      this.reenterAltScreen();
      return;
    }

    // Main screen: start fresh to prevent clobbering terminal content
    this.frontFrame = emptyFrame(this.frontFrame.viewport.height, this.frontFrame.viewport.width, this.stylePool, this.charPool, this.hyperlinkPool);
    this.backFrame = emptyFrame(this.backFrame.viewport.height, this.backFrame.viewport.width, this.stylePool, this.charPool, this.hyperlinkPool);
    this.log.reset();
    // Physical cursor position is unknown after the shell took over during
    // suspend. Clear displayCursor so the next frame's cursor preamble
    // doesn't emit a relative move from a stale park position.
    this.displayCursor = null;
  };

  // Deliberately *not* debounced. A debounce window opens a short
  // interval where `stdout.columns` already reflects the new size but
  // `this.terminalColumns` (and the Yoga layout that depends on it)
  // are still the old size. Any unrelated `scheduleRender` during that
  // window — a spinner tick, a clock tick — makes `log-update` notice
  // the width disagreement, force a screen clear, then the debounce
  // fires and clears again. The user sees a double blank-to-paint
  // flicker for every resize. Per-resize layout cost is already
  // bounded by `useVirtualScroll`'s height scaling, so synchronous
  // handling is the lesser evil.
  private handleResize = () => {
    const cols = this.options.stdout.columns || 80;
    const rows = this.options.stdout.rows || 24;
    // Terminals often emit 2+ resize events for one user action (window
    // settling). Same-dimension events are no-ops; skip to avoid redundant
    // frame resets and renders.
    if (cols === this.terminalColumns && rows === this.terminalRows) return;
    this.terminalColumns = cols;
    this.terminalRows = rows;
    this.altScreenParkPatch = makeAltScreenParkPatch(this.terminalRows);

    // Alt screen: reset frame buffers so the next render repaints from
    // scratch (prevFrameContaminated → every cell written, wrapped in
    // BSU/ESU — old content stays visible until the new frame swaps
    // atomically). Re-assert mouse tracking (some emulators reset it on
    // resize). Do NOT write ENTER_ALT_SCREEN: iTerm2 treats ?1049h as a
    // buffer clear even when already in alt — that's the blank flicker.
    // Self-healing re-entry (if something kicked us out of alt) is handled
    // by handleResume (SIGCONT) and the sleep-wake detector; resize itself
    // doesn't exit alt-screen. Do NOT write ERASE_SCREEN: render() below
    // can take ~80ms; erasing first leaves the screen blank that whole time.
    if (this.altScreenActive && !this.isPaused && this.options.stdout.isTTY) {
      if (this.altScreenMouseTracking) {
        this.options.stdout.write(ENABLE_MOUSE_TRACKING);
      }
      this.resetFramesForAltScreen();
      this.needsEraseBeforePaint = true;
    }

    // Re-render the React tree with updated props so the context value changes.
    // React's commit phase will call onComputeLayout() to recalculate yoga layout
    // with the new dimensions, then call onRender() to render the updated frame.
    // We don't call scheduleRender() here because that would render before the
    // layout is updated, causing a mismatch between viewport and content dimensions.
    if (this.currentNode !== null) {
      this.render(this.currentNode);
    }
  };
  resolveExitPromise: () => void = () => {};
  rejectExitPromise: (reason?: Error) => void = () => {};
  unsubscribeExit: () => void = () => {};

  /** Pause Ink and hand the terminal over to an external TUI (e.g. */
  enterAlternateScreen(): void {
    this.pause();
    this.suspendStdin();
    this.options.stdout.write(
    // Disable extended key reporting first — editors that don't speak
    // CSI-u (e.g. nano) show "Unknown sequence" for every Ctrl-<key> if
    // kitty/modifyOtherKeys stays active. exitAlternateScreen re-enables.
    DISABLE_KITTY_KEYBOARD + DISABLE_MODIFY_OTHER_KEYS + (this.altScreenMouseTracking ? DISABLE_MOUSE_TRACKING : '') + (
    // disable mouse (no-op if off)
    this.altScreenActive ? '' : '\x1b[?1049h') +
    // enter alt (already in alt if fullscreen)
    '\x1b[?1004l' +
    // disable focus reporting
    '\x1b[0m' +
    // reset attributes
    '\x1b[?25h' +
    // show cursor
    '\x1b[2J' +
    // clear screen
    '\x1b[H' // cursor home
    );
  }

  /** Resume Ink after an external TUI handoff with a full repaint. */
  exitAlternateScreen(): void {
    this.options.stdout.write((this.altScreenActive ? ENTER_ALT_SCREEN : '') +
    // re-enter alt — vim's rmcup dropped us to main
    '\x1b[2J' +
    // clear screen (now alt if fullscreen)
    '\x1b[H' + (
    // cursor home
    this.altScreenMouseTracking ? ENABLE_MOUSE_TRACKING : '') + (
    // re-enable mouse (skip if REASONIX_DISABLE_MOUSE)
    this.altScreenActive ? '' : '\x1b[?1049l') +
    // exit alt (non-fullscreen only)
    '\x1b[?25l' // hide cursor (Ink manages)
    );
    this.resumeStdin();
    if (this.altScreenActive) {
      this.resetFramesForAltScreen();
    } else {
      this.repaint();
    }
    this.resume();
    // Re-enable focus reporting and extended key reporting — terminal
    // editors (vim, nano, etc.) write their own modifyOtherKeys level on
    // entry and reset it on exit, leaving us unable to distinguish
    // ctrl+shift+<letter> from ctrl+<letter>. Pop-before-push keeps the
    // Kitty stack balanced (a well-behaved editor restores our entry, so
    // without the pop we'd accumulate depth on each editor round-trip).
    this.options.stdout.write('\x1b[?1004h' + (supportsExtendedKeys() ? DISABLE_KITTY_KEYBOARD + ENABLE_KITTY_KEYBOARD + ENABLE_MODIFY_OTHER_KEYS : ''));
  }
  onRender() {
    if (this.isUnmounted || this.isPaused) {
      return;
    }
    // Entering a render cancels any pending drain tick — this render will
    // handle the drain (and re-schedule below if needed). Prevents a
    // wheel-event-triggered render AND a drain-timer render both firing.
    if (this.drainTimer !== null) {
      clearTimeout(this.drainTimer);
      this.drainTimer = null;
    }

    // Reset the "last user activity" timestamp at frame start. Read via
    // the public `getLastInteractionTime`, the value is meant to mean
    // "ms since the last input event during this frame"; without the
    // flush, an app that committed once a day ago would still report
    // itself as "just interacted" forever.
    flushInteractionTime();
    const renderStart = performance.now();
    const terminalWidth = this.options.stdout.columns || 80;
    const terminalRows = this.options.stdout.rows || 24;
    const frame = this.renderer({
      frontFrame: this.frontFrame,
      backFrame: this.backFrame,
      isTTY: this.options.stdout.isTTY,
      terminalWidth,
      terminalRows,
      altScreen: this.altScreenActive,
      prevFrameContaminated: this.prevFrameContaminated
    });
    const rendererMs = performance.now() - renderStart;

    // Sticky/auto-follow scrolled the ScrollBox this frame. Translate the
    // selection by the same delta so the highlight stays anchored to the
    // TEXT (native terminal behavior — the selection walks up the screen
    // as content scrolls, eventually clipping at the top). frontFrame
    // still holds the PREVIOUS frame's screen (swap is at ~500 below), so
    // captureScrolledRows reads the rows that are about to scroll out
    // before they're overwritten — the text stays copyable until the
    // selection scrolls entirely off. During drag, focus tracks the mouse
    // (screen-local) so only anchor shifts — selection grows toward the
    // mouse as the anchor walks up. After release, both ends are text-
    // anchored and move as a block.
    const follow = consumeFollowScroll();
    if (follow && this.selection.anchor &&
    // Only translate if the selection is ON scrollbox content. Selections
    // in the footer/prompt/StickyPromptHeader are on static text — the
    // scroll doesn't move what's under them. Without this guard, a
    // footer selection would be shifted by -delta then clamped to
    // viewportBottom, teleporting it into the scrollbox. Mirror the
    // bounds check the deleted check() in ScrollKeybindingHandler had.
    this.selection.anchor.row >= follow.viewportTop && this.selection.anchor.row <= follow.viewportBottom) {
      const {
        delta,
        viewportTop,
        viewportBottom
      } = follow;
      // captureScrolledRows and shift* are a pair: capture grabs rows about
      // to scroll off, shift moves the selection endpoint so the same rows
      // won't intersect again next frame. Capturing without shifting leaves
      // the endpoint in place, so the SAME viewport rows re-intersect every
      // frame and scrolledOffAbove grows without bound — getSelectedText
      // then returns ever-growing text on each re-copy. Keep capture inside
      // each shift branch so the pairing can't be broken by a new guard.
      if (this.selection.isDragging) {
        if (hasSelection(this.selection)) {
          captureScrolledRows(this.selection, this.frontFrame.screen, viewportTop, viewportTop + delta - 1, 'above');
        }
        shiftAnchor(this.selection, -delta, viewportTop, viewportBottom);
      } else if (
      // Flag-3 guard: the anchor check above only proves ONE endpoint is
      // on scrollbox content. A drag from row 3 (scrollbox) into the
      // footer at row 6, then release, leaves focus outside the viewport
      // — shiftSelectionForFollow would clamp it to viewportBottom,
      // teleporting the highlight from static footer into the scrollbox.
      // Symmetric check: require BOTH ends inside to translate. A
      // straddling selection falls through to NEITHER shift NOR capture:
      // the footer endpoint pins the selection, text scrolls away under
      // the highlight, and getSelectedText reads the CURRENT screen
      // contents — no accumulation. Dragging branch doesn't need this:
      // shiftAnchor ignores focus, and the anchor DOES shift (so capture
      // is correct there even when focus is in the footer).
      !this.selection.focus || this.selection.focus.row >= viewportTop && this.selection.focus.row <= viewportBottom) {
        if (hasSelection(this.selection)) {
          captureScrolledRows(this.selection, this.frontFrame.screen, viewportTop, viewportTop + delta - 1, 'above');
        }
        const cleared = shiftSelectionForFollow(this.selection, -delta, viewportTop, viewportBottom);
        // Auto-clear (both ends overshot minRow) must notify React-land
        // so useHasSelection re-renders and the footer copy/escape hint
        // disappears. notifySelectionChange() would recurse into onRender;
        // fire the listeners directly — they schedule a React update for
        // LATER, they don't re-enter this frame.
        if (cleared) for (const cb of this.selectionListeners) cb();
      }
    }

    // Selection overlay: invert cell styles in the screen buffer itself,
    // so the diff picks up selection as ordinary cell changes and
    // LogUpdate remains a pure diff engine.
    //
    // Full-screen damage (PR #20120) is a correctness backstop for the
    // sibling-resize bleed: when flexbox siblings resize between frames
    // (spinner appears → bottom grows → scrollbox shrinks), the
    // cached-clear + clip-and-cull + setCellAt damage union can miss
    // transition cells at the boundary. But that only happens when layout
    // actually SHIFTS — didLayoutShift() tracks exactly this (any node's
    // cached yoga position/size differs from current, or a child was
    // removed). Steady-state frames (spinner rotate, clock tick, text
    // stream into fixed-height box) don't shift layout, so normal damage
    // bounds are correct and diffEach only compares the damaged region.
    //
    // Selection also requires full damage: overlay writes via setCellStyleId
    // which doesn't track damage, and prev-frame overlay cells need to be
    // compared when selection moves/clears. prevFrameContaminated covers
    // the frame-after-selection-clears case.
    let selActive = false;
    let hlActive = false;
    if (this.altScreenActive) {
      selActive = hasSelection(this.selection);
      if (selActive) {
        applySelectionOverlay(frame.screen, this.selection, this.stylePool);
      }
      // Scan-highlight: inverse on ALL visible matches (less/vim style).
      // Position-highlight (below) overlays CURRENT (yellow) on top.
      hlActive = applySearchHighlight(frame.screen, this.searchHighlightQuery, this.stylePool);
      // Position-based CURRENT: write yellow at positions[currentIdx] +
      // rowOffset. No scanning — positions came from a prior scan when
      // the message first mounted. Message-relative + rowOffset = screen.
      if (this.searchPositions) {
        const sp = this.searchPositions;
        const posApplied = applyPositionedHighlight(frame.screen, this.stylePool, sp.positions, sp.rowOffset, sp.currentIdx);
        hlActive = hlActive || posApplied;
      }
    }

    // Full-damage backstop: applies on BOTH alt-screen and main-screen.
    // Layout shifts (spinner appears, status line resizes) can leave stale
    // cells at sibling boundaries that per-node damage tracking misses.
    // Selection/highlight overlays write via setCellStyleId which doesn't
    // track damage. prevFrameContaminated covers the cleanup frame.
    if (didLayoutShift() || selActive || hlActive || this.prevFrameContaminated) {
      frame.screen.damage = {
        x: 0,
        y: 0,
        width: frame.screen.width,
        height: frame.screen.height
      };
    }

    // Alt-screen: anchor the physical cursor to (0,0) before every diff.
    // All cursor moves in log-update are RELATIVE to prev.cursor; if tmux
    // (or any emulator) perturbs the physical cursor out-of-band (status
    // bar refresh, pane redraw, Cmd+K wipe), the relative moves drift and
    // content creeps up 1 row/frame. CSI H resets the physical cursor;
    // passing prev.cursor=(0,0) makes the diff compute from the same spot.
    // Self-healing against any external cursor manipulation. Main-screen
    // can't do this — cursor.y tracks scrollback rows CSI H can't reach.
    // The CSI H write is deferred until after the diff is computed so we
    // can skip it for empty diffs (no writes → physical cursor unused).
    let prevFrame = this.frontFrame;
    if (this.altScreenActive) {
      prevFrame = {
        ...this.frontFrame,
        cursor: ALT_SCREEN_ANCHOR_CURSOR
      };
    }
    const tDiff = performance.now();
    const diff = this.log.render(prevFrame, frame, this.altScreenActive,
    // DECSTBM needs BSU/ESU atomicity — without it the outer terminal
    // renders the scrolled-but-not-yet-repainted intermediate state.
    // tmux is the main case (re-emits DECSTBM with its own timing and
    // doesn't implement DEC 2026, so SYNC_OUTPUT_SUPPORTED is false).
    SYNC_OUTPUT_SUPPORTED);
    const diffMs = performance.now() - tDiff;
    // Swap buffers
    this.backFrame = this.frontFrame;
    this.frontFrame = frame;

    // Periodically reset char/hyperlink pools to prevent unbounded growth
    // during long sessions. 5 minutes is infrequent enough that the O(cells)
    // migration cost is negligible. Reuses renderStart to avoid extra clock call.
    if (renderStart - this.lastPoolResetTime > 5 * 60 * 1000) {
      this.resetPools();
      this.lastPoolResetTime = renderStart;
    }
    const flickers: FrameEvent['flickers'] = [];
    for (const patch of diff) {
      if (patch.type === 'clearTerminal') {
        flickers.push({
          desiredHeight: frame.screen.height,
          availableHeight: frame.viewport.height,
          reason: patch.reason
        });
        if (isDebugRepaintsEnabled() && patch.debug) {
          const chain = dom.findOwnerChainAtRow(this.rootNode, patch.debug.triggerY);
          logForDebugging(`[REPAINT] full reset · ${patch.reason} · row ${patch.debug.triggerY}\n` + `  prev: "${patch.debug.prevLine}"\n` + `  next: "${patch.debug.nextLine}"\n` + `  culprit: ${chain.length ? chain.join(' < ') : '(no owner chain captured)'}`, {
            level: 'warn'
          });
        }
      }
    }
    const tOptimize = performance.now();
    const optimized = optimize(diff);
    const optimizeMs = performance.now() - tOptimize;
    const hasDiff = optimized.length > 0;
    if (this.altScreenActive && hasDiff) {
      // Prepend CSI H to anchor the physical cursor to (0,0) so
      // log-update's relative moves compute from a known spot (self-healing
      // against out-of-band cursor drift, see the ALT_SCREEN_ANCHOR_CURSOR
      // comment above). Append CSI row;1 H to park the cursor at the bottom
      // row (where the prompt input is) — without this, the cursor ends
      // wherever the last diff write landed (a different row every frame),
      // making iTerm2's cursor guide flicker as it chases the cursor.
      // BSU/ESU protects content atomicity but iTerm2's guide tracks cursor
      // position independently. Parking at bottom (not 0,0) keeps the guide
      // where the user's attention is.
      //
      // After resize, prepend ERASE_SCREEN too. The diff only writes cells
      // that changed; cells where new=blank and prev-buffer=blank get skipped
      // — but the physical terminal still has stale content there (shorter
      // lines at new width leave old-width text tails visible). ERASE inside
      // BSU/ESU is atomic: old content stays visible until the whole
      // erase+paint lands, then swaps in one go. Writing ERASE_SCREEN
      // synchronously in handleResize would blank the screen for the ~80ms
      // render() takes.
      if (this.needsEraseBeforePaint) {
        this.needsEraseBeforePaint = false;
        optimized.unshift(ERASE_THEN_HOME_PATCH);
      } else {
        optimized.unshift(CURSOR_HOME_PATCH);
      }
      optimized.push(this.altScreenParkPatch);
    }

    // Native cursor positioning: park the terminal cursor at the declared
    // position so IME preedit text renders inline and screen readers /
    // magnifiers can follow the input. nodeCache holds the absolute screen
    // rect populated by renderNodeToOutput this frame (including scrollTop
    // translation) — if the declared node didn't render (stale declaration
    // after remount, or scrolled out of view), it won't be in the cache
    // and no move is emitted.
    const decl = this.cursorDeclaration;
    const rect = decl !== null ? nodeCache.get(decl.node) : undefined;
    const target = decl !== null && rect !== undefined ? {
      x: rect.x + decl.relativeX,
      y: rect.y + decl.relativeY
    } : null;
    const parked = this.displayCursor;

    // Preserve the empty-diff zero-write fast path: skip all cursor writes
    // when nothing rendered AND the park target is unchanged.
    const targetMoved = target !== null && (parked === null || parked.x !== target.x || parked.y !== target.y);
    if (hasDiff || targetMoved || target === null && parked !== null) {
      // Main-screen preamble: log-update's relative moves assume the
      // physical cursor is at prevFrame.cursor. If last frame parked it
      // elsewhere, move back before the diff runs. Alt-screen's CSI H
      // already resets to (0,0) so no preamble needed.
      if (parked !== null && !this.altScreenActive && hasDiff) {
        const pdx = prevFrame.cursor.x - parked.x;
        const pdy = prevFrame.cursor.y - parked.y;
        if (pdx !== 0 || pdy !== 0) {
          optimized.unshift({
            type: 'stdout',
            content: cursorMove(pdx, pdy)
          });
        }
      }
      if (target !== null) {
        if (this.altScreenActive) {
          // Absolute CUP (1-indexed); next frame's CSI H resets regardless.
          // Emitted after altScreenParkPatch so the declared position wins.
          const row = Math.min(Math.max(target.y + 1, 1), terminalRows);
          const col = Math.min(Math.max(target.x + 1, 1), terminalWidth);
          optimized.push({
            type: 'stdout',
            content: cursorPosition(row, col)
          });
        } else {
          // After the diff (or preamble), cursor is at frame.cursor. If no
          // diff AND previously parked, it's still at the old park position
          // (log-update wrote nothing). Otherwise it's at frame.cursor.
          const from = !hasDiff && parked !== null ? parked : {
            x: frame.cursor.x,
            y: frame.cursor.y
          };
          const dx = target.x - from.x;
          const dy = target.y - from.y;
          if (dx !== 0 || dy !== 0) {
            optimized.push({
              type: 'stdout',
              content: cursorMove(dx, dy)
            });
          }
        }
        this.displayCursor = target;
      } else {
        // Declaration cleared (input blur, unmount). Restore physical cursor
        // to frame.cursor before forgetting the park position — otherwise
        // displayCursor=null lies about where the cursor is, and the NEXT
        // frame's preamble (or log-update's relative moves) computes from a
        // wrong spot. The preamble above handles hasDiff; this handles
        // !hasDiff (e.g. accessibility mode where blur doesn't change
        // renderedValue since invert is identity).
        if (parked !== null && !this.altScreenActive && !hasDiff) {
          const rdx = frame.cursor.x - parked.x;
          const rdy = frame.cursor.y - parked.y;
          if (rdx !== 0 || rdy !== 0) {
            optimized.push({
              type: 'stdout',
              content: cursorMove(rdx, rdy)
            });
          }
        }
        this.displayCursor = null;
      }
    }
    const tWrite = performance.now();
    writeDiffToTerminal(this.terminal, optimized, this.altScreenActive && !SYNC_OUTPUT_SUPPORTED);
    const writeMs = performance.now() - tWrite;

    // Update blit safety for the NEXT frame. The frame just rendered
    // becomes frontFrame (= next frame's prevScreen). If we applied the
    // selection overlay, that buffer has inverted cells. selActive/hlActive
    // are only ever true in alt-screen; in main-screen this is false→false.
    this.prevFrameContaminated = selActive || hlActive;

    // A ScrollBox has pendingScrollDelta left to drain — schedule the next
    // frame. MUST NOT call this.scheduleRender() here: we're inside a
    // trailing-edge throttle invocation, timerId is undefined, and lodash's
    // debounce sees timeSinceLastCall >= wait (last call was at the start
    // of this window) → leadingEdge fires IMMEDIATELY → double render ~0.1ms
    // apart → jank. Use a plain timeout. If a wheel event arrives first,
    // its scheduleRender path fires a render which clears this timer at
    // the top of onRender — no double.
    //
    // Drain frames are cheap (DECSTBM + ~10 patches, ~200 bytes) so run at
    // quarter interval (~250fps, setTimeout practical floor) for max scroll
    // speed. Regular renders stay at FRAME_INTERVAL_MS via the throttle.
    if (frame.scrollDrainPending) {
      this.drainTimer = setTimeout(() => this.onRender(), FRAME_INTERVAL_MS >> 2);
    }
    const yogaMs = getLastYogaMs();
    const commitMs = getLastCommitMs();
    const yc = this.lastYogaCounters;
    // Reset so drain-only frames (no React commit) don't repeat stale values.
    resetProfileCounters();
    this.lastYogaCounters = {
      ms: 0,
      visited: 0,
      measured: 0,
      cacheHits: 0,
      live: 0
    };
    this.options.onFrame?.({
      durationMs: performance.now() - renderStart,
      phases: {
        renderer: rendererMs,
        diff: diffMs,
        optimize: optimizeMs,
        write: writeMs,
        patches: diff.length,
        yoga: yogaMs,
        commit: commitMs,
        yogaVisited: yc.visited,
        yogaMeasured: yc.measured,
        yogaCacheHits: yc.cacheHits,
        yogaLive: yc.live
      },
      flickers
    });
  }
  pause(): void {
    // Flush pending React updates and render before pausing.
    reconciler.flushSyncFromReconciler();
    this.onRender();
    this.isPaused = true;
  }
  resume(): void {
    this.isPaused = false;
    this.onRender();
  }

  /** Reset frame buffers so the next render writes the full screen from scratch. */
  repaint(): void {
    this.frontFrame = emptyFrame(this.frontFrame.viewport.height, this.frontFrame.viewport.width, this.stylePool, this.charPool, this.hyperlinkPool);
    this.backFrame = emptyFrame(this.backFrame.viewport.height, this.backFrame.viewport.width, this.stylePool, this.charPool, this.hyperlinkPool);
    this.log.reset();
    // Physical cursor position is unknown after external terminal corruption.
    // Clear displayCursor so the cursor preamble doesn't emit a stale
    // relative move from where we last parked it.
    this.displayCursor = null;
  }

  forceRedraw(): void {
    if (!this.options.stdout.isTTY || this.isUnmounted || this.isPaused) return;
    this.options.stdout.write(ERASE_SCREEN + CURSOR_HOME);
    if (this.altScreenActive) {
      this.resetFramesForAltScreen();
    } else {
      this.repaint();
      // repaint() resets frontFrame to 0×0. Without this flag the next
      // frame's blit optimization copies from that empty screen and the
      // diff sees no content. onRender resets the flag at frame end.
      this.prevFrameContaminated = true;
    }
    this.onRender();
  }

  invalidatePrevFrame(): void {
    this.prevFrameContaminated = true;
  }

  /** Called by the <AlternateScreen> component on mount/unmount. */
  setAltScreenActive(active: boolean, mouseTracking = false): void {
    if (this.altScreenActive === active) return;
    this.altScreenActive = active;
    this.altScreenMouseTracking = active && mouseTracking;
    if (active) {
      this.resetFramesForAltScreen();
    } else {
      this.repaint();
    }
  }
  get isAltScreenActive(): boolean {
    return this.altScreenActive;
  }

  reassertTerminalModes = (includeAltScreen = false): void => {
    if (!this.options.stdout.isTTY) return;
    // Don't touch the terminal during an editor handoff — re-enabling kitty
    // keyboard here would undo enterAlternateScreen's disable and nano would
    // start seeing CSI-u sequences again.
    if (this.isPaused) return;
    // Extended keys — re-assert if enabled (App.tsx enables these on
    // allowlisted terminals at raw-mode entry; a terminal reset clears them).
    // Pop-before-push keeps Kitty stack depth at 1 instead of accumulating
    // on each call.
    if (supportsExtendedKeys()) {
      this.options.stdout.write(DISABLE_KITTY_KEYBOARD + ENABLE_KITTY_KEYBOARD + ENABLE_MODIFY_OTHER_KEYS);
    }
    if (!this.altScreenActive) return;
    // Mouse tracking — idempotent, safe to re-assert on every stdin gap.
    if (this.altScreenMouseTracking) {
      this.options.stdout.write(ENABLE_MOUSE_TRACKING);
    }
    // Alt-screen re-entry — destructive (ERASE_SCREEN). Only for callers that
    // have a strong signal the terminal actually dropped mode 1049.
    if (includeAltScreen) {
      this.reenterAltScreen();
    }
  };

  /** Mark this instance as unmounted so future unmount() calls early-return. */
  detachForShutdown(): void {
    this.isUnmounted = true;
    // Cancel any pending throttled render so it doesn't fire between
    // cleanupTerminalModes() and process.exit() and write to main screen.
    this.scheduleRender.cancel?.();
    // Restore stdin from raw mode. unmount() used to do this via React
    // unmount (App.componentWillUnmount → handleSetRawMode(false)) but we're
    // short-circuiting that path. Must use this.options.stdin — NOT
    // process.stdin — because getStdinOverride() may have opened /dev/tty
    // when stdin is piped.
    const stdin = this.options.stdin as NodeJS.ReadStream & {
      isRaw?: boolean;
      setRawMode?: (m: boolean) => void;
    };
    this.drainStdin();
    if (stdin.isTTY && stdin.isRaw && stdin.setRawMode) {
      stdin.setRawMode(false);
    }
  }

  /** @see drainStdin */
  drainStdin(): void {
    drainStdin(this.options.stdin);
  }

  private reenterAltScreen(): void {
    this.options.stdout.write(ENTER_ALT_SCREEN + ERASE_SCREEN + CURSOR_HOME + (this.altScreenMouseTracking ? ENABLE_MOUSE_TRACKING : ''));
    this.resetFramesForAltScreen();
  }

  private resetFramesForAltScreen(): void {
    const rows = this.terminalRows;
    const cols = this.terminalColumns;
    const blank = (): Frame => ({
      screen: createScreen(cols, rows, this.stylePool, this.charPool, this.hyperlinkPool),
      viewport: {
        width: cols,
        height: rows + 1
      },
      cursor: {
        x: 0,
        y: 0,
        visible: true
      }
    });
    this.frontFrame = blank();
    this.backFrame = blank();
    this.log.reset();
    // Defense-in-depth: alt-screen skips the cursor preamble anyway (CSI H
    // resets), but a stale displayCursor would be misleading if we later
    // exit to main-screen without an intervening render.
    this.displayCursor = null;
    // Fresh frontFrame is blank rows×cols — blitting from it would copy
    // blanks over content. Next alt-screen frame must full-render.
    this.prevFrameContaminated = true;
  }

  copySelectionNoClear(): string {
    if (!hasSelection(this.selection)) return '';
    const text = getSelectedText(this.selection, this.frontFrame.screen);
    if (text) {
      // Raw OSC 52, or DCS-passthrough-wrapped OSC 52 inside tmux (tmux
      // drops it silently unless allow-passthrough is on — no regression).
      void setClipboard(text).then(raw => {
        if (raw) this.options.stdout.write(raw);
      });
    }
    return text;
  }

  copySelection(): string {
    if (!hasSelection(this.selection)) return '';
    const text = this.copySelectionNoClear();
    clearSelection(this.selection);
    this.notifySelectionChange();
    return text;
  }

  /** Clear the current text selection without copying. */
  clearTextSelection(): void {
    if (!hasSelection(this.selection)) return;
    clearSelection(this.selection);
    this.notifySelectionChange();
  }

  /** Set the search highlight query. */
  setSearchHighlight(query: string): void {
    if (this.searchHighlightQuery === query) return;
    this.searchHighlightQuery = query;
    this.scheduleRender();
  }

  scanElementSubtree(el: dom.DOMElement): MatchPosition[] {
    if (!this.searchHighlightQuery || !el.yogaNode) return [];
    const width = Math.ceil(el.yogaNode.getComputedWidth());
    const height = Math.ceil(el.yogaNode.getComputedHeight());
    if (width <= 0 || height <= 0) return [];
    // renderNodeToOutput adds el's OWN computedLeft/Top to offsetX/Y.
    // Passing -elLeft/-elTop nets to 0 → paints at (0,0) in our buffer.
    const elLeft = el.yogaNode.getComputedLeft();
    const elTop = el.yogaNode.getComputedTop();
    const screen = createScreen(width, height, this.stylePool, this.charPool, this.hyperlinkPool);
    const output = new Output({
      width,
      height,
      stylePool: this.stylePool,
      screen
    });
    renderNodeToOutput(el, output, {
      offsetX: -elLeft,
      offsetY: -elTop,
      prevScreen: undefined
    });
    const rendered = output.get();
    // renderNodeToOutput wrote our offset positions to nodeCache —
    // corrupts the main render (it'd blit from wrong coords). Mark the
    // subtree dirty so the next main render repaints + re-caches
    // correctly. One extra paint of this message, but correct > fast.
    dom.markDirty(el);
    const positions = scanPositions(rendered, this.searchHighlightQuery);
    logForDebugging(`scanElementSubtree: q='${this.searchHighlightQuery}' ` + `el=${width}x${height}@(${elLeft},${elTop}) n=${positions.length} ` + `[${positions.slice(0, 10).map(p => `${p.row}:${p.col}`).join(',')}` + `${positions.length > 10 ? ',…' : ''}]`);
    return positions;
  }

  /** Set the position-based highlight state. */
  setSearchPositions(state: {
    positions: MatchPosition[];
    rowOffset: number;
    currentIdx: number;
  } | null): void {
    this.searchPositions = state;
    this.scheduleRender();
  }

  /** Set the selection highlight background color. */
  setSelectionBgColor(color: string): void {
    // Wrap a NUL marker, then split on it to extract the open/close SGR.
    // colorize returns the input unchanged if the color string is bad —
    // no NUL-split then, so fall through to null (inverse fallback).
    const wrapped = colorize('\0', color, 'background');
    const nul = wrapped.indexOf('\0');
    if (nul <= 0 || nul === wrapped.length - 1) {
      this.stylePool.setSelectionBg(null);
      return;
    }
    this.stylePool.setSelectionBg({
      type: 'ansi',
      code: wrapped.slice(0, nul),
      endCode: wrapped.slice(nul + 1) // always \x1b[49m for bg
    });
    // No scheduleRender: this is called from a React effect that already
    // runs inside the render cycle, and the bg only matters once a
    // selection exists (which itself triggers a full-damage frame).
  }

  captureScrolledRows(firstRow: number, lastRow: number, side: 'above' | 'below'): void {
    captureScrolledRows(this.selection, this.frontFrame.screen, firstRow, lastRow, side);
  }

  /** Shift anchor AND focus by dRow, clamped to [minRow, maxRow]. */
  shiftSelectionForScroll(dRow: number, minRow: number, maxRow: number): void {
    const hadSel = hasSelection(this.selection);
    shiftSelection(this.selection, dRow, minRow, maxRow, this.frontFrame.screen.width);
    // shiftSelection clears when both endpoints overshoot the same edge
    // (Home/g/End/G page-jump past the selection). Notify subscribers so
    // useHasSelection updates. Safe to call notifySelectionChange here —
    // this runs from keyboard handlers, not inside onRender().
    if (hadSel && !hasSelection(this.selection)) {
      this.notifySelectionChange();
    }
  }

  /** Keyboard selection extension (shift+arrow/home/end). */
  moveSelectionFocus(move: FocusMove): void {
    if (!this.altScreenActive) return;
    const {
      focus
    } = this.selection;
    if (!focus) return;
    const {
      width,
      height
    } = this.frontFrame.screen;
    const maxCol = width - 1;
    const maxRow = height - 1;
    let {
      col,
      row
    } = focus;
    switch (move) {
      case 'left':
        if (col > 0) col--;else if (row > 0) {
          col = maxCol;
          row--;
        }
        break;
      case 'right':
        if (col < maxCol) col++;else if (row < maxRow) {
          col = 0;
          row++;
        }
        break;
      case 'up':
        if (row > 0) row--;
        break;
      case 'down':
        if (row < maxRow) row++;
        break;
      case 'lineStart':
        col = 0;
        break;
      case 'lineEnd':
        col = maxCol;
        break;
    }
    if (col === focus.col && row === focus.row) return;
    moveFocus(this.selection, col, row);
    this.notifySelectionChange();
  }

  /** Whether there is an active text selection. */
  hasTextSelection(): boolean {
    return hasSelection(this.selection);
  }

  /** Subscribe to selection state changes. */
  subscribeToSelectionChange(cb: () => void): () => void {
    this.selectionListeners.add(cb);
    return () => this.selectionListeners.delete(cb);
  }
  private notifySelectionChange(): void {
    this.onRender();
    for (const cb of this.selectionListeners) cb();
  }

  dispatchClick(col: number, row: number): boolean {
    if (!this.altScreenActive) return false;
    const blank = isEmptyCellAt(this.frontFrame.screen, col, row);
    return dispatchClick(this.rootNode, col, row, blank);
  }
  dispatchHover(col: number, row: number): void {
    if (!this.altScreenActive) return;
    dispatchHover(this.rootNode, col, row, this.hoveredNodes);
  }
  dispatchKeyboardEvent(parsedKey: ParsedKey): void {
    const target = this.focusManager.activeElement ?? this.rootNode;
    const event = new KeyboardEvent(parsedKey);
    dispatcher.dispatchDiscrete(target, event);

    // Tab cycling is the default action — only fires if no handler
    // called preventDefault(). Mirrors browser behavior.
    if (!event.defaultPrevented && parsedKey.name === 'tab' && !parsedKey.ctrl && !parsedKey.meta) {
      if (parsedKey.shift) {
        this.focusManager.focusPrevious(this.rootNode);
      } else {
        this.focusManager.focusNext(this.rootNode);
      }
    }
  }
  /** Look up the URL at (col, row) in the current front frame. */
  getHyperlinkAt(col: number, row: number): string | undefined {
    if (!this.altScreenActive) return undefined;
    const screen = this.frontFrame.screen;
    const cell = cellAt(screen, col, row);
    let url = cell?.hyperlink;
    // SpacerTail cells (right half of wide/CJK/emoji chars) store the
    // hyperlink on the head cell at col-1.
    if (!url && cell?.width === CellWidth.SpacerTail && col > 0) {
      url = cellAt(screen, col - 1, row)?.hyperlink;
    }
    return url ?? findPlainTextUrlAt(screen, col, row);
  }

  onHyperlinkClick: ((url: string) => void) | undefined;

  /** Stable prototype wrapper for onHyperlinkClick. */
  openHyperlink(url: string): void {
    this.onHyperlinkClick?.(url);
  }

  handleMultiClick(col: number, row: number, count: 2 | 3): void {
    if (!this.altScreenActive) return;
    const screen = this.frontFrame.screen;
    // selectWordAt/selectLineAt no-op on noSelect/out-of-bounds. Seed with
    // a char-mode selection so the press still starts a drag even if the
    // word/line scan finds nothing selectable.
    startSelection(this.selection, col, row);
    if (count === 2) selectWordAt(this.selection, screen, col, row);else selectLineAt(this.selection, screen, row);
    // Ensure hasSelection is true so release doesn't re-dispatch onClickAt.
    // selectWordAt no-ops on noSelect; selectLineAt no-ops out-of-bounds.
    if (!this.selection.focus) this.selection.focus = this.selection.anchor;
    this.notifySelectionChange();
  }

  /** Handle a drag-motion at (col, row). */
  handleSelectionDrag(col: number, row: number): void {
    if (!this.altScreenActive) return;
    const sel = this.selection;
    if (sel.anchorSpan) {
      extendSelection(sel, this.frontFrame.screen, col, row);
    } else {
      updateSelection(sel, col, row);
    }
    this.notifySelectionChange();
  }

  // Methods to properly suspend stdin for external editor usage
  // This is needed to prevent Ink from swallowing keystrokes when an external editor is active
  private stdinListeners: Array<{
    event: string;
    listener: (...args: unknown[]) => void;
  }> = [];
  private wasRawMode = false;
  suspendStdin(): void {
    const stdin = this.options.stdin;
    if (!stdin.isTTY) {
      return;
    }

    // Store and remove all 'readable' event listeners temporarily
    // This prevents Ink from consuming stdin while the editor is active
    const readableListeners = stdin.listeners('readable');
    logForDebugging(`[stdin] suspendStdin: removing ${readableListeners.length} readable listener(s), wasRawMode=${(stdin as NodeJS.ReadStream & {
      isRaw?: boolean;
    }).isRaw ?? false}`);
    readableListeners.forEach(listener => {
      this.stdinListeners.push({
        event: 'readable',
        listener: listener as (...args: unknown[]) => void
      });
      stdin.removeListener('readable', listener as (...args: unknown[]) => void);
    });

    // If raw mode is enabled, disable it temporarily
    const stdinWithRaw = stdin as NodeJS.ReadStream & {
      isRaw?: boolean;
      setRawMode?: (mode: boolean) => void;
    };
    if (stdinWithRaw.isRaw && stdinWithRaw.setRawMode) {
      stdinWithRaw.setRawMode(false);
      this.wasRawMode = true;
    }
  }
  resumeStdin(): void {
    const stdin = this.options.stdin;
    if (!stdin.isTTY) {
      return;
    }

    // Re-attach all the stored listeners
    if (this.stdinListeners.length === 0 && !this.wasRawMode) {
      logForDebugging('[stdin] resumeStdin: called with no stored listeners and wasRawMode=false (possible desync)', {
        level: 'warn'
      });
    }
    logForDebugging(`[stdin] resumeStdin: re-attaching ${this.stdinListeners.length} listener(s), wasRawMode=${this.wasRawMode}`);
    this.stdinListeners.forEach(({
      event,
      listener
    }) => {
      stdin.addListener(event, listener);
    });
    this.stdinListeners = [];

    // Re-enable raw mode if it was enabled before
    if (this.wasRawMode) {
      const stdinWithRaw = stdin as NodeJS.ReadStream & {
        setRawMode?: (mode: boolean) => void;
      };
      if (stdinWithRaw.setRawMode) {
        stdinWithRaw.setRawMode(true);
      }
      this.wasRawMode = false;
    }
  }

  // Stable identity for TerminalWriteContext. An inline arrow here would
  // change on every render() call (initial mount + each resize), which
  // cascades through useContext → <AlternateScreen>'s useLayoutEffect dep
  // array → spurious exit+re-enter of the alt screen on every SIGWINCH.
  private writeRaw(data: string): void {
    this.options.stdout.write(data);
  }
  private setCursorDeclaration: CursorDeclarationSetter = (decl, clearIfNode) => {
    if (decl === null && clearIfNode !== undefined && this.cursorDeclaration?.node !== clearIfNode) {
      return;
    }
    this.cursorDeclaration = decl;
  };
  render(node: ReactNode): void {
    this.currentNode = node;
    const tree = <App stdin={this.options.stdin} stdout={this.options.stdout} stderr={this.options.stderr} exitOnCtrlC={this.options.exitOnCtrlC} onExit={this.unmount} terminalColumns={this.terminalColumns} terminalRows={this.terminalRows} selection={this.selection} onSelectionChange={this.notifySelectionChange} onClickAt={this.dispatchClick} onHoverAt={this.dispatchHover} getHyperlinkAt={this.getHyperlinkAt} onOpenHyperlink={this.openHyperlink} onMultiClick={this.handleMultiClick} onSelectionDrag={this.handleSelectionDrag} onStdinResume={this.reassertTerminalModes} onCursorDeclaration={this.setCursorDeclaration} dispatchKeyboardEvent={this.dispatchKeyboardEvent}>
        <TerminalWriteProvider value={this.writeRaw}>
          {node}
        </TerminalWriteProvider>
      </App>;

    reconciler.updateContainerSync(tree, this.container, null, noop);
    reconciler.flushSyncWork();
  }
  unmount(error?: Error | number | null): void {
    if (this.isUnmounted) {
      return;
    }
    this.onRender();
    this.unsubscribeExit();
    if (typeof this.restoreConsole === 'function') {
      this.restoreConsole();
    }
    this.restoreStderr?.();
    this.unsubscribeTTYHandlers?.();

    // Non-TTY environments don't handle erasing ansi escapes well, so it's better to
    // only render last frame of non-static output
    const diff = this.log.renderPreviousOutput_DEPRECATED(this.frontFrame);
    writeDiffToTerminal(this.terminal, optimize(diff));

    // Clean up terminal modes synchronously before process exit.
    // React's componentWillUnmount won't run in time when process.exit() is called,
    // so we must reset terminal modes here to prevent escape sequence leakage.
    // Use writeSync to stdout (fd 1) to ensure writes complete before exit.
    // We unconditionally send all disable sequences because terminal detection
    // may not work correctly (e.g., in tmux, screen) and these are no-ops on
    // terminals that don't support them.
    /* eslint-disable custom-rules/no-sync-fs -- process exiting; async writes would be dropped */
    if (this.options.stdout.isTTY) {
      if (this.altScreenActive) {
        // <AlternateScreen>'s unmount effect won't run during signal-exit.
        // Exit alt screen FIRST so other cleanup sequences go to the main screen.
        writeSync(1, EXIT_ALT_SCREEN);
      }
      // Disable mouse tracking — unconditional because altScreenActive can be
      // stale if AlternateScreen's unmount (which flips the flag) raced a
      // blocked event loop + SIGINT. No-op if tracking was never enabled.
      writeSync(1, DISABLE_MOUSE_TRACKING);
      // Drain stdin so in-flight mouse events don't leak to the shell
      this.drainStdin();
      // Disable extended key reporting (both kitty and modifyOtherKeys)
      writeSync(1, DISABLE_MODIFY_OTHER_KEYS);
      writeSync(1, DISABLE_KITTY_KEYBOARD);
      // Disable focus events (DECSET 1004)
      writeSync(1, DFE);
      // Disable bracketed paste mode
      writeSync(1, DBP);
      // Show cursor
      writeSync(1, SHOW_CURSOR);
      // Clear iTerm2 progress bar
      writeSync(1, CLEAR_ITERM2_PROGRESS);
      // Clear tab status (OSC 21337) so a stale dot doesn't linger
      if (supportsTabStatus()) writeSync(1, wrapForMultiplexer(CLEAR_TAB_STATUS));
    }
    /* eslint-enable custom-rules/no-sync-fs */

    this.isUnmounted = true;

    // Cancel any pending throttled renders to prevent accessing freed Yoga nodes
    this.scheduleRender.cancel?.();
    if (this.drainTimer !== null) {
      clearTimeout(this.drainTimer);
      this.drainTimer = null;
    }

    reconciler.updateContainerSync(null, this.container, null, noop);
    reconciler.flushSyncWork();
    instances.delete(this.options.stdout);

    // Free the root yoga node, then clear its reference. Children are already
    // freed by the reconciler's removeChildFromContainer; using .free() (not
    // .freeRecursive()) avoids double-freeing them.
    this.rootNode.yogaNode?.free();
    this.rootNode.yogaNode = undefined;
    if (error instanceof Error) {
      this.rejectExitPromise(error);
    } else {
      this.resolveExitPromise();
    }
  }
  async waitUntilExit(): Promise<void> {
    this.exitPromise ||= new Promise((resolve, reject) => {
      this.resolveExitPromise = resolve;
      this.rejectExitPromise = reject;
    });
    return this.exitPromise;
  }
  resetLineCount(): void {
    if (this.options.stdout.isTTY) {
      // Swap so old front becomes back (for screen reuse), then reset front
      this.backFrame = this.frontFrame;
      this.frontFrame = emptyFrame(this.frontFrame.viewport.height, this.frontFrame.viewport.width, this.stylePool, this.charPool, this.hyperlinkPool);
      this.log.reset();
      // frontFrame is reset, so frame.cursor on the next render is (0,0).
      // Clear displayCursor so the preamble doesn't compute a stale delta.
      this.displayCursor = null;
    }
  }

  resetPools(): void {
    this.charPool = new CharPool();
    this.hyperlinkPool = new HyperlinkPool();
    migrateScreenPools(this.frontFrame.screen, this.charPool, this.hyperlinkPool);
    // Back frame's data is zeroed by resetScreen before reads, but its pool
    // references are used by the renderer to intern new characters. Point
    // them at the new pools so the next frame's IDs are comparable.
    this.backFrame.screen.charPool = this.charPool;
    this.backFrame.screen.hyperlinkPool = this.hyperlinkPool;
  }
  patchConsole(): () => void {
    // biome-ignore lint/suspicious/noConsole: intentionally patching global console
    const con = console;
    const originals: Partial<Record<keyof Console, Console[keyof Console]>> = {};
    const toDebug = (...args: unknown[]) => logForDebugging(`console.log: ${format(...args)}`);
    const toError = (...args: unknown[]) => logError(new Error(`console.error: ${format(...args)}`));
    for (const m of CONSOLE_STDOUT_METHODS) {
      originals[m] = con[m];
      con[m] = toDebug;
    }
    for (const m of CONSOLE_STDERR_METHODS) {
      originals[m] = con[m];
      con[m] = toError;
    }
    originals.assert = con.assert;
    con.assert = (condition: unknown, ...args: unknown[]) => {
      if (!condition) toError(...args);
    };
    return () => Object.assign(con, originals);
  }

  private patchStderr(): () => void {
    const stderr = process.stderr;
    const originalWrite = stderr.write;
    let reentered = false;
    const intercept = (chunk: Uint8Array | string, encodingOrCb?: BufferEncoding | ((err?: Error) => void), cb?: (err?: Error) => void): boolean => {
      const callback = typeof encodingOrCb === 'function' ? encodingOrCb : cb;
      // Reentrancy guard: logForDebugging → writeToStderr → here. Pass
      // through to the original so --debug-to-stderr still works and we
      // don't stack-overflow.
      if (reentered) {
        const encoding = typeof encodingOrCb === 'string' ? encodingOrCb : undefined;
        return originalWrite.call(stderr, chunk, encoding, callback);
      }
      reentered = true;
      try {
        const text = typeof chunk === 'string' ? chunk : Buffer.from(chunk).toString('utf8');
        logForDebugging(`[stderr] ${text}`, {
          level: 'warn'
        });
        if (this.altScreenActive && !this.isUnmounted && !this.isPaused) {
          this.prevFrameContaminated = true;
          this.scheduleRender();
        }
      } finally {
        reentered = false;
        callback?.();
      }
      return true;
    };
    stderr.write = intercept;
    return () => {
      if (stderr.write === intercept) {
        stderr.write = originalWrite;
      }
    };
  }
}

/* eslint-disable custom-rules/no-sync-fs -- must be sync; called from signal handler / unmount */
export function drainStdin(stdin: NodeJS.ReadStream = process.stdin): void {
  if (!stdin.isTTY) return;
  // Drain Node's stream buffer (bytes libuv already pulled in). read()
  // returns null when empty — never blocks.
  try {
    while (stdin.read() !== null) {
      /* discard */
    }
  } catch {
    /* stream may be destroyed */
  }
  // No /dev/tty on Windows; CONIN$ doesn't support O_NONBLOCK semantics.
  // Windows Terminal also doesn't buffer mouse reports the same way.
  if (process.platform === 'win32') return;
  // termios is per-device: flip stdin to raw so canonical-mode line
  // buffering doesn't hide partial input from the non-blocking read.
  // Restored in the finally block.
  const tty = stdin as NodeJS.ReadStream & {
    isRaw?: boolean;
    setRawMode?: (raw: boolean) => void;
  };
  const wasRaw = tty.isRaw === true;
  // Drain the kernel TTY buffer via a fresh O_NONBLOCK fd. Bounded at 64
  // reads (64KB) — a real mouse burst is a few hundred bytes; the cap
  // guards against a terminal that ignores O_NONBLOCK.
  let fd = -1;
  try {
    // setRawMode inside try: on revoked TTY (SIGHUP/SSH disconnect) the
    // ioctl throws EBADF — same recovery path as openSync/readSync below.
    if (!wasRaw) tty.setRawMode?.(true);
    fd = openSync('/dev/tty', fsConstants.O_RDONLY | fsConstants.O_NONBLOCK);
    const buf = Buffer.alloc(1024);
    for (let i = 0; i < 64; i++) {
      if (readSync(fd, buf, 0, buf.length, null) <= 0) break;
    }
  } catch {
    // EAGAIN (buffer empty — expected), ENXIO/ENOENT (no controlling tty),
    // EBADF/EIO (TTY revoked — SIGHUP, SSH disconnect)
  } finally {
    if (fd >= 0) {
      try {
        closeSync(fd);
      } catch {
        /* ignore */
      }
    }
    if (!wasRaw) {
      try {
        tty.setRawMode?.(false);
      } catch {
        /* TTY may be gone */
      }
    }
  }
}
/* eslint-enable custom-rules/no-sync-fs */

const CONSOLE_STDOUT_METHODS = ['log', 'info', 'debug', 'dir', 'dirxml', 'count', 'countReset', 'group', 'groupCollapsed', 'groupEnd', 'table', 'time', 'timeEnd', 'timeLog'] as const;
const CONSOLE_STDERR_METHODS = ['warn', 'error', 'trace'] as const;
