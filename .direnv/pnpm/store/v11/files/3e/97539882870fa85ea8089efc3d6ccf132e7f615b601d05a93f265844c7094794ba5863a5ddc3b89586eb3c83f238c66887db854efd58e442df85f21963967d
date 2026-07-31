/**
 * This function performs a [postorder traversal][] of the graph `g` starting
 * at the nodes `vs`. For each node visited, `v`,  the function `callback(v)`
 * is called.
 *
 * [postorder traversal]: https://en.wikipedia.org/wiki/Tree_traversal#Depth-first
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/preorder.png)
 *
 * ```js
 * graphlib.alg.postorder(g, "A");
 * // => One of:
 * // [ "B", "D", "E", C", "A" ]
 * // [ "B", "E", "D", C", "A" ]
 * // [ "D", "E", "C", B", "A" ]
 * // [ "E", "D", "C", B", "A" ]
 * ```
 *
 * @param {Parameters<typeof dfs>[0]} g - The graph to traverse.
 * @param {Parameters<typeof dfs>[1]} vs - Nodes to start the traversal from.
 * @returns {ReturnType<typeof dfs>} The nodes in the order they were visited.
 */
export function postorder(g: Parameters<typeof dfs>[0], vs: Parameters<typeof dfs>[1]): ReturnType<typeof dfs>;
import { dfs } from './dfs.js';
