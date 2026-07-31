import { assertU8, fromBuffer, E_STRING, E_STRICT_UNICODE } from './fallback/_utils.js'
import { E_STRICT } from './fallback/utf8.js'
import { isAscii } from 'node:buffer'

if (Buffer.TYPED_ARRAY_SUPPORT) throw new Error('Unexpected Buffer polyfill')

let decoderFatal
const decoderLoose = new TextDecoder('utf-8', { ignoreBOM: true })
const { isWellFormed } = String.prototype
const isDeno = !!globalThis.Deno

try {
  decoderFatal = new TextDecoder('utf-8', { ignoreBOM: true, fatal: true })
} catch {
  // Without ICU, Node.js doesn't support fatal option for utf-8
}

function encode(str, loose, format) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  const strLength = str.length
  if (strLength === 0) return new Uint8Array() // faster than Uint8Array.of
  let res
  if (strLength > 0x4_00 && !isDeno) {
    // Faster for large strings
    const byteLength = Buffer.byteLength(str)
    res = format === 'buffer' ? Buffer.allocUnsafe(byteLength) : Buffer.allocUnsafeSlow(byteLength)
    const written = byteLength === strLength ? res.latin1Write(str) : res.utf8Write(str)
    if (written !== byteLength) throw new Error('Failed to write all bytes') // safeguard just in case
  } else {
    res = Buffer.from(str)
  }

  // Loose and ascii do not need the check
  if (!loose && res.length !== strLength && !isWellFormed.call(str)) {
    throw new TypeError(E_STRICT_UNICODE)
  }

  return fromBuffer(res, format)
}

function decode(arr, loose = false) {
  assertU8(arr)
  const byteLength = arr.byteLength
  if (byteLength === 0) return ''
  if (byteLength > 0x6_00 && !(isDeno && loose) && isAscii(arr)) {
    // On non-ascii strings, this loses ~10% * [relative position of the first non-ascii byte] (up to 10% total)
    // On ascii strings, this wins 1.5x on loose = false and 1.3x on loose = true
    // Only makes sense for large enough strings
    const buf = Buffer.from(arr.buffer, arr.byteOffset, arr.byteLength)
    if (isDeno) return buf.toString() // Deno suffers from .latin1Slice
    return buf.latin1Slice(0, arr.byteLength) // .latin1Slice is faster than .asciiSlice
  }

  if (loose) return decoderLoose.decode(arr)
  if (decoderFatal) return decoderFatal.decode(arr)

  // We are in an env without native fatal decoder support (non-fixed Node.js without ICU)
  // Well, just recheck against encode if it contains replacement then, this is still faster than js impl
  const str = decoderLoose.decode(arr)
  if (str.includes('\uFFFD') && !Buffer.from(str).equals(arr)) throw new TypeError(E_STRICT)
  return str
}

export const utf8fromString = (str, format = 'uint8') => encode(str, false, format)
export const utf8fromStringLoose = (str, format = 'uint8') => encode(str, true, format)
export const utf8toString = (arr) => decode(arr, false)
export const utf8toStringLoose = (arr) => decode(arr, true)
