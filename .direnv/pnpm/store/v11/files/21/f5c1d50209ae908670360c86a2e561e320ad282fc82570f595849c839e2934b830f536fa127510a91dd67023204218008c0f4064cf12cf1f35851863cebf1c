import { assertU8, E_STRING } from './fallback/_utils.js'
import { nativeDecoderLatin1, nativeEncoder } from './fallback/platform.js'
import { encodeAsciiPrefix, encodeLatin1 } from './fallback/latin1.js'
import { assertEncoding, encodingDecoder, encodeMap, E_STRICT } from './fallback/single-byte.js'

const { TextDecoder, btoa } = globalThis

let windows1252works

// prettier-ignore
const skipNative = new Set([
  'iso-8859-1', 'iso-8859-9', 'iso-8859-11', // non-WHATWG
  'iso-8859-6', 'iso-8859-8', 'iso-8859-8-i', // slow in all 3 engines
  'iso-8859-16', // iso-8859-16 is somehow broken in WebKit, at least on CI
])

function shouldUseNative(enc) {
  // https://issues.chromium.org/issues/468458388
  // Also might be incorrectly imlemented on platforms as Latin1 (e.g. in Node.js) or regress
  // This is the most significant single-byte encoding, 'ascii' and 'latin1' alias to this
  // Even after Chrome bug is fixed, this should serve as a quick correctness check that it's actually windows-1252
  if (enc === 'windows-1252') {
    if (windows1252works === undefined) {
      windows1252works = false
      try {
        const u = new Uint8Array(9) // using 9 bytes is significant to catch the bug
        u[8] = 128
        windows1252works = new TextDecoder(enc).decode(u).codePointAt(8) === 0x20_ac
      } catch {}
    }

    return windows1252works
  }

  return !skipNative.has(enc)
}

export function createSinglebyteDecoder(encoding, loose = false) {
  if (typeof loose !== 'boolean') throw new TypeError('loose option should be boolean')
  assertEncoding(encoding)

  if (nativeDecoderLatin1 && shouldUseNative(encoding)) {
    // In try, as not all encodings might be implemented in all engines which have native TextDecoder
    try {
      const decoder = new TextDecoder(encoding, { fatal: !loose })
      return (arr) => {
        assertU8(arr)
        if (arr.byteLength === 0) return ''
        return decoder.decode(arr)
      }
    } catch {}
  }

  const jsDecoder = encodingDecoder(encoding)
  return (arr) => {
    assertU8(arr)
    if (arr.byteLength === 0) return ''
    return jsDecoder(arr, loose)
  }
}

const NON_LATIN = /[^\x00-\xFF]/ // eslint-disable-line no-control-regex

function encode(s, m) {
  const len = s.length
  const x = new Uint8Array(len)
  let i = nativeEncoder ? 0 : encodeAsciiPrefix(x, s)

  for (const len3 = len - 3; i < len3; i += 4) {
    const x0 = s.charCodeAt(i), x1 = s.charCodeAt(i + 1), x2 = s.charCodeAt(i + 2), x3 = s.charCodeAt(i + 3) // prettier-ignore
    const c0 = m[x0], c1 = m[x1], c2 = m[x2], c3 = m[x3] // prettier-ignore
    if ((!c0 && x0) || (!c1 && x1) || (!c2 && x2) || (!c3 && x3)) return null

    x[i] = c0
    x[i + 1] = c1
    x[i + 2] = c2
    x[i + 3] = c3
  }

  for (; i < len; i++) {
    const x0 = s.charCodeAt(i)
    const c0 = m[x0]
    if (!c0 && x0) return null
    x[i] = c0
  }

  return x
}

// fromBase64+btoa path is faster on everything where fromBase64 is fast
const useLatin1btoa = Uint8Array.fromBase64 && btoa

export function latin1fromString(s) {
  if (typeof s !== 'string') throw new TypeError(E_STRING)
  // max limit is to not produce base64 strings that are too long
  if (useLatin1btoa && s.length >= 1024 && s.length < 1e8) {
    try {
      return Uint8Array.fromBase64(btoa(s)) // fails on non-latin1
    } catch {
      throw new TypeError(E_STRICT)
    }
  }

  if (NON_LATIN.test(s)) throw new TypeError(E_STRICT)
  return encodeLatin1(s)
}

export function createSinglebyteEncoder(encoding, { mode = 'fatal' } = {}) {
  // TODO: replacement, truncate (replacement will need varying length)
  if (mode !== 'fatal') throw new Error('Unsupported mode')
  if (encoding === 'iso-8859-1') return latin1fromString
  const m = encodeMap(encoding) // asserts

  // No single-byte encoder produces surrogate pairs, so any surrogate is invalid
  // This needs special treatment only to decide how many replacement chars to output, one or two
  // Not much use in running isWellFormed, most likely cause of error is unmapped chars, not surrogate pairs
  return (s) => {
    if (typeof s !== 'string') throw new TypeError(E_STRING)

    // Instead of an ASCII regex check, encode optimistically - this is faster
    // Check for 8-bit string with a regex though, this is instant on 8-bit strings so doesn't hurt the ASCII fast path
    if (nativeEncoder && !NON_LATIN.test(s)) {
      const u8 = nativeEncoder.encode(s)
      if (u8.length === s.length) return u8
    }

    const res = encode(s, m)
    if (!res) throw new TypeError(E_STRICT)
    return res
  }
}

export const latin1toString = /* @__PURE__ */ createSinglebyteDecoder('iso-8859-1')
export const windows1252toString = /* @__PURE__ */ createSinglebyteDecoder('windows-1252')
export const windows1252fromString = /* @__PURE__ */ createSinglebyteEncoder('windows-1252')
