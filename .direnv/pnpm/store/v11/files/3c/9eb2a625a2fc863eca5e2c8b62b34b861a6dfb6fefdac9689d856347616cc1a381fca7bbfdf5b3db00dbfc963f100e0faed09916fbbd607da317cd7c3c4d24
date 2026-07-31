"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const CSSStyleDeclaration = require("../../../generated/idl/CSSStyleDeclaration.js");

class CSSFontFaceRuleImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);

    this.style = CSSStyleDeclaration.createImpl(globalObject, [], {
      parentRule: this
    });
  }

  get type() {
    return 5; // FONT_FACE_RULE
  }

  get cssText() {
    return `@font-face { ${this.style.cssText} }`;
  }
}

exports.implementation = CSSFontFaceRuleImpl;
