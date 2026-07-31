"use strict";

const parsers = require("../helpers/css-values");

// Constants
const { AST_TYPES } = parsers;

const property = "display";

/* keywords */
const displayOutside = ["block", "inline", "run-in"];
const displayFlow = ["flow", "flow-root"];

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
 * Parses the display property value.
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
    switch (value.length) {
      case 1: {
        const [{ name, type }] = value;
        switch (type) {
          case AST_TYPES.GLOBAL_KEYWORD: {
            return name;
          }
          case AST_TYPES.IDENTIFIER: {
            if (name === "flow") {
              return "block";
            }
            return name;
          }
          default:
        }
        break;
      }
      case 2: {
        const [part1, part2] = value;
        const val1 = part1.type === AST_TYPES.IDENTIFIER && part1.name;
        const val2 = part2.type === AST_TYPES.IDENTIFIER && part2.name;
        if (val1 && val2) {
          let outerValue = "";
          let innerValue = "";
          if (val1 === "list-item") {
            outerValue = val2;
            innerValue = val1;
          } else if (val2 === "list-item") {
            outerValue = val1;
            innerValue = val2;
          } else if (displayOutside.includes(val1)) {
            outerValue = val1;
            innerValue = val2;
          } else if (displayOutside.includes(val2)) {
            outerValue = val2;
            innerValue = val1;
          }
          if (innerValue === "list-item") {
            switch (outerValue) {
              case "block":
              case "flow": {
                return innerValue;
              }
              case "flow-root":
              case "inline":
              case "run-in": {
                return `${outerValue} ${innerValue}`;
              }
              default:
            }
          } else if (outerValue === "block") {
            switch (innerValue) {
              case "flow": {
                return outerValue;
              }
              case "flow-root":
              case "flex":
              case "grid":
              case "table": {
                return innerValue;
              }
              case "ruby": {
                return `${outerValue} ${innerValue}`;
              }
              default:
            }
          } else if (outerValue === "inline") {
            switch (innerValue) {
              case "flow": {
                return outerValue;
              }
              case "flow-root": {
                return `${outerValue}-block`;
              }
              case "flex":
              case "grid":
              case "table": {
                return `${outerValue}-${innerValue}`;
              }
              case "ruby": {
                return innerValue;
              }
              default:
            }
          } else if (outerValue === "run-in") {
            switch (innerValue) {
              case "flow": {
                return outerValue;
              }
              case "flow-root":
              case "flex":
              case "grid":
              case "table":
              case "ruby": {
                return `${outerValue} ${innerValue}`;
              }
              default:
            }
          }
        }
        break;
      }
      case 3: {
        const [part1, part2, part3] = value;
        const val1 = part1.type === AST_TYPES.IDENTIFIER && part1.name;
        const val2 = part2.type === AST_TYPES.IDENTIFIER && part2.name;
        const val3 = part3.type === AST_TYPES.IDENTIFIER && part3.name;
        if (val1 && val2 && part3) {
          let outerValue = "";
          let flowValue = "";
          let listItemValue = "";
          if (val1 === "list-item") {
            listItemValue = val1;
            if (displayFlow.includes(val2)) {
              flowValue = val2;
              outerValue = val3;
            } else if (displayFlow.includes(val3)) {
              flowValue = val3;
              outerValue = val2;
            }
          } else if (val2 === "list-item") {
            listItemValue = val2;
            if (displayFlow.includes(val1)) {
              flowValue = val1;
              outerValue = val3;
            } else if (displayFlow.includes(val3)) {
              flowValue = val3;
              outerValue = val1;
            }
          } else if (val3 === "list-item") {
            listItemValue = val3;
            if (displayFlow.includes(val1)) {
              flowValue = val1;
              outerValue = val2;
            } else if (displayFlow.includes(val2)) {
              flowValue = val2;
              outerValue = val1;
            }
          }
          if (outerValue && flowValue && listItemValue) {
            switch (outerValue) {
              case "block": {
                if (flowValue === "flow") {
                  return listItemValue;
                }
                return `${flowValue} ${listItemValue}`;
              }
              case "inline":
              case "run-in": {
                if (flowValue === "flow") {
                  return `${outerValue} ${listItemValue}`;
                }
                return `${outerValue} ${flowValue} ${listItemValue}`;
              }
            }
          }
        }
        break;
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
