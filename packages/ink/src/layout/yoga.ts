import Yoga, {
  Align,
  Direction,
  Display,
  Edge,
  FlexDirection,
  Gutter,
  Justify,
  MeasureMode,
  Overflow,
  PositionType,
  Wrap,
  type Node as YogaNode,
} from '../_internal/yoga-layout.js';
import {
  type LayoutAlign,
  LayoutDisplay,
  type LayoutEdge,
  type LayoutFlexDirection,
  type LayoutGutter,
  type LayoutJustify,
  type LayoutMeasureFunc,
  LayoutMeasureMode,
  type LayoutNode,
  type LayoutOverflow,
  type LayoutPositionType,
  type LayoutWrap,
} from './node.js';

// String-union → Yoga-enum lookup tables.
//
// Built once at module load and reused on every style write. A `Record`
// lookup is both faster and easier to audit than a switch, and keeps the
// mapping in one visual block — so when Yoga renames an enum value the
// damage stays contained to this file.

const EDGE_MAP: Record<LayoutEdge, Edge> = {
  all: Edge.All,
  horizontal: Edge.Horizontal,
  vertical: Edge.Vertical,
  left: Edge.Left,
  right: Edge.Right,
  top: Edge.Top,
  bottom: Edge.Bottom,
  start: Edge.Start,
  end: Edge.End,
};

const GUTTER_MAP: Record<LayoutGutter, Gutter> = {
  all: Gutter.All,
  column: Gutter.Column,
  row: Gutter.Row,
};

const FLEX_DIRECTION_MAP: Record<LayoutFlexDirection, FlexDirection> = {
  row: FlexDirection.Row,
  'row-reverse': FlexDirection.RowReverse,
  column: FlexDirection.Column,
  'column-reverse': FlexDirection.ColumnReverse,
};

const WRAP_MAP: Record<LayoutWrap, Wrap> = {
  nowrap: Wrap.NoWrap,
  wrap: Wrap.Wrap,
  'wrap-reverse': Wrap.WrapReverse,
};

const ALIGN_MAP: Record<LayoutAlign, Align> = {
  auto: Align.Auto,
  stretch: Align.Stretch,
  'flex-start': Align.FlexStart,
  center: Align.Center,
  'flex-end': Align.FlexEnd,
};

const JUSTIFY_MAP: Record<LayoutJustify, Justify> = {
  'flex-start': Justify.FlexStart,
  center: Justify.Center,
  'flex-end': Justify.FlexEnd,
  'space-between': Justify.SpaceBetween,
  'space-around': Justify.SpaceAround,
  'space-evenly': Justify.SpaceEvenly,
};

const OVERFLOW_MAP: Record<LayoutOverflow, Overflow> = {
  visible: Overflow.Visible,
  hidden: Overflow.Hidden,
  scroll: Overflow.Scroll,
};

// Concrete `LayoutNode` over a Yoga node.
//
// Each method is a thin pass-through; the value of the adapter is the
// shape it enforces. If Yoga shifts an enum or renames a method, only the
// translations above and the wrappers below need to be updated — the
// renderer keeps talking to `LayoutNode`.
export class YogaLayoutNode implements LayoutNode {
  readonly yoga: YogaNode;

  constructor(yoga: YogaNode) {
    this.yoga = yoga;
  }

  // Tree

  insertChild(child: LayoutNode, index: number): void {
    this.yoga.insertChild((child as YogaLayoutNode).yoga, index);
  }

  removeChild(child: LayoutNode): void {
    this.yoga.removeChild((child as YogaLayoutNode).yoga);
  }

  getChildCount(): number {
    return this.yoga.getChildCount();
  }

  getParent(): LayoutNode | null {
    const parent = this.yoga.getParent();
    return parent ? new YogaLayoutNode(parent) : null;
  }

  // Layout

  calculateLayout(width?: number, _height?: number): void {
    // Height is intentionally left undefined: the terminal scrolls
    // vertically, so we measure-to-content along that axis. Direction is
    // always LTR — bidi reordering happens later in the text pipeline,
    // not at the flex level.
    this.yoga.calculateLayout(width, undefined, Direction.LTR);
  }

  setMeasureFunc(fn: LayoutMeasureFunc): void {
    this.yoga.setMeasureFunc((w, wMode) => {
      // Translate Yoga's numeric measure mode back to our string union
      // before handing it to the renderer-supplied callback.
      const mode =
        wMode === MeasureMode.Exactly
          ? LayoutMeasureMode.Exactly
          : wMode === MeasureMode.AtMost
            ? LayoutMeasureMode.AtMost
            : LayoutMeasureMode.Undefined;
      return fn(w, mode);
    });
  }

  unsetMeasureFunc(): void {
    this.yoga.unsetMeasureFunc();
  }

  markDirty(): void {
    this.yoga.markDirty();
  }

  // Computed layout

  getComputedLeft(): number {
    return this.yoga.getComputedLeft();
  }

  getComputedTop(): number {
    return this.yoga.getComputedTop();
  }

  getComputedWidth(): number {
    return this.yoga.getComputedWidth();
  }

  getComputedHeight(): number {
    return this.yoga.getComputedHeight();
  }

  getComputedBorder(edge: LayoutEdge): number {
    return this.yoga.getComputedBorder(EDGE_MAP[edge]!);
  }

  getComputedPadding(edge: LayoutEdge): number {
    return this.yoga.getComputedPadding(EDGE_MAP[edge]!);
  }

  // Style setters

  setWidth(value: number): void {
    this.yoga.setWidth(value);
  }
  setWidthPercent(value: number): void {
    this.yoga.setWidthPercent(value);
  }
  setWidthAuto(): void {
    this.yoga.setWidthAuto();
  }
  setHeight(value: number): void {
    this.yoga.setHeight(value);
  }
  setHeightPercent(value: number): void {
    this.yoga.setHeightPercent(value);
  }
  setHeightAuto(): void {
    this.yoga.setHeightAuto();
  }
  setMinWidth(value: number): void {
    this.yoga.setMinWidth(value);
  }
  setMinWidthPercent(value: number): void {
    this.yoga.setMinWidthPercent(value);
  }
  setMinHeight(value: number): void {
    this.yoga.setMinHeight(value);
  }
  setMinHeightPercent(value: number): void {
    this.yoga.setMinHeightPercent(value);
  }
  setMaxWidth(value: number): void {
    this.yoga.setMaxWidth(value);
  }
  setMaxWidthPercent(value: number): void {
    this.yoga.setMaxWidthPercent(value);
  }
  setMaxHeight(value: number): void {
    this.yoga.setMaxHeight(value);
  }
  setMaxHeightPercent(value: number): void {
    this.yoga.setMaxHeightPercent(value);
  }

  setFlexDirection(dir: LayoutFlexDirection): void {
    this.yoga.setFlexDirection(FLEX_DIRECTION_MAP[dir]!);
  }

  setFlexGrow(value: number): void {
    this.yoga.setFlexGrow(value);
  }
  setFlexShrink(value: number): void {
    this.yoga.setFlexShrink(value);
  }
  setFlexBasis(value: number): void {
    this.yoga.setFlexBasis(value);
  }
  setFlexBasisPercent(value: number): void {
    this.yoga.setFlexBasisPercent(value);
  }

  setFlexWrap(wrap: LayoutWrap): void {
    this.yoga.setFlexWrap(WRAP_MAP[wrap]!);
  }

  setAlignItems(align: LayoutAlign): void {
    this.yoga.setAlignItems(ALIGN_MAP[align]!);
  }

  setAlignSelf(align: LayoutAlign): void {
    this.yoga.setAlignSelf(ALIGN_MAP[align]!);
  }

  setJustifyContent(justify: LayoutJustify): void {
    this.yoga.setJustifyContent(JUSTIFY_MAP[justify]!);
  }

  setDisplay(display: LayoutDisplay): void {
    this.yoga.setDisplay(display === 'flex' ? Display.Flex : Display.None);
  }

  getDisplay(): LayoutDisplay {
    return this.yoga.getDisplay() === Display.None
      ? LayoutDisplay.None
      : LayoutDisplay.Flex;
  }

  setPositionType(type: LayoutPositionType): void {
    this.yoga.setPositionType(
      type === 'absolute' ? PositionType.Absolute : PositionType.Relative,
    );
  }

  setPosition(edge: LayoutEdge, value: number): void {
    this.yoga.setPosition(EDGE_MAP[edge]!, value);
  }

  setPositionPercent(edge: LayoutEdge, value: number): void {
    this.yoga.setPositionPercent(EDGE_MAP[edge]!, value);
  }

  setOverflow(overflow: LayoutOverflow): void {
    this.yoga.setOverflow(OVERFLOW_MAP[overflow]!);
  }

  setMargin(edge: LayoutEdge, value: number): void {
    this.yoga.setMargin(EDGE_MAP[edge]!, value);
  }
  setPadding(edge: LayoutEdge, value: number): void {
    this.yoga.setPadding(EDGE_MAP[edge]!, value);
  }
  setBorder(edge: LayoutEdge, value: number): void {
    this.yoga.setBorder(EDGE_MAP[edge]!, value);
  }
  setGap(gutter: LayoutGutter, value: number): void {
    this.yoga.setGap(GUTTER_MAP[gutter]!, value);
  }

  // Lifecycle

  free(): void {
    this.yoga.free();
  }
  freeRecursive(): void {
    this.yoga.freeRecursive();
  }
}

// The TypeScript port of yoga-layout is synchronous — no WASM loading, no
// linear-memory growth, no module-init lifecycle. The Yoga instance is just
// a plain JS object at import time, so no preload/swap/reset plumbing is
// needed and the factory can stay a one-liner.
export function createYogaLayoutNode(): LayoutNode {
  return new YogaLayoutNode(Yoga.Node.create());
}
