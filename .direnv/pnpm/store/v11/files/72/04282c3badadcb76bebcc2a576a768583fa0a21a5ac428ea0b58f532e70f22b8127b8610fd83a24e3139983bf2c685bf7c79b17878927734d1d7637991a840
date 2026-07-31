/**
 * Implements base64 and base64url from [RFC4648](https://datatracker.ietf.org/doc/html/rfc4648)
 * (no differences from [RFC3548](https://datatracker.ietf.org/doc/html/rfc4648)).
 *
 * ```js
 * import { fromBase64, toBase64 } from '@exodus/bytes/base64.js'
 * import { fromBase64url, toBase64url } from '@exodus/bytes/base64.js'
 * import { fromBase64any } from '@exodus/bytes/base64.js'
 * ```
 *
 * @module @exodus/bytes/base64.js
 */

/// <reference types="node" />

import type { OutputFormat, Uint8ArrayBuffer } from './array.js';

/**
 * Options for base64 encoding
 */
export interface ToBase64Options {
  /** Whether to include padding characters (default: true for base64, false for base64url) */
  padding?: boolean;
}

/**
 * Padding mode for base64 decoding
 * - `true`: padding is required
 * - `false`: padding is not allowed (default for base64url)
 * - `'both'`: padding is optional (default for base64)
 */
export type PaddingMode = boolean | 'both';

/**
 * Options for base64 decoding
 */
export interface FromBase64Options {
  /** Output format (default: 'uint8') */
  format?: OutputFormat;
  /** Padding mode */
  padding?: PaddingMode;
}

/**
 * Encode a `Uint8Array` to a base64 string (RFC 4648)
 *
 * @param arr - The input bytes
 * @param options - Encoding options
 * @returns The base64 encoded string
 */
export function toBase64(arr: Uint8Array, options?: ToBase64Options): string;

/**
 * Encode a `Uint8Array` to a base64url string (RFC 4648)
 *
 * @param arr - The input bytes
 * @param options - Encoding options (padding defaults to false)
 * @returns The base64url encoded string
 */
export function toBase64url(arr: Uint8Array, options?: ToBase64Options): string;

/**
 * Decode a base64 string to bytes
 *
 * Operates in strict mode for last chunk, does not allow whitespace
 *
 * @param string - The base64 encoded string
 * @param options - Decoding options
 * @returns The decoded bytes
 */
export function fromBase64(string: string, options?: FromBase64Options & { format?: 'uint8' }): Uint8ArrayBuffer;
export function fromBase64(string: string, options: FromBase64Options & { format: 'arraybuffer' }): ArrayBuffer;
export function fromBase64(string: string, options: FromBase64Options & { format: 'buffer' }): Buffer;
export function fromBase64(string: string, options?: FromBase64Options): Uint8ArrayBuffer | ArrayBuffer | Buffer;

/**
 * Decode a base64url string to bytes
 *
 * Operates in strict mode for last chunk, does not allow whitespace
 *
 * @param string - The base64url encoded string
 * @param options - Decoding options (padding defaults to false)
 * @returns The decoded bytes
 */
export function fromBase64url(string: string, options?: FromBase64Options & { format?: 'uint8' }): Uint8ArrayBuffer;
export function fromBase64url(string: string, options: FromBase64Options & { format: 'arraybuffer' }): ArrayBuffer;
export function fromBase64url(string: string, options: FromBase64Options & { format: 'buffer' }): Buffer;
export function fromBase64url(string: string, options?: FromBase64Options): Uint8ArrayBuffer | ArrayBuffer | Buffer;

/**
 * Decode either base64 or base64url string to bytes
 *
 * Automatically detects the variant based on characters present
 *
 * @param string - The base64 or base64url encoded string
 * @param options - Decoding options
 * @returns The decoded bytes
 */
export function fromBase64any(string: string, options?: FromBase64Options & { format?: 'uint8' }): Uint8ArrayBuffer;
export function fromBase64any(string: string, options: FromBase64Options & { format: 'arraybuffer' }): ArrayBuffer;
export function fromBase64any(string: string, options: FromBase64Options & { format: 'buffer' }): Buffer;
export function fromBase64url(string: string, options?: FromBase64Options): Uint8ArrayBuffer | ArrayBuffer | Buffer;
