export * from './platform.js'

const Buffer = /* @__PURE__ */ (() => globalThis.Buffer)()

export function assert(condition, msg) {
  if (!condition) throw new Error(msg)
}

export function assertU8(arg) {
  if (arg && arg instanceof Uint8Array) return
  throw new TypeError('Expected an Uint8Array')
}

// On arrays in heap (<= 64) it's cheaper to copy into a pooled buffer than lazy-create the ArrayBuffer storage
export const toBuf = (x) =>
  x.byteLength <= 64 && x.BYTES_PER_ELEMENT === 1
    ? Buffer.from(x)
    : Buffer.from(x.buffer, x.byteOffset, x.byteLength)

export const E_STRING = 'Input is not a string'
export const E_STRICT_UNICODE = 'Input is not well-formed Unicode'

// Input is never pooled
export function fromUint8(arr, format) {
  switch (format) {
    case 'uint8':
      if (arr.constructor !== Uint8Array) throw new Error('Unexpected')
      return arr
    case 'arraybuffer':
      if (arr.byteLength !== arr.buffer.byteLength) throw new Error('Unexpected')
      return arr.buffer
    case 'buffer':
      if (arr.length <= 64) return Buffer.from(arr)
      return Buffer.from(arr.buffer, arr.byteOffset, arr.byteLength)
  }

  throw new TypeError('Unexpected format')
}

// Input can be pooled
export function fromBuffer(arr, format) {
  switch (format) {
    case 'uint8':
      // byteOffset check is slightly faster and covers most pooling, so it comes first
      if (arr.length <= 64 || arr.byteOffset !== 0 || arr.byteLength !== arr.buffer.byteLength) {
        return new Uint8Array(arr)
      }

      return new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength)
    case 'arraybuffer':
      return arr.buffer.byteLength === arr.byteLength
        ? arr.buffer
        : arr.buffer.slice(arr.byteOffset, arr.byteOffset + arr.byteLength)
    case 'buffer':
      if (arr.constructor !== Buffer) throw new Error('Unexpected')
      return arr
  }

  throw new TypeError('Unexpected format')
}
