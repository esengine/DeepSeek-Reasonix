import type { LayoutNode } from './node.js';
import { createYogaLayoutNode } from './yoga.js';

// Public factory for creating layout nodes. Returning the abstract
// `LayoutNode` (not the concrete `YogaLayoutNode`) keeps the rest of the
// renderer agnostic of which backend computed the box model — see the
// adapter rationale at the top of `node.ts`.
export function createLayoutNode(): LayoutNode {
  return createYogaLayoutNode();
}
