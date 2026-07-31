/**
 * Transform a DOM tree to a hast tree.
 *
 * @param {Node} tree
 *   DOM tree to transform.
 * @param {Options | null | undefined} [options]
 *   Configuration (optional).
 * @returns {HastNodes}
 *   Equivalent hast node.
 */
export function fromDom(tree: Node, options?: Options | null | undefined): HastNodes;
/**
 * Callback called when each node is transformed.
 */
export type AfterTransform = (domNode: Node, hastNode: HastNodes) => undefined | void;
/**
 * Configuration.
 */
export type Options = {
    /**
     * Callback called when each node is transformed (optional).
     */
    afterTransform?: AfterTransform | null | undefined;
};
import type { Nodes as HastNodes } from 'hast';
//# sourceMappingURL=index.d.ts.map