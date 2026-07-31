/**
 * Decode / encode the legacy single-byte encodings according to the
 * [Encoding standard](https://encoding.spec.whatwg.org/)
 * ([§9](https://encoding.spec.whatwg.org/#legacy-single-byte-encodings),
 * [§14.5](https://encoding.spec.whatwg.org/#x-user-defined)),
 * and [unicode.org](https://unicode.org/Public/MAPPINGS/ISO8859) `iso-8859-*` mappings.
 *
 * ```js
 * import { createSinglebyteDecoder, createSinglebyteEncoder } from '@exodus/bytes/single-byte.js'
 * import { windows1252toString, windows1252fromString } from '@exodus/bytes/single-byte.js'
 * import { latin1toString, latin1fromString } from '@exodus/bytes/single-byte.js'
 * ```
 *
 * > [!WARNING]
 * > This is a lower-level API for single-byte encodings.
 * > It might not match what you expect, as it supports both WHATWG and unicode.org encodings under
 * > different names, with the main intended usecase for the latter being either non-web or legacy contexts.
 * >
 * > For a safe WHATWG Encoding-compatible API, see `@exodus/bytes/encoding.js` import (and variants of it).
 * >
 * > Be sure to know what you are doing and check documentation when directly using encodings from this file.
 *
 * Supports all single-byte encodings listed in the WHATWG Encoding standard:
 * `ibm866`, `iso-8859-2`, `iso-8859-3`, `iso-8859-4`, `iso-8859-5`, `iso-8859-6`, `iso-8859-7`, `iso-8859-8`,
 * `iso-8859-8-i`, `iso-8859-10`, `iso-8859-13`, `iso-8859-14`, `iso-8859-15`, `iso-8859-16`, `koi8-r`, `koi8-u`,
 * `macintosh`, `windows-874`, `windows-1250`, `windows-1251`, `windows-1252`, `windows-1253`, `windows-1254`,
 * `windows-1255`, `windows-1256`, `windows-1257`, `windows-1258`, `x-mac-cyrillic` and `x-user-defined`.
 *
 * Also supports `iso-8859-1`, `iso-8859-9`, `iso-8859-11` as defined at
 * [unicode.org](https://unicode.org/Public/MAPPINGS/ISO8859)
 * (and all other `iso-8859-*` encodings there as they match WHATWG).
 *
 * > [!NOTE]
 * > While all `iso-8859-*` encodings supported by the [WHATWG Encoding standard](https://encoding.spec.whatwg.org/) match
 * > [unicode.org](https://unicode.org/Public/MAPPINGS/ISO8859), the WHATWG Encoding spec doesn't support
 * > `iso-8859-1`, `iso-8859-9`, `iso-8859-11`, and instead maps them as labels to `windows-1252`, `windows-1254`, `windows-874`.\
 * > `createSinglebyteDecoder()` (unlike `TextDecoder` or `legacyHookDecode()`) does not do such mapping,
 * > so its results will differ from `TextDecoder` for those encoding names.
 *
 * ```js
 * > new TextDecoder('iso-8859-1').encoding
 * 'windows-1252'
 * > new TextDecoder('iso-8859-9').encoding
 * 'windows-1254'
 * > new TextDecoder('iso-8859-11').encoding
 * 'windows-874'
 * > new TextDecoder('iso-8859-9').decode(Uint8Array.of(0x80, 0x81, 0xd0))
 * '€\x81Ğ' // this is actually decoded according to windows-1254 per TextDecoder spec
 * > createSinglebyteDecoder('iso-8859-9')(Uint8Array.of(0x80, 0x81, 0xd0))
 * '\x80\x81Ğ' // this is iso-8859-9 as defined at https://unicode.org/Public/MAPPINGS/ISO8859/8859-9.txt
 * ```
 *
 * All WHATWG Encoding spec [`windows-*` encodings](https://encoding.spec.whatwg.org/#windows-874) are supersets of
 * corresponding [unicode.org encodings](https://unicode.org/Public/MAPPINGS/VENDORS/MICSFT/WINDOWS/), meaning that
 * they encode/decode all the old valid (non-replacement) strings / byte sequences identically, but can also support
 * a wider range of inputs.
 *
 * @module @exodus/bytes/single-byte.js
 */

/// <reference types="node" />

import type { Uint8ArrayBuffer } from './array.js';

/**
 * Create a decoder for a supported one-byte `encoding`, given its lowercased name `encoding`.
 *
 * Returns a function `decode(arr)` that decodes bytes to a string.
 *
 * @param encoding - The encoding name (e.g., 'iso-8859-1', 'windows-1252')
 * @param loose - If true, replaces unmapped bytes with replacement character instead of throwing (default: false)
 * @returns A function that decodes bytes to string
 */
export function createSinglebyteDecoder(
  encoding: string,
  loose?: boolean
): (arr: Uint8Array) => string;

/**
 * Create an encoder for a supported one-byte `encoding`, given its lowercased name `encoding`.
 *
 * Returns a function `encode(string)` that encodes a string to bytes.
 *
 * In `'fatal'` mode (default), will throw on non well-formed strings or any codepoints which could
 * not be encoded in the target encoding.
 *
 * @param encoding - The encoding name (e.g., 'iso-8859-1', 'windows-1252')
 * @param options - Encoding options
 * @param options.mode - Encoding mode (default: 'fatal'). Currently, only 'fatal' mode is supported.
 * @returns A function that encodes string to bytes
 */
export function createSinglebyteEncoder(
  encoding: string,
  options?: { mode?: 'fatal' }
): (string: string) => Uint8ArrayBuffer;

/**
 * Decode `iso-8859-1` bytes to a string.
 *
 * There is no loose variant for this encoding, all bytes can be decoded.
 *
 * Same as:
 * ```js
 * const latin1toString = createSinglebyteDecoder('iso-8859-1')
 * ```
 *
 * > [!NOTE]
 * > This is different from `new TextDecoder('iso-8859-1')` and `new TextDecoder('latin1')`, as those
 * > alias to `new TextDecoder('windows-1252')`.
 *
 * Prefer using `isomorphicDecode()` from `@exodus/bytes/encoding.js` or `@exodus/bytes/encoding-lite.js`,
 * which is identical to this but allows more input types.
 *
 * @deprecated Use `import { isomorphicDecode } from '@exodus/bytes/encoding-lite.js'`
 * @param arr - The bytes to decode
 * @returns The decoded string
 */
export function latin1toString(arr: Uint8Array): string;

/**
 * Encode a string to `iso-8859-1` bytes.
 *
 * Throws on non well-formed strings or any codepoints which could not be encoded in `iso-8859-1`.
 *
 * Same as:
 * ```js
 * const latin1fromString = createSinglebyteEncoder('iso-8859-1', { mode: 'fatal' })
 * ```
 *
 * Prefer using `isomorphicEncode()` from `@exodus/bytes/encoding.js` or `@exodus/bytes/encoding-lite.js`.
 *
 * @deprecated Use `import { isomorphicEncode } from '@exodus/bytes/encoding-lite.js'`
 * @param string - The string to encode
 * @returns The encoded bytes
 */
export function latin1fromString(string: string): Uint8ArrayBuffer;

/**
 * Decode `windows-1252` bytes to a string.
 *
 * There is no loose variant for this encoding, all bytes can be decoded.
 *
 * Same as:
 * ```js
 * const windows1252toString = createSinglebyteDecoder('windows-1252')
 * ```
 *
 * @param arr - The bytes to decode
 * @returns The decoded string
 */
export function windows1252toString(arr: Uint8Array): string;

/**
 * Encode a string to `windows-1252` bytes.
 *
 * Throws on non well-formed strings or any codepoints which could not be encoded in `windows-1252`.
 *
 * Same as:
 * ```js
 * const windows1252fromString = createSinglebyteEncoder('windows-1252', { mode: 'fatal' })
 * ```
 *
 * @param string - The string to encode
 * @returns The encoded bytes
 */
export function windows1252fromString(string: string): Uint8ArrayBuffer;
