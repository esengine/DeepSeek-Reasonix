import React, { Ref, ReactNode, PropsWithChildren, ReactElement } from 'react';
import { Except } from 'type-fest';
import { Boxes, BoxStyle } from 'cli-boxes';
import { EventEmitter as EventEmitter$1 } from 'events';

/**
 * Component that parses ANSI escape codes and renders them using Text components.
 *
 * Use this as an escape hatch when you have pre-formatted ANSI strings from
 * external tools (like cli-highlight) that need to be rendered in Ink.
 *
 * Memoized to prevent re-renders when parent changes but children string is the same.
 */
declare const Ansi: React.NamedExoticComponent<{
    children: React.ReactNode;
    dimColor?: boolean;
}>;

declare class Event {
    private _didStopImmediatePropagation;
    didStopImmediatePropagation(): boolean;
    stopImmediatePropagation(): void;
}

type EventPhase = 'none' | 'capturing' | 'at_target' | 'bubbling';
type TerminalEventInit = {
    bubbles?: boolean;
    cancelable?: boolean;
};
/**
 * Base class for all terminal events with DOM-style propagation.
 *
 * Extends Event so existing event types (ClickEvent, InputEvent,
 * TerminalFocusEvent) share a common ancestor and can migrate later.
 *
 * Mirrors the browser's Event API: target, currentTarget, eventPhase,
 * stopPropagation(), preventDefault(), timeStamp.
 */
declare class TerminalEvent extends Event {
    readonly type: string;
    readonly timeStamp: number;
    readonly bubbles: boolean;
    readonly cancelable: boolean;
    private _target;
    private _currentTarget;
    private _eventPhase;
    private _propagationStopped;
    private _defaultPrevented;
    constructor(type: string, init?: TerminalEventInit);
    get target(): EventTarget | null;
    get currentTarget(): EventTarget | null;
    get eventPhase(): EventPhase;
    get defaultPrevented(): boolean;
    stopPropagation(): void;
    stopImmediatePropagation(): void;
    preventDefault(): void;
    /** @internal */
    _setTarget(target: EventTarget): void;
    /** @internal */
    _setCurrentTarget(target: EventTarget | null): void;
    /** @internal */
    _setEventPhase(phase: EventPhase): void;
    /** @internal */
    _isPropagationStopped(): boolean;
    /** @internal */
    _isImmediatePropagationStopped(): boolean;
    /**
     * Hook for subclasses to do per-node setup before each handler fires.
     * Default is a no-op.
     */
    _prepareForTarget(_target: EventTarget): void;
}
type EventTarget = {
    parentNode: EventTarget | undefined;
    _eventHandlers?: Record<string, unknown>;
};

/**
 * Focus event for component focus changes.
 *
 * Dispatched when focus moves between elements. 'focus' fires on the
 * newly focused element, 'blur' fires on the previously focused one.
 * Both bubble, matching react-dom's use of focusin/focusout semantics
 * so parent components can observe descendant focus changes.
 */
declare class FocusEvent extends TerminalEvent {
    readonly relatedTarget: EventTarget | null;
    constructor(type: 'focus' | 'blur', relatedTarget?: EventTarget | null);
}

/**
 * DOM-like focus manager for the Ink terminal UI.
 *
 * Pure state — tracks activeElement and a focus stack. Has no reference
 * to the tree; callers pass the root when tree walks are needed.
 *
 * Stored on the root DOMElement so any node can reach it by walking
 * parentNode (like browser's `node.ownerDocument`).
 */
declare class FocusManager {
    activeElement: DOMElement | null;
    private dispatchFocusEvent;
    private enabled;
    private focusStack;
    constructor(dispatchFocusEvent: (target: DOMElement, event: FocusEvent) => boolean);
    focus(node: DOMElement): void;
    blur(): void;
    /**
     * Called by the reconciler when a node is removed from the tree.
     * Handles both the exact node and any focused descendant within
     * the removed subtree. Dispatches blur and restores focus from stack.
     */
    handleNodeRemoved(node: DOMElement, root: DOMElement): void;
    handleAutoFocus(node: DOMElement): void;
    handleClickFocus(node: DOMElement): void;
    enable(): void;
    disable(): void;
    focusNext(root: DOMElement): void;
    focusPrevious(root: DOMElement): void;
    private moveFocus;
}

declare const LayoutEdge: {
    readonly All: "all";
    readonly Horizontal: "horizontal";
    readonly Vertical: "vertical";
    readonly Left: "left";
    readonly Right: "right";
    readonly Top: "top";
    readonly Bottom: "bottom";
    readonly Start: "start";
    readonly End: "end";
};
type LayoutEdge = (typeof LayoutEdge)[keyof typeof LayoutEdge];
declare const LayoutGutter: {
    readonly All: "all";
    readonly Column: "column";
    readonly Row: "row";
};
type LayoutGutter = (typeof LayoutGutter)[keyof typeof LayoutGutter];
declare const LayoutDisplay: {
    readonly Flex: "flex";
    readonly None: "none";
};
type LayoutDisplay = (typeof LayoutDisplay)[keyof typeof LayoutDisplay];
declare const LayoutFlexDirection: {
    readonly Row: "row";
    readonly RowReverse: "row-reverse";
    readonly Column: "column";
    readonly ColumnReverse: "column-reverse";
};
type LayoutFlexDirection = (typeof LayoutFlexDirection)[keyof typeof LayoutFlexDirection];
declare const LayoutAlign: {
    readonly Auto: "auto";
    readonly Stretch: "stretch";
    readonly FlexStart: "flex-start";
    readonly Center: "center";
    readonly FlexEnd: "flex-end";
};
type LayoutAlign = (typeof LayoutAlign)[keyof typeof LayoutAlign];
declare const LayoutJustify: {
    readonly FlexStart: "flex-start";
    readonly Center: "center";
    readonly FlexEnd: "flex-end";
    readonly SpaceBetween: "space-between";
    readonly SpaceAround: "space-around";
    readonly SpaceEvenly: "space-evenly";
};
type LayoutJustify = (typeof LayoutJustify)[keyof typeof LayoutJustify];
declare const LayoutWrap: {
    readonly NoWrap: "nowrap";
    readonly Wrap: "wrap";
    readonly WrapReverse: "wrap-reverse";
};
type LayoutWrap = (typeof LayoutWrap)[keyof typeof LayoutWrap];
declare const LayoutPositionType: {
    readonly Relative: "relative";
    readonly Absolute: "absolute";
};
type LayoutPositionType = (typeof LayoutPositionType)[keyof typeof LayoutPositionType];
declare const LayoutOverflow: {
    readonly Visible: "visible";
    readonly Hidden: "hidden";
    readonly Scroll: "scroll";
};
type LayoutOverflow = (typeof LayoutOverflow)[keyof typeof LayoutOverflow];
type LayoutMeasureFunc = (width: number, widthMode: LayoutMeasureMode) => {
    width: number;
    height: number;
};
declare const LayoutMeasureMode: {
    readonly Undefined: "undefined";
    readonly Exactly: "exactly";
    readonly AtMost: "at-most";
};
type LayoutMeasureMode = (typeof LayoutMeasureMode)[keyof typeof LayoutMeasureMode];
type LayoutNode = {
    insertChild(child: LayoutNode, index: number): void;
    removeChild(child: LayoutNode): void;
    getChildCount(): number;
    getParent(): LayoutNode | null;
    calculateLayout(width?: number, height?: number): void;
    setMeasureFunc(fn: LayoutMeasureFunc): void;
    unsetMeasureFunc(): void;
    markDirty(): void;
    getComputedLeft(): number;
    getComputedTop(): number;
    getComputedWidth(): number;
    getComputedHeight(): number;
    getComputedBorder(edge: LayoutEdge): number;
    getComputedPadding(edge: LayoutEdge): number;
    setWidth(value: number): void;
    setWidthPercent(value: number): void;
    setWidthAuto(): void;
    setHeight(value: number): void;
    setHeightPercent(value: number): void;
    setHeightAuto(): void;
    setMinWidth(value: number): void;
    setMinWidthPercent(value: number): void;
    setMinHeight(value: number): void;
    setMinHeightPercent(value: number): void;
    setMaxWidth(value: number): void;
    setMaxWidthPercent(value: number): void;
    setMaxHeight(value: number): void;
    setMaxHeightPercent(value: number): void;
    setFlexDirection(dir: LayoutFlexDirection): void;
    setFlexGrow(value: number): void;
    setFlexShrink(value: number): void;
    setFlexBasis(value: number): void;
    setFlexBasisPercent(value: number): void;
    setFlexWrap(wrap: LayoutWrap): void;
    setAlignItems(align: LayoutAlign): void;
    setAlignSelf(align: LayoutAlign): void;
    setJustifyContent(justify: LayoutJustify): void;
    setDisplay(display: LayoutDisplay): void;
    getDisplay(): LayoutDisplay;
    setPositionType(type: LayoutPositionType): void;
    setPosition(edge: LayoutEdge, value: number): void;
    setPositionPercent(edge: LayoutEdge, value: number): void;
    setOverflow(overflow: LayoutOverflow): void;
    setMargin(edge: LayoutEdge, value: number): void;
    setPadding(edge: LayoutEdge, value: number): void;
    setBorder(edge: LayoutEdge, value: number): void;
    setGap(gutter: LayoutGutter, value: number): void;
    free(): void;
    freeRecursive(): void;
};

type BorderTextOptions = {
    content: string;
    position: 'top' | 'bottom';
    align: 'start' | 'end' | 'center';
    offset?: number;
};
declare const CUSTOM_BORDER_STYLES: {
    readonly dashed: {
        readonly top: "╌";
        readonly left: "╎";
        readonly right: "╎";
        readonly bottom: "╌";
        readonly topLeft: " ";
        readonly topRight: " ";
        readonly bottomLeft: " ";
        readonly bottomRight: " ";
    };
};
type BorderStyle = keyof Boxes | keyof typeof CUSTOM_BORDER_STYLES | BoxStyle;

type RGBColor = `rgb(${number},${number},${number})`;
type HexColor = `#${string}`;
type Ansi256Color = `ansi256(${number})`;
type AnsiColor = 'ansi:black' | 'ansi:red' | 'ansi:green' | 'ansi:yellow' | 'ansi:blue' | 'ansi:magenta' | 'ansi:cyan' | 'ansi:white' | 'ansi:blackBright' | 'ansi:redBright' | 'ansi:greenBright' | 'ansi:yellowBright' | 'ansi:blueBright' | 'ansi:magentaBright' | 'ansi:cyanBright' | 'ansi:whiteBright';
/** Raw color value - not a theme key */
type Color = RGBColor | HexColor | Ansi256Color | AnsiColor;
/**
 * Structured text styling properties.
 * Used to style text without relying on ANSI string transforms.
 * Colors are raw values - theme resolution happens at the component layer.
 */
type TextStyles = {
    readonly color?: Color;
    readonly backgroundColor?: Color;
    readonly dim?: boolean;
    readonly bold?: boolean;
    readonly italic?: boolean;
    readonly underline?: boolean;
    readonly strikethrough?: boolean;
    readonly inverse?: boolean;
};
type Styles = {
    readonly textWrap?: 'wrap' | 'wrap-trim' | 'end' | 'middle' | 'truncate-end' | 'truncate' | 'truncate-middle' | 'truncate-start';
    readonly position?: 'absolute' | 'relative';
    readonly top?: number | `${number}%`;
    readonly bottom?: number | `${number}%`;
    readonly left?: number | `${number}%`;
    readonly right?: number | `${number}%`;
    /**
     * Size of the gap between an element's columns.
     */
    readonly columnGap?: number;
    /**
     * Size of the gap between element's rows.
     */
    readonly rowGap?: number;
    /**
     * Size of the gap between an element's columns and rows. Shorthand for `columnGap` and `rowGap`.
     */
    readonly gap?: number;
    /**
     * Margin on all sides. Equivalent to setting `marginTop`, `marginBottom`, `marginLeft` and `marginRight`.
     */
    readonly margin?: number;
    /**
     * Horizontal margin. Equivalent to setting `marginLeft` and `marginRight`.
     */
    readonly marginX?: number;
    /**
     * Vertical margin. Equivalent to setting `marginTop` and `marginBottom`.
     */
    readonly marginY?: number;
    /**
     * Top margin.
     */
    readonly marginTop?: number;
    /**
     * Bottom margin.
     */
    readonly marginBottom?: number;
    /**
     * Left margin.
     */
    readonly marginLeft?: number;
    /**
     * Right margin.
     */
    readonly marginRight?: number;
    /**
     * Padding on all sides. Equivalent to setting `paddingTop`, `paddingBottom`, `paddingLeft` and `paddingRight`.
     */
    readonly padding?: number;
    /**
     * Horizontal padding. Equivalent to setting `paddingLeft` and `paddingRight`.
     */
    readonly paddingX?: number;
    /**
     * Vertical padding. Equivalent to setting `paddingTop` and `paddingBottom`.
     */
    readonly paddingY?: number;
    /**
     * Top padding.
     */
    readonly paddingTop?: number;
    /**
     * Bottom padding.
     */
    readonly paddingBottom?: number;
    /**
     * Left padding.
     */
    readonly paddingLeft?: number;
    /**
     * Right padding.
     */
    readonly paddingRight?: number;
    /**
     * This property defines the ability for a flex item to grow if necessary.
     * See [flex-grow](https://css-tricks.com/almanac/properties/f/flex-grow/).
     */
    readonly flexGrow?: number;
    /**
     * It specifies the “flex shrink factor”, which determines how much the flex item will shrink relative to the rest of the flex items in the flex container when there isn’t enough space on the row.
     * See [flex-shrink](https://css-tricks.com/almanac/properties/f/flex-shrink/).
     */
    readonly flexShrink?: number;
    /**
     * It establishes the main-axis, thus defining the direction flex items are placed in the flex container.
     * See [flex-direction](https://css-tricks.com/almanac/properties/f/flex-direction/).
     */
    readonly flexDirection?: 'row' | 'column' | 'row-reverse' | 'column-reverse';
    /**
     * It specifies the initial size of the flex item, before any available space is distributed according to the flex factors.
     * See [flex-basis](https://css-tricks.com/almanac/properties/f/flex-basis/).
     */
    readonly flexBasis?: number | string;
    /**
     * It defines whether the flex items are forced in a single line or can be flowed into multiple lines. If set to multiple lines, it also defines the cross-axis which determines the direction new lines are stacked in.
     * See [flex-wrap](https://css-tricks.com/almanac/properties/f/flex-wrap/).
     */
    readonly flexWrap?: 'nowrap' | 'wrap' | 'wrap-reverse';
    /**
     * The align-items property defines the default behavior for how items are laid out along the cross axis (perpendicular to the main axis).
     * See [align-items](https://css-tricks.com/almanac/properties/a/align-items/).
     */
    readonly alignItems?: 'flex-start' | 'center' | 'flex-end' | 'stretch';
    /**
     * It makes possible to override the align-items value for specific flex items.
     * See [align-self](https://css-tricks.com/almanac/properties/a/align-self/).
     */
    readonly alignSelf?: 'flex-start' | 'center' | 'flex-end' | 'auto';
    /**
     * It defines the alignment along the main axis.
     * See [justify-content](https://css-tricks.com/almanac/properties/j/justify-content/).
     */
    readonly justifyContent?: 'flex-start' | 'flex-end' | 'space-between' | 'space-around' | 'space-evenly' | 'center';
    /**
     * Width of the element in spaces.
     * You can also set it in percent, which will calculate the width based on the width of parent element.
     */
    readonly width?: number | string;
    /**
     * Height of the element in lines (rows).
     * You can also set it in percent, which will calculate the height based on the height of parent element.
     */
    readonly height?: number | string;
    /**
     * Sets a minimum width of the element.
     */
    readonly minWidth?: number | string;
    /**
     * Sets a minimum height of the element.
     */
    readonly minHeight?: number | string;
    /**
     * Sets a maximum width of the element.
     */
    readonly maxWidth?: number | string;
    /**
     * Sets a maximum height of the element.
     */
    readonly maxHeight?: number | string;
    /**
     * Set this property to `none` to hide the element.
     */
    readonly display?: 'flex' | 'none';
    /**
     * Add a border with a specified style.
     * If `borderStyle` is `undefined` (which it is by default), no border will be added.
     */
    readonly borderStyle?: BorderStyle;
    /**
     * Determines whether top border is visible.
     *
     * @default true
     */
    readonly borderTop?: boolean;
    /**
     * Determines whether bottom border is visible.
     *
     * @default true
     */
    readonly borderBottom?: boolean;
    /**
     * Determines whether left border is visible.
     *
     * @default true
     */
    readonly borderLeft?: boolean;
    /**
     * Determines whether right border is visible.
     *
     * @default true
     */
    readonly borderRight?: boolean;
    /**
     * Change border color.
     * Shorthand for setting `borderTopColor`, `borderRightColor`, `borderBottomColor` and `borderLeftColor`.
     */
    readonly borderColor?: Color;
    /**
     * Change top border color.
     * Accepts raw color values (rgb, hex, ansi).
     */
    readonly borderTopColor?: Color;
    /**
     * Change bottom border color.
     * Accepts raw color values (rgb, hex, ansi).
     */
    readonly borderBottomColor?: Color;
    /**
     * Change left border color.
     * Accepts raw color values (rgb, hex, ansi).
     */
    readonly borderLeftColor?: Color;
    /**
     * Change right border color.
     * Accepts raw color values (rgb, hex, ansi).
     */
    readonly borderRightColor?: Color;
    /**
     * Dim the border color.
     * Shorthand for setting `borderTopDimColor`, `borderBottomDimColor`, `borderLeftDimColor` and `borderRightDimColor`.
     *
     * @default false
     */
    readonly borderDimColor?: boolean;
    /**
     * Dim the top border color.
     *
     * @default false
     */
    readonly borderTopDimColor?: boolean;
    /**
     * Dim the bottom border color.
     *
     * @default false
     */
    readonly borderBottomDimColor?: boolean;
    /**
     * Dim the left border color.
     *
     * @default false
     */
    readonly borderLeftDimColor?: boolean;
    /**
     * Dim the right border color.
     *
     * @default false
     */
    readonly borderRightDimColor?: boolean;
    /**
     * Add text within the border. Only applies to top or bottom borders.
     */
    readonly borderText?: BorderTextOptions;
    /**
     * Background color for the box. Fills the interior with background-colored
     * spaces and is inherited by child text nodes as their default background.
     */
    readonly backgroundColor?: Color;
    /**
     * Fill the box's interior (padding included) with spaces before
     * rendering children, so nothing behind it shows through. Like
     * `backgroundColor` but without emitting any SGR — the terminal's
     * default background is used. Useful for absolute-positioned overlays
     * where Box padding/gaps would otherwise be transparent.
     */
    readonly opaque?: boolean;
    /**
     * Behavior for an element's overflow in both directions.
     * 'scroll' constrains the container's size (children do not expand it)
     * and enables scrollTop-based virtualized scrolling at render time.
     *
     * @default 'visible'
     */
    readonly overflow?: 'visible' | 'hidden' | 'scroll';
    /**
     * Behavior for an element's overflow in horizontal direction.
     *
     * @default 'visible'
     */
    readonly overflowX?: 'visible' | 'hidden' | 'scroll';
    /**
     * Behavior for an element's overflow in vertical direction.
     *
     * @default 'visible'
     */
    readonly overflowY?: 'visible' | 'hidden' | 'scroll';
    /**
     * Exclude this box's cells from text selection in fullscreen mode.
     * Cells inside this region are skipped by both the selection highlight
     * and the copied text — useful for fencing off gutters (line numbers,
     * diff sigils) so click-drag over a diff yields clean copyable code.
     * Only affects alt-screen text selection; no-op otherwise.
     *
     * `'from-left-edge'` extends the exclusion from column 0 to the box's
     * right edge for every row it occupies — this covers any upstream
     * indentation (tool message prefix, tree lines) so a multi-row drag
     * doesn't pick up leading whitespace from middle rows.
     */
    readonly noSelect?: boolean | 'from-left-edge';
};

type InkNode = {
    parentNode: DOMElement | undefined;
    yogaNode?: LayoutNode;
    style: Styles;
};
type TextName = '#text';
type ElementNames = 'ink-root' | 'ink-box' | 'ink-text' | 'ink-virtual-text' | 'ink-link' | 'ink-progress' | 'ink-raw-ansi';
type NodeNames = ElementNames | TextName;
type DOMElement = {
    nodeName: ElementNames;
    attributes: Record<string, DOMNodeAttribute>;
    childNodes: DOMNode[];
    textStyles?: TextStyles;
    onComputeLayout?: () => void;
    onRender?: () => void;
    onImmediateRender?: () => void;
    hasRenderedContent?: boolean;
    dirty: boolean;
    isHidden?: boolean;
    _eventHandlers?: Record<string, unknown>;
    scrollTop?: number;
    pendingScrollDelta?: number;
    scrollClampMin?: number;
    scrollClampMax?: number;
    scrollHeight?: number;
    scrollViewportHeight?: number;
    scrollViewportTop?: number;
    stickyScroll?: boolean;
    scrollAnchor?: {
        el: DOMElement;
        offset: number;
    };
    focusManager?: FocusManager;
    debugOwnerChain?: string[];
} & InkNode;
type TextNode = {
    nodeName: TextName;
    nodeValue: string;
} & InkNode;
type DOMNode<T = {
    nodeName: NodeNames;
}> = T extends {
    nodeName: infer U;
} ? U extends '#text' ? TextNode : DOMElement : never;
type DOMNodeAttribute = boolean | string | number;

/**
 * Mouse click event. Fired on left-button release without drag, only when
 * mouse tracking is enabled (i.e. inside <AlternateScreen>).
 *
 * Bubbles from the deepest hit node up through parentNode. Call
 * stopImmediatePropagation() to prevent ancestors' onClick from firing.
 */
declare class ClickEvent extends Event {
    /** 0-indexed screen column of the click */
    readonly col: number;
    /** 0-indexed screen row of the click */
    readonly row: number;
    /**
     * Click column relative to the current handler's Box (col - box.x).
     * Recomputed by dispatchClick before each handler fires, so an onClick
     * on a container sees coords relative to that container, not to any
     * child the click landed on.
     */
    localCol: number;
    /** Click row relative to the current handler's Box (row - box.y). */
    localRow: number;
    /**
     * True if the clicked cell has no visible content (unwritten in the
     * screen buffer — both packed words are 0). Handlers can check this to
     * ignore clicks on blank space to the right of text, so accidental
     * clicks on empty terminal space don't toggle state.
     */
    readonly cellIsBlank: boolean;
    constructor(col: number, row: number, cellIsBlank: boolean);
}

/**
 * Keyboard input parser - converts terminal input to key events
 *
 * Uses the termio tokenizer for escape sequence boundary detection,
 * then interprets sequences as keypresses.
 */

/**
 * A response sequence received from the terminal (not a keypress).
 * Emitted in answer to queries like DECRQM, DA1, OSC 11, etc.
 */
type TerminalResponse = 
/** DECRPM: answer to DECRQM (request DEC private mode status) */
{
    type: 'decrpm';
    mode: number;
    status: number;
}
/** DA1: primary device attributes (used as a universal sentinel) */
 | {
    type: 'da1';
    params: number[];
}
/** DA2: secondary device attributes (terminal version info) */
 | {
    type: 'da2';
    params: number[];
}
/** Kitty keyboard protocol: current flags (answer to CSI ? u) */
 | {
    type: 'kittyKeyboard';
    flags: number;
}
/** DSR: cursor position report (answer to CSI 6 n) */
 | {
    type: 'cursorPosition';
    row: number;
    col: number;
}
/** OSC response: generic operating-system-command reply (e.g. OSC 11 bg color) */
 | {
    type: 'osc';
    code: number;
    data: string;
}
/** XTVERSION: terminal name/version string (answer to CSI > 0 q).
 *  Example values: "xterm.js(5.5.0)", "ghostty 1.2.0", "iTerm2 3.6". */
 | {
    type: 'xtversion';
    name: string;
};
type ParsedKey = {
    kind: 'key';
    fn: boolean;
    name: string | undefined;
    ctrl: boolean;
    meta: boolean;
    shift: boolean;
    option: boolean;
    super: boolean;
    sequence: string | undefined;
    raw: string | undefined;
    code?: string;
    isPasted: boolean;
};

/**
 * Keyboard event dispatched through the DOM tree via capture/bubble.
 *
 * Follows browser KeyboardEvent semantics: `key` is the literal character
 * for printable keys ('a', '3', ' ', '/') and a multi-char name for
 * special keys ('down', 'return', 'escape', 'f1'). The idiomatic
 * printable-char check is `e.key.length === 1`.
 */
declare class KeyboardEvent extends TerminalEvent {
    readonly key: string;
    readonly ctrl: boolean;
    readonly shift: boolean;
    readonly meta: boolean;
    readonly superKey: boolean;
    readonly fn: boolean;
    constructor(parsedKey: ParsedKey);
}

type Props$3 = Except<Styles, 'textWrap'> & {
    ref?: Ref<DOMElement>;
    /**
     * Tab order index. Nodes with `tabIndex >= 0` participate in
     * Tab/Shift+Tab cycling; `-1` means programmatically focusable only.
     */
    tabIndex?: number;
    /**
     * Focus this element when it mounts. Like the HTML `autofocus`
     * attribute — the FocusManager calls `focus(node)` during the
     * reconciler's `commitMount` phase.
     */
    autoFocus?: boolean;
    /**
     * Fired on left-button click (press + release without drag). Only works
     * inside `<AlternateScreen>` where mouse tracking is enabled — no-op
     * otherwise. The event bubbles from the deepest hit Box up through
     * ancestors; call `event.stopImmediatePropagation()` to stop bubbling.
     */
    onClick?: (event: ClickEvent) => void;
    onFocus?: (event: FocusEvent) => void;
    onFocusCapture?: (event: FocusEvent) => void;
    onBlur?: (event: FocusEvent) => void;
    onBlurCapture?: (event: FocusEvent) => void;
    onKeyDown?: (event: KeyboardEvent) => void;
    onKeyDownCapture?: (event: KeyboardEvent) => void;
    /**
     * Fired when the mouse moves into this Box's rendered rect. Like DOM
     * `mouseenter`, does NOT bubble — moving between children does not
     * re-fire on the parent. Only works inside `<AlternateScreen>` where
     * mode-1003 mouse tracking is enabled.
     */
    onMouseEnter?: () => void;
    /** Fired when the mouse moves out of this Box's rendered rect. */
    onMouseLeave?: () => void;
};
/**
 * `<Box>` is an essential Ink component to build your layout. It's like `<div style="display: flex">` in the browser.
 */
declare function Box(t0: any): any;

type BaseProps = {
    /**
     * Change text color. Accepts a raw color value (rgb, hex, ansi).
     */
    readonly color?: Color;
    /**
     * Same as `color`, but for background.
     */
    readonly backgroundColor?: Color;
    /**
     * Make the text italic.
     */
    readonly italic?: boolean;
    /**
     * Make the text underlined.
     */
    readonly underline?: boolean;
    /**
     * Make the text crossed with a line.
     */
    readonly strikethrough?: boolean;
    /**
     * Inverse background and foreground colors.
     */
    readonly inverse?: boolean;
    /**
     * This property tells Ink to wrap or truncate text if its width is larger than container.
     * If `wrap` is passed (by default), Ink will wrap text and split it into multiple lines.
     * If `truncate-*` is passed, Ink will truncate text instead, which will result in one line of text with the rest cut off.
     */
    readonly wrap?: Styles['textWrap'];
    readonly children?: ReactNode;
};
/**
 * Bold and dim are mutually exclusive in terminals.
 * This type ensures you can use one or the other, but not both.
 */
type WeightProps = {
    bold?: never;
    dim?: never;
} | {
    bold: boolean;
    dim?: never;
} | {
    dim: boolean;
    bold?: never;
};
type Props$2 = BaseProps & WeightProps;
/**
 * This component can display text, and change its style to make it colorful, bold, underline, italic or strikethrough.
 */
declare function Text(t0: any): any;

/**
 * Hook for synchronized animations that pause when offscreen.
 *
 * Returns a ref to attach to the animated element and the current animation time.
 * All instances share the same clock, so animations stay in sync.
 * The clock only runs when at least one keepAlive subscriber exists.
 *
 * Pass `null` to pause — unsubscribes from the clock so no ticks fire.
 * Time freezes at the last value and resumes from the current clock time
 * when a number is passed again.
 *
 * @param intervalMs - How often to update, or null to pause
 * @returns [ref, time] - Ref to attach to element, elapsed time in ms
 *
 * @example
 * function Spinner() {
 *   const [ref, time] = useAnimationFrame(120)
 *   const frame = Math.floor(time / 120) % FRAMES.length
 *   return <Box ref={ref}>{FRAMES[frame]}</Box>
 * }
 *
 * The clock automatically slows when the terminal is blurred,
 * so consumers don't need to handle focus state.
 */
declare function useAnimationFrame(intervalMs?: number | null): [ref: (element: DOMElement | null) => void, time: number];

type Props$1 = {
    /**
     * Exit (unmount) the whole Ink app.
     */
    readonly exit: (error?: Error) => void;
};

/**
 * `useApp` is a React hook, which exposes a method to manually exit the app (unmount).
 */
declare const useApp: () => Props$1;

type Key = {
    upArrow: boolean;
    downArrow: boolean;
    leftArrow: boolean;
    rightArrow: boolean;
    pageDown: boolean;
    pageUp: boolean;
    wheelUp: boolean;
    wheelDown: boolean;
    home: boolean;
    end: boolean;
    return: boolean;
    escape: boolean;
    ctrl: boolean;
    shift: boolean;
    fn: boolean;
    tab: boolean;
    backspace: boolean;
    delete: boolean;
    meta: boolean;
    super: boolean;
};
declare class InputEvent extends Event {
    readonly keypress: ParsedKey;
    readonly key: Key;
    readonly input: string;
    constructor(keypress: ParsedKey);
}

type Handler = (input: string, key: Key, event: InputEvent) => void;
type Options$1 = {
    /**
     * Enable or disable capturing of user input.
     * Useful when there are multiple useInput hooks used at once to avoid handling the same input several times.
     *
     * @default true
     */
    isActive?: boolean;
};
/**
 * This hook is used for handling user input.
 * It's a more convenient alternative to using `StdinContext` and listening to `data` events.
 * The callback you pass to `useInput` is called for each character when user enters any input.
 * However, if user pastes text and it's more than one character, the callback will be called only once and the whole string will be passed as `input`.
 *
 * ```
 * import {useInput} from 'ink';
 *
 * const UserInput = () => {
 *   useInput((input, key) => {
 *     if (input === 'q') {
 *       // Exit program
 *     }
 *
 *     if (key.leftArrow) {
 *       // Left arrow key pressed
 *     }
 *   });
 *
 *   return …
 * };
 * ```
 */
declare const useInput: (inputHandler: Handler, options?: Options$1) => void;

/**
 * Returns the clock time, updating at the given interval.
 * Subscribes as non-keepAlive — won't keep the clock alive on its own,
 * but updates whenever a keepAlive subscriber (e.g. the spinner)
 * is driving the clock.
 *
 * Use this to drive pure time-based computations (shimmer position,
 * frame index) from the shared clock.
 */
declare function useAnimationTimer(intervalMs: number): number;
/**
 * Interval hook backed by the shared Clock.
 *
 * Unlike `useInterval` from `usehooks-ts` (which creates its own setInterval),
 * this piggybacks on the single shared clock so all timers consolidate into
 * one wake-up. Pass `null` for intervalMs to pause.
 */
declare function useInterval(callback: () => void, intervalMs: number | null): void;

/**
 * Text selection state for fullscreen mode.
 *
 * Tracks a linear selection in screen-buffer coordinates (0-indexed col/row).
 * Selection is line-based: cells from (startCol, startRow) through
 * (endCol, endRow) inclusive, wrapping across line boundaries. This matches
 * terminal-native selection behavior (not rectangular/block).
 *
 * The selection is stored as ANCHOR (where the drag started) + FOCUS (where
 * the cursor is now). The rendered highlight normalizes to start ≤ end.
 */

type Point = {
    col: number;
    row: number;
};
type SelectionState = {
    /** Where the mouse-down occurred. Null when no selection. */
    anchor: Point | null;
    /** Current drag position (updated on mouse-move while dragging). */
    focus: Point | null;
    /** True between mouse-down and mouse-up. */
    isDragging: boolean;
    /** For word/line mode: the initial word/line bounds from the first
     *  multi-click. Drag extends from this span to the word/line at the
     *  current mouse position so the original word/line stays selected
     *  even when dragging backward past it. Null ⇔ char mode. The kind
     *  tells extendSelection whether to snap to word or line boundaries. */
    anchorSpan: {
        lo: Point;
        hi: Point;
        kind: 'word' | 'line';
    } | null;
    /** Text from rows that scrolled out ABOVE the viewport during
     *  drag-to-scroll. The screen buffer only holds the current viewport,
     *  so without this accumulator, dragging down past the bottom edge
     *  loses the top of the selection once the anchor clamps. Prepended
     *  to the on-screen text by getSelectedText. Reset on start/clear. */
    scrolledOffAbove: string[];
    /** Symmetric: rows scrolled out BELOW when dragging up. Appended. */
    scrolledOffBelow: string[];
    /** Soft-wrap bits parallel to scrolledOffAbove — true means the row
     *  is a continuation of the one before it (the `\n` was inserted by
     *  word-wrap, not in the source). Captured alongside the text at
     *  scroll time since the screen's softWrap bitmap shifts with content.
     *  getSelectedText uses these to join wrapped rows back into logical
     *  lines. */
    scrolledOffAboveSW: boolean[];
    /** Parallel to scrolledOffBelow. */
    scrolledOffBelowSW: boolean[];
    /** Pre-clamp anchor row. Set when shiftSelection clamps anchor so a
     *  reverse scroll can restore the true position and pop accumulators.
     *  Without this, PgDn (clamps anchor) → PgUp leaves anchor at the wrong
     *  row AND scrolledOffAbove stale — highlight ≠ copy. Undefined when
     *  anchor is in-bounds (no clamp debt). Cleared on start/clear. */
    virtualAnchorRow?: number;
    /** Same for focus. */
    virtualFocusRow?: number;
    /** True if the mouse-down that started this selection had the alt
     *  modifier set (SGR button bit 0x08). On macOS xterm.js this is a
     *  signal that VS Code's macOptionClickForcesSelection is OFF — if it
     *  were on, xterm.js would have consumed the event for native selection
     *  and we'd never receive it. Used by the footer to show the right hint. */
    lastPressHadAlt: boolean;
};
/** Semantic keyboard focus moves. See moveSelectionFocus in ink.tsx for
 *  how screen bounds + row-wrap are applied. */
type FocusMove = 'left' | 'right' | 'up' | 'down' | 'lineStart' | 'lineEnd';

/**
 * Access to text selection operations on the Ink instance (fullscreen only).
 * Returns no-op functions when fullscreen mode is disabled.
 */
declare function useSelection(): {
    copySelection: () => string;
    /** Copy without clearing the highlight (for copy-on-select). */
    copySelectionNoClear: () => string;
    clearSelection: () => void;
    hasSelection: () => boolean;
    /** Read the raw mutable selection state (for drag-to-scroll). */
    getState: () => SelectionState | null;
    /** Subscribe to selection mutations (start/update/finish/clear). */
    subscribe: (cb: () => void) => () => void;
    /** Shift the anchor row by dRow, clamped to [minRow, maxRow]. */
    shiftAnchor: (dRow: number, minRow: number, maxRow: number) => void;
    /** Shift anchor AND focus by dRow (keyboard scroll: whole selection
     *  tracks content). Clamped points get col reset to the full-width edge
     *  since their content was captured by captureScrolledRows. Reads
     *  screen.width from the ink instance for the col-reset boundary. */
    shiftSelection: (dRow: number, minRow: number, maxRow: number) => void;
    /** Keyboard selection extension (shift+arrow): move focus, anchor fixed.
     *  Left/right wrap across rows; up/down clamp at viewport edges. */
    moveFocus: (move: FocusMove) => void;
    /** Capture text from rows about to scroll out of the viewport (call
     *  BEFORE scrollBy so the screen buffer still has the outgoing rows). */
    captureScrolledRows: (firstRow: number, lastRow: number, side: 'above' | 'below') => void;
    /** Set the selection highlight bg color (theme-piping; solid bg
     *  replaces the old SGR-7 inverse so syntax highlighting stays readable
     *  under selection). Call once on mount + whenever theme changes. */
    setSelectionBgColor: (color: string) => void;
};

declare class EventEmitter extends EventEmitter$1 {
    constructor();
    emit(type: string | symbol, ...args: unknown[]): boolean;
}

/**
 * Query the terminal and await responses without timeouts.
 *
 * Terminal queries (DECRQM, DA1, OSC 11, etc.) share the stdin stream
 * with keyboard input. Response sequences are syntactically
 * distinguishable from key events, so the input parser recognizes them
 * and dispatches them here.
 *
 * To avoid timeouts, each query batch is terminated by a DA1 sentinel
 * (CSI c) — every terminal since VT100 responds to DA1, and terminals
 * answer queries in order. So: if your query's response arrives before
 * DA1's, the terminal supports it; if DA1 arrives first, it doesn't.
 *
 * Usage:
 *   const [sync, grapheme] = await Promise.all([
 *     querier.send(decrqm(2026)),
 *     querier.send(decrqm(2027)),
 *     querier.flush(),
 *   ])
 *   // sync and grapheme are DECRPM responses or undefined if unsupported
 */

/** A terminal query: an outbound request sequence paired with a matcher
 *  that recognizes the expected inbound response. Built by `decrqm()`,
 *  `oscColor()`, `kittyKeyboard()`, etc. */
type TerminalQuery<T extends TerminalResponse = TerminalResponse> = {
    /** Escape sequence to write to stdout */
    request: string;
    /** Recognizes the expected response in the inbound stream */
    match: (r: TerminalResponse) => r is T;
};
declare class TerminalQuerier {
    private stdout;
    /**
     * Interleaved queue of queries and sentinels in send order. Terminals
     * respond in order, so each flush() barrier only drains queries queued
     * before it — concurrent batches from independent callers stay isolated.
     */
    private queue;
    constructor(stdout: NodeJS.WriteStream);
    /**
     * Send a query and wait for its response.
     *
     * Resolves with the response when `query.match` matches an incoming
     * TerminalResponse, or with `undefined` when a flush() sentinel arrives
     * before any matching response (meaning the terminal ignored the query).
     *
     * Never rejects; never times out on its own. If you never call flush()
     * and the terminal doesn't respond, the promise remains pending.
     */
    send<T extends TerminalResponse>(query: TerminalQuery<T>): Promise<T | undefined>;
    /**
     * Send the DA1 sentinel. Resolves when DA1's response arrives.
     *
     * As a side effect, all queries still pending when DA1 arrives are
     * resolved with `undefined` (terminal didn't respond → doesn't support
     * the query). This is the barrier that makes send() timeout-free.
     *
     * Safe to call with no pending queries — still waits for a round-trip.
     */
    flush(): Promise<void>;
    /**
     * Dispatch a response parsed from stdin. Called by App.tsx's
     * processKeysInBatch for every `kind: 'response'` item.
     *
     * Matching strategy:
     * - First, try to match a pending query (FIFO, first match wins).
     *   This lets callers send(da1()) explicitly if they want the DA1
     *   params — a separate DA1 write means the terminal sends TWO DA1
     *   responses. The first matches the explicit query; the second
     *   (unmatched) fires the sentinel.
     * - Otherwise, if this is a DA1, fire the FIRST pending sentinel:
     *   resolve any queries queued before that sentinel with undefined
     *   (the terminal answered DA1 without answering them → unsupported)
     *   and signal its flush() completion. Only draining up to the first
     *   sentinel keeps later batches intact when multiple callers have
     *   concurrent queries in flight.
     * - Unsolicited responses (no match, no sentinel) are silently dropped.
     */
    onResponse(r: TerminalResponse): void;
}

type Props = {
    /**
     * Stdin stream passed to `render()` in `options.stdin` or `process.stdin` by default. Useful if your app needs to handle user input.
     */
    readonly stdin: NodeJS.ReadStream;
    /**
     * Ink exposes this function via own `<StdinContext>` to be able to handle Ctrl+C, that's why you should use Ink's `setRawMode` instead of `process.stdin.setRawMode`.
     * If the `stdin` stream passed to Ink does not support setRawMode, this function does nothing.
     */
    readonly setRawMode: (value: boolean) => void;
    /**
     * A boolean flag determining if the current `stdin` supports `setRawMode`. A component using `setRawMode` might want to use `isRawModeSupported` to nicely fall back in environments where raw mode is not supported.
     */
    readonly isRawModeSupported: boolean;
    readonly internal_exitOnCtrlC: boolean;
    readonly internal_eventEmitter: EventEmitter;
    /** Query the terminal and await responses (DECRQM, OSC 11, etc.).
     *  Null only in the never-reached default context value. */
    readonly internal_querier: TerminalQuerier | null;
};

/**
 * `useStdin` is a React hook, which exposes stdin stream.
 */
declare const useStdin: () => Props;

type TabStatusKind = 'idle' | 'busy' | 'waiting';
/**
 * Declaratively set the tab-status indicator (OSC 21337).
 *
 * Emits a colored dot + short status text to the tab sidebar. Terminals
 * that don't support OSC 21337 discard the sequence silently, so this is
 * safe to call unconditionally. Wrapped for tmux/screen passthrough.
 *
 * Pass `null` to opt out. If a status was previously set, transitioning to
 * `null` emits CLEAR_TAB_STATUS so toggling off mid-session doesn't leave
 * a stale dot. Process-exit cleanup is handled by ink.tsx's unmount path.
 */
declare function useTabStatus(kind: TabStatusKind | null): void;

/**
 * Hook to check if the terminal has focus.
 *
 * Uses DECSET 1004 focus reporting - the terminal sends escape sequences
 * when it gains or loses focus. These are handled automatically
 * by Ink and filtered from useInput.
 *
 * @returns true if the terminal is focused (or focus state is unknown)
 */
declare function useTerminalFocus(): boolean;

/**
 * Declaratively set the terminal tab/window title.
 *
 * Pass a string to set the title. ANSI escape sequences are stripped
 * automatically so callers don't need to know about terminal encoding.
 * Pass `null` to opt out — the hook becomes a no-op and leaves the
 * terminal title untouched.
 *
 * On Windows, uses `process.title` (classic conhost doesn't support OSC).
 * Elsewhere, writes OSC 0 (set title+icon) via Ink's stdout.
 */
declare function useTerminalTitle(title: string | null): void;

type ViewportEntry = {
    /**
     * Whether the element is currently within the terminal viewport
     */
    isVisible: boolean;
};
/**
 * Hook to detect if a component is within the terminal viewport.
 *
 * Returns a callback ref and a viewport entry object.
 * Attach the ref to the component you want to track.
 *
 * The entry is updated during the layout phase (useLayoutEffect) so callers
 * always read fresh values during render. Visibility changes do NOT trigger
 * re-renders on their own — callers that re-render for other reasons (e.g.
 * animation ticks, state changes) will pick up the latest value naturally.
 * This avoids infinite update loops when combined with other layout effects
 * that also call setState.
 *
 * @example
 * const [ref, entry] = useTerminalViewport()
 * return <Box ref={ref}><Animation enabled={entry.isVisible}>...</Animation></Box>
 */
declare function useTerminalViewport(): [
    ref: (element: DOMElement | null) => void,
    entry: ViewportEntry
];

type Output = {
    /**
     * Element width.
     */
    width: number;
    /**
     * Element height.
     */
    height: number;
};
/**
 * Measure the dimensions of a particular `<Box>` element.
 */
declare const measureElement: (node: DOMElement) => Output;

/**
 * Adds one or more newline (\n) characters. Must be used within <Text> components.
 */
declare function Newline(t0: any): any;

/**
 * A flexible space that expands along the major axis of its containing layout.
 * It's useful as a shortcut for filling all the available spaces between elements.
 */
declare function Spacer(): any;

declare function Link(t0: any): any;

declare function Button(t0: any): any;

/**
 * Run children in the terminal's alternate screen buffer, constrained to
 * the viewport height. While mounted:
 *
 * - Enters the alt screen (DEC 1049), clears it, homes the cursor
 * - Constrains its own height to the terminal row count, so overflow must
 *   be handled via `overflow: scroll` / flexbox (no native scrollback)
 * - Optionally enables SGR mouse tracking (wheel + click/drag) — events
 *   surface as `ParsedKey` (wheel) and update the Ink instance's
 *   selection state (click/drag)
 *
 * On unmount, disables mouse tracking and exits the alt screen, restoring
 * the main screen's content. Safe for use in ctrl-o transcript overlays
 * and similar temporary fullscreen views — the main screen is preserved.
 *
 * Notifies the Ink instance via `setAltScreenActive()` so the renderer
 * keeps the cursor inside the viewport (preventing the cursor-restore LF
 * from scrolling content) and so signal-exit cleanup can exit the alt
 * screen if the component's own unmount doesn't run.
 */
declare function AlternateScreen(t0: any): any;

/**
 * Marks its contents as non-selectable in fullscreen text selection.
 * Cells inside this box are skipped by both the selection highlight and
 * the copied text — the gutter stays visually unchanged while the user
 * drags, making it clear what will be copied.
 *
 * Use to fence off gutters (line numbers, diff +/- sigils, list bullets)
 * so click-drag over rendered code yields clean pasteable content:
 *
 *   <Box flexDirection="row">
 *     <NoSelect fromLeftEdge><Text dimColor> 42 +</Text></NoSelect>
 *     <Text>const x = 1</Text>
 *   </Box>
 *
 * Only affects alt-screen text selection (<AlternateScreen> with mouse
 * tracking). No-op in the main-screen scrollback render where the
 * terminal's native selection is used instead.
 */
declare function NoSelect(t0: any): any;

/**
 * Bypass the <Ansi> → React tree → Yoga → squash → re-serialize roundtrip for
 * content that is already terminal-ready.
 *
 * Use this when an external renderer (e.g. the ColorDiff NAPI module) has
 * already produced ANSI-escaped, width-wrapped output. A normal <Ansi> mount
 * reparses that output into one React <Text> per style span, lays out each
 * span as a Yoga flex child, then walks the tree to re-emit the same escape
 * codes it was given. For a long transcript full of syntax-highlighted diffs
 * that roundtrip is the dominant cost of the render.
 *
 * This component emits a single Yoga leaf with a constant-time measure func
 * (width × lines.length) and hands the joined string straight to output.write(),
 * which already splits on '\n' and parses ANSI into the screen buffer.
 */
declare function RawAnsi(t0: any): any;

type ScrollBoxHandle = {
    scrollTo: (y: number) => void;
    scrollBy: (dy: number) => void;
    /**
     * Scroll so `el`'s top is at the viewport top (plus `offset`). Unlike
     * scrollTo which bakes a number that's stale by the time the throttled
     * render fires, this defers the position read to render time —
     * render-node-to-output reads `el.yogaNode.getComputedTop()` in the
     * SAME Yoga pass that computes scrollHeight. Deterministic. One-shot.
     */
    scrollToElement: (el: DOMElement, offset?: number) => void;
    scrollToBottom: () => void;
    getScrollTop: () => number;
    getPendingDelta: () => number;
    getScrollHeight: () => number;
    /**
     * Like getScrollHeight, but reads Yoga directly instead of the cached
     * value written by render-node-to-output (throttled, up to 16ms stale).
     * Use when you need a fresh value in useLayoutEffect after a React commit
     * that grew content. Slightly more expensive (native Yoga call).
     */
    getFreshScrollHeight: () => number;
    getViewportHeight: () => number;
    /**
     * Absolute screen-buffer row of the first visible content line (inside
     * padding). Used for drag-to-scroll edge detection.
     */
    getViewportTop: () => number;
    /**
     * True when scroll is pinned to the bottom. Set by scrollToBottom, the
     * initial stickyScroll attribute, and by the renderer when positional
     * follow fires (scrollTop at prevMax, content grows). Cleared by
     * scrollTo/scrollBy. Stable signal for "at bottom" that doesn't depend on
     * layout values (unlike scrollTop+viewportH >= scrollHeight).
     */
    isSticky: () => boolean;
    /**
     * Subscribe to imperative scroll changes (scrollTo/scrollBy/scrollToBottom).
     * Does NOT fire for stickyScroll updates done by the Ink renderer — those
     * happen during Ink's render phase after React has committed. Callers that
     * care about the sticky case should treat "at bottom" as a fallback.
     */
    subscribe: (listener: () => void) => () => void;
    /**
     * Set the render-time scrollTop clamp to the currently-mounted children's
     * coverage span. Called by useVirtualScroll after computing its range;
     * render-node-to-output clamps scrollTop to [min, max] so burst scrollTo
     * calls that race past React's async re-render show the edge of mounted
     * content instead of blank spacer. Pass undefined to disable (sticky,
     * cold start).
     */
    setClampBounds: (min: number | undefined, max: number | undefined) => void;
};
type ScrollBoxProps = Except<Styles, 'textWrap' | 'overflow' | 'overflowX' | 'overflowY'> & {
    ref?: Ref<ScrollBoxHandle>;
    /**
     * When true, automatically pins scroll position to the bottom when content
     * grows. Unset manually via scrollTo/scrollBy to break the stickiness.
     */
    stickyScroll?: boolean;
};
/**
 * A Box with `overflow: scroll` and an imperative scroll API.
 *
 * Children are laid out at their full Yoga-computed height inside a
 * constrained container. At render time, only children intersecting the
 * visible window (scrollTop..scrollTop+height) are rendered (viewport
 * culling). Content is translated by -scrollTop and clipped to the box bounds.
 *
 * Works best inside a fullscreen (constrained-height root) Ink tree.
 */
declare function ScrollBox({ children, ref, stickyScroll, ...style }: PropsWithChildren<ScrollBoxProps>): React.ReactNode;

type TextInputProps = {
    readonly value: string;
    readonly onChange: (value: string) => void;
    readonly onSubmit?: (value: string) => void;
    readonly placeholder?: string;
    readonly focus?: boolean;
    readonly mask?: string;
    readonly showCursor?: boolean;
    readonly highlightPastedText?: boolean;
};
declare function TextInput({ value: originalValue, onChange, onSubmit, placeholder, focus, mask, showCursor, highlightPastedText, }: TextInputProps): React.ReactElement;

type TerminalFocusEventType = 'terminalfocus' | 'terminalblur';
/**
 * Event fired when the terminal window gains or loses focus.
 *
 * Uses DECSET 1004 focus reporting - the terminal sends:
 * - CSI I (\x1b[I) when the terminal gains focus
 * - CSI O (\x1b[O) when the terminal loses focus
 */
declare class TerminalFocusEvent extends Event {
    readonly type: TerminalFocusEventType;
    constructor(type: TerminalFocusEventType);
}

declare const stringWidth: (str: string) => number;

declare function wrapText(text: string, maxWidth: number, wrapType: Styles['textWrap']): string;

type FlickerReason = 'resize' | 'offscreen' | 'clear';
type FrameEvent = {
    durationMs: number;
    /** Phase breakdown in ms + patch count. Populated when the ink instance
     *  has frame-timing instrumentation enabled (via onFrame wiring). */
    phases?: {
        /** createRenderer output: DOM → yoga layout → screen buffer */
        renderer: number;
        /** LogUpdate.render(): screen diff → Patch[] (the hot path this PR optimizes) */
        diff: number;
        /** optimize(): patch merge/dedupe */
        optimize: number;
        /** writeDiffToTerminal(): serialize patches → ANSI → stdout */
        write: number;
        /** Pre-optimize patch count (proxy for how much changed this frame) */
        patches: number;
        /** yoga calculateLayout() time (runs in resetAfterCommit, before onRender) */
        yoga: number;
        /** React reconcile time: scrollMutated → resetAfterCommit. 0 if no commit. */
        commit: number;
        /** layoutNode() calls this frame (recursive, includes cache-hit returns) */
        yogaVisited: number;
        /** measureFunc (text wrap/width) calls — the expensive part */
        yogaMeasured: number;
        /** early returns via _hasL single-slot cache */
        yogaCacheHits: number;
        /** total yoga Node instances alive (create - free). Growth = leak. */
        yogaLive: number;
    };
    flickers: Array<{
        desiredHeight: number;
        availableHeight: number;
        reason: FlickerReason;
    }>;
};

/** Position of a match within a rendered message, relative to the message's
 *  own bounding box (row 0 = message top). Stable across scroll — to
 *  highlight on the real screen, add the message's screen-row offset. */
type MatchPosition = {
    row: number;
    col: number;
    /** Number of CELLS the match spans (= query.length for ASCII, more
     *  for wide chars in the query). */
    len: number;
};

type Options = {
    stdout: NodeJS.WriteStream;
    stdin: NodeJS.ReadStream;
    stderr: NodeJS.WriteStream;
    exitOnCtrlC: boolean;
    patchConsole: boolean;
    waitUntilExit?: () => Promise<void>;
    onFrame?: (event: FrameEvent) => void;
};
declare class Ink {
    private readonly options;
    private readonly log;
    private readonly terminal;
    private scheduleRender;
    private isUnmounted;
    private isPaused;
    private readonly container;
    private rootNode;
    readonly focusManager: FocusManager;
    private renderer;
    private readonly stylePool;
    private charPool;
    private hyperlinkPool;
    private exitPromise?;
    private restoreConsole?;
    private restoreStderr?;
    private readonly unsubscribeTTYHandlers?;
    private terminalColumns;
    private terminalRows;
    private currentNode;
    private frontFrame;
    private backFrame;
    private lastPoolResetTime;
    private drainTimer;
    private lastYogaCounters;
    private altScreenParkPatch;
    readonly selection: SelectionState;
    private searchHighlightQuery;
    private searchPositions;
    private readonly selectionListeners;
    private readonly hoveredNodes;
    private altScreenActive;
    private altScreenMouseTracking;
    private prevFrameContaminated;
    private needsEraseBeforePaint;
    private cursorDeclaration;
    private displayCursor;
    constructor(options: Options);
    private handleResume;
    private handleResize;
    resolveExitPromise: () => void;
    rejectExitPromise: (reason?: Error) => void;
    unsubscribeExit: () => void;
    /**
     * Pause Ink and hand the terminal over to an external TUI (e.g. git
     * commit editor). In non-fullscreen mode this enters the alt screen;
     * in fullscreen mode we're already in alt so we just clear it.
     * Call `exitAlternateScreen()` when done to restore Ink.
     */
    enterAlternateScreen(): void;
    /**
     * Resume Ink after an external TUI handoff with a full repaint.
     * In non-fullscreen mode this exits the alt screen back to main;
     * in fullscreen mode we re-enter alt and clear + repaint.
     *
     * The re-enter matters: terminal editors (vim, nano, less) write
     * smcup/rmcup (?1049h/?1049l), so even though we started in alt,
     * the editor's rmcup on exit drops us to main screen. Without
     * re-entering, the 2J below wipes the user's main-screen scrollback
     * and subsequent renders land in main — native terminal scroll
     * returns, fullscreen scroll is dead.
     */
    exitAlternateScreen(): void;
    onRender(): void;
    pause(): void;
    resume(): void;
    /**
     * Reset frame buffers so the next render writes the full screen from scratch.
     * Call this before resume() when the terminal content has been corrupted by
     * an external process (e.g. tmux, shell, full-screen TUI).
     */
    repaint(): void;
    /**
     * Clear the physical terminal and force a full redraw.
     *
     * The traditional readline ctrl+l — clears the visible screen and
     * redraws the current content. Also the recovery path when the terminal
     * was cleared externally (macOS Cmd+K) and Ink's diff engine thinks
     * unchanged cells don't need repainting. Scrollback is preserved.
     */
    forceRedraw(): void;
    /**
     * Mark the previous frame as untrustworthy for blit, forcing the next
     * render to do a full-damage diff instead of the per-node fast path.
     *
     * Lighter than forceRedraw() — no screen clear, no extra write. Call
     * from a useLayoutEffect cleanup when unmounting a tall overlay: the
     * blit fast path can copy stale cells from the overlay frame into rows
     * the shrunken layout no longer reaches, leaving a ghost title/divider.
     * onRender resets the flag at frame end so it's one-shot.
     */
    invalidatePrevFrame(): void;
    /**
     * Called by the <AlternateScreen> component on mount/unmount.
     * Controls cursor.y clamping in the renderer and gates alt-screen-aware
     * behavior in SIGCONT/resize/unmount handlers. Repaints on change so
     * the first alt-screen frame (and first main-screen frame on exit) is
     * a full redraw with no stale diff state.
     */
    setAltScreenActive(active: boolean, mouseTracking?: boolean): void;
    get isAltScreenActive(): boolean;
    /**
     * Re-assert terminal modes after a gap (>5s stdin silence or event-loop
     * stall). Catches tmux detach→attach, ssh reconnect, and laptop
     * sleep/wake — none of which send SIGCONT. The terminal may reset DEC
     * private modes on reconnect; this method restores them.
     *
     * Always re-asserts extended key reporting and mouse tracking. Mouse
     * tracking is idempotent (DEC private mode set-when-set is a no-op). The
     * Kitty keyboard protocol is NOT — CSI >1u is a stack push, so we pop
     * first to keep depth balanced (pop on empty stack is a no-op per spec,
     * so after a terminal reset this still restores depth 0→1). Without the
     * pop, each >5s idle gap adds a stack entry, and the single pop on exit
     * or suspend can't drain them — the shell is left in CSI u mode where
     * Ctrl+C/Ctrl+D leak as escape sequences. The alt-screen
     * re-entry (ERASE_SCREEN + frame reset) is NOT idempotent — it blanks the
     * screen — so it's opt-in via includeAltScreen. The stdin-gap caller fires
     * on ordinary >5s idle + keypress and must not erase; the event-loop stall
     * detector fires on genuine sleep/wake and opts in. tmux attach / ssh
     * reconnect typically send a resize, which already covers alt-screen via
     * handleResize.
     */
    reassertTerminalModes: (includeAltScreen?: boolean) => void;
    /**
     * Mark this instance as unmounted so future unmount() calls early-return.
     * Called by gracefulShutdown's cleanupTerminalModes() after it has sent
     * EXIT_ALT_SCREEN but before the remaining terminal-reset sequences.
     * Without this, signal-exit's deferred ink.unmount() (triggered by
     * process.exit()) runs the full unmount path: onRender() + writeSync
     * cleanup block + updateContainerSync → AlternateScreen unmount cleanup.
     * The result is 2-3 redundant EXIT_ALT_SCREEN sequences landing on the
     * main screen AFTER printResumeHint(), which tmux (at least) interprets
     * as restoring the saved cursor position — clobbering the resume hint.
     */
    detachForShutdown(): void;
    /** @see drainStdin */
    drainStdin(): void;
    /**
     * Re-enter alt-screen, clear, home, re-enable mouse tracking, and reset
     * frame buffers so the next render repaints from scratch. Self-heal for
     * SIGCONT, resize, and stdin-gap/event-loop-stall (sleep/wake) — any of
     * which can leave the terminal in main-screen mode while altScreenActive
     * stays true. ENTER_ALT_SCREEN is a terminal-side no-op if already in alt.
     */
    private reenterAltScreen;
    /**
     * Seed prev/back frames with full-size BLANK screens (rows×cols of empty
     * cells, not 0×0). In alt-screen mode, next.screen.height is always
     * terminalRows; if prev.screen.height is 0 (emptyFrame's default),
     * log-update sees heightDelta > 0 ('growing') and calls renderFrameSlice,
     * whose trailing per-row CR+LF at the last row scrolls the alt screen,
     * permanently desyncing the virtual and physical cursors by 1 row.
     *
     * With a rows×cols blank prev, heightDelta === 0 → standard diffEach
     * → moveCursorTo (CSI cursorMove, no LF, no scroll).
     *
     * viewport.height = rows + 1 matches the renderer's alt-screen output,
     * preventing a spurious resize trigger on the first frame. cursor.y = 0
     * matches the physical cursor after ENTER_ALT_SCREEN + CSI H (home).
     */
    private resetFramesForAltScreen;
    /**
     * Copy the current selection to the clipboard without clearing the
     * highlight. Matches iTerm2's copy-on-select behavior where the selected
     * region stays visible after the automatic copy.
     */
    copySelectionNoClear(): string;
    /**
     * Copy the current text selection to the system clipboard via OSC 52
     * and clear the selection. Returns the copied text (empty if no selection).
     */
    copySelection(): string;
    /** Clear the current text selection without copying. */
    clearTextSelection(): void;
    /**
     * Set the search highlight query. Non-empty → all visible occurrences
     * are inverted (SGR 7) on the next frame; first one also underlined.
     * Empty → clears (prevFrameContaminated handles the frame after). Same
     * damage-tracking machinery as selection — setCellStyleId doesn't track
     * damage, so the overlay forces full-frame damage while active.
     */
    setSearchHighlight(query: string): void;
    /** Paint an EXISTING DOM subtree to a fresh Screen at its natural
     *  height, scan for query. Returns positions relative to the element's
     *  bounding box (row 0 = element top).
     *
     *  The element comes from the MAIN tree — built with all real
     *  providers, yoga already computed. We paint it to a fresh buffer
     *  with offsets so it lands at (0,0). Same paint path as the main
     *  render. Zero drift. No second React root, no context bridge.
     *
     *  ~1-2ms (paint only, no reconcile — the DOM is already built). */
    scanElementSubtree(el: DOMElement): MatchPosition[];
    /** Set the position-based highlight state. Every frame, writes CURRENT
     *  style at positions[currentIdx] + rowOffset. null clears. The scan-
     *  highlight (inverse on all matches) still runs — this overlays yellow
     *  on top. rowOffset changes as the user scrolls (= message's current
     *  screen-top); positions stay stable (message-relative). */
    setSearchPositions(state: {
        positions: MatchPosition[];
        rowOffset: number;
        currentIdx: number;
    } | null): void;
    /**
     * Set the selection highlight background color. Replaces the per-cell
     * SGR-7 inverse with a solid theme-aware bg (matches native terminal
     * selection). Accepts the same color formats as Text backgroundColor
     * (rgb(), ansi:name, #hex, ansi256()) — colorize() routes through
     * chalk so the tmux/xterm.js level clamps in colorize.ts apply and
     * the emitted SGR is correct for the current terminal.
     *
     * Called by React-land once theme is known (ScrollKeybindingHandler's
     * useEffect watching useTheme). Before that call, withSelectionBg
     * falls back to withInverse so selection still renders on the first
     * frame; the effect fires before any mouse input so the fallback is
     * unobservable in practice.
     */
    setSelectionBgColor(color: string): void;
    /**
     * Capture text from rows about to scroll out of the viewport during
     * drag-to-scroll. Must be called BEFORE the ScrollBox scrolls so the
     * screen buffer still holds the outgoing content. Accumulated into
     * the selection state and joined back in by getSelectedText.
     */
    captureScrolledRows(firstRow: number, lastRow: number, side: 'above' | 'below'): void;
    /**
     * Shift anchor AND focus by dRow, clamped to [minRow, maxRow]. Used by
     * keyboard scroll handlers (PgUp/PgDn etc.) so the highlight tracks the
     * content instead of disappearing. Unlike shiftAnchor (drag-to-scroll),
     * this moves BOTH endpoints — the user isn't holding the mouse at one
     * edge. Supplies screen.width for the col-reset-on-clamp boundary.
     */
    shiftSelectionForScroll(dRow: number, minRow: number, maxRow: number): void;
    /**
     * Keyboard selection extension (shift+arrow/home/end). Moves focus;
     * anchor stays fixed so the highlight grows or shrinks relative to it.
     * Left/right wrap across row boundaries — native macOS text-edit
     * behavior: shift+left at col 0 wraps to end of the previous row.
     * Up/down clamp at viewport edges (no scroll-to-extend yet). Drops to
     * char mode. No-op outside alt-screen or without an active selection.
     */
    moveSelectionFocus(move: FocusMove): void;
    /** Whether there is an active text selection. */
    hasTextSelection(): boolean;
    /**
     * Subscribe to selection state changes. Fires whenever the selection
     * is started, updated, cleared, or copied. Returns an unsubscribe fn.
     */
    subscribeToSelectionChange(cb: () => void): () => void;
    private notifySelectionChange;
    /**
     * Hit-test the rendered DOM tree at (col, row) and bubble a ClickEvent
     * from the deepest hit node up through ancestors with onClick handlers.
     * Returns true if a DOM handler consumed the click. Gated on
     * altScreenActive — clicks only make sense with a fixed viewport where
     * nodeCache rects map 1:1 to terminal cells (no scrollback offset).
     */
    dispatchClick(col: number, row: number): boolean;
    dispatchHover(col: number, row: number): void;
    dispatchKeyboardEvent(parsedKey: ParsedKey): void;
    /**
     * Look up the URL at (col, row) in the current front frame. Checks for
     * an OSC 8 hyperlink first, then falls back to scanning the row for a
     * plain-text URL (mouse tracking intercepts the terminal's native
     * Cmd+Click URL detection, so we replicate it). This is a pure lookup
     * with no side effects — call it synchronously at click time so the
     * result reflects the screen the user actually clicked on, then defer
     * the browser-open action via a timer.
     */
    getHyperlinkAt(col: number, row: number): string | undefined;
    /**
     * Optional callback fired when clicking an OSC 8 hyperlink in fullscreen
     * mode. Set by FullscreenLayout via useLayoutEffect.
     */
    onHyperlinkClick: ((url: string) => void) | undefined;
    /**
     * Stable prototype wrapper for onHyperlinkClick. Passed to <App> as
     * onOpenHyperlink so the prop is a bound method (autoBind'd) that reads
     * the mutable field at call time — not the undefined-at-render value.
     */
    openHyperlink(url: string): void;
    /**
     * Handle a double- or triple-click at (col, row): select the word or
     * line under the cursor by reading the current screen buffer. Called on
     * PRESS (not release) so the highlight appears immediately and drag can
     * extend the selection word-by-word / line-by-line. Falls back to
     * char-mode startSelection if the click lands on a noSelect cell.
     */
    handleMultiClick(col: number, row: number, count: 2 | 3): void;
    /**
     * Handle a drag-motion at (col, row). In char mode updates focus to the
     * exact cell. In word/line mode snaps to word/line boundaries so the
     * selection extends by word/line like native macOS. Gated on
     * altScreenActive for the same reason as dispatchClick.
     */
    handleSelectionDrag(col: number, row: number): void;
    private stdinListeners;
    private wasRawMode;
    suspendStdin(): void;
    resumeStdin(): void;
    private writeRaw;
    private setCursorDeclaration;
    render(node: ReactNode): void;
    unmount(error?: Error | number | null): void;
    waitUntilExit(): Promise<void>;
    resetLineCount(): void;
    /**
     * Replace char/hyperlink pools with fresh instances to prevent unbounded
     * growth during long sessions. Migrates the front frame's screen IDs into
     * the new pools so diffing remains correct. The back frame doesn't need
     * migration — resetScreen zeros it before any reads.
     *
     * Call between conversation turns or periodically.
     */
    resetPools(): void;
    patchConsole(): () => void;
    /**
     * Intercept process.stderr.write so stray writes (config.ts, hooks.ts,
     * third-party deps) don't corrupt the alt-screen buffer. patchConsole only
     * hooks console.* methods — direct stderr writes bypass it, land at the
     * parked cursor, scroll the alt-screen, and desync frontFrame from the
     * physical terminal. Next diff writes only changed-in-React cells at
     * absolute coords → interleaved garbage.
     *
     * Swallows the write (routes text to the debug log) and, in alt-screen,
     * forces a full-damage repaint as a defensive recovery. Not patching
     * process.stdout — Ink itself writes there.
     */
    private patchStderr;
}

type RenderOptions = {
    /**
     * Output stream where app will be rendered.
     *
     * @default process.stdout
     */
    stdout?: NodeJS.WriteStream;
    /**
     * Input stream where app will listen for input.
     *
     * @default process.stdin
     */
    stdin?: NodeJS.ReadStream;
    /**
     * Error stream.
     * @default process.stderr
     */
    stderr?: NodeJS.WriteStream;
    /**
     * Configure whether Ink should listen to Ctrl+C keyboard input and exit the app. This is needed in case `process.stdin` is in raw mode, because then Ctrl+C is ignored by default and process is expected to handle it manually.
     *
     * @default true
     */
    exitOnCtrlC?: boolean;
    /**
     * Patch console methods to ensure console output doesn't mix with Ink output.
     *
     * @default true
     */
    patchConsole?: boolean;
    /**
     * Called after each frame render with timing and flicker information.
     */
    onFrame?: (event: FrameEvent) => void;
    /** Reserved; consumed by select code paths to enable incremental rendering. */
    incrementalRendering?: boolean;
};
type Instance = {
    /**
     * Replace previous root node with a new one or update props of the current root node.
     */
    rerender: Ink['render'];
    /**
     * Manually unmount the whole Ink app.
     */
    unmount: Ink['unmount'];
    /**
     * Returns a promise, which resolves when app is unmounted.
     */
    waitUntilExit: Ink['waitUntilExit'];
    cleanup: () => void;
};
/**
 * A managed Ink root, similar to react-dom's createRoot API.
 * Separates instance creation from rendering so the same root
 * can be reused for multiple sequential screens.
 */
type Root = {
    render: (node: ReactNode) => void;
    unmount: () => void;
    waitUntilExit: () => Promise<void>;
};
/**
 * Mount a component and render the output.
 */
declare const renderSync: (node: ReactNode, options?: NodeJS.WriteStream | RenderOptions) => Instance;
declare const wrappedRender: (node: ReactNode, options?: NodeJS.WriteStream | RenderOptions) => Promise<Instance>;

/**
 * Create an Ink root without rendering anything yet.
 * Like react-dom's createRoot — call root.render() to mount a tree.
 */
declare function createRoot({ stdout, stdin, stderr, exitOnCtrlC, patchConsole, onFrame, }?: RenderOptions): Promise<Root>;

declare function useStdout(): {
    stdout: NodeJS.WriteStream;
    write: (data: string) => void;
};
declare function useTerminalSize(): {
    columns: number;
    rows: number;
};
declare const useAnimation: typeof useAnimationFrame;
type StaticProps<T> = {
    items: readonly T[];
    children: (item: T, index: number) => ReactNode;
    style?: unknown;
};
declare function Static<T>({ items, children }: StaticProps<T>): ReactElement;
type TransformProps = {
    transform: (text: string, index: number) => string;
    children: ReactNode;
};
declare function Transform({ children }: TransformProps): ReactElement;
declare function useBoxMetrics(ref: {
    current: DOMElement | null;
}): {
    width: number;
    height: number;
};

export { AlternateScreen, Ansi, Box, type Props$3 as BoxProps, Button, ClickEvent, type DOMElement, Event, EventEmitter, FocusManager, InputEvent, type Instance, type Key, Link, Newline, NoSelect, RawAnsi, type RenderOptions, type Root, ScrollBox, type ScrollBoxHandle, type ScrollBoxProps, Spacer, Static, TerminalFocusEvent, Text, TextInput, type TextInputProps, type Props$2 as TextProps, Transform, createRoot, measureElement, renderSync as render, wrappedRender as renderAsync, renderSync, stringWidth, useAnimation, useAnimationFrame, useAnimationTimer, useApp, useBoxMetrics, useInput, useInterval, useSelection, useStdin, useStdout, useTabStatus, useTerminalFocus, useTerminalSize, useTerminalTitle, useTerminalViewport, wrapText };
