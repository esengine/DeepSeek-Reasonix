"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const CSSStyleSheet = require("../../../generated/idl/CSSStyleSheet.js");
const csstree = require("./helpers/patched-csstree.js");

class CSSImportRuleImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);

    this.href = privateData.href;
    this.layerName = privateData.layerName;
    this.supportsText = privateData.supportsText;

    // Starts empty; populated later by stylesheets.js when the @import URL is fetched.
    this.styleSheet = CSSStyleSheet.createImpl(globalObject, [], {
      ownerRule: this,
      parentStyleSheet: this.parentStyleSheet,
      mediaText: privateData.mediaText,
      constructed: false
    });
  }

  // https://drafts.csswg.org/cssom/#dom-cssimportrule-media
  get media() {
    return this.styleSheet.media;
  }

  get type() {
    return 3; // IMPORT_RULE
  }

  get cssText() {
    // https://drafts.csswg.org/cssom/#serialize-a-url
    // https://github.com/csstree/csstree/issues/360
    let result = `@import url(${csstree.string.encode(this.href)})`;
    if (this.layerName !== null) {
      result += this.layerName ? ` layer(${this.layerName})` : " layer";
    }
    if (this.supportsText !== null) {
      result += ` supports(${this.supportsText})`;
    }
    const { mediaText } = this.media;
    if (mediaText) {
      result += ` ${mediaText}`;
    }
    return result + ";";
  }
}

exports.implementation = CSSImportRuleImpl;
