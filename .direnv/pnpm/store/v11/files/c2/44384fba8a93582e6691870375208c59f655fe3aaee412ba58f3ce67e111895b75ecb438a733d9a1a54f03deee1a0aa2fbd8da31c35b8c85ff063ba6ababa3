"use strict";

const parsers = require("../helpers/css-values");

// Constants
const { AST_TYPES } = parsers;

const property = "font-style";
const shorthand = "font";

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
 * Parses the font-style property value.
 *
 * @param {string} v - The value to parse.
 * @returns {string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const value = parsers.parsePropertyValue(property, v);
  if (Array.isArray(value) && value.length) {
    if (value.length === 1) {
      const [{ name, type }] = value;
      switch (type) {
        case AST_TYPES.GLOBAL_KEYWORD:
        case AST_TYPES.IDENTIFIER: {
          return name;
        }
        default:
      }
    } else if (value.length === 2) {
      const [part1, part2] = value;
      const val1 = part1.type === AST_TYPES.IDENTIFIER && part1.name;
      const val2 = parsers.resolveNumericValue([part2], {
        type: "angle"
      });
      if (val1 && val1 === "oblique" && val2) {
        return `${val1} ${val2}`;
      }
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
