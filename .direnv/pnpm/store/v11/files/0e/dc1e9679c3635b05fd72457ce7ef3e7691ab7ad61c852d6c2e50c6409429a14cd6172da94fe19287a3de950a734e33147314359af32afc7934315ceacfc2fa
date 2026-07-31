import { E_STRICT_UNICODE } from './_utils.js'
import { isHermes } from './platform.js'
import { asciiPrefix, decodeLatin1, encodeAsciiPrefix } from './latin1.js'

export const E_STRICT = 'Input is not well-formed utf8'

const replacementPoint = 0xff_fd
const shouldUseEscapePath = isHermes // faster only on Hermes, js path beats it on normal engines
const { decodeURIComponent, escape } = globalThis

export function decodeFast(arr, loose) {
  // Fast path for ASCII prefix, this is faster than all alternatives below
  const prefix = decodeLatin1(arr, 0, asciiPrefix(arr)) // No native decoder to use, so decodeAscii is useless here
  if (prefix.length === arr.length) return prefix

  // This codepath gives a ~3x perf boost on Hermes
  if (shouldUseEscapePath && escape && decodeURIComponent) {
    const o = escape(decodeLatin1(arr, prefix.length, arr.length))
    try {
      return prefix + decodeURIComponent(o) // Latin1 to utf8
    } catch {
      if (!loose) throw new TypeError(E_STRICT)
      // Ok, we have to use manual implementation for loose decoder
    }
  }

  return prefix + decode(arr, loose, prefix.length)
}

// https://encoding.spec.whatwg.org/#utf-8-decoder
// We are most likely in loose mode, for non-loose escape & decodeURIComponent solved everything
export function decode(arr, loose, start = 0) {
  start |= 0
  const end = arr.length
  let out = ''
  const chunkSize = 0x2_00 // far below MAX_ARGUMENTS_LENGTH in npmjs.com/buffer, we use smaller chunks
  const tmpSize = Math.min(end - start, chunkSize + 1) // need 1 extra slot for last codepoint, which can be 2 charcodes
  const tmp = new Array(tmpSize).fill(0)
  let ti = 0

  for (let i = start; i < end; i++) {
    if (ti >= chunkSize) {
      tmp.length = ti // can be larger by 1 if last codepoint is two charcodes
      out += String.fromCharCode.apply(String, tmp)
      if (tmp.length <= chunkSize) tmp.push(0) // restore 1 extra slot for last codepoint
      ti = 0
    }

    const byte = arr[i]
    if (byte < 0x80) {
      tmp[ti++] = byte
      // ascii fast path is in decodeFast(), this is called only on non-ascii input
      // so we don't unroll this anymore
    } else if (byte < 0xc2) {
      if (!loose) throw new TypeError(E_STRICT)
      tmp[ti++] = replacementPoint
    } else if (byte < 0xe0) {
      // need 1 more
      if (i + 1 >= end) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        break
      }

      const byte1 = arr[i + 1]
      if (byte1 < 0x80 || byte1 > 0xbf) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        continue
      }

      i++
      tmp[ti++] = ((byte & 0x1f) << 6) | (byte1 & 0x3f)
    } else if (byte < 0xf0) {
      // need 2 more
      if (i + 1 >= end) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        break
      }

      const lower = byte === 0xe0 ? 0xa0 : 0x80
      const upper = byte === 0xed ? 0x9f : 0xbf
      const byte1 = arr[i + 1]
      if (byte1 < lower || byte1 > upper) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        continue
      }

      i++
      if (i + 1 >= end) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        break
      }

      const byte2 = arr[i + 1]
      if (byte2 < 0x80 || byte2 > 0xbf) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        continue
      }

      i++
      tmp[ti++] = ((byte & 0xf) << 12) | ((byte1 & 0x3f) << 6) | (byte2 & 0x3f)
    } else if (byte <= 0xf4) {
      // need 3 more
      if (i + 1 >= end) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        break
      }

      const lower = byte === 0xf0 ? 0x90 : 0x80
      const upper = byte === 0xf4 ? 0x8f : 0xbf
      const byte1 = arr[i + 1]
      if (byte1 < lower || byte1 > upper) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        continue
      }

      i++
      if (i + 1 >= end) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        break
      }

      const byte2 = arr[i + 1]
      if (byte2 < 0x80 || byte2 > 0xbf) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        continue
      }

      i++
      if (i + 1 >= end) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        break
      }

      const byte3 = arr[i + 1]
      if (byte3 < 0x80 || byte3 > 0xbf) {
        if (!loose) throw new TypeError(E_STRICT)
        tmp[ti++] = replacementPoint
        continue
      }

      i++
      const codePoint =
        ((byte & 0xf) << 18) | ((byte1 & 0x3f) << 12) | ((byte2 & 0x3f) << 6) | (byte3 & 0x3f)
      if (codePoint > 0xff_ff) {
        // split into char codes as String.fromCharCode is faster than String.fromCodePoint
        const u = codePoint - 0x1_00_00
        tmp[ti++] = 0xd8_00 + ((u >> 10) & 0x3_ff)
        tmp[ti++] = 0xdc_00 + (u & 0x3_ff)
      } else {
        tmp[ti++] = codePoint
      }
      // eslint-disable-next-line sonarjs/no-duplicated-branches
    } else {
      if (!loose) throw new TypeError(E_STRICT)
      tmp[ti++] = replacementPoint
    }
  }

  if (ti === 0) return out
  tmp.length = ti
  return out + String.fromCharCode.apply(String, tmp)
}

export function encode(string, loose) {
  const length = string.length
  let small = true
  let bytes = new Uint8Array(length) // assume ascii

  let i = encodeAsciiPrefix(bytes, string)
  let p = i
  for (; i < length; i++) {
    let code = string.charCodeAt(i)
    if (code < 0x80) {
      bytes[p++] = code
      // Unroll the loop a bit for faster ops
      while (true) {
        i++
        if (i >= length) break
        code = string.charCodeAt(i)
        if (code >= 0x80) break
        bytes[p++] = code
        i++
        if (i >= length) break
        code = string.charCodeAt(i)
        if (code >= 0x80) break
        bytes[p++] = code
        i++
        if (i >= length) break
        code = string.charCodeAt(i)
        if (code >= 0x80) break
        bytes[p++] = code
        i++
        if (i >= length) break
        code = string.charCodeAt(i)
        if (code >= 0x80) break
        bytes[p++] = code
      }

      if (i >= length) break
      // now, code is present and >= 0x80
    }

    if (small) {
      // TODO: use resizable array buffers? will have to return a non-resizeable one
      if (p !== i) /* c8 ignore next */ throw new Error('Unreachable') // Here, p === i (only when small is still true)
      const bytesNew = new Uint8Array(p + (length - i) * 3) // maximium can be 3x of the string length in charcodes
      bytesNew.set(bytes)
      bytes = bytesNew
      small = false
    }

    // surrogate, charcodes = [d800 + a & 3ff, dc00 + b & 3ff]; codePoint = 0x1_00_00 | (a << 10) | b
    // lead: d800 - dbff
    // trail: dc00 - dfff
    if (code >= 0xd8_00 && code < 0xe0_00) {
      // Can't be a valid trail as we already processed that below

      if (code > 0xdb_ff || i + 1 >= length) {
        // An unexpected trail or a lead at the very end of input
        if (!loose) throw new TypeError(E_STRICT_UNICODE)
        bytes[p++] = 0xef
        bytes[p++] = 0xbf
        bytes[p++] = 0xbd
        continue
      }

      const next = string.charCodeAt(i + 1) // Process valid pairs immediately
      if (next >= 0xdc_00 && next < 0xe0_00) {
        // here, codePoint is always between 0x1_00_00 and 0x11_00_00, we encode as 4 bytes
        const codePoint = (((code - 0xd8_00) << 10) | (next - 0xdc_00)) + 0x1_00_00
        bytes[p++] = (codePoint >> 18) | 0xf0
        bytes[p++] = ((codePoint >> 12) & 0x3f) | 0x80
        bytes[p++] = ((codePoint >> 6) & 0x3f) | 0x80
        bytes[p++] = (codePoint & 0x3f) | 0x80
        i++ // consume next
      } else {
        // Next is not a trail, leave next unconsumed but process unmatched lead error
        if (!loose) throw new TypeError(E_STRICT_UNICODE)
        bytes[p++] = 0xef
        bytes[p++] = 0xbf
        bytes[p++] = 0xbd
      }

      continue
    }

    // We are left with a non-pair char code above ascii, it gets encoded to 2 or 3 bytes
    if (code < 0x8_00) {
      bytes[p++] = (code >> 6) | 0xc0
      bytes[p++] = (code & 0x3f) | 0x80
    } else {
      bytes[p++] = (code >> 12) | 0xe0
      bytes[p++] = ((code >> 6) & 0x3f) | 0x80
      bytes[p++] = (code & 0x3f) | 0x80
    }
  }

  return bytes.length === p ? bytes : bytes.slice(0, p)
}
