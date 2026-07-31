/**
 * UTF-16 encoding/decoding
 *
 * ```js
 * import { utf16fromString, utf16toString } from '@exodus/bytes/utf16.js'
 *
 * // loose
 * import { utf16fromStringLoose, utf16toStringLoose } from '@exodus/bytes/utf16.js'
 * ```
 *
 * _These methods by design encode/decode BOM (codepoint `U+FEFF` Byte Order Mark) as-is._\
 * _If you need BOM handling or detection, use `@exodus/bytes/encoding.js`_
 *
 * @module @exodus/bytes/utf16.js
 */

/// <reference types="node" />

import type { Uint8ArrayBuffer, Uint16ArrayBuffer } from './array.js';

/**
 * Output format for UTF-16 encoding
 */
export type Utf16Format = 'uint16' | 'uint8-le' | 'uint8-be';

/**
 * Encode a string to UTF-16 bytes (strict mode)
 *
 * Throws on invalid Unicode (unpaired surrogates)
 *
 * @param string - The string to encode
 * @param format - Output format (default: 'uint16')
 * @returns The encoded bytes
 */
export function utf16fromString(string: string, format?: 'uint16'): Uint16ArrayBuffer;
export function utf16fromString(string: string, format: 'uint8-le'): Uint8ArrayBuffer;
export function utf16fromString(string: string, format: 'uint8-be'): Uint8ArrayBuffer;
export function utf16fromString(string: string, format?: Utf16Format): Uint16ArrayBuffer | Uint8ArrayBuffer;

/**
 * Encode a string to UTF-16 bytes (loose mode)
 *
 * Replaces invalid Unicode (unpaired surrogates) with replacement codepoints `U+FFFD`
 * per [WHATWG Encoding](https://encoding.spec.whatwg.org/) specification.
 *
 * _Such replacement is a non-injective function, is irreversible and causes collisions.\
 * Prefer using strict throwing methods for cryptography applications._
 *
 * @param string - The string to encode
 * @param format - Output format (default: 'uint16')
 * @returns The encoded bytes
 */
export function utf16fromStringLoose(string: string, format?: 'uint16'): Uint16ArrayBuffer;
export function utf16fromStringLoose(string: string, format: 'uint8-le'): Uint8ArrayBuffer;
export function utf16fromStringLoose(string: string, format: 'uint8-be'): Uint8ArrayBuffer;
export function utf16fromStringLoose(string: string, format?: Utf16Format): Uint16ArrayBuffer | Uint8ArrayBuffer;

/**
 * Decode UTF-16 bytes to a string (strict mode)
 *
 * Throws on invalid UTF-16 byte sequences
 *
 * Throws on non-even byte length.
 *
 * @param arr - The bytes to decode
 * @param format - Input format (default: 'uint16')
 * @returns The decoded string
 */
export function utf16toString(arr: Uint16Array, format?: 'uint16'): string;
export function utf16toString(arr: Uint8Array, format: 'uint8-le'): string;
export function utf16toString(arr: Uint8Array, format: 'uint8-be'): string;
export function utf16toString(arr: Uint16Array | Uint8Array, format?: Utf16Format): string;

/**
 * Decode UTF-16 bytes to a string (loose mode)
 *
 * Replaces invalid UTF-16 byte sequences with replacement codepoints `U+FFFD`
 * per [WHATWG Encoding](https://encoding.spec.whatwg.org/) specification.
 *
 * _Such replacement is a non-injective function, is irreversible and causes collisions.\
 * Prefer using strict throwing methods for cryptography applications._
 *
 * Throws on non-even byte length.
 *
 * @param arr - The bytes to decode
 * @param format - Input format (default: 'uint16')
 * @returns The decoded string
 */
export function utf16toStringLoose(arr: Uint16Array, format?: 'uint16'): string;
export function utf16toStringLoose(arr: Uint8Array, format: 'uint8-le'): string;
export function utf16toStringLoose(arr: Uint8Array, format: 'uint8-be'): string;
export function utf16toStringLoose(arr: Uint16Array | Uint8Array, format?: Utf16Format): string;
