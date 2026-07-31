import { dfs } from './dfs.js';

export { preorder };

/**
 * This function performs a [preorder traversal][] of the graph `g` starting
 * at the nodes `vs`. For each node visited, `v`,  the function `callback(v)`
 * is called.
 *
 * [preorder traversal]: https://en.wikipedia.org/wiki/Tree_traversal#Depth-first
 *
 * @example
 *
 * ![](https://github.com/dagrejs/graphlib/wiki/images/preorder.png)
 * <!-- SOURCE:
 * http://dagrejs.github.io/project/dagre-d3/latest/demo/interactive-demo.html?graph=digraph%20%7B%0Anode%20%5Bshape%3Dcircle%2C%20style%3D%22fill%3Awhite%3Bstroke%3A%23333%3Bstroke-width%3A1.5px%22%5D%0Aedge%20%5Blabeloffset%3D2%20labelpos%3Dr%5D%0Arankdir%3Dlr%0A%20%20A%20-%3E%20B%0A%20%20A%20-%3E%20C%0A%20%20C%20-%3E%20D%0A%20%20C%20-%3E%20E%0A%7D
 * -->
 *
 * ```js
 * graphlib.alg.preorder(g, "A");
 * // => One of:
 * // [ "A", "B", "C", "D", "E" ]
 * // [ "A", "B", "C", "E", "D" ]
 * // [ "A", "C", "D", "E", "B" ]
 * // [ "A", "C", "E", "D", "B" ]
 * ```
 *
 * @param {Parameters<typeof dfs>[0]} g - The graph to traverse.
 * @param {Parameters<typeof dfs>[1]} vs - Nodes to start the traversal from.
 * @returns {ReturnType<typeof dfs>} The nodes in the order they were visited.
 */
function preorder(g, vs) {
  return dfs(g, vs, 'pre');
}
