import { isAscii } from 'node:buffer'
import { assertU8, toBuf, E_STRING } from './fallback/_utils.js'
import { isDeno, isLE } from './fallback/platform.js'
import { asciiPrefix } from './fallback/latin1.js'
import { encodingMapper, encodingDecoder, encodeMap, E_STRICT } from './fallback/single-byte.js'

function latin1Prefix(arr, start) {
  let p = start | 0
  const length = arr.length
  for (const len3 = length - 3; p < len3; p += 4) {
    if ((arr[p] & 0xe0) === 0x80) return p
    if ((arr[p + 1] & 0xe0) === 0x80) return p + 1
    if ((arr[p + 2] & 0xe0) === 0x80) return p + 2
    if ((arr[p + 3] & 0xe0) === 0x80) return p + 3
  }

  for (; p < length; p++) {
    if ((arr[p] & 0xe0) === 0x80) return p
  }

  return length
}

export function createSinglebyteDecoder(encoding, loose = false) {
  if (typeof loose !== 'boolean') throw new TypeError('loose option should be boolean')
  if (isDeno) {
    const jsDecoder = encodingDecoder(encoding) // asserts
    return (arr) => {
      assertU8(arr)
      if (arr.byteLength === 0) return ''
      if (isAscii(arr)) return toBuf(arr).toString()
      return jsDecoder(arr, loose) // somewhy faster on Deno anyway, TODO: optimize?
    }
  }

  const isLatin1 = encoding === 'iso-8859-1'
  const latin1path = encoding === 'windows-1252'
  const { incomplete, mapper } = encodingMapper(encoding) // asserts
  return (arr) => {
    assertU8(arr)
    if (arr.byteLength === 0) return ''
    if (isLatin1 || isAscii(arr)) return toBuf(arr).latin1Slice(0, arr.byteLength) // .latin1Slice is faster than .asciiSlice

    // Node.js TextDecoder is broken, so we can't use it. It's also slow anyway

    let prefixBytes = asciiPrefix(arr)
    let prefix = ''
    if (latin1path) prefixBytes = latin1Prefix(arr, prefixBytes)
    if (prefixBytes > 64 || prefixBytes === arr.length) {
      prefix = toBuf(arr).latin1Slice(0, prefixBytes) // .latin1Slice is faster than .asciiSlice
      if (prefixBytes === arr.length) return prefix
    }

    const b = toBuf(mapper(arr, prefix.length)) // prefix.length can mismatch prefixBytes
    if (!isLE) b.swap16()
    const suffix = b.ucs2Slice(0, b.byteLength)
    if (!loose && incomplete && suffix.includes('\uFFFD')) throw new TypeError(E_STRICT)
    return prefix + suffix
  }
}

const NON_LATIN = /[^\x00-\xFF]/ // eslint-disable-line no-control-regex

function encode(s, m) {
  const len = s.length
  let i = 0
  const b = Buffer.from(s, 'utf-16le') // aligned
  if (!isLE) b.swap16()
  const x = new Uint16Array(b.buffer, b.byteOffset, b.byteLength / 2)
  for (const len3 = len - 3; i < len3; i += 4) {
    const x0 = x[i], x1 = x[i + 1], x2 = x[i + 2], x3 = x[i + 3] // prettier-ignore
    const c0 = m[x0], c1 = m[x1], c2 = m[x2], c3 = m[x3] // prettier-ignore
    if (!(c0 && c1 && c2 && c3) && ((!c0 && x0) || (!c1 && x1) || (!c2 && x2) || (!c3 && x3))) return null // prettier-ignore
    x[i] = c0
    x[i + 1] = c1
    x[i + 2] = c2
    x[i + 3] = c3
  }

  for (; i < len; i++) {
    const x0 = x[i]
    const c0 = m[x0]
    if (!c0 && x0) return null
    x[i] = c0
  }

  return new Uint8Array(x)
}

export function latin1fromString(s) {
  if (typeof s !== 'string') throw new TypeError(E_STRING)
  if (NON_LATIN.test(s)) throw new TypeError(E_STRICT)
  const ab = new ArrayBuffer(s.length)
  Buffer.from(ab).latin1Write(s)
  return new Uint8Array(ab)
}

export function createSinglebyteEncoder(encoding, { mode = 'fatal' } = {}) {
  // TODO: replacement, truncate (replacement will need varying length)
  if (mode !== 'fatal') throw new Error('Unsupported mode')
  if (encoding === 'iso-8859-1') return latin1fromString
  const m = encodeMap(encoding) // asserts

  return (s) => {
    if (typeof s !== 'string') throw new TypeError(E_STRING)

    // Instead of an ASCII regex check, encode optimistically - this is faster
    // Check for 8-bit string with a regex though, this is instant on 8-bit strings so doesn't hurt the ASCII fast path
    if (!NON_LATIN.test(s)) {
      const byteLength = Buffer.byteLength(s)
      // ascii/latin1 coerces, we need to check
      if (byteLength === s.length) {
        const ab = new ArrayBuffer(byteLength)
        Buffer.from(ab).latin1Write(s)
        return new Uint8Array(ab)
      }
    }

    const res = encode(s, m)
    if (!res) throw new TypeError(E_STRICT)
    return res
  }
}

export const latin1toString = /* @__PURE__ */ createSinglebyteDecoder('iso-8859-1')
export const windows1252toString = /* @__PURE__ */ createSinglebyteDecoder('windows-1252')
export const windows1252fromString = /* @__PURE__ */ createSinglebyteEncoder('windows-1252')
