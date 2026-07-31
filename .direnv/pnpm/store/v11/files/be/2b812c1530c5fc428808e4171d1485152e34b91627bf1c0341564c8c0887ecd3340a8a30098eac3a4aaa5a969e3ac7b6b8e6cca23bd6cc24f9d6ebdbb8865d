/**
 * Implements Base16 from [RFC4648](https://datatracker.ietf.org/doc/html/rfc4648)
 * (no differences from [RFC3548](https://datatracker.ietf.org/doc/html/rfc4648)).
 *
 * ```js
 * import { fromHex, toHex } from '@exodus/bytes/hex.js'
 * ```
 *
 * @module @exodus/bytes/hex.js
 */

/// <reference types="node" />

import type { OutputFormat, Uint8ArrayBuffer } from './array.js';

/**
 * Encode a `Uint8Array` to a lowercase hex string
 *
 * @param arr - The input bytes
 * @returns The hex encoded string
 */
export function toHex(arr: Uint8Array): string;

/**
 * Decode a hex string to bytes
 *
 * Unlike `Buffer.from()`, throws on invalid input
 *
 * @param string - The hex encoded string (case-insensitive)
 * @param format - Output format (default: 'uint8')
 * @returns The decoded bytes
 */
export function fromHex(string: string, format?: 'uint8'): Uint8ArrayBuffer;
export function fromHex(string: string, format: 'arraybuffer'): ArrayBuffer;
export function fromHex(string: string, format: 'buffer'): Buffer;
export function fromHex(string: string, format?: OutputFormat): Uint8ArrayBuffer | ArrayBuffer | Buffer;
