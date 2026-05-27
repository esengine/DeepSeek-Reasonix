// Backend-agnostic layout interface.
//
// The renderer talks to this interface, not to Yoga directly, for two
// reasons:
//
//   1. Yoga's TypeScript bindings drift across versions (enum values,
//      method signatures, freeing semantics). Pinning every renderer file
//      to a specific binding would turn every Yoga upgrade into a
//      cross-codebase refactor. The adapter in `yoga.ts` localises that
//      churn.
//
//   2. The flex enums in Yoga are numeric constants; the renderer code
//      reads better when expressed as string unions ('row', 'flex-start',
//      …). Translating once at the adapter boundary keeps the rest of the
//      tree typed in CSS-style vocabulary.
//
// Renaming or extending this interface is cheap — only `yoga.ts` has to
// stay in sync.

export const LayoutEdge = {
  All: 'all',
  Horizontal: 'horizontal',
  Vertical: 'vertical',
  Left: 'left',
  Right: 'right',
  Top: 'top',
  Bottom: 'bottom',
  Start: 'start',
  End: 'end',
} as const;
export type LayoutEdge = (typeof LayoutEdge)[keyof typeof LayoutEdge];

export const LayoutGutter = {
  All: 'all',
  Column: 'column',
  Row: 'row',
} as const;
export type LayoutGutter = (typeof LayoutGutter)[keyof typeof LayoutGutter];

export const LayoutDisplay = {
  Flex: 'flex',
  None: 'none',
} as const;
export type LayoutDisplay = (typeof LayoutDisplay)[keyof typeof LayoutDisplay];

export const LayoutFlexDirection = {
  Row: 'row',
  RowReverse: 'row-reverse',
  Column: 'column',
  ColumnReverse: 'column-reverse',
} as const;
export type LayoutFlexDirection =
  (typeof LayoutFlexDirection)[keyof typeof LayoutFlexDirection];

export const LayoutAlign = {
  Auto: 'auto',
  Stretch: 'stretch',
  FlexStart: 'flex-start',
  Center: 'center',
  FlexEnd: 'flex-end',
} as const;
export type LayoutAlign = (typeof LayoutAlign)[keyof typeof LayoutAlign];

export const LayoutJustify = {
  FlexStart: 'flex-start',
  Center: 'center',
  FlexEnd: 'flex-end',
  SpaceBetween: 'space-between',
  SpaceAround: 'space-around',
  SpaceEvenly: 'space-evenly',
} as const;
export type LayoutJustify = (typeof LayoutJustify)[keyof typeof LayoutJustify];

export const LayoutWrap = {
  NoWrap: 'nowrap',
  Wrap: 'wrap',
  WrapReverse: 'wrap-reverse',
} as const;
export type LayoutWrap = (typeof LayoutWrap)[keyof typeof LayoutWrap];

export const LayoutPositionType = {
  Relative: 'relative',
  Absolute: 'absolute',
} as const;
export type LayoutPositionType =
  (typeof LayoutPositionType)[keyof typeof LayoutPositionType];

export const LayoutOverflow = {
  Visible: 'visible',
  Hidden: 'hidden',
  Scroll: 'scroll',
} as const;
export type LayoutOverflow =
  (typeof LayoutOverflow)[keyof typeof LayoutOverflow];

// Width/height measurement callback invoked by the layout pass for nodes
// whose intrinsic size depends on their content (text, in practice).
// Height is derived from the wrapped width, so only the width axis is
// reported to the caller.
export type LayoutMeasureFunc = (
  width: number,
  widthMode: LayoutMeasureMode,
) => { width: number; height: number };

export const LayoutMeasureMode = {
  Undefined: 'undefined',
  Exactly: 'exactly',
  AtMost: 'at-most',
} as const;
export type LayoutMeasureMode =
  (typeof LayoutMeasureMode)[keyof typeof LayoutMeasureMode];

export type LayoutNode = {
  // Tree mutation
  insertChild(child: LayoutNode, index: number): void;
  removeChild(child: LayoutNode): void;
  getChildCount(): number;
  getParent(): LayoutNode | null;

  // Layout computation. `calculateLayout` is the entry point; the others
  // hook into measurement and invalidation.
  calculateLayout(width?: number, height?: number): void;
  setMeasureFunc(fn: LayoutMeasureFunc): void;
  unsetMeasureFunc(): void;
  markDirty(): void;

  // Computed values — only meaningful after `calculateLayout` has run on
  // the root.
  getComputedLeft(): number;
  getComputedTop(): number;
  getComputedWidth(): number;
  getComputedHeight(): number;
  getComputedBorder(edge: LayoutEdge): number;
  getComputedPadding(edge: LayoutEdge): number;

  // Style setters. Mirrors Yoga's API one-to-one (Auto/Percent variants
  // included) so the adapter remains a thin pass-through and styles.ts
  // stays free of mode-detection branching.
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

  // Lifecycle. Yoga nodes back onto manually-managed memory (originally
  // C++/WASM); the JS port preserves the explicit-free API so this
  // interface stays portable across backends.
  free(): void;
  freeRecursive(): void;
};
