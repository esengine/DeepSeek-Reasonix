"use strict";

const NODE_TYPE = require("../node-type");
const { domSymbolTree } = require("./internal-constants");

// For use in implementing getElementById(). Notably it ensures that when you get the result, you will always get the
// first in tree order, not the most- or least-recently-inserted. Somewhat modeled after
// https://source.chromium.org/chromium/chromium/src/+/main:third_party/blink/renderer/core/dom/tree_ordered_map.h.

module.exports = class ByIdCache {
  constructor(root) {
    this._root = root;

    // Keys are IDs (strings).
    // Values are `{ element, count }` tuples, where `element` can be `null` indicating we need to recompute.
    // `count` tracks the count of times `add()` was called for this ID. We need to track it because it lets us
    // determine what to do when calling `delete()`: a count of 0 means we can remove the entry, whereas any other
    // count means we need to set `element` to `null` for future recomputation.
    this._map = new Map();
  }

  add(id, element) {
    const value = this._map.get(id);
    if (!value) {
      this._map.set(id, { element, count: 1 });
    } else {
      value.element = null;
      ++value.count;
    }
  }

  delete(id, element) {
    const value = this._map.get(id);
    if (!value) {
      return;
    }

    --value.count;
    if (value.count === 0) {
      this._map.delete(id);
    } else if (value.element === element) {
      value.element = null;
    }
  }

  get(id) {
    const value = this._map.get(id);
    if (!value) {
      return null;
    }

    if (value.element) {
      return value.element;
    }

    for (const descendant of domSymbolTree.treeIterator(this._root)) {
      if (descendant.nodeType === NODE_TYPE.ELEMENT_NODE && descendant.getAttributeNS(null, "id") === id) {
        value.element = descendant;
        return descendant;
      }
    }

    // If we didn't find any elements for this key, we can remove it.
    this._map.delete(id);
    return null;
  }
};
