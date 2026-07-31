"use strict";

const parsers = require("../helpers/css-values");

const property = "padding-left";
const shorthand = "padding";

const position = "left";

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._setProperty(shorthand, "");
      this._setProperty(property, v);
    } else {
      const val = parse(v);
      if (typeof val === "string") {
        const priority =
          !this._priorities.get(shorthand) && this._priorities.has(property) ? this._priorities.get(property) : "";
        this._positionLonghandSetter(property, val, priority, shorthand);
      }
    }
  },
  get() {
    return this.getPropertyValue(property);
  },
  enumerable: true,
  configurable: true
};

/**
 * Parses the padding-left property value.
 *
 * @param {string} v - The value to parse.
 * @returns {string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const value = parsers.parsePropertyValue(property, v);
  if (Array.isArray(value) && value.length === 1) {
    return parsers.resolveNumericValue(value, {
      min: 0,
      type: "length"
    });
  } else if (typeof value === "string") {
    return value;
  }
  return undefined;
}

module.exports = {
  descriptor,
  parse,
  position,
  property
};
