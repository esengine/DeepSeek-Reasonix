"use strict";

// https://fetch.spec.whatwg.org/#header-name
function isHeaderName(name) {
  return /^[!#$%&'*+\-.^`|~\w]+$/.test(name);
}

// https://fetch.spec.whatwg.org/#header-value
function isHeaderValue(value) {
  return value[0] !== "\t" &&
    value[0] !== " " &&
    value[value.length - 1] !== "\t" &&
    value[value.length - 1] !== " " &&
    !/[\0\r\n]/.test(value);
}

// https://fetch.spec.whatwg.org/#concept-header-value-normalize
function normalizeHeaderValue(potentialValue) {
  return potentialValue.replace(/^[\n\r\t ]+|[\n\r\t ]+$/g, "");
}

module.exports = {
  isHeaderName,
  isHeaderValue,
  normalizeHeaderValue
};
