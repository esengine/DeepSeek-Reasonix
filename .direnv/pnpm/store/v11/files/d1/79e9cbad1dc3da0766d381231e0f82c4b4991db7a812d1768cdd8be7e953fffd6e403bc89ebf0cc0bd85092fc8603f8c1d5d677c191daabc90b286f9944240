"use strict";
const { utf8Encode, utf8Decode } = require("../helpers/encoding");
const Blob = require("../../../generated/idl/Blob");
const { isArrayBuffer } = require("../../../generated/idl/utils");
const { concatTypedArrays, copyToArrayBufferInTargetRealm } = require("../helpers/binary-data");

function convertLineEndingsToNative(s) {
  // jsdom always pretends to be *nix, for consistency.
  // See also https://github.com/jsdom/jsdom/issues/2396.
  return s.replace(/\r\n|\r|\n/g, "\n");
}

exports.implementation = class BlobImpl {
  constructor(globalObject, [parts, properties], { fastPathArrayBufferToWrap } = {}) {
    this._globalObject = globalObject;

    this.type = properties.type;
    if (/[^\u0020-\u007E]/.test(this.type)) {
      this.type = "";
    } else {
      this.type = this.type.toLowerCase();
    }

    // A word about `this._bytes`:
    //
    // It is a `Uint8Array`. The realm of that `Uint8Array`, and/or the realm of its underlying `ArrayBuffer`, may be
    // arbitrary. In particular, they likely do *not* match `this._globalObject`. The underlying `ArrayBuffer` may have
    // been acquired from some other part of the system, e.g., the `ws` library, or aliased to another `BlobImpl`'s
    // `_bytes`.
    //
    // This is fine, and indeed desirable, for efficiency. The key is that `Blob` is conceptually immutable, so users
    // will never mutate the underlying `ArrayBuffer`. And, we never expose `this._bytes` or the underlying
    // `ArrayBuffer` directly to the user: we always use something like `copyToArrayBufferInTargetRealm()` to ensure the
    // result is in the realm appropriate for the user's request, and that if the user mutates the exposed bytes, this
    // doesn't impact `this._bytes`.

    // Used internally in jsdom when we receive an `ArrayBuffer` from elsewhere in the system and know we don't need to
    // copy it because the user doesn't have any references to it. It's OK if `fastPathArrayBufferToWrap` is in the
    // wrong realm even, because it never directly escapes the `BlobImpl` without a copy.
    if (fastPathArrayBufferToWrap) {
      this._bytes = new Uint8Array(fastPathArrayBufferToWrap);
      return;
    }

    const chunks = [];
    if (parts !== undefined) {
      for (const part of parts) {
        let chunk;
        if (isArrayBuffer(part)) {
          // Create a wrapper. The copying will happen in `concatTypedArrays()`.
          chunk = new Uint8Array(part);
        } else if (ArrayBuffer.isView(part)) {
          // Use the part as-is. The copying will happen in `concatTypedArrays()`.
          chunk = part;
        } else if (Blob.isImpl(part)) {
          // Use the existing `Uint8Array` as-is. The copying will happen in `concatTypedArrays()`.
          chunk = part._bytes;
        } else {
          let s = part;
          if (properties.endings === "native") {
            s = convertLineEndingsToNative(s);
          }
          chunk = utf8Encode(s);
        }
        chunks.push(chunk);
      }
    }
    this._bytes = concatTypedArrays(chunks);
  }

  get size() {
    return this._bytes.length;
  }

  arrayBuffer() {
    const ab = copyToArrayBufferInTargetRealm(this._bytes.buffer, this._globalObject);
    return this._globalObject.Promise.resolve(ab);
  }

  bytes() {
    const ab = copyToArrayBufferInTargetRealm(this._bytes.buffer, this._globalObject);
    return this._globalObject.Promise.resolve(new this._globalObject.Uint8Array(ab));
  }

  text() {
    return this._globalObject.Promise.resolve(utf8Decode(this._bytes));
  }

  slice(start, end, contentType) {
    const { size } = this;

    let relativeStart, relativeEnd, relativeContentType;

    if (start === undefined) {
      relativeStart = 0;
    } else if (start < 0) {
      relativeStart = Math.max(size + start, 0);
    } else {
      relativeStart = Math.min(start, size);
    }
    if (end === undefined) {
      relativeEnd = size;
    } else if (end < 0) {
      relativeEnd = Math.max(size + end, 0);
    } else {
      relativeEnd = Math.min(end, size);
    }

    if (contentType === undefined) {
      relativeContentType = "";
    } else {
      // sanitization (lower case and invalid char check) is done in the
      // constructor
      relativeContentType = contentType;
    }

    const span = Math.max(relativeEnd - relativeStart, 0);

    const slicedBuffer = this._bytes.slice(relativeStart, relativeStart + span);

    const blob = Blob.createImpl(this._globalObject, [[], { type: relativeContentType }], {});
    blob._bytes = slicedBuffer;
    return blob;
  }
};
