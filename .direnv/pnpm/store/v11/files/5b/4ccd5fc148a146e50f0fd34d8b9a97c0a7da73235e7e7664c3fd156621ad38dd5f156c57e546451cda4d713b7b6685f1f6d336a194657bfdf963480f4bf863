import { assertEmptyRest } from './assert.js'
import { assertU8, fromUint8, fromBuffer, E_STRING } from './fallback/_utils.js'
import { isHermes } from './fallback/platform.js'
import { decodeLatin1, encodeLatin1 } from './fallback/latin1.js'
import * as js from './fallback/base64.js'

// See https://datatracker.ietf.org/doc/html/rfc4648

// base64:    A-Za-z0-9+/ and = if padding not disabled
// base64url: A-Za-z0-9_- and = if padding enabled

const { Buffer, atob, btoa } = globalThis // Buffer is optional, only used when native
const haveNativeBuffer = Buffer && !Buffer.TYPED_ARRAY_SUPPORT
const { toBase64: web64 } = Uint8Array.prototype // Modern engines have this

const { E_CHAR, E_PADDING, E_LENGTH, E_LAST } = js

// faster only on Hermes (and a little in old Chrome), js path beats it on normal engines
const shouldUseBtoa = btoa && isHermes
const shouldUseAtob = atob && isHermes

// For native Buffer codepaths only
const isBuffer = (x) => x.constructor === Buffer && Buffer.isBuffer(x)
const toBuffer = (x) => (isBuffer(x) ? x : Buffer.from(x.buffer, x.byteOffset, x.byteLength))

function maybeUnpad(res, padding) {
  if (padding) return res
  const at = res.indexOf('=', res.length - 3)
  return at === -1 ? res : res.slice(0, at)
}

function maybePad(res, padding) {
  return padding && res.length % 4 !== 0 ? res + '='.repeat(4 - (res.length % 4)) : res
}

const toUrl = (x) => x.replaceAll('+', '-').replaceAll('/', '_')
const haveWeb = (x) => web64 && x.toBase64 === web64

export function toBase64(x, { padding = true } = {}) {
  assertU8(x)
  if (haveWeb(x)) return padding ? x.toBase64() : x.toBase64({ omitPadding: !padding }) // Modern, optionless is slightly faster
  if (haveNativeBuffer) return maybeUnpad(toBuffer(x).base64Slice(0, x.byteLength), padding) // Older Node.js
  if (shouldUseBtoa) return maybeUnpad(btoa(decodeLatin1(x)), padding)
  return js.toBase64(x, false, padding) // Fallback
}

// NOTE: base64url omits padding by default
export function toBase64url(x, { padding = false } = {}) {
  assertU8(x)
  if (haveWeb(x)) return x.toBase64({ alphabet: 'base64url', omitPadding: !padding }) // Modern
  if (haveNativeBuffer) return maybePad(toBuffer(x).base64urlSlice(0, x.byteLength), padding) // Older Node.js
  if (shouldUseBtoa) return maybeUnpad(toUrl(btoa(decodeLatin1(x))), padding)
  return js.toBase64(x, true, padding) // Fallback
}

// Unlike Buffer.from(), throws on invalid input (non-base64 symbols and incomplete chunks)
// Unlike Buffer.from() and Uint8Array.fromBase64(), does not allow spaces
// NOTE: Always operates in strict mode for last chunk

// By default accepts both padded and non-padded variants, only strict base64
export function fromBase64(str, options) {
  if (typeof options === 'string') options = { format: options } // Compat due to usage, TODO: remove
  if (!options) return fromBase64common(str, false, 'both', 'uint8', null)
  const { format = 'uint8', padding = 'both', ...rest } = options
  return fromBase64common(str, false, padding, format, rest)
}

// By default accepts only non-padded strict base64url
export function fromBase64url(str, options) {
  if (!options) return fromBase64common(str, true, false, 'uint8', null)
  const { format = 'uint8', padding = false, ...rest } = options
  return fromBase64common(str, true, padding, format, rest)
}

// By default accepts both padded and non-padded variants, base64 or base64url
export function fromBase64any(str, { format = 'uint8', padding = 'both', ...rest } = {}) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  const isBase64url = !str.includes('+') && !str.includes('/') // likely to fail fast, as most input is non-url, also double scan is faster than regex
  return fromBase64common(str, isBase64url, padding, format, rest)
}

function fromBase64common(str, isBase64url, padding, format, rest) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  if (rest !== null) assertEmptyRest(rest)
  const auto = padding === 'both' ? str.endsWith('=') : undefined
  // Older JSC supporting Uint8Array.fromBase64 lacks proper checks
  if (padding === true || auto === true) {
    if (str.length % 4 !== 0) throw new SyntaxError(E_PADDING) // JSC misses this
    if (str[str.length - 3] === '=') throw new SyntaxError(E_PADDING) // no more than two = at the end
  } else if (padding === false || auto === false) {
    if (str.length % 4 === 1) throw new SyntaxError(E_LENGTH) // JSC misses this in fromBase64
    if (padding === false && str.endsWith('=')) {
      throw new SyntaxError('Did not expect padding in base64 input') // inclusion is checked separately
    }
  } else {
    throw new TypeError('Invalid padding option')
  }

  return fromBase64impl(str, isBase64url, padding, format)
}

// ASCII whitespace is U+0009 TAB, U+000A LF, U+000C FF, U+000D CR, or U+0020 SPACE
const ASCII_WHITESPACE = /[\t\n\f\r ]/ // non-u for JSC perf

function noWhitespaceSeen(str, arr) {
  const at = str.indexOf('=', str.length - 3)
  const paddingLength = at >= 0 ? str.length - at : 0
  const chars = str.length - paddingLength
  const e = chars % 4 // extra chars past blocks of 4
  const b = arr.length - ((chars - e) / 4) * 3 // remaining bytes not covered by full blocks of chars
  return (e === 0 && b === 0) || (e === 2 && b === 1) || (e === 3 && b === 2)
}

let fromBase64impl
if (Uint8Array.fromBase64) {
  // NOTICE: this is actually slower than our JS impl in older JavaScriptCore and (slightly) in SpiderMonkey, but faster on V8 and new JavaScriptCore
  fromBase64impl = (str, isBase64url, padding, format) => {
    const alphabet = isBase64url ? 'base64url' : 'base64'

    let arr
    if (padding === true) {
      // Padding is required from user, and we already checked that string length is divisible by 4
      // Padding might still be wrong due to whitespace, but in that case native impl throws expected error
      arr = Uint8Array.fromBase64(str, { alphabet, lastChunkHandling: 'strict' })
    } else {
      try {
        const padded = str.length % 4 > 0 ? `${str}${'='.repeat(4 - (str.length % 4))}` : str
        arr = Uint8Array.fromBase64(padded, { alphabet, lastChunkHandling: 'strict' })
      } catch (err) {
        // Normalize error: whitespace in input could have caused added padding to be invalid
        // But reporting that as a padding error would be confusing
        throw ASCII_WHITESPACE.test(str) ? new SyntaxError(E_CHAR) : err
      }
    }

    // We don't allow whitespace in input, but that can be rechecked based on output length
    // All other chars are checked natively
    if (!noWhitespaceSeen(str, arr)) throw new SyntaxError(E_CHAR)
    return fromUint8(arr, format)
  }
} else if (haveNativeBuffer) {
  fromBase64impl = (str, isBase64url, padding, format) => {
    const size = Buffer.byteLength(str, 'base64')
    const arr = Buffer.allocUnsafeSlow(size) // non-pooled
    if (arr.base64Write(str) !== size) throw new SyntaxError(E_PADDING)
    // Rechecking by re-encoding is cheaper than regexes on Node.js
    const got = isBase64url ? maybeUnpad(str, padding === false) : maybePad(str, padding !== true)
    const valid = isBase64url ? arr.base64urlSlice(0, arr.length) : arr.base64Slice(0, arr.length)
    if (got !== valid) throw new SyntaxError(E_PADDING)
    return fromBuffer(arr, format) // fully checked
  }
} else if (shouldUseAtob) {
  // atob is faster than manual parsing on Hermes
  fromBase64impl = (str, isBase64url, padding, format) => {
    let arr
    if (isBase64url) {
      if (/[\t\n\f\r +/]/.test(str)) throw new SyntaxError(E_CHAR) // atob verifies other invalid input
      str = str.replaceAll('-', '+').replaceAll('_', '/') // from url to normal
    }

    try {
      arr = encodeLatin1(atob(str))
    } catch {
      throw new SyntaxError(E_CHAR) // convert atob errors
    }

    if (!isBase64url && !noWhitespaceSeen(str, arr)) throw new SyntaxError(E_CHAR) // base64url checks input above

    if (arr.length % 3 !== 0) {
      // Check last chunk to be strict if it was incomplete
      const expected = toBase64(arr.subarray(-(arr.length % 3))) // str is normalized to non-url already
      const end = str.length % 4 === 0 ? str.slice(-4) : str.slice(-(str.length % 4)).padEnd(4, '=')
      if (expected !== end) throw new SyntaxError(E_LAST)
    }

    return fromUint8(arr, format)
  }
} else {
  fromBase64impl = (str, isBase64url, padding, format) =>
    fromUint8(js.fromBase64(str, isBase64url), format) // validated in js
}
