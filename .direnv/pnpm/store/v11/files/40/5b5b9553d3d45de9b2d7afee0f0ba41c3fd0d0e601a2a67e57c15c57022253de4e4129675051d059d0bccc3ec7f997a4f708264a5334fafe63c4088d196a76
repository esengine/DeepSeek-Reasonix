/**
 * @typedef {import('unist').Node} Node
 */

/**
 * @typedef Options
 *   Configuration.
 * @property {boolean | null | undefined} [force=false]
 *   Whether to use `delete` to remove `position` fields.
 *
 *   The default is to set them to `undefined`.
 */

import {visit} from 'unist-util-visit'

/**
 * Remove the `position` field from a tree.
 *
 * @param {Node} tree
 *   Tree to clean.
 * @param {Options | null | undefined} [options={force: false}]
 *   Configuration (default: `{force: false}`).
 * @returns {undefined}
 *   Nothing.
 */
export function removePosition(tree, options) {
  const config = options || {}
  const force = config.force || false

  visit(tree, remove)

  /**
   * @param {Node} node
   */
  function remove(node) {
    if (force) {
      delete node.position
    } else {
      node.position = undefined
    }
  }
}
