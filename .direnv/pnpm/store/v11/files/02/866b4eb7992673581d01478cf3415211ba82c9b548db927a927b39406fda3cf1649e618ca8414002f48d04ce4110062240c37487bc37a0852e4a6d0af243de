"use strict";
// Small wrapper around Node.js's native `TextEncoder`/`TextDecoder`to expose functions with names matching the
// Encoding Standard, so it's more obvious what to use when implementing specs. See also discussion at
// https://github.com/ExodusOSS/bytes/issues/17.
//
// Note that we could also use `@exodus/bytes` instead of Node.js's native `TextEncoder`/`TextDecoder`. This could give
// benefits on non-Node.js environments. But, on Node.js, `@exodus/bytes` just delegates to the native classes, plus
// adds some extra type checks that we don't want. Per the discussion above, it fixes some bugs, but they are not
// relevant for our use cases.

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { ignoreBOM: false, fatal: false });
const decoderWithoutBOM = new TextDecoder("utf-8", { ignoreBOM: true, fatal: false });
const decoderWithoutBOMOrFail = new TextDecoder("utf-8", { ignoreBOM: true, fatal: true });

/**
 * Implements the <https://encoding.spec.whatwg.org/#utf-8-decode> algorithm. If there are three preceding BOM bytes,
 * they are discarded, and any lone surrogates become U+FFFD.
 *
 * @param {Uint8Array} bytes - The bytes to decode.
 * @returns {string} - The decoded string.
 */
exports.utf8Decode = bytes => {
  return decoder.decode(bytes);
};

/**
 * Implements the <https://encoding.spec.whatwg.org/#utf-8-decode-without-bom> algorithm. If there are three preceding
 * BOM bytes, they become U+FEFF, and any lone surrogates become U+FFFD.
 *
 * @param {Uint8Array} bytes - The bytes to decode.
 * @returns {string} - The decoded string.
 */
exports.utf8DecodeWithoutBOM = bytes => {
  return decoderWithoutBOM.decode(bytes);
};

/**
 * Implements the <https://encoding.spec.whatwg.org/#utf-8-decode-without-bom-or-fail> algorithm. If there are three
 * preceding BOM bytes, they become U+FEFF, and any lone surrogates cause an exception.
 *
 * @param {Uint8Array} bytes - The bytes to decode.
 * @returns {string} - The decoded string.
 */
exports.utf8DecodeWithoutBOMOrFail = bytes => {
  return decoderWithoutBOMOrFail.decode(bytes);
};

/**
 * Implements the <https://encoding.spec.whatwg.org/#utf-8-decode> algorithm, but also bundles in `USVString` conversion
 * (i.e. lone surrogates become 0xEF 0xBF 0xBD).
 *
 * @param {string} string - A string, possibly containing lone surrogates. If the string contains no lone surrogates,
 * then this behaves as the spec algorithm. Otherwise, it behaves as the composition of the spec algorithm and
 * `USVString` conversion.
 * @returns {Uint8Array} - The UTF-8 encoded bytes of the input string.
 */
exports.utf8Encode = string => {
  return encoder.encode(string);
};
