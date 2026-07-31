"use strict";

const NODE_TYPE = require("../node-type");
const { domSymbolTree } = require("./internal-constants");

// https://dom.spec.whatwg.org/#concept-node-length
function nodeLength(node) {
  switch (node.nodeType) {
    case NODE_TYPE.DOCUMENT_TYPE_NODE:
      return 0;

    case NODE_TYPE.TEXT_NODE:
    case NODE_TYPE.PROCESSING_INSTRUCTION_NODE:
    case NODE_TYPE.COMMENT_NODE:
      return node.data.length;

    default:
      return domSymbolTree.childrenCount(node);
  }
}

// https://dom.spec.whatwg.org/#concept-tree-root
function nodeRoot(node) {
  if (node._cachedRoot !== null) {
    return node._cachedRoot;
  }

  // First pass: walk upward to find the root. This only advances `root`; intermediate nodes are not cached yet.
  let root = node;
  let parent;
  while ((parent = domSymbolTree.parent(root))) {
    if (parent._cachedRoot !== null) {
      // Short-circuit: an ancestor already has a cached root, so we don't need to walk further — the ancestor's
      // cached root is our root too. We can skip the second pass below because the ancestor's cache was itself set
      // by a previous second pass (or short-circuit), meaning it already points at the tree's actual root.
      const cachedRoot = parent._cachedRoot;
      let current = node;
      while (current !== parent) {
        current._cachedRoot = cachedRoot;
        current = domSymbolTree.parent(current);
      }
      return cachedRoot;
    }
    root = parent;
  }

  // Second pass (path compression): walk the same path again to cache every intermediate node. This makes the first
  // call ~2x more expensive, but subsequent calls from any node on this path become O(1).
  //
  // We only cache when the root is a Document. Cache invalidation is piggybacked on the existing descendants loop in
  // Node-impl.js's _remove(), which is gated on `this.isConnected`. That gate is true exactly when the tree is rooted
  // at a Document (via shadowIncludingRoot). So Document-rooted caches are always properly invalidated on removal.
  //
  // Non-Document roots — standalone elements, DocumentFragments, and even ShadowRoots on disconnected hosts — don't
  // get this invalidation, so caching them would produce stale entries. This is fine in practice: disconnected trees
  // are typically being built up (write-heavy), so caching wouldn't help much anyway. Connected ShadowRoots *could*
  // be cached safely (their host's `isConnected` is true, so removal would invalidate), but distinguishing connected
  // vs. disconnected ShadowRoots here adds complexity for little gain.
  if (root.nodeType === NODE_TYPE.DOCUMENT_NODE) {
    let current = node;
    while (current !== root) {
      current._cachedRoot = root;
      current = domSymbolTree.parent(current);
    }
    root._cachedRoot = root;
  }

  return root;
}

// https://dom.spec.whatwg.org/#concept-tree-inclusive-ancestor
function isInclusiveAncestor(ancestorNode, node) {
  while (node) {
    if (ancestorNode === node) {
      return true;
    }

    node = domSymbolTree.parent(node);
  }

  return false;
}

// https://dom.spec.whatwg.org/#concept-tree-following
function isFollowing(nodeA, nodeB) {
  if (nodeA === nodeB) {
    return false;
  }

  let current = nodeB;
  while (current) {
    if (current === nodeA) {
      return true;
    }

    current = domSymbolTree.following(current);
  }

  return false;
}

module.exports = {
  nodeLength,
  nodeRoot,

  isInclusiveAncestor,
  isFollowing
};
