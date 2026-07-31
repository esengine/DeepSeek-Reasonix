"use strict";
const XMLDocument = require("../../generated/idl/XMLDocument.js");
const Document = require("../../generated/idl/Document.js");
const { wrapperForImpl } = require("../../generated/idl/utils.js");

exports.createImpl = (globalObject, options, { alwaysUseDocumentClass = false } = {}) => {
  if (options.parsingMode === "xml" && !alwaysUseDocumentClass) {
    return XMLDocument.createImpl(globalObject, [], { options });
  }
  return Document.createImpl(globalObject, [], { options });
};

exports.createWrapper = (...args) => {
  return wrapperForImpl(exports.createImpl(...args));
};
