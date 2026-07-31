"use strict";

const parsers = require("./css-values");

// Constants
const { AST_TYPES } = parsers;

/**
 * Creates a generic property descriptor for a given property. Such descriptors are used whenever we don't have a
 * specific handler in `./properties/*.js`. They perform some basic logic that works as a fallback, and is correct for
 * simple properties, but properties with more complex grammars will need their own handlers.
 *
 * @param {string} property - The canonical CSS property name (e.g. "backdrop-filter", not "backdropFilter").
 * @param {object} opts - The options object.
 * @param {boolean} opts.caseSensitive - True if value is case-sensitive, false otherwise.
 * @param {object} [opts.dimensionTypes={}] - An object containing information about the dimension types used by this
 * property, if any. Keys are a type of dimension, which determines which serializer to use, and values are the
 * information used by the serializer to serialize a parsed value.
 * @param {object} [opts.functionTypes={}] - An object containing information about the function types used by this
 * property, if any. Keys are a type of function, which determines which function to use; values are ignored.
 * @returns {object} The property descriptor object.
 */
function createGenericPropertyDescriptor(property, { caseSensitive, dimensionTypes = {}, functionTypes = {} }) {
  return {
    set(v) {
      v = v.trim();
      if (parsers.hasVarFunc(v)) {
        this._setProperty(property, v);
      } else {
        const parsedValue = parsers.parsePropertyValue(property, v, {
          caseSensitive
        });
        const priority = this._priorities.get(property) ?? "";
        if (Array.isArray(parsedValue)) {
          if (parsedValue.length === 1) {
            const {
              angle: angleType,
              dimension: dimensionType,
              length: lengthType,
              number: numberType,
              percentage: percentageType,
              ratio: ratioType
            } = dimensionTypes;
            const { color: colorType, image: imageType, paint: paintType } = functionTypes;
            const [{ name, type, value: itemValue }] = parsedValue;
            switch (type) {
              case AST_TYPES.CALC: {
                this._setProperty(property, `${name}(${itemValue})`, priority);
                break;
              }
              case AST_TYPES.DIMENSION: {
                let val;
                if (dimensionType && lengthType) {
                  val = parsers.serializeLength(parsedValue, lengthType);
                  if (!val) {
                    val = parsers.serializeDimension(parsedValue, dimensionType);
                  }
                } else if (lengthType) {
                  val = parsers.serializeLength(parsedValue, lengthType);
                } else {
                  val = parsers.serializeDimension(parsedValue, dimensionType);
                }
                this._setProperty(property, val, priority);
                break;
              }
              case AST_TYPES.HASH: {
                this._setProperty(property, parsers.serializeColor(parsedValue), priority);
                break;
              }
              case AST_TYPES.NUMBER: {
                let val;
                if (numberType) {
                  val = parsers.serializeNumber(parsedValue, numberType);
                } else if (angleType) {
                  val = parsers.serializeAngle(parsedValue, angleType);
                } else if (lengthType) {
                  val = parsers.serializeLength(parsedValue, lengthType);
                } else if (percentageType) {
                  val = parsers.serializePercentage(parsedValue, percentageType);
                } else if (ratioType) {
                  val = parsers.serializeRatio(parsedValue, ratioType);
                }
                this._setProperty(property, val, priority);
                break;
              }
              case AST_TYPES.GLOBAL_KEYWORD:
              case AST_TYPES.IDENTIFIER: {
                this._setProperty(property, name, priority);
                break;
              }
              case AST_TYPES.PERCENTAGE: {
                let numericType;
                if (percentageType) {
                  numericType = percentageType;
                } else if (dimensionType) {
                  numericType = dimensionType;
                } else if (angleType) {
                  numericType = angleType;
                } else if (lengthType) {
                  numericType = lengthType;
                }
                if (numericType) {
                  this._setProperty(property, parsers.resolveNumericValue(parsedValue, numericType), priority);
                }
                break;
              }
              case AST_TYPES.STRING: {
                this._setProperty(property, parsers.serializeString(parsedValue), priority);
                break;
              }
              case AST_TYPES.URL: {
                this._setProperty(property, parsers.serializeURL(parsedValue), priority);
                break;
              }
              case AST_TYPES.FUNCTION:
              default: {
                if (colorType || paintType) {
                  this._setProperty(property, parsers.serializeColor(parsedValue), priority);
                } else if (imageType) {
                  this._setProperty(property, parsers.serializeGradient(parsedValue), priority);
                } else {
                  this._setProperty(property, v, priority);
                }
              }
            }
          } else {
            // Set the prepared value for lists containing multiple values.
            this._setProperty(property, v, priority);
          }
        } else if (typeof parsedValue === "string") {
          this._setProperty(property, parsedValue, priority);
        }
      }
    },
    get() {
      return this.getPropertyValue(property);
    },
    enumerable: true,
    configurable: true
  };
}

module.exports = {
  createGenericPropertyDescriptor
};
