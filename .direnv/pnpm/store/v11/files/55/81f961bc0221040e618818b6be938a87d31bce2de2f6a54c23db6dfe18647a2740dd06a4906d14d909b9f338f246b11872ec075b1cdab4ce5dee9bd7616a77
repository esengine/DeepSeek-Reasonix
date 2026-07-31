/**
 * Implements [base58](https://www.ietf.org/archive/id/draft-msporny-base58-03.txt) encoding.
 *
 * Supports both standard base58 and XRP variant alphabets.
 *
 * ```js
 * import { fromBase58, toBase58 } from '@exodus/bytes/base58.js'
 * import { fromBase58xrp, toBase58xrp } from '@exodus/bytes/base58.js'
 * ```
 *
 * @module @exodus/bytes/base58.js
 */

/// <reference types="node" />

import type { OutputFormat, Uint8ArrayBuffer } from './array.js';

/**
 * Encode a `Uint8Array` to a base58 string
 *
 * Uses the standard Bitcoin base58 alphabet
 *
 * @param arr - The input bytes
 * @returns The base58 encoded string
 */
export function toBase58(arr: Uint8Array): string;

/**
 * Decode a base58 string to bytes
 *
 * Uses the standard Bitcoin base58 alphabet
 *
 * @param string - The base58 encoded string
 * @param format - Output format (default: 'uint8')
 * @returns The decoded bytes
 */
export function fromBase58(string: string, format?: 'uint8'): Uint8ArrayBuffer;
export function fromBase58(string: string, format: 'arraybuffer'): ArrayBuffer;
export function fromBase58(string: string, format: 'buffer'): Buffer;
export function fromBase58(string: string, format?: OutputFormat): Uint8ArrayBuffer | ArrayBuffer | Buffer;

/**
 * Encode a `Uint8Array` to a base58 string using XRP alphabet
 *
 * Uses the XRP variant base58 alphabet
 *
 * @param arr - The input bytes
 * @returns The base58 encoded string
 */
export function toBase58xrp(arr: Uint8Array): string;

/**
 * Decode a base58 string to bytes using XRP alphabet
 *
 * Uses the XRP variant base58 alphabet
 *
 * @param string - The base58 encoded string
 * @param format - Output format (default: 'uint8')
 * @returns The decoded bytes
 */
export function fromBase58xrp(string: string, format?: 'uint8'): Uint8ArrayBuffer;
export function fromBase58xrp(string: string, format: 'arraybuffer'): ArrayBuffer;
export function fromBase58xrp(string: string, format: 'buffer'): Buffer;
export function fromBase58xrp(string: string, format?: OutputFormat): Uint8ArrayBuffer | ArrayBuffer | Buffer;
