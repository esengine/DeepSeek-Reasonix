/**
 * WHATWG helpers
 *
 * ```js
 * import '@exodus/bytes/encoding.js' // For full legacy multi-byte encodings support
 * import { percentEncodeAfterEncoding } from '@exodus/bytes/whatwg.js'
 * ```
 *
 * @module @exodus/bytes/whatwg.js
 */

/**
 * Implements [percent-encode after encoding](https://url.spec.whatwg.org/#string-percent-encode-after-encoding)
 * per WHATWG URL specification.
 *
 * > [!IMPORTANT]
 * > You must import `@exodus/bytes/encoding.js` for this API to accept legacy multi-byte encodings.
 *
 * Encodings `utf16-le`, `utf16-be`, and `replacement` are not accepted.
 *
 * [C0 control percent-encode set](https://url.spec.whatwg.org/#c0-control-percent-encode-set) is
 * always percent-encoded.
 *
 * `percentEncodeSet` is an addition to that, and must be a string of unique increasing codepoints
 * in range 0x20 - 0x7e, e.g. `' "#<>'`.
 *
 * This method accepts [DOMStrings](https://webidl.spec.whatwg.org/#idl-DOMString) and converts them
 * to [USVStrings](https://webidl.spec.whatwg.org/#idl-USVString).
 * This is different from e.g. `encodeURI` and `encodeURIComponent` which throw on surrogates:
 * ```js
 * > percentEncodeAfterEncoding('utf8', '\ud800', ' "#$%&+,/:;<=>?@[\\]^`{|}') // component
 * '%EF%BF%BD'
 * > encodeURIComponent('\ud800')
 * Uncaught URIError: URI malformed
 * ```
 *
 * @param encoding - The encoding label per WHATWG Encoding spec
 * @param input - Input scalar-value string to encode
 * @param percentEncodeSet - A string of ASCII chars to escape in addition to C0 control percent-encode set
 * @param spaceAsPlus - Whether to encode space as `'+'` instead of `'%20'` or `' '` (default: false)
 * @returns The percent-encoded string
 */
export function percentEncodeAfterEncoding(
  encoding: string,
  input: string,
  percentEncodeSet: string,
  spaceAsPlus?: boolean
): string;
