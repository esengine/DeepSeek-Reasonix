import React, { PureComponent, type ReactNode } from 'react';
import { logForDebugging } from '../_internal/debug.js';
import { stopCapturingEarlyInput } from '../_internal/earlyInput.js';
import { isEnvTruthy } from '../_internal/envUtils.js';
import { isMouseClicksDisabled } from '../_internal/fullscreen.js';
import { logError } from '../_internal/log.js';
import { updateLastInteractionTime } from '../_internal/state.js';
import { EventEmitter } from '../events/emitter.js';
import { InputEvent } from '../events/input-event.js';
import { TerminalFocusEvent } from '../events/terminal-focus-event.js';
import { INITIAL_STATE, type ParsedInput, type ParsedKey, type ParsedMouse, parseMultipleKeypresses } from '../parse-keypress.js';
import reconciler from '../reconciler.js';
import { finishSelection, hasSelection, type SelectionState, startSelection } from '../selection.js';
import { isXtermJs, setXtversionName, supportsExtendedKeys } from '../terminal.js';
import { getTerminalFocused, setTerminalFocused } from '../terminal-focus-state.js';
import { TerminalQuerier, xtversion } from '../terminal-querier.js';
import { DISABLE_KITTY_KEYBOARD, DISABLE_MODIFY_OTHER_KEYS, ENABLE_KITTY_KEYBOARD, ENABLE_MODIFY_OTHER_KEYS, FOCUS_IN, FOCUS_OUT } from '../termio/csi.js';
import { DBP, DFE, DISABLE_MOUSE_TRACKING, EBP, EFE, HIDE_CURSOR, SHOW_CURSOR } from '../termio/dec.js';
import AppContext from './AppContext.js';
import { ClockProvider } from './ClockContext.js';
import CursorDeclarationContext, { type CursorDeclarationSetter } from './CursorDeclarationContext.js';
import ErrorOverview from './ErrorOverview.js';
import StdinContext from './StdinContext.js';
import { TerminalFocusProvider } from './TerminalFocusContext.js';
import { TerminalSizeContext } from './TerminalSizeContext.js';

// Windows doesn't have SIGSTOP/SIGCONT semantics, so the Ctrl+Z suspend
// path is a no-op there. Cached at module load — the platform isn't
// going to change at runtime.
const SUPPORTS_SUSPEND = process.platform !== 'win32';

// If stdin goes silent for this long and then receives a chunk, treat
// the next chunk as a resume signal and re-assert terminal modes (mouse
// tracking, etc.). Real reattach scenarios — tmux detach/attach, SSH
// reconnect, laptop wake — reset the terminal's DEC private modes
// without sending us any signal we could hook into directly. Five
// seconds is comfortably longer than any normal inter-keystroke gap but
// short enough that the first scroll after reattach still works.
const STDIN_RESUME_GAP_MS = 5000;

// Short flush timeout for a stranded ESC-prefixed sequence (cursor
// keys, etc.). 50ms is enough to receive the rest of an SS3/CSI tail
// across most terminals; long enough that a deliberate single ESC keypress
// resolves to "Escape" instead of being treated as the start of a sequence.
const NORMAL_FLUSH_TIMEOUT_MS = 50;

// Pastes can interleave with our processing if the terminal sends them in
// chunks larger than the readable buffer. Hold incomplete paste content
// longer so we don't split it across two parse runs.
const PASTE_FLUSH_TIMEOUT_MS = 500;

// macOS double/triple-click default: a follow-up click within this many
// ms and this many cells of the previous click counts as a continuation
// (word-select on 2, line-select on 3). The position tolerance allows
// for trackpad jitter between consecutive taps.
const MULTI_CLICK_TIMEOUT_MS = 500;
const MULTI_CLICK_DISTANCE = 1;

// Bits in the SGR mouse button code we need to inspect by name. Names
// match what the terminal protocol calls them.
const MOUSE_BUTTON_BASE_MASK = 0x03;
const MOUSE_BUTTON_MOTION_BIT = 0x20;
const MOUSE_BUTTON_ALT_BIT = 0x08;
const MOUSE_BUTTON_BASE_RELEASE_LEGACY = 3; // "no button" from X10 encoding
type Props = {
  readonly children: ReactNode;
  readonly stdin: NodeJS.ReadStream;
  readonly stdout: NodeJS.WriteStream;
  readonly stderr: NodeJS.WriteStream;
  readonly exitOnCtrlC: boolean;
  readonly onExit: (error?: Error) => void;
  readonly terminalColumns: number;
  readonly terminalRows: number;
  /** Shared selection state. */
  readonly selection: SelectionState;
  readonly onSelectionChange: () => void;
  /** Dispatch a click at `(col, row)`. */
  readonly onClickAt: (col: number, row: number) => boolean;
  readonly onHoverAt: (col: number, row: number) => void;
  readonly getHyperlinkAt: (col: number, row: number) => string | undefined;
  /** Open a hyperlink URL in the browser. Called when the defer fires. */
  readonly onOpenHyperlink: (url: string) => void;
  /** Fired on the press half of a double/triple click at `(col, row)`. */
  readonly onMultiClick: (col: number, row: number, count: 2 | 3) => void;
  /** Fired on drag-motion. */
  readonly onSelectionDrag: (col: number, row: number) => void;
  readonly onStdinResume?: () => void;
  readonly onCursorDeclaration?: CursorDeclarationSetter;
  /** Dispatch a parsed keyboard event through the DOM tree. */
  readonly dispatchKeyboardEvent: (parsedKey: ParsedKey) => void;
};

type State = {
  readonly error?: Error;
};

/** Root component for every rendered tree. */
export default class App extends PureComponent<Props, State> {
  static displayName = 'InternalApp';
  static getDerivedStateFromError(error: Error) {
    return {
      error,
    };
  }
  override state = {
    error: undefined,
  };

  // Reference count for raw-mode enablement. Components that need raw
  // keyboard input call `handleSetRawMode(true)` independently; we only
  // disable raw mode once every consumer has released.
  rawModeEnabledCount = 0;
  internal_eventEmitter = new EventEmitter();
  keyParseState = INITIAL_STATE;
  // Timer for flushing incomplete escape sequences. Always either null
  // or a live timer reference — `flushIncomplete` clears it itself.
  incompleteEscapeTimer: NodeJS.Timeout | null = null;

  // Terminal query/response dispatcher. Responses (DECRPM, DA1, OSC
  // replies, ...) arrive on stdin and `parse-keypress` separates them
  // from real keys; this querier routes them to pending resolvers.
  querier = new TerminalQuerier(this.props.stdout);

  // Multi-click tracking for double/triple-click text selection. A
  // click within `MULTI_CLICK_TIMEOUT_MS` and `MULTI_CLICK_DISTANCE`
  // of the previous one increments `clickCount`; otherwise it resets
  // to 1 (the new chain starts).
  lastClickTime = 0;
  lastClickCol = -1;
  lastClickRow = -1;
  clickCount = 0;
  // Pending hyperlink open. Cancelled if a second click arrives within
  // `MULTI_CLICK_TIMEOUT_MS` so double-clicking an OSC 8 link selects
  // the word without also opening the browser. DOM `onClick` dispatch
  // does NOT go through this timer — `onClickAt` returning `true`
  // short-circuits before the link path runs.
  pendingHyperlinkTimer: ReturnType<typeof setTimeout> | null = null;
  // Last mode-1003 motion position. Terminals dedupe at cell granularity
  // already, but tracking it ourselves lets us skip `dispatchHover`
  // entirely on duplicate events (drag-then-release at the same cell,
  // for example).
  lastHoverCol = -1;
  lastHoverRow = -1;

  // Timestamp of the last stdin chunk. Used by the resume-gap check to
  // detect tmux attach / SSH reconnect / laptop wake. Initialised to
  // `now` so the first chunk after startup doesn't false-trigger.
  lastStdinTime = Date.now();

  // True iff the configured stdin is a TTY (and therefore supports
  // entering raw mode).
  isRawModeSupported(): boolean {
    return this.props.stdin.isTTY;
  }
  // Memoize terminal size context value to prevent unnecessary re-renders
  // in consumers (useTerminalViewport). Without this, a new object is
  // created on every render, defeating dependency-array checks.
  private _lastTerminalColumns = 0;
  private _lastTerminalRows = 0;
  private _terminalSizeValue: { columns: number; rows: number } = { columns: 0, rows: 0 };

  override render() {
    if (this.props.terminalColumns !== this._lastTerminalColumns ||
        this.props.terminalRows !== this._lastTerminalRows) {
      this._lastTerminalColumns = this.props.terminalColumns;
      this._lastTerminalRows = this.props.terminalRows;
      this._terminalSizeValue = {
        columns: this.props.terminalColumns,
        rows: this.props.terminalRows,
      };
    }
    return <TerminalSizeContext.Provider value={this._terminalSizeValue}>
        <AppContext.Provider value={{
        exit: this.handleExit
      }}>
          <StdinContext.Provider value={{
          stdin: this.props.stdin,
          setRawMode: this.handleSetRawMode,
          isRawModeSupported: this.isRawModeSupported(),
          internal_exitOnCtrlC: this.props.exitOnCtrlC,
          internal_eventEmitter: this.internal_eventEmitter,
          internal_querier: this.querier
        }}>
            <TerminalFocusProvider>
              <ClockProvider>
                <CursorDeclarationContext.Provider value={this.props.onCursorDeclaration ?? (() => {})}>
                  {this.state.error ? <ErrorOverview error={this.state.error as Error} /> : this.props.children}
                </CursorDeclarationContext.Provider>
              </ClockProvider>
            </TerminalFocusProvider>
          </StdinContext.Provider>
        </AppContext.Provider>
      </TerminalSizeContext.Provider>;
  }
  override componentDidMount() {
    // Hide the native cursor while the app is running. In accessibility
    // mode the cursor stays visible because screen magnifiers and similar
    // assistive tools track its position to follow focus.
    if (this.props.stdout.isTTY && !isEnvTruthy(process.env.REASONIX_ACCESSIBILITY)) {
      this.props.stdout.write(HIDE_CURSOR);
    }
  }
  override componentWillUnmount() {
    if (this.props.stdout.isTTY) {
      this.props.stdout.write(SHOW_CURSOR);
    }

    // Drop any pending timers so they don't fire into a torn-down tree.
    if (this.incompleteEscapeTimer) {
      clearTimeout(this.incompleteEscapeTimer);
      this.incompleteEscapeTimer = null;
    }
    if (this.pendingHyperlinkTimer) {
      clearTimeout(this.pendingHyperlinkTimer);
      this.pendingHyperlinkTimer = null;
    }
    // Skip `setRawMode` on non-TTY stdin; calling it would throw the
    // same error the prop-time guard reports, and unmount paths are not
    // a useful place to surface that.
    if (this.isRawModeSupported()) {
      this.handleSetRawMode(false);
    }
  }
  override componentDidCatch(error: Error) {
    this.handleExit(error);
  }
  handleSetRawMode = (isEnabled: boolean): void => {
    const {
      stdin
    } = this.props;
    if (!this.isRawModeSupported()) {
      if (stdin === process.stdin) {
        throw new Error('Raw mode is not supported on the default process.stdin used as the input stream.\nThis usually means the process was launched without a TTY (e.g. piped input, CI runner). Pass an explicit `stdin` option whose `isTTY` is true, or render with `{ stdin: process.stdin }` only inside an interactive terminal.');
      } else {
        throw new Error('Raw mode is not supported on the provided `stdin` stream.\nThe stream must be a TTY (its `isTTY` property must be true). Streams from pipes, files, or non-interactive shells cannot enter raw mode; supply a TTY-backed stream or skip the components that depend on raw keyboard input.');
      }
    }
    stdin.setEncoding('utf8');
    if (isEnabled) {
      // Only do the heavy setup once, on the first enable.
      if (this.rawModeEnabledCount === 0) {
        // Stop early-input capture before installing our own readable
        // listener. Both use the same `'readable' + read()` pattern, so
        // they cannot coexist on the same stream — whichever drains
        // stdin first wins, and the loser sees no events. Any buffered
        // text the early capture collected is preserved separately and
        // can be consumed via `consumeEarlyInput()`.
        stopCapturingEarlyInput();
        stdin.ref();
        stdin.setRawMode(true);
        stdin.addListener('readable', this.handleReadable);
        // Bracketed paste mode: pastes arrive wrapped in
        // `ESC [ 200 ~` / `ESC [ 201 ~` so the parser can tell them
        // apart from typed input.
        this.props.stdout.write(EBP);
        // DECSET 1004 — terminal focus reporting.
        this.props.stdout.write(EFE);
        // Extended key reporting, so ctrl+shift+<letter> is
        // distinguishable from plain ctrl+<letter>. We push the kitty
        // keyboard stack (CSI >1u) AND set xterm modifyOtherKeys level
        // 2 (CSI >4;2m); terminals respect whichever they implement
        // (tmux honours only the latter), and the unused one is a
        // harmless no-op on the rest.
        if (supportsExtendedKeys()) {
          this.props.stdout.write(ENABLE_KITTY_KEYBOARD);
          this.props.stdout.write(ENABLE_MODIFY_OTHER_KEYS);
        }
        // Probe terminal identity via XTVERSION. Unlike `TERM_PROGRAM`
        // this query/reply travels through the pty, which means it
        // survives SSH and tmux. The reply (when we get one) is used
        // for wheel-scroll base detection on hosts where env vars
        // aren't reliable. Fire-and-forget: the querier's DA1 sentinel
        // bounds the round-trip, so a silent terminal just leaves
        // `name` undefined and we move on.
        //
        // Deferred to the next macrotask so it doesn't interleave with
        // alt-screen / mouse-tracking writes that may happen later in
        // the same render cycle.
        setImmediate(() => {
          void Promise.all([this.querier.send(xtversion()), this.querier.flush()]).then(([r]) => {
            if (r) {
              setXtversionName(r.name);
              logForDebugging(`XTVERSION: terminal identified as "${r.name}"`);
            } else {
              logForDebugging('XTVERSION: no reply (terminal ignored query)');
            }
          });
        });
      }
      this.rawModeEnabledCount++;
      return;
    }

    // Disable for real only when every consumer has released.
    if (--this.rawModeEnabledCount === 0) {
      this.props.stdout.write(DISABLE_MODIFY_OTHER_KEYS);
      this.props.stdout.write(DISABLE_KITTY_KEYBOARD);
      // DECRST 1004 — leave terminal focus reporting off.
      this.props.stdout.write(DFE);
      // Disable bracketed paste mode.
      this.props.stdout.write(DBP);
      stdin.setRawMode(false);
      stdin.removeListener('readable', this.handleReadable);
      stdin.unref();
    }
  };

  /** Flush whatever incomplete escape sequence the parser is holding. */
  flushIncomplete = (): void => {
    this.incompleteEscapeTimer = null;

    if (!this.keyParseState.incomplete) return;

    if (this.props.stdin.readableLength > 0) {
      this.incompleteEscapeTimer = setTimeout(this.flushIncomplete, NORMAL_FLUSH_TIMEOUT_MS);
      return;
    }

    // Run the parser with `input=null` to signal "no more bytes are
    // coming, finalise whatever you have". Reuses all the regular parse
    // dispatch code below.
    this.processInput(null);
  };

  processInput = (input: string | Buffer | null): void => {
    const [keys, newState] = parseMultipleKeypresses(this.keyParseState, input);
    this.keyParseState = newState;

    if (keys.length > 0) {
      reconciler.discreteUpdates(processKeysInBatch, this, keys, undefined, undefined);
    }

    // Re-arm the flush timer if the parser is still holding a partial
    // sequence. Cancel an existing timer first so the timeout reflects
    // the most recent input (especially important when we transition
    // into paste mode and need the longer timeout).
    if (this.keyParseState.incomplete) {
      if (this.incompleteEscapeTimer) {
        clearTimeout(this.incompleteEscapeTimer);
      }
      this.incompleteEscapeTimer = setTimeout(
        this.flushIncomplete,
        this.keyParseState.mode === 'IN_PASTE' ? PASTE_FLUSH_TIMEOUT_MS : NORMAL_FLUSH_TIMEOUT_MS,
      );
    }
  };
  handleReadable = (): void => {
    // Detect long stdin gaps (tmux attach, SSH reconnect, laptop wake).
    // The terminal probably reset its DEC private modes during the gap,
    // so we ask the host to re-assert mouse tracking and friends. One
    // `Date.now()` per readable event covers every chunk we drain in
    // this turn.
    const now = Date.now();
    if (now - this.lastStdinTime > STDIN_RESUME_GAP_MS) {
      this.props.onStdinResume?.();
    }
    this.lastStdinTime = now;
    try {
      let chunk;
      while ((chunk = this.props.stdin.read() as string | null) !== null) {
        this.processInput(chunk);
      }
    } catch (error) {
      // Bun's behaviour on an uncaught throw inside a stream 'readable'
      // handler is to wedge the stream permanently — data stays
      // buffered and 'readable' never re-fires. Logging + recovering
      // here keeps the input pipeline healthy so subsequent keystrokes
      // are still delivered.
      logError(error);

      // Re-attach the listener if Bun detached it as part of the
      // exception path. Without this the session is alive but the
      // stdin reader is silently dead — every key vanishes into the
      // void until the user kills the process.
      const { stdin } = this.props;
      if (this.rawModeEnabledCount > 0 && !stdin.listeners('readable').includes(this.handleReadable)) {
        logForDebugging('handleReadable: re-attaching stdin readable listener after error recovery', {
          level: 'warn',
        });
        stdin.addListener('readable', this.handleReadable);
      }
    }
  };
  handleInput = (input: string | undefined): void => {
    // Ctrl+C → exit. The Ctrl+Z (suspend) path used to live here too,
    // but it now runs in `processKeysInBatch` against the parsed key so
    // it works under both legacy (`\x1a`) and Kitty CSI-u (`\x1b[122;5u`)
    // encodings — Ghostty, iTerm2, kitty, and WezTerm all emit the latter.
    if (input === '\x03' && this.props.exitOnCtrlC) {
      this.handleExit();
    }
  };
  handleExit = (error?: Error): void => {
    if (this.isRawModeSupported()) {
      this.handleSetRawMode(false);
    }
    this.props.onExit(error);
  };
  handleTerminalFocus = (isFocused: boolean): void => {
    // The focus store fans out to `TerminalFocusProvider` (context) and
    // to the clock (interval speed), so we don't need to setState here
    // — App.tsx is intentionally insulated from focus churn.
    setTerminalFocused(isFocused);
  };
  handleSuspend = (): void => {
    if (!this.isRawModeSupported()) {
      return;
    }

    // Snapshot the raw-mode reference count so we can restore the
    // exact same depth on resume — components that were holding raw
    // mode at suspend time still expect it to be on when we come back.
    const rawModeCountBeforeSuspend = this.rawModeEnabledCount;

    // Fully unwind raw mode so the parent shell sees a normal cooked
    // terminal while we're stopped.
    while (this.rawModeEnabledCount > 0) {
      this.handleSetRawMode(false);
    }

    // Restore cursor visibility, turn off focus reporting, and disable
    // mouse tracking before stopping. `DISABLE_MOUSE_TRACKING` is a
    // no-op if tracking was never enabled, but emitting it
    // unconditionally is necessary: otherwise leftover SGR mouse
    // sequences would arrive at the shell prompt and render as
    // garbled text until the user runs `reset`.
    if (this.props.stdout.isTTY) {
      this.props.stdout.write(SHOW_CURSOR + DFE + DISABLE_MOUSE_TRACKING);
    }

    // Notify subscribers so app-level housekeeping (saving state,
    // suspending background work) can run before we actually stop.
    this.internal_eventEmitter.emit('suspend');

    const resumeHandler = () => {
      // Restore raw mode to the depth it was at when we suspended.
      for (let i = 0; i < rawModeCountBeforeSuspend; i++) {
        if (this.isRawModeSupported()) {
          this.handleSetRawMode(true);
        }
      }

      // Hide the cursor again (unless in accessibility mode) and
      // re-enable focus reporting so the terminal is back in the state
      // the app expected before suspension.
      if (this.props.stdout.isTTY) {
        if (!isEnvTruthy(process.env.REASONIX_ACCESSIBILITY)) {
          this.props.stdout.write(HIDE_CURSOR);
        }
        this.props.stdout.write(EFE);
      }

      this.internal_eventEmitter.emit('resume');
      process.removeListener('SIGCONT', resumeHandler);
    };
    process.on('SIGCONT', resumeHandler);
    process.kill(process.pid, 'SIGSTOP');
  };
}

function processKeysInBatch(app: App, items: ParsedInput[], _unused1: undefined, _unused2: undefined): void {
  // Refresh the "last interaction" timestamp used by notification
  // dampening and other idle-gated background work. Only real
  // engagement counts: keys + mouse clicks/drags, but the passive
  // no-button motion that mode-1003 emits when the user just brushes
  // the cursor through the window does not — counting it would
  // suppress idle notifications and defer housekeeping forever.
  // Terminal responses (XTVERSION etc.) are automated and excluded.
  if (items.some(i => i.kind === 'key' || (i.kind === 'mouse' && !isPassiveHover(i.button)))) {
    updateLastInteractionTime();
  }
  for (const item of items) {
    // Terminal responses (DECRPM, DA1, OSC replies, ...) are not user
    // input — resolve any pending promise on the querier and stop.
    if (item.kind === 'response') {
      app.querier.onResponse(item.response);
      continue;
    }

    // Mouse click/drag/motion. The terminal protocol sends 1-indexed
    // columns and rows; the screen buffer uses 0-indexed, so the
    // handler does the conversion.
    if (item.kind === 'mouse') {
      handleMouseEvent(app, item);
      continue;
    }
    const sequence = item.sequence;

    // DECSET 1004 terminal focus events.
    if (sequence === FOCUS_IN) {
      app.handleTerminalFocus(true);
      const event = new TerminalFocusEvent('terminalfocus');
      app.internal_eventEmitter.emit('terminalfocus', event);
      continue;
    }
    if (sequence === FOCUS_OUT) {
      app.handleTerminalFocus(false);
      // Defensive selection close: if we never saw the mouse release
      // (e.g. iTerm2 doesn't capture the pointer past the window
      // bounds, so the release SGR never arrives), the focus-out event
      // is our next observable signal that the drag is over. Without
      // this, `isDragging` stays true forever and drag-to-scroll's
      // timer keeps firing.
      if (app.props.selection.isDragging) {
        finishSelection(app.props.selection);
        app.props.onSelectionChange();
      }
      const event = new TerminalFocusEvent('terminalblur');
      app.internal_eventEmitter.emit('terminalblur', event);
      continue;
    }

    // Receiving real input implies the terminal window is focused.
    // Some emulators (notably tmux without `focus-events on`) never
    // emit focus-in, so this is the only signal we'll get that they
    // came back. Without it the clock stays at the blurred-tick rate
    // and animations look choppy.
    if (!getTerminalFocused()) {
      setTerminalFocused(true);
    }

    // Ctrl+Z (suspend). Reading the parsed key here, rather than
    // matching `\x1a` on the raw byte, handles both legacy and Kitty
    // CSI-u (`\x1b[122;5u`) encodings.
    if (item.name === 'z' && item.ctrl && SUPPORTS_SUSPEND) {
      app.handleSuspend();
      continue;
    }
    app.handleInput(sequence);
    const event = new InputEvent(item);
    app.internal_eventEmitter.emit('input', event);

    // Mirror the input through the DOM tree so `onKeyDown` handlers on
    // focused Boxes can react.
    app.props.dispatchKeyboardEvent(item);
  }
}

function isPassiveHover(button: number): boolean {
  return (button & MOUSE_BUTTON_MOTION_BIT) !== 0
    && (button & MOUSE_BUTTON_BASE_MASK) === MOUSE_BUTTON_BASE_RELEASE_LEGACY;
}

export function handleMouseEvent(app: App, m: ParsedMouse): void {
  // Allow disabling click handling without disabling wheel scroll.
  // Wheel events arrive through the keybinding system as
  // 'wheelup'/'wheeldown', not here, so this gate doesn't affect them.
  if (isMouseClicksDisabled()) return;
  const sel = app.props.selection;
  // Terminal protocol is 1-indexed; the screen buffer is 0-indexed.
  const col = m.col - 1;
  const row = m.row - 1;
  const baseButton = m.button & MOUSE_BUTTON_BASE_MASK;
  if (m.action === 'press') {
    if ((m.button & MOUSE_BUTTON_MOTION_BIT) !== 0 && baseButton === MOUSE_BUTTON_BASE_RELEASE_LEGACY) {
      // Mode-1003 motion with no button held: dispatch hover and stop.
      // No selection logic, no click-count side effects.
      //
      // Lost-release recovery: if the user is "dragging" but we see
      // no-button motion, the release happened outside the terminal
      // window (iTerm2 doesn't capture the pointer past window
      // bounds, so its SGR 'm' release never arrives). Finish the
      // selection here so copy-on-select still fires. The FOCUS_OUT
      // path handles "switched apps", but not "released past the edge,
      // came back" — and tmux without `focus-events on` doesn't even
      // emit focus changes, so this is the more reliable signal.
      if (sel.isDragging) {
        finishSelection(sel);
        app.props.onSelectionChange();
      }
      if (col === app.lastHoverCol && row === app.lastHoverRow) return;
      app.lastHoverCol = col;
      app.lastHoverRow = row;
      app.props.onHoverAt(col, row);
      return;
    }
    if (baseButton !== 0) {
      // Non-left press breaks the multi-click chain.
      app.clickCount = 0;
      return;
    }
    if ((m.button & MOUSE_BUTTON_MOTION_BIT) !== 0) {
      // Drag motion: mode-aware extension (char / word / line).
      // `onSelectionDrag` calls `notifySelectionChange` internally so
      // we don't double-up by calling `onSelectionChange` here.
      app.props.onSelectionDrag(col, row);
      return;
    }
    // Lost-release fallback for mode-1002-only terminals (no motion
    // reports when no button is held). Seeing a fresh press while
    // `isDragging=true` means the previous release was dropped (cursor
    // left the window). Finish that selection so copy-on-select fires
    // before `startSelection` / `onMultiClick` clobbers the anchor.
    // Mode-1003 terminals hit the no-button-motion recovery above
    // instead, so this branch only fires on the older protocol.
    if (sel.isDragging) {
      finishSelection(sel);
      app.props.onSelectionChange();
    }
    // Fresh left press. Detect multi-click here, on press, not on
    // release. Two reasons:
    //
    // 1. The word/line highlight needs to appear immediately so the
    //    feedback feels native.
    // 2. Detecting on release used to let double-click+drag fall
    //    through to char-mode selection, because by the time the
    //    second release came in we were already mid-drag.
    const now = Date.now();
    const nearLast = now - app.lastClickTime < MULTI_CLICK_TIMEOUT_MS
      && Math.abs(col - app.lastClickCol) <= MULTI_CLICK_DISTANCE
      && Math.abs(row - app.lastClickRow) <= MULTI_CLICK_DISTANCE;
    app.clickCount = nearLast ? app.clickCount + 1 : 1;
    app.lastClickTime = now;
    app.lastClickCol = col;
    app.lastClickRow = row;
    if (app.clickCount >= 2) {
      // Cancel any pending hyperlink-open from the prior click — this
      // is a double-click, not a single-click on a link.
      if (app.pendingHyperlinkTimer) {
        clearTimeout(app.pendingHyperlinkTimer);
        app.pendingHyperlinkTimer = null;
      }
      // Cap at 3 (line select) for quadruple-and-up clicks.
      const count = app.clickCount === 2 ? 2 : 3;
      app.props.onMultiClick(col, row, count);
      return;
    }
    startSelection(sel, col, row);
    // SGR bit 0x08 carries the alt-key state. (xterm.js wires its
    // `altKey` field here, NOT `metaKey` — see the hyperlink guard
    // below for the consequences.) On macOS xterm.js, receiving alt
    // also implies `macOptionClickForcesSelection=OFF`; otherwise
    // xterm.js would have consumed the event for native selection
    // before forwarding it.
    sel.lastPressHadAlt = (m.button & MOUSE_BUTTON_ALT_BIT) !== 0;
    app.props.onSelectionChange();
    return;
  }

  // Release path. End any drag even when the base button is non-zero:
  // some terminals encode the release with the motion bit set, or with
  // `button=3` "no button" (left over from the pre-SGR X10 encoding).
  // Filtering those out would leave `isDragging=true` permanently and
  // let drag-to-scroll's timer keep firing until the scroll boundary.
  // For non-left releases we still only act when we ARE dragging, so an
  // unrelated middle/right release doesn't touch the selection.
  if (baseButton !== 0) {
    if (!sel.isDragging) return;
    finishSelection(sel);
    app.props.onSelectionChange();
    return;
  }
  finishSelection(sel);
  // Note: we deliberately do NOT reset `clickCount` on a release that
  // followed a drag. This matches NSEvent.clickCount semantics — an
  // intervening drag doesn't break the click chain — and the practical
  // payoff is that trackpad jitter during an intended double-click
  // (press → wobble → release → press) resolves to word-select instead
  // of degrading to a fresh single click. The `nearLast` window
  // (`MULTI_CLICK_TIMEOUT_MS` / `MULTI_CLICK_DISTANCE`) keeps the
  // effect bounded; a deliberate long drag past that window just
  // starts a fresh chain.
  //
  // Click semantics by mode:
  //   - char mode: press sets anchor, focus stays null until drag, so
  //     a press+release with no drag is `hasSelection=false` → click.
  //   - word/line mode: the press already set both anchor and focus
  //     (`hasSelection=true`), so release just keeps the highlight.
  //
  // The `sel.anchor` guard handles orphaned releases — e.g. the button
  // was already held when mouse tracking was enabled, so we get a
  // release with no prior press recorded.
  if (!hasSelection(sel) && sel.anchor) {
    // Single click: dispatch DOM `onClick` immediately. Cursor
    // repositioning and similar handlers are latency-sensitive and
    // shouldn't be deferred. If no DOM handler consumed the click, we
    // fall through to hyperlink handling, which IS deferred so a
    // second click can cancel it.
    if (!app.props.onClickAt(col, row)) {
      // Resolve the URL synchronously while the screen buffer still
      // shows what the user clicked. Only the browser-open itself is
      // deferred — that's what double-click can cancel.
      const url = app.props.getHyperlinkAt(col, row);
      // xterm.js (VS Code, Cursor, Windsurf, ...) ships its own OSC 8
      // handler that fires on Cmd+click *without consuming the mouse
      // event*: Linkifier._handleMouseUp calls link.activate() but
      // never preventDefault/stopPropagation, so the click is still
      // forwarded to the pty as SGR. If we opened the URL too, both
      // VS Code's terminalLinkManager AND we would open it — twice.
      // We can't filter by Cmd from this end: xterm.js drops metaKey
      // before SGR encoding (its ICoreMouseEvent has no meta field;
      // the SGR bit we call "meta" is wired to alt). The least-bad
      // option is to defer entirely to xterm.js when we know we're
      // inside it; Cmd+click is the native UX there anyway.
      //
      // TERM_PROGRAM='vscode' is the synchronous fast-path; the
      // `isXtermJs()` fallback uses the XTVERSION probe result, which
      // catches SSH sessions and non-VS Code embedders like Hyper.
      if (url && process.env.TERM_PROGRAM !== 'vscode' && !isXtermJs()) {
        // Clicking a second link supersedes the first — only the
        // most recent click opens.
        if (app.pendingHyperlinkTimer) {
          clearTimeout(app.pendingHyperlinkTimer);
        }
        app.pendingHyperlinkTimer = setTimeout((app, url) => {
          app.pendingHyperlinkTimer = null;
          app.props.onOpenHyperlink(url);
        }, MULTI_CLICK_TIMEOUT_MS, app, url);
      }
    }
  }
  app.props.onSelectionChange();
}
