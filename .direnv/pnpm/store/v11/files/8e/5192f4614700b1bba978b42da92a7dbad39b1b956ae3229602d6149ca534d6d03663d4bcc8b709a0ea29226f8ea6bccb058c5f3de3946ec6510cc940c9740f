"use strict";

const parsers = require("../helpers/css-values");
const borderBottomWidth = require("./borderBottomWidth");
const borderBottomStyle = require("./borderBottomStyle");
const borderBottomColor = require("./borderBottomColor");

const property = "border-bottom";
const shorthand = "border";

const subProps = {
  width: borderBottomWidth.property,
  style: borderBottomStyle.property,
  color: borderBottomColor.property
};

const initialValues = new Map([
  [borderBottomWidth.property, "medium"],
  [borderBottomStyle.property, "none"],
  [borderBottomColor.property, "currentcolor"]
]);

const shorthandFor = new Map([
  [borderBottomWidth.property, borderBottomWidth],
  [borderBottomStyle.property, borderBottomStyle],
  [borderBottomColor.property, borderBottomColor]
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
 * Parses the border-bottom property value.
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
      [borderBottomWidth.property]: "medium"
    };
    for (const key of keys) {
      if (parsedValues.has(key)) {
        const parsedValue = parsedValues.get(key);
        if (parsedValue !== initialValues.get(key)) {
          obj[key] = parsedValues.get(key);
          if (obj[borderBottomWidth.property] && obj[borderBottomWidth.property] === "medium") {
            delete obj[borderBottomWidth.property];
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
