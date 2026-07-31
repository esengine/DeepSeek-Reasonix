"use strict";
const serialize = require("w3c-xmlserializer");
const DOMException = require("../../../generated/idl/DOMException");
const utils = require("../../../generated/idl/utils");

exports.implementation = class XMLSerializerImpl {
  constructor(globalObject) {
    this._globalObject = globalObject;
  }

  serializeToString(root) {
    try {
      return serialize(utils.wrapperForImpl(root), { requireWellFormed: false });
    } catch (e) {
      throw DOMException.create(this._globalObject, [e.message, "InvalidStateError"]);
    }
  }
};
