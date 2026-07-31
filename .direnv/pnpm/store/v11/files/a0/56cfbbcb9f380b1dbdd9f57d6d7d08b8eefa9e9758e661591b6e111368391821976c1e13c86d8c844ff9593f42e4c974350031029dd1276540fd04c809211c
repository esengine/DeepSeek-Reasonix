/**
 * Decode / encode the legacy multi-byte encodings according to the
 * [Encoding standard](https://encoding.spec.whatwg.org/)
 * ([§10](https://encoding.spec.whatwg.org/#legacy-multi-byte-chinese-(simplified)-encodings),
 * [§11](https://encoding.spec.whatwg.org/#legacy-multi-byte-chinese-(traditional)-encodings),
 * [§12](https://encoding.spec.whatwg.org/#legacy-multi-byte-japanese-encodings),
 * [§13](https://encoding.spec.whatwg.org/#legacy-multi-byte-korean-encodings)).
 *
 * ```js
 * import { createMultibyteDecoder, createMultibyteEncoder } from '@exodus/bytes/multi-byte.js'
 * ```
 *
 * > [!WARNING]
 * > This is a lower-level API for legacy multi-byte encodings.
 * >
 * > For a safe WHATWG Encoding-compatible API, see `@exodus/bytes/encoding.js` import (and variants of it).
 * >
 * > Be sure to know what you are doing and check documentation when directly using encodings from this file.
 *
 * Supports all legacy multi-byte encodings listed in the WHATWG Encoding standard:
 * `gbk`, `gb18030`, `big5`, `euc-jp`, `iso-2022-jp`, `shift_jis`, `euc-kr`.
 *
 * @module @exodus/bytes/multi-byte.js
 */

/// <reference types="node" />

import type { Uint8ArrayBuffer } from './array.js';

/**
 * Create a decoder for a supported legacy multi-byte `encoding`, given its lowercased name `encoding`.
 *
 * Returns a function `decode(arr, stream = false)` that decodes bytes to a string.
 *
 * The returned function will maintain internal state while `stream = true` is used, allowing it to
 * handle incomplete multi-byte sequences across multiple calls.
 * State is reset when `stream = false` or when the function is called without the `stream` parameter.
 *
 * @param encoding - The encoding name (e.g., 'gbk', 'gb18030', 'big5', 'euc-jp', 'iso-2022-jp', 'shift_jis', 'euc-kr')
 * @param loose - If true, replaces unmapped bytes with replacement character instead of throwing (default: false)
 * @returns A function that decodes bytes to string, with optional streaming support
 */
export function createMultibyteDecoder(
  encoding: string,
  loose?: boolean
): (arr: Uint8Array, stream?: boolean) => string;

/**
 * Create an encoder for a supported legacy multi-byte `encoding`, given its lowercased name `encoding`.
 *
 * Returns a function `encode(string)` that encodes a string to bytes.
 *
 * In `'fatal'` mode (default), will throw on non well-formed strings or any codepoints which could
 * not be encoded in the target encoding.
 *
 * @param encoding - The encoding name (e.g., 'gbk', 'gb18030', 'big5', 'euc-jp', 'iso-2022-jp', 'shift_jis', 'euc-kr')
 * @param options - Encoding options
 * @param options.mode - Encoding mode (default: 'fatal'). Currently, only 'fatal' mode is supported.
 * @returns A function that encodes string to bytes
 */
export function createMultibyteEncoder(
  encoding: string,
  options?: { mode?: 'fatal' }
): (string: string) => Uint8ArrayBuffer;
