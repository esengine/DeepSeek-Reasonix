/**
 * Implements base32 and base32hex from [RFC4648](https://datatracker.ietf.org/doc/html/rfc4648)
 * (no differences from [RFC3548](https://datatracker.ietf.org/doc/html/rfc4648)).
 *
 * ```js
 * import { fromBase32, toBase32 } from '@exodus/bytes/base32.js'
 * import { fromBase32hex, toBase32hex } from '@exodus/bytes/base32.js'
 * ```
 *
 * @module @exodus/bytes/base32.js
 */

/// <reference types="node" />

import type { OutputFormat, Uint8ArrayBuffer } from './array.js';

/**
 * Options for base32 encoding
 */
export interface ToBase32Options {
  /** Whether to include padding characters (default: false) */
  padding?: boolean;
}

/**
 * Padding mode for base32 decoding
 * - `true`: padding is required
 * - `false`: padding is not allowed
 * - `'both'`: padding is optional (default)
 */
export type PaddingMode = boolean | 'both';

/**
 * Options for base32 decoding
 */
export interface FromBase32Options {
  /** Output format (default: 'uint8') */
  format?: OutputFormat;
  /** Padding mode */
  padding?: PaddingMode;
}

/**
 * Encode a `Uint8Array` to a base32 string (RFC 4648)
 *
 * @param arr - The input bytes
 * @param options - Encoding options
 * @returns The base32 encoded string
 */
export function toBase32(arr: Uint8Array, options?: ToBase32Options): string;

/**
 * Encode a `Uint8Array` to a base32hex string (RFC 4648)
 *
 * @param arr - The input bytes
 * @param options - Encoding options (padding defaults to false)
 * @returns The base32hex encoded string
 */
export function toBase32hex(arr: Uint8Array, options?: ToBase32Options): string;

/**
 * Encode a `Uint8Array` to a Crockford base32 string
 *
 * @param arr - The input bytes
 * @param options - Encoding options (padding defaults to false)
 * @returns The Crockford base32 encoded string
 */
export function toBase32crockford(arr: Uint8Array, options?: ToBase32Options): string;

/**
 * Decode a base32 string to bytes
 *
 * Operates in strict mode for last chunk, does not allow whitespace
 *
 * @param string - The base32 encoded string
 * @param options - Decoding options
 * @returns The decoded bytes
 */
export function fromBase32(string: string, options?: FromBase32Options & { format?: 'uint8' }): Uint8ArrayBuffer;
export function fromBase32(string: string, options: FromBase32Options & { format: 'arraybuffer' }): ArrayBuffer;
export function fromBase32(string: string, options: FromBase32Options & { format: 'buffer' }): Buffer;
export function fromBase32(string: string, options?: FromBase32Options): Uint8ArrayBuffer | ArrayBuffer | Buffer;

/**
 * Decode a base32hex string to bytes
 *
 * Operates in strict mode for last chunk, does not allow whitespace
 *
 * @param string - The base32hex encoded string
 * @param options - Decoding options
 * @returns The decoded bytes
 */
export function fromBase32hex(string: string, options?: FromBase32Options & { format?: 'uint8' }): Uint8ArrayBuffer;
export function fromBase32hex(string: string, options: FromBase32Options & { format: 'arraybuffer' }): ArrayBuffer;
export function fromBase32hex(string: string, options: FromBase32Options & { format: 'buffer' }): Buffer;
export function fromBase32hex(string: string, options?: FromBase32Options): Uint8ArrayBuffer | ArrayBuffer | Buffer;

/**
 * Decode a Crockford base32 string to bytes
 *
 * Operates in strict mode for last chunk, does not allow whitespace
 *
 * Crockford base32 decoding follows extra mapping per spec: `LIli -> 1, Oo -> 0`
 *
 * @param string - The Crockford base32 encoded string
 * @param options - Decoding options
 * @returns The decoded bytes
 */
export function fromBase32crockford(string: string, options?: FromBase32Options & { format?: 'uint8' }): Uint8ArrayBuffer;
export function fromBase32crockford(string: string, options: FromBase32Options & { format: 'arraybuffer' }): ArrayBuffer;
export function fromBase32crockford(string: string, options: FromBase32Options & { format: 'buffer' }): Buffer;
export function fromBase32crockford(string: string, options?: FromBase32Options): Uint8ArrayBuffer | ArrayBuffer | Buffer;
