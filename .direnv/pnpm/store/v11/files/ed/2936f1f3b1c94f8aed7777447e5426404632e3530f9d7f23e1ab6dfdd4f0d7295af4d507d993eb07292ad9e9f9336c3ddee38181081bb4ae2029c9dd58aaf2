import { assertTypedArray } from './assert.js'

const Buffer = globalThis.Buffer // Buffer is optional

export function typedView(arr, format) {
  assertTypedArray(arr)
  switch (format) {
    case 'uint8':
      if (arr.constructor === Uint8Array) return arr // fast path
      return new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength)
    case 'buffer':
      if (arr.constructor === Buffer && Buffer.isBuffer(arr)) return arr
      return Buffer.from(arr.buffer, arr.byteOffset, arr.byteLength)
  }

  throw new TypeError('Unexpected format')
}

export function typedCopyBytes(arr, format) {
  assertTypedArray(arr)
  if (!(arr instanceof Uint8Array)) {
    arr = new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength)
  }

  switch (format) {
    case 'uint8':
      return Uint8Array.from(arr) // never pooled
    case 'buffer':
      return Buffer.from(arr)
    case 'arraybuffer':
      return Uint8Array.from(arr).buffer
  }

  throw new TypeError('Unexpected format')
}
