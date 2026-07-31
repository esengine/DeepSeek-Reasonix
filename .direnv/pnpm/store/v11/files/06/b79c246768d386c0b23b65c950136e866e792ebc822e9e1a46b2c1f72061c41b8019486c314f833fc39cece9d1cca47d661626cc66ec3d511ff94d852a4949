"use strict";

const parsers = require("../helpers/css-values");
const borderTopWidth = require("./borderTopWidth");
const borderTopStyle = require("./borderTopStyle");
const borderTopColor = require("./borderTopColor");

const property = "border-top";
const shorthand = "border";

const subProps = {
  width: borderTopWidth.property,
  style: borderTopStyle.property,
  color: borderTopColor.property
};

const initialValues = new Map([
  [borderTopWidth.property, "medium"],
  [borderTopStyle.property, "none"],
  [borderTopColor.property, "currentcolor"]
]);

const shorthandFor = new Map([
  [borderTopWidth.property, borderTopWidth],
  [borderTopStyle.property, borderTopStyle],
  [borderTopColor.property, borderTopColor]
]);

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._borderSetter(property, v, "");
    } else {
      const val = parse(v);
      if (val || typeof val === "string") {
        const priority =
          !this._priorities.get(shorthand) && this._priorities.has(property) ? this._priorities.get(property) : "";
        this._borderSetter(property, val, priority);
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
 * Parses the border-top property value.
 *
 * @param {string} v - The value to parse.
 * @returns {object|string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const values = parsers.splitValue(v);
  const parsedValues = new Map();
  for (const val of values) {
    const value = parsers.parsePropertyValue(property, val);
    if (Array.isArray(value) && value.length === 1) {
      const parsedValue = parsers.resolveBorderShorthandValue(value, subProps, parsedValues);
      if (typeof parsedValue === "string") {
        return parsedValue;
      } else if (Array.isArray(parsedValue)) {
        const [key, resolvedVal] = parsedValue;
        parsedValues.set(key, resolvedVal);
      } else {
        return undefined;
      }
    } else {
      return undefined;
    }
  }
  if (parsedValues.size) {
    const keys = shorthandFor.keys();
    const obj = {
      [borderTopWidth.property]: "medium"
    };
    for (const key of keys) {
      if (parsedValues.has(key)) {
        const parsedValue = parsedValues.get(key);
        if (parsedValue !== initialValues.get(key)) {
          obj[key] = parsedValues.get(key);
          if (obj[borderTopWidth.property] && obj[borderTopWidth.property] === "medium") {
            delete obj[borderTopWidth.property];
          }
        }
      }
    }
    return obj;
  }
  return undefined;
}

module.exports = {
  descriptor,
  initialValues,
  parse,
  property,
  shorthandFor
};
