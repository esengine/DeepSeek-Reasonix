/**
 * Implements the [Encoding standard](https://encoding.spec.whatwg.org/):
 * [TextDecoder](https://encoding.spec.whatwg.org/#interface-textdecoder),
 * [TextEncoder](https://encoding.spec.whatwg.org/#interface-textencoder),
 * [TextDecoderStream](https://encoding.spec.whatwg.org/#interface-textdecoderstream),
 * [TextEncoderStream](https://encoding.spec.whatwg.org/#interface-textencoderstream),
 * some [hooks](https://encoding.spec.whatwg.org/#specification-hooks).
 *
 * ```js
 * import { TextDecoder, TextEncoder } from '@exodus/bytes/encoding.js'
 * import { TextDecoderStream, TextEncoderStream } from '@exodus/bytes/encoding.js' // Requires Streams
 * import { isomorphicDecode, isomorphicEncode } from '@exodus/bytes/encoding.js'
 *
 * // Hooks for standards
 * import { getBOMEncoding, legacyHookDecode, labelToName, normalizeEncoding } from '@exodus/bytes/encoding.js'
 * ```
 *
 * @module @exodus/bytes/encoding.js
 */

/// <reference types="node" />

/**
 * Convert an encoding [label](https://encoding.spec.whatwg.org/#names-and-labels) to its name,
 * as an ASCII-lowercased string.
 *
 * If an encoding with that label does not exist, returns `null`.
 *
 * This is the same as [`decoder.encoding` getter](https://encoding.spec.whatwg.org/#dom-textdecoder-encoding),
 * except that it:
 *  1. Supports [`replacement` encoding](https://encoding.spec.whatwg.org/#replacement) and its
 *     [labels](https://encoding.spec.whatwg.org/#ref-for-replacement%E2%91%A1)
 *  2. Does not throw for invalid labels and instead returns `null`
 *
 * It is identical to:
 * ```js
 * labelToName(label)?.toLowerCase() ?? null
 * ```
 *
 * All encoding names are also valid labels for corresponding encodings.
 *
 * @param label - The encoding label to normalize
 * @returns The normalized encoding name, or null if invalid
 */
export function normalizeEncoding(label: string): string | null;

/**
 * Implements [BOM sniff](https://encoding.spec.whatwg.org/#bom-sniff) legacy hook.
 *
 * Given a `TypedArray` or an `ArrayBuffer` instance `input`, returns either of:
 * - `'utf-8'`, if `input` starts with UTF-8 byte order mark.
 * - `'utf-16le'`, if `input` starts with UTF-16LE byte order mark.
 * - `'utf-16be'`, if `input` starts with UTF-16BE byte order mark.
 * - `null` otherwise.
 *
 * @param input - The bytes to check for BOM
 * @returns The encoding ('utf-8', 'utf-16le', 'utf-16be'), or null if no BOM found
 */
export function getBOMEncoding(
  input: ArrayBufferLike | ArrayBufferView
): 'utf-8' | 'utf-16le' | 'utf-16be' | null;

/**
 * Implements [decode](https://encoding.spec.whatwg.org/#decode) legacy hook.
 *
 * Given a `TypedArray` or an `ArrayBuffer` instance `input` and an optional `fallbackEncoding`
 * encoding [label](https://encoding.spec.whatwg.org/#names-and-labels),
 * sniffs encoding from BOM with `fallbackEncoding` fallback and then
 * decodes the `input` using that encoding, skipping BOM if it was present.
 *
 * Notes:
 *
 * - BOM-sniffed encoding takes precedence over `fallbackEncoding` option per spec.
 *   Use with care.
 * - Always operates in non-fatal [mode](https://encoding.spec.whatwg.org/#textdecoder-error-mode),
 *   aka replacement. It can convert different byte sequences to equal strings.
 *
 * This method is similar to the following code, except that it doesn't support encoding labels and
 * only expects lowercased encoding name:
 *
 * ```js
 * new TextDecoder(getBOMEncoding(input) ?? fallbackEncoding).decode(input)
 * ```
 *
 * @param input - The bytes to decode
 * @param fallbackEncoding - The encoding to use if no BOM detected (default: 'utf-8')
 * @returns The decoded string
 */
export function legacyHookDecode(
  input: ArrayBufferLike | ArrayBufferView,
  fallbackEncoding?: string
): string;

/**
 * Implements [isomorphic decode](https://infra.spec.whatwg.org/#isomorphic-decode).
 *
 * Given a `TypedArray` or an `ArrayBuffer` instance `input`, creates a string of the same length
 * as input byteLength, using bytes from input as codepoints.
 *
 * E.g. for `Uint8Array` input, this is similar to `String.fromCodePoint(...input)`.
 *
 * Wider `TypedArray` inputs, e.g. `Uint16Array`, are interpreted as underlying _bytes_.
 *
 * @param input - The bytes to decode
 * @returns The decoded string
 */
export function isomorphicDecode(input: ArrayBufferLike | ArrayBufferView): string;

/**
 * Implements [isomorphic encode](https://infra.spec.whatwg.org/#isomorphic-encode).
 *
 * Given a string, creates an `Uint8Array` of the same length with the string codepoints as byte values.
 *
 * Accepts only [isomorphic string](https://infra.spec.whatwg.org/#isomorphic-string) input
 * and asserts that, throwing on any strings containing codepoints higher than `U+00FF`.
 *
 * @param input - The bytes to decode
 * @returns An Uint8Array containing the input bytes.
 */
export function isomorphicEncode(str: string): Uint8Array;

/**
 * Implements [get an encoding from a string `label`](https://encoding.spec.whatwg.org/#concept-encoding-get).
 *
 * Convert an encoding [label](https://encoding.spec.whatwg.org/#names-and-labels) to its name,
 * as a case-sensitive string.
 *
 * If an encoding with that label does not exist, returns `null`.
 *
 * All encoding names are also valid labels for corresponding encodings.
 *
 * @param label - The encoding label
 * @returns The proper case encoding name, or null if invalid
 */
export function labelToName(label: string): string | null;

/**
 * [TextDecoder](https://encoding.spec.whatwg.org/#interface-textdecoder) implementation/polyfill.
 *
 * Decode bytes to strings according to [WHATWG Encoding](https://encoding.spec.whatwg.org) specification.
 */
export const TextDecoder: typeof globalThis.TextDecoder;

/**
 * [TextEncoder](https://encoding.spec.whatwg.org/#interface-textencoder) implementation/polyfill.
 *
 * Encode strings to UTF-8 bytes according to [WHATWG Encoding](https://encoding.spec.whatwg.org) specification.
 */
export const TextEncoder: typeof globalThis.TextEncoder;

/**
 * [TextDecoderStream](https://encoding.spec.whatwg.org/#interface-textdecoderstream) implementation/polyfill.
 *
 * A [Streams](https://streams.spec.whatwg.org/) wrapper for `TextDecoder`.
 *
 * Requires [Streams](https://streams.spec.whatwg.org/) to be either supported by the platform or
 * [polyfilled](https://npmjs.com/package/web-streams-polyfill).
 */
export const TextDecoderStream: typeof globalThis.TextDecoderStream;

/**
 * [TextEncoderStream](https://encoding.spec.whatwg.org/#interface-textencoderstream) implementation/polyfill.
 *
 * A [Streams](https://streams.spec.whatwg.org/) wrapper for `TextEncoder`.
 *
 * Requires [Streams](https://streams.spec.whatwg.org/) to be either supported by the platform or
 * [polyfilled](https://npmjs.com/package/web-streams-polyfill).
 */
export const TextEncoderStream: typeof globalThis.TextEncoderStream;
