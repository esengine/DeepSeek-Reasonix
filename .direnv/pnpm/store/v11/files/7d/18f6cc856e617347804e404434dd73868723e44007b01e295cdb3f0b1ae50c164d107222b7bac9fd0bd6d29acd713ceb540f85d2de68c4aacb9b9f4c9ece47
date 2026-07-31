/**
 * @import {
 *   Comment as HastComment,
 *   Doctype as HastDoctype,
 *   Element as HastElement,
 *   Nodes as HastNodes,
 *   RootContent as HastRootContent,
 *   Root as HastRoot,
 *   Text as HastText,
 * } from 'hast'
 */

/**
 * @callback AfterTransform
 *   Callback called when each node is transformed.
 * @param {Node} domNode
 *   DOM node that was handled.
 * @param {HastNodes} hastNode
 *   Corresponding hast node.
 * @returns {undefined | void}
 *   Nothing.
 *
 *   Note: `void` included until TS infers `undefined` nicely.
 *
 * @typedef Options
 *   Configuration.
 * @property {AfterTransform | null | undefined} [afterTransform]
 *   Callback called when each node is transformed (optional).
 */

import {h, s} from 'hastscript'
import {webNamespaces} from 'web-namespaces'

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
export function fromDom(tree, options) {
  return transform(tree, options || {}) || {type: 'root', children: []}
}

/**
 * @param {Node} node
 *   DOM node to transform.
 * @param {Options} options
 *   Configuration.
 * @returns {HastNodes | undefined}
 *   Equivalent hast node.
 *
 *   Note that certain legacy DOM nodes (i.e., Attr nodes (2),  CDATA, processing instructions)
 */
function transform(node, options) {
  const transformed = one(node, options)
  if (transformed && options.afterTransform)
    options.afterTransform(node, transformed)
  return transformed
}

/**
 * @param {Node} node
 *   DOM node to transform.
 * @param {Options} options
 *   Configuration.
 * @returns {HastNodes | undefined}
 *   Equivalent hast node.
 */
function one(node, options) {
  switch (node.nodeType) {
    case 1 /* Element */: {
      const domNode = /** @type {Element} */ (node)
      return element(domNode, options)
    }

    // Ignore: Attr (2).

    case 3 /* Text */: {
      const domNode = /** @type {Text} */ (node)
      return text(domNode)
    }

    // Ignore: CDATA (4).
    // Removed: Entity reference (5)
    // Removed: Entity (6)
    // Ignore: Processing instruction (7).

    case 8 /* Comment */: {
      const domNode = /** @type {Comment} */ (node)
      return comment(domNode)
    }

    case 9 /* Document */: {
      const domNode = /** @type {Document} */ (node)
      return root(domNode, options)
    }

    case 10 /* Document type */: {
      return doctype()
    }

    case 11 /* Document fragment */: {
      const domNode = /** @type {DocumentFragment} */ (node)
      return root(domNode, options)
    }

    default: {
      return undefined
    }
  }
}

/**
 * Transform a document.
 *
 * @param {Document | DocumentFragment} node
 *   DOM node to transform.
 * @param {Options} options
 *   Configuration.
 * @returns {HastRoot}
 *   Equivalent hast node.
 */
function root(node, options) {
  return {type: 'root', children: all(node, options)}
}

/**
 * Transform a doctype.
 *
 * @returns {HastDoctype}
 *   Equivalent hast node.
 */
function doctype() {
  return {type: 'doctype'}
}

/**
 * Transform a text.
 *
 * @param {Text} node
 *   DOM node to transform.
 * @returns {HastText}
 *   Equivalent hast node.
 */
function text(node) {
  return {type: 'text', value: node.nodeValue || ''}
}

/**
 * Transform a comment.
 *
 * @param {Comment} node
 *   DOM node to transform.
 * @returns {HastComment}
 *   Equivalent hast node.
 */
function comment(node) {
  return {type: 'comment', value: node.nodeValue || ''}
}

/**
 * Transform an element.
 *
 * @param {Element} node
 *   DOM node to transform.
 * @param {Options} options
 *   Configuration.
 * @returns {HastElement}
 *   Equivalent hast node.
 */
function element(node, options) {
  const space = node.namespaceURI
  const x = space === webNamespaces.svg ? s : h
  const tagName =
    space === webNamespaces.html ? node.tagName.toLowerCase() : node.tagName
  /** @type {DocumentFragment | Element} */
  const content =
    // @ts-expect-error: DOM types are wrong, content can exist.
    space === webNamespaces.html && tagName === 'template' ? node.content : node
  const attributes = node.getAttributeNames()
  /** @type {Record<string, string>} */
  const properties = {}
  let index = -1

  while (++index < attributes.length) {
    properties[attributes[index]] = node.getAttribute(attributes[index]) || ''
  }

  return x(tagName, properties, all(content, options))
}

/**
 * Transform child nodes in a parent.
 *
 * @param {Document | DocumentFragment | Element} node
 *   DOM node to transform.
 * @param {Options} options
 *   Configuration.
 * @returns {Array<HastRootContent>}
 *   Equivalent hast nodes.
 */
function all(node, options) {
  const nodes = node.childNodes
  /** @type {Array<HastRootContent>} */
  const children = []
  let index = -1

  while (++index < nodes.length) {
    const child = transform(nodes[index], options)

    if (child !== undefined) {
      // @ts-expect-error Assume no document inside document.
      children.push(child)
    }
  }

  return children
}
