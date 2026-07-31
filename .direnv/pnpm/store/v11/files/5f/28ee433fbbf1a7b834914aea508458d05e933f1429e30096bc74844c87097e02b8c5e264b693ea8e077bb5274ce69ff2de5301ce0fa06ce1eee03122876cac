/**
 * Implements [base58check](https://en.bitcoin.it/wiki/Base58Check_encoding) encoding.
 *
 * ```js
 * import { fromBase58check, toBase58check } from '@exodus/bytes/base58check.js'
 * import { fromBase58checkSync, toBase58checkSync } from '@exodus/bytes/base58check.js'
 * import { makeBase58check } from '@exodus/bytes/base58check.js'
 * ```
 *
 * On non-Node.js, requires peer dependency [@noble/hashes](https://www.npmjs.com/package/@noble/hashes) to be installed.
 *
 * @module @exodus/bytes/base58check.js
 */

/// <reference types="node" />

import type { OutputFormat, Uint8ArrayBuffer } from './array.js';

/**
 * Hash function type that takes Uint8Array and returns a Promise of Uint8Array
 */
export type HashFunction = (data: Uint8Array) => Promise<Uint8Array>;

/**
 * Synchronous hash function type that takes Uint8Array and returns Uint8Array
 */
export type HashFunctionSync = (data: Uint8Array) => Uint8Array;

/**
 * Base58Check encoder/decoder instance with async methods
 */
export interface Base58CheckAsync {
  /**
   * Encode bytes to base58check string asynchronously
   *
   * @param arr - The input bytes to encode
   * @returns A Promise that resolves to the base58check encoded string
   */
  encode(arr: Uint8Array): Promise<string>;

  /**
   * Decode a base58check string to bytes asynchronously
   *
   * @param string - The base58check encoded string
   * @param format - Output format (default: 'uint8')
   * @returns A Promise that resolves to the decoded bytes
   */
  decode(string: string, format?: 'uint8'): Promise<Uint8ArrayBuffer>;
  decode(string: string, format: 'arraybuffer'): Promise<ArrayBuffer>;
  decode(string: string, format: 'buffer'): Promise<Buffer>;
  decode(string: string, format?: OutputFormat): Promise<Uint8ArrayBuffer | ArrayBuffer | Buffer>;
}

/**
 * Base58Check encoder/decoder instance with both async and sync methods
 */
export interface Base58CheckSync extends Base58CheckAsync {
  /**
   * Encode bytes to base58check string synchronously
   *
   * @param arr - The input bytes to encode
   * @returns The base58check encoded string
   */
  encodeSync(arr: Uint8Array): string;

  /**
   * Decode a base58check string to bytes synchronously
   *
   * @param string - The base58check encoded string
   * @param format - Output format (default: 'uint8')
   * @returns The decoded bytes
   */
  decodeSync(string: string, format?: 'uint8'): Uint8ArrayBuffer;
  decodeSync(string: string, format: 'arraybuffer'): ArrayBuffer;
  decodeSync(string: string, format: 'buffer'): Buffer;
  decodeSync(string: string, format?: OutputFormat): Uint8ArrayBuffer | ArrayBuffer | Buffer;
}

/**
 * Create a base58check encoder/decoder with custom hash functions
 *
 * @param hashAlgo - Async hash function (typically double SHA-256)
 * @param hashAlgoSync - Optional sync hash function
 * @returns Base58Check encoder/decoder instance
 */
export function makeBase58check(hashAlgo: HashFunction | HashFunctionSync, hashAlgoSync: HashFunctionSync): Base58CheckSync;
export function makeBase58check(hashAlgo: HashFunction | HashFunctionSync, hashAlgoSync?: undefined): Base58CheckAsync;

/**
 * Encode bytes to base58check string asynchronously
 *
 * Uses double SHA-256 for checksum calculation
 *
 * @param arr - The input bytes to encode
 * @returns A Promise that resolves to the base58check encoded string
 */
export function toBase58check(arr: Uint8Array): Promise<string>;

/**
 * Decode a base58check string to bytes asynchronously
 *
 * Validates the checksum using double SHA-256
 *
 * @param string - The base58check encoded string
 * @param format - Output format (default: 'uint8')
 * @returns A Promise that resolves to the decoded bytes
 */
export function fromBase58check(string: string, format?: 'uint8'): Promise<Uint8ArrayBuffer>;
export function fromBase58check(string: string, format: 'arraybuffer'): Promise<ArrayBuffer>;
export function fromBase58check(string: string, format: 'buffer'): Promise<Buffer>;
export function fromBase58check(string: string, format?: OutputFormat): Promise<Uint8ArrayBuffer | ArrayBuffer | Buffer>;

/**
 * Encode bytes to base58check string synchronously
 *
 * Uses double SHA-256 for checksum calculation
 *
 * @param arr - The input bytes to encode
 * @returns The base58check encoded string
 */
export function toBase58checkSync(arr: Uint8Array): string;

/**
 * Decode a base58check string to bytes synchronously
 *
 * Validates the checksum using double SHA-256
 *
 * @param string - The base58check encoded string
 * @param format - Output format (default: 'uint8')
 * @returns The decoded bytes
 */
export function fromBase58checkSync(string: string, format?: 'uint8'): Uint8ArrayBuffer;
export function fromBase58checkSync(string: string, format: 'arraybuffer'): ArrayBuffer;
export function fromBase58checkSync(string: string, format: 'buffer'): Buffer;
export function fromBase58checkSync(string: string, format?: OutputFormat): Uint8ArrayBuffer | ArrayBuffer | Buffer;
