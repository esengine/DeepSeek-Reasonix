"use strict";

const parsers = require("../helpers/css-values");
const flexGrow = require("./flexGrow");
const flexShrink = require("./flexShrink");
const flexBasis = require("./flexBasis");

// Constants
const { AST_TYPES } = parsers;

const property = "flex";

const initialValues = new Map([
  [flexGrow.property, "0"],
  [flexShrink.property, "1"],
  [flexBasis.property, "auto"]
]);

const shorthandFor = new Map([
  [flexGrow.property, flexGrow],
  [flexShrink.property, flexShrink],
  [flexBasis.property, flexBasis]
]);

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      for (const [longhand] of shorthandFor) {
        this._setProperty(longhand, "");
      }
      this._setProperty(property, v);
    } else {
      const val = parse(v);
      const priority = this._priorities.get(property) ?? "";
      if (typeof val === "string") {
        for (const [longhand] of shorthandFor) {
          this._setProperty(longhand, val, priority);
        }
        this._setProperty(property, val, priority);
      } else if (val) {
        const values = [];
        for (const [longhand, value] of Object.entries(val)) {
          values.push(value);
          this._setProperty(longhand, value, priority);
        }
        this._setProperty(property, values.join(" "), priority);
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
 * Parses the flex property value.
 *
 * @param {string} v - The value to parse.
 * @returns {object|string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const value = parsers.parsePropertyValue(property, v);
  if (Array.isArray(value) && value.length) {
    const flex = {
      [flexGrow.property]: "1",
      [flexShrink.property]: "1",
      [flexBasis.property]: "0%"
    };
    if (value.length === 1) {
      const [{ isNumber, name, type, unit, value: itemValue }] = value;
      switch (type) {
        case AST_TYPES.CALC: {
          if (isNumber) {
            flex[flexGrow.property] = `${name}(${itemValue})`;
            return flex;
          }
          flex[flexBasis.property] = `${name}(${itemValue})`;
          return flex;
        }
        case AST_TYPES.DIMENSION: {
          flex[flexBasis.property] = `${itemValue}${unit}`;
          return flex;
        }
        case AST_TYPES.GLOBAL_KEYWORD: {
          return name;
        }
        case AST_TYPES.IDENTIFIER: {
          if (name === "none") {
            return {
              [flexGrow.property]: "0",
              [flexShrink.property]: "0",
              [flexBasis.property]: "auto"
            };
          }
          flex[flexBasis.property] = name;
          return flex;
        }
        case AST_TYPES.NUMBER: {
          flex[flexGrow.property] = itemValue;
          return flex;
        }
        case AST_TYPES.PERCENTAGE: {
          flex[flexBasis.property] = `${itemValue}%`;
          return flex;
        }
        default:
      }
    } else {
      const [val1, val2, val3] = value;
      if (val1.type === AST_TYPES.CALC && val1.isNumber) {
        flex[flexGrow.property] = `${val1.name}(${val1.value})`;
      } else if (val1.type === AST_TYPES.NUMBER) {
        flex[flexGrow.property] = val1.value;
      } else {
        return undefined;
      }
      if (val3) {
        if (val2.type === AST_TYPES.CALC && val2.isNumber) {
          flex[flexShrink.property] = `${val2.name}(${val2.value})`;
        } else if (val2.type === AST_TYPES.NUMBER) {
          flex[flexShrink.property] = val2.value;
        } else {
          return undefined;
        }
        if (val3.type === AST_TYPES.GLOBAL_KEYWORD || val3.type === AST_TYPES.IDENTIFIER) {
          flex[flexBasis.property] = val3.name;
        } else if (val3.type === AST_TYPES.CALC && !val3.isNumber) {
          flex[flexBasis.property] = `${val3.name}(${val3.value})`;
        } else if (val3.type === AST_TYPES.DIMENSION) {
          flex[flexBasis.property] = `${val3.value}${val3.unit}`;
        } else if (val3.type === AST_TYPES.PERCENTAGE) {
          flex[flexBasis.property] = `${val3.value}%`;
        } else {
          return undefined;
        }
      } else {
        switch (val2.type) {
          case AST_TYPES.CALC: {
            if (val2.isNumber) {
              flex[flexShrink.property] = `${val2.name}(${val2.value})`;
            } else {
              flex[flexBasis.property] = `${val2.name}(${val2.value})`;
            }
            break;
          }
          case AST_TYPES.DIMENSION: {
            flex[flexBasis.property] = `${val2.value}${val2.unit}`;
            break;
          }
          case AST_TYPES.NUMBER: {
            flex[flexShrink.property] = val2.value;
            break;
          }
          case AST_TYPES.PERCENTAGE: {
            flex[flexBasis.property] = `${val2.value}%`;
            break;
          }
          case AST_TYPES.IDENTIFIER: {
            flex[flexBasis.property] = val2.name;
            break;
          }
          default: {
            return undefined;
          }
        }
      }
      return flex;
    }
  } else if (typeof value === "string") {
    return value;
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
