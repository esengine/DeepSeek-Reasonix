import { assertU8, fromUint8, E_STRING, E_STRICT_UNICODE } from './fallback/_utils.js'
import { nativeDecoder, nativeEncoder } from './fallback/platform.js'
import * as js from './fallback/utf8.auto.js'

// ignoreBOM: true means that BOM will be left as-is, i.e. will be present in the output
// We don't want to strip anything unexpectedly
const decoderLoose = nativeDecoder
const decoderFatal = nativeDecoder
  ? new TextDecoder('utf-8', { ignoreBOM: true, fatal: true })
  : null
const { isWellFormed } = String.prototype

function deLoose(str, loose, res) {
  if (loose || str.length === res.length) return res // length is equal only for ascii, which is automatically fine
  if (isWellFormed) {
    // We have a fast native method
    if (isWellFormed.call(str)) return res
    throw new TypeError(E_STRICT_UNICODE)
  }

  // Recheck if the string was encoded correctly
  let start = 0
  const last = res.length - 3
  // Search for EFBFBD (3-byte sequence)
  while (start <= last) {
    const pos = res.indexOf(0xef, start)
    if (pos === -1 || pos > last) break
    start = pos + 1
    if (res[pos + 1] === 0xbf && res[pos + 2] === 0xbd) {
      // Found a replacement char in output, need to recheck if we encoded the input correctly
      if (js.decodeFast && !nativeDecoder && str.length < 1e7) {
        // This is ~2x faster than decode in Hermes
        try {
          if (encodeURI(str) !== null) return res // guard against optimizing out
        } catch {}
      } else if (str === decode(res)) return res
      throw new TypeError(E_STRICT_UNICODE)
    }
  }

  return res
}

function encode(str, loose = false) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  if (str.length === 0) return new Uint8Array() // faster than Uint8Array.of
  if (nativeEncoder || !js.encode) return deLoose(str, loose, nativeEncoder.encode(str))
  // No reason to use unescape + encodeURIComponent: it's slower than JS on normal engines, and modern Hermes already has TextEncoder
  return js.encode(str, loose)
}

function decode(arr, loose = false) {
  assertU8(arr)
  if (arr.byteLength === 0) return ''
  if (nativeDecoder || !js.decodeFast) {
    return loose ? decoderLoose.decode(arr) : decoderFatal.decode(arr) // Node.js and browsers
  }

  return js.decodeFast(arr, loose)
}

export const utf8fromString = (str, format = 'uint8') => fromUint8(encode(str, false), format)
export const utf8fromStringLoose = (str, format = 'uint8') => fromUint8(encode(str, true), format)
export const utf8toString = (arr) => decode(arr, false)
export const utf8toStringLoose = (arr) => decode(arr, true)
