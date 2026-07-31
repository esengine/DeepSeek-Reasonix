"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const csstree = require("./helpers/patched-csstree.js");

class CSSNamespaceRuleImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.namespaceURI = privateData.namespaceURI;
    this.prefix = privateData.prefix;
  }

  get type() {
    return 10; // NAMESPACE_RULE
  }

  get cssText() {
    // https://drafts.csswg.org/cssom/#serialize-a-url
    // https://github.com/csstree/csstree/issues/360
    const url = `url(${csstree.string.encode(this.namespaceURI)})`;
    if (this.prefix) {
      return `@namespace ${this.prefix} ${url};`;
    }
    return `@namespace ${url};`;
  }
}

exports.implementation = CSSNamespaceRuleImpl;
