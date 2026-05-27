import { LayoutEdge, type LayoutNode } from './layout/node.js';

const getMaxWidth = (yogaNode: LayoutNode): number =>
  yogaNode.getComputedWidth() -
  yogaNode.getComputedPadding(LayoutEdge.Left) -
  yogaNode.getComputedPadding(LayoutEdge.Right) -
  yogaNode.getComputedBorder(LayoutEdge.Left) -
  yogaNode.getComputedBorder(LayoutEdge.Right);

export default getMaxWidth;
