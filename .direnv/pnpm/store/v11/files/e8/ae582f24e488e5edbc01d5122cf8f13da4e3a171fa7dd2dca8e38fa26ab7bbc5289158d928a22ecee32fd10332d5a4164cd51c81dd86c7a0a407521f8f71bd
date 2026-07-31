"use strict";
const { TextEncoder } = require("@exodus/bytes/encoding.js");
const { copyToArrayBufferInTargetRealm } = require("../helpers/binary-data");

// We can't directly use the native TextEncoder as the impl class, because its encode() method returns a Uint8Array in
// the Node.js realm. We need to wrap it so that encode() returns a Uint8Array in the jsdom window's realm, so that
// `result.buffer instanceof ArrayBuffer` works correctly in user code.

const encoder = new TextEncoder();

exports.implementation = class TextEncoderImpl {
  constructor(globalObject) {
    this._globalObject = globalObject;
    this.encoding = "utf-8";
  }

  encode(input) {
    const encoded = encoder.encode(input);
    const ab = copyToArrayBufferInTargetRealm(encoded.buffer, this._globalObject);
    return new this._globalObject.Uint8Array(ab);
  }

  encodeInto(source, destination) {
    return encoder.encodeInto(source, destination);
  }
};
