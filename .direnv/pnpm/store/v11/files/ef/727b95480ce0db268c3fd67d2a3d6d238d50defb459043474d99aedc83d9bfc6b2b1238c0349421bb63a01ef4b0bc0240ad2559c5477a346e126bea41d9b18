"use strict";
const { utf8Decode } = require("./encoding");

// https://infra.spec.whatwg.org/#parse-json-bytes-to-a-javascript-value
exports.parseJSONFromBytes = bytes => {
  return JSON.parse(utf8Decode(bytes));
};
