"use strict";
// deprecated
// @see https://drafts.csswg.org/css-masking-1/#clip-property

const parsers = require("../helpers/css-values");
const csstree = require("../helpers/patched-csstree");

// Constants
const { AST_TYPES } = parsers;

const property = "clip";

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._setProperty(property, v);
    } else {
      const val = parse(v);
      if (typeof val === "string") {
        const priority = this._priorities.get(property) ?? "";
        this._setProperty(property, val, priority);
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
 * Parses the clip property value.
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
    const [{ name, type, value: itemValue }] = value;
    switch (type) {
      case AST_TYPES.FUNCTION: {
        const values = parsers.splitValue(itemValue, {
          delimiter: ","
        });
        const parsedValues = [];
        for (const item of values) {
          const parsedValue = csstree.parse(item, { context: "value" });
          const val = parsers.resolveNumericValue(parsedValue.children, {
            type: "length"
          });
          if (val) {
            parsedValues.push(val);
          } else {
            return undefined;
          }
        }
        return `${name}(${parsedValues.join(", ")})`;
      }
      case AST_TYPES.GLOBAL_KEYWORD:
      case AST_TYPES.IDENTIFIER: {
        return name;
      }
      default:
    }
  } else if (typeof value === "string") {
    return value;
  }
  return undefined;
}

module.exports = {
  descriptor,
  parse,
  property
};
