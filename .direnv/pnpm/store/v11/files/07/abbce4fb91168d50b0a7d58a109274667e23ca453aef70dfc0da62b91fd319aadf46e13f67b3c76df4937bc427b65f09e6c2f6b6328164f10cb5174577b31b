/**
 * The exact same exports as `@exodus/bytes/encoding.js` are also exported as
 * `@exodus/bytes/encoding-lite.js`, with the difference that the lite version does not load
 * multi-byte `TextDecoder` encodings by default to reduce bundle size ~12x.
 *
 * ```js
 * import { TextDecoder, TextEncoder } from '@exodus/bytes/encoding-lite.js'
 * import { TextDecoderStream, TextEncoderStream } from '@exodus/bytes/encoding-lite.js' // Requires Streams
 * import { isomorphicDecode, isomorphicEncode } from '@exodus/bytes/encoding-lite.js'
 *
 * // Hooks for standards
 * import { getBOMEncoding, legacyHookDecode, labelToName, normalizeEncoding } from '@exodus/bytes/encoding-lite.js'
 * ```
 *
 * The only affected encodings are: `gbk`, `gb18030`, `big5`, `euc-jp`, `iso-2022-jp`, `shift_jis`
 * and their [labels](https://encoding.spec.whatwg.org/#names-and-labels) when used with `TextDecoder`.
 *
 * Legacy single-byte encodingds are loaded by default in both cases.
 *
 * `TextEncoder` and hooks for standards (including `labelToName` / `normalizeEncoding`) do not have any behavior
 * differences in the lite version and support full range if inputs.
 *
 * To avoid inconsistencies, the exported classes and methods are exactly the same objects.
 *
 * ```console
 * > lite = require('@exodus/bytes/encoding-lite.js')
 * [Module: null prototype] {
 *   TextDecoder: [class TextDecoder],
 *   TextDecoderStream: [class TextDecoderStream],
 *   TextEncoder: [class TextEncoder],
 *   TextEncoderStream: [class TextEncoderStream],
 *   getBOMEncoding: [Function: getBOMEncoding],
 *   labelToName: [Function: labelToName],
 *   legacyHookDecode: [Function: legacyHookDecode],
 *   normalizeEncoding: [Function: normalizeEncoding]
 * }
 * > new lite.TextDecoder('big5').decode(Uint8Array.of(0x25))
 * Uncaught:
 * Error: Legacy multi-byte encodings are disabled in /encoding-lite.js, use /encoding.js for full encodings range support
 *
 * > full = require('@exodus/bytes/encoding.js')
 * [Module: null prototype] {
 *   TextDecoder: [class TextDecoder],
 *   TextDecoderStream: [class TextDecoderStream],
 *   TextEncoder: [class TextEncoder],
 *   TextEncoderStream: [class TextEncoderStream],
 *   getBOMEncoding: [Function: getBOMEncoding],
 *   labelToName: [Function: labelToName],
 *   legacyHookDecode: [Function: legacyHookDecode],
 *   normalizeEncoding: [Function: normalizeEncoding]
 * }
 * > full.TextDecoder === lite.TextDecoder
 * true
 * > new full.TextDecoder('big5').decode(Uint8Array.of(0x25))
 * '%'
 * > new lite.TextDecoder('big5').decode(Uint8Array.of(0x25))
 * '%'
 * ```
 *
 * @module @exodus/bytes/encoding-lite.js
 */

export * from './encoding.js'
