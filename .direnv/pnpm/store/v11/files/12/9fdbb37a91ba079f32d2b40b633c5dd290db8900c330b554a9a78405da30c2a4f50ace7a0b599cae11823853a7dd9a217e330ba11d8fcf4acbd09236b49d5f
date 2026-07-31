/**
 * TypedArray utils and conversions.
 *
 * ```js
 * import { typedCopyBytes, typedView } from '@exodus/bytes/array.js'
 * ```
 *
 * @module @exodus/bytes/array.js
 */

/// <reference types="node" />

// >= TypeScript 5.9 made Uint8Array templated with <> and defaulted to ArrayBufferLike
// which would incorrectly accept SharedArrayBuffer instances.
// < TypeScript 5.7 doesn't support templates for Uint8Array.
// So this type is defined as a workaround to evaluate to Uint8Array<ArrayBuffer> on all versions of TypeScript.

/**
 * This is `Uint8Array<ArrayBuffer>`
 * (as opposed to `Uint8Array<SharedArrayBuffer>` and `Uint8Array<ArrayBufferLike>`)
 * on TypeScript versions that support that distinction.
 *
 * On TypeScript < 5.7, this is just `Uint8Array`, as it's not a template there.
 */
export type Uint8ArrayBuffer = ReturnType<typeof Uint8Array.from>;

/**
 * This is `Uint16Array<ArrayBuffer>`
 * (as opposed to `Uint16Array<SharedArrayBuffer>` and `Uint16Array<ArrayBufferLike>`)
 * on TypeScript versions that support that distinction.
 *
 * On TypeScript < 5.7, this is just `Uint16Array`, as it's not a template there.
 */
export type Uint16ArrayBuffer = ReturnType<typeof Uint16Array.from>;

/**
 * This is `Uint32Array<ArrayBuffer>`
 * (as opposed to `Uint32Array<SharedArrayBuffer>` and `Uint32Array<ArrayBufferLike>`)
 * on TypeScript versions that support that distinction.
 *
 * On TypeScript < 5.7, this is just `Uint32Array`, as it's not a template there.
 */
export type Uint32ArrayBuffer = ReturnType<typeof Uint32Array.from>;

/**
 * Output format for typed array conversions
 */
export type OutputFormat = 'uint8' | 'arraybuffer' | 'buffer';

/**
 * Create a view of a TypedArray in the specified format (`'uint8'` or `'buffer'`)
 *
 * > [!IMPORTANT]
 * > Does not copy data, returns a view on the same underlying buffer
 *
 * > [!WARNING]
 * > Viewing `Uint16Array` (or other with `BYTES_PER_ELEMENT > 1`) as bytes
 * > is platform endianness-dependent.
 *
 * @param arr - The input TypedArray
 * @param format - The desired output format (`'uint8'` or `'buffer'`)
 * @returns A view on the same underlying buffer
 */
export function typedView(arr: ArrayBufferView, format: 'uint8'): Uint8Array;
export function typedView(arr: ArrayBufferView, format: 'buffer'): Buffer;
export function typedView(arr: ArrayBufferView, format: 'uint8' | 'buffer'): Uint8Array | Buffer;

/**
 * Create a copy of TypedArray underlying bytes in the specified format (`'uint8'`, `'buffer'`, or `'arraybuffer'`)
 *
 * This does not copy _values_, but copies the underlying bytes.
 * The result is similar to that of `typedView()`, but this function provides a copy, not a view of the same memory.
 *
 * > [!WARNING]
 * > Copying underlying bytes from `Uint16Array` (or other with `BYTES_PER_ELEMENT > 1`)
 * > is platform endianness-dependent.
 *
 * > [!NOTE]
 * > Buffer might be pooled.
 * > Uint8Array return values are not pooled and match their underlying ArrayBuffer.
 *
 * @param arr - The input TypedArray
 * @param format - The desired output format (`'uint8'`, `'buffer'`, or `'arraybuffer'`)
 * @returns A copy of the underlying buffer
 */
export function typedCopyBytes(arr: ArrayBufferView, format: 'uint8'): Uint8Array;
export function typedCopyBytes(arr: ArrayBufferView, format: 'arraybuffer'): ArrayBuffer;
export function typedCopyBytes(arr: ArrayBufferView, format: 'buffer'): Buffer;
export function typedCopyBytes(arr: ArrayBufferView, format: OutputFormat): Uint8Array | ArrayBuffer | Buffer;
