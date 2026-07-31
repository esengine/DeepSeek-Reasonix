"use strict";
const { TextDecoder } = require("@exodus/bytes/encoding.js");

// A thin wrapper is needed since there are constructor arguments, which mismatches webidl2js's assumed pattern.
exports.implementation = class TextDecoderImpl {
  constructor(globalObject, [label, options]) {
    this._innerImpl = new TextDecoder(label, options);
  }

  decode(input, options) {
    return this._innerImpl.decode(input, options);
  }

  get encoding() {
    return this._innerImpl.encoding;
  }

  get fatal() {
    return this._innerImpl.fatal;
  }

  get ignoreBOM() {
    return this._innerImpl.ignoreBOM;
  }
};
