"use strict";

const SVGNumber = require("../../../generated/idl/SVGNumber");
const SVGRect = require("../../../generated/idl/SVGRect");
const SVGGraphicsElementImpl = require("./SVGGraphicsElement-impl").implementation;
const { domSymbolTree } = require("../helpers/internal-constants");
const { ELEMENT_NODE } = require("../node-type");

class SVGSVGElementImpl extends SVGGraphicsElementImpl {
  createSVGNumber() {
    return SVGNumber.createImpl(this._globalObject, [], {});
  }

  createSVGRect() {
    return SVGRect.createImpl(this._globalObject, [], {});
  }

  // Unlike `Document`'s `getElementById()`, this searches within this element's subtree, so the document-level
  // `ByIdCache` can't be used (it might return an element outside this subtree). A per-`<svg>` cache isn't worthwhile
  // since `SVGSVGElement`'s `getElementById()` is rarely called and `<svg>` subtrees are typically small.
  getElementById(elementId) {
    for (const node of domSymbolTree.treeIterator(this)) {
      if (node.nodeType === ELEMENT_NODE && node.getAttributeNS(null, "id") === elementId) {
        return node;
      }
    }
    return null;
  }

  suspendRedraw() {
    return 1;
  }
  unsuspendRedraw() {}
  unsuspendRedrawAll() {}
  forceRedraw() {}
}

module.exports = {
  implementation: SVGSVGElementImpl
};
