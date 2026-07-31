/**
 * UTF-8 encoding/decoding
 *
 * ```js
 * import { utf8fromString, utf8toString } from '@exodus/bytes/utf8.js'
 *
 * // loose
 * import { utf8fromStringLoose, utf8toStringLoose } from '@exodus/bytes/utf8.js'
 * ```
 *
 * _These methods by design encode/decode BOM (codepoint `U+FEFF` Byte Order Mark) as-is._\
 * _If you need BOM handling or detection, use `@exodus/bytes/encoding.js`_
 *
 * @module @exodus/bytes/utf8.js
 */

/// <reference types="node" />

import type { OutputFormat, Uint8ArrayBuffer } from './array.js';

/**
 * Encode a string to UTF-8 bytes (strict mode)
 *
 * Throws on invalid Unicode (unpaired surrogates)
 *
 * This is similar to the following snippet (but works on all engines):
 * ```js
 * // Strict encode, requiring Unicode codepoints to be valid
 * if (typeof string !== 'string' || !string.isWellFormed()) throw new TypeError()
 * return new TextEncoder().encode(string)
 * ```
 *
 * @param string - The string to encode
 * @param format - Output format (default: 'uint8')
 * @returns The encoded bytes
 */
export function utf8fromString(string: string, format?: 'uint8'): Uint8ArrayBuffer;
export function utf8fromString(string: string, format: 'arraybuffer'): ArrayBuffer;
export function utf8fromString(string: string, format: 'buffer'): Buffer;
export function utf8fromString(string: string, format?: OutputFormat): Uint8ArrayBuffer | ArrayBuffer | Buffer;

/**
 * Encode a string to UTF-8 bytes (loose mode)
 *
 * Replaces invalid Unicode (unpaired surrogates) with replacement codepoints `U+FFFD`
 * per [WHATWG Encoding](https://encoding.spec.whatwg.org/) specification.
 *
 * _Such replacement is a non-injective function, is irreversable and causes collisions.\
 * Prefer using strict throwing methods for cryptography applications._
 *
 * This is similar to the following snippet (but works on all engines):
 * ```js
 * // Loose encode, replacing invalid Unicode codepoints with U+FFFD
 * if (typeof string !== 'string') throw new TypeError()
 * return new TextEncoder().encode(string)
 * ```
 *
 * @param string - The string to encode
 * @param format - Output format (default: 'uint8')
 * @returns The encoded bytes
 */
export function utf8fromStringLoose(string: string, format?: 'uint8'): Uint8ArrayBuffer;
export function utf8fromStringLoose(string: string, format: 'arraybuffer'): ArrayBuffer;
export function utf8fromStringLoose(string: string, format: 'buffer'): Buffer;
export function utf8fromStringLoose(
  string: string,
  format?: OutputFormat
): Uint8ArrayBuffer | ArrayBuffer | Buffer;

/**
 * Decode UTF-8 bytes to a string (strict mode)
 *
 * Throws on invalid UTF-8 byte sequences
 *
 * This is similar to `new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(arr)`,
 * but works on all engines.
 *
 * @param arr - The bytes to decode
 * @returns The decoded string
 */
export function utf8toString(arr: Uint8Array): string;

/**
 * Decode UTF-8 bytes to a string (loose mode)
 *
 * Replaces invalid UTF-8 byte sequences with replacement codepoints `U+FFFD`
 * per [WHATWG Encoding](https://encoding.spec.whatwg.org/) specification.
 *
 * _Such replacement is a non-injective function, is irreversable and causes collisions.\
 * Prefer using strict throwing methods for cryptography applications._
 *
 * This is similar to `new TextDecoder('utf-8', { ignoreBOM: true }).decode(arr)`,
 * but works on all engines.
 *
 * @param arr - The bytes to decode
 * @returns The decoded string
 */
export function utf8toStringLoose(arr: Uint8Array): string;
