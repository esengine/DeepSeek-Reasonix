"use strict";

const parsers = require("../helpers/css-values");

// Constants
const { AST_TYPES } = parsers;

const property = "font-family";
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
 * Parses the font-family property value.
 *
 * @param {string} v - The value to parse.
 * @returns {string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const values = parsers.splitValue(v, {
    delimiter: ","
  });
  const parsedValues = [];
  for (const val of values) {
    if (!val) {
      return "";
    }
    const value = parsers.parsePropertyValue(property, val, {
      caseSensitive: true
    });
    if (Array.isArray(value) && value.length) {
      if (value.length === 1) {
        const [{ name, type, value: itemValue }] = value;
        switch (type) {
          case AST_TYPES.FUNCTION: {
            parsedValues.push(`${name}(${itemValue})`);
            break;
          }
          case AST_TYPES.GLOBAL_KEYWORD:
          case AST_TYPES.IDENTIFIER: {
            if (name === "undefined") {
              return undefined;
            }
            parsedValues.push(name);
            break;
          }
          case "String": {
            const parsedValue = itemValue.replaceAll("\\", "").replaceAll('"', '\\"');
            parsedValues.push(`"${parsedValue}"`);
            break;
          }
          default: {
            return undefined;
          }
        }
      } else {
        const parts = [];
        for (const item of value) {
          const { name, type } = item;
          if (type !== AST_TYPES.IDENTIFIER) {
            return undefined;
          }
          parts.push(name);
        }
        const parsedValue = parts.join(" ").replaceAll("\\", "").replaceAll('"', '\\"');
        parsedValues.push(`"${parsedValue}"`);
      }
    } else if (typeof value === "string") {
      parsedValues.push(value);
    } else {
      return undefined;
    }
  }
  if (parsedValues.length) {
    return parsedValues.join(", ");
  }
  return undefined;
}

module.exports = {
  descriptor,
  parse,
  property
};
