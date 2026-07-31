"use strict";

const CSSStyleDeclarationImpl = require("./CSSStyleDeclaration-impl.js").implementation;
const propertyDescriptors = require("../../../generated/css-property-descriptors");

class CSSStylePropertiesImpl extends CSSStyleDeclarationImpl {
  get cssFloat() {
    return this.getPropertyValue("float");
  }

  set cssFloat(value) {
    propertyDescriptors.float.set.call(this, value);
  }
}

Object.defineProperties(CSSStylePropertiesImpl.prototype, propertyDescriptors);

exports.implementation = CSSStylePropertiesImpl;
