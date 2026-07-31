import { nativeEncoder, nativeDecoder } from './platform.js'
import { encodeAscii, decodeAscii } from './latin1.js'

// See https://datatracker.ietf.org/doc/html/rfc4648

const BASE64 = [...'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/']
const BASE64URL = [...'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_']
const BASE64_HELPERS = {}
const BASE64URL_HELPERS = {}

export const E_CHAR = 'Invalid character in base64 input'
export const E_PADDING = 'Invalid base64 padding'
export const E_LENGTH = 'Invalid base64 length'
export const E_LAST = 'Invalid last chunk'

// We construct output by concatenating chars, this seems to be fine enough on modern JS engines
// Expects a checked Uint8Array
export function toBase64(arr, isURL, padding) {
  const fullChunks = (arr.length / 3) | 0
  const fullChunksBytes = fullChunks * 3
  let o = ''
  let i = 0

  const alphabet = isURL ? BASE64URL : BASE64
  const helpers = isURL ? BASE64URL_HELPERS : BASE64_HELPERS
  if (!helpers.pairs) {
    helpers.pairs = []
    if (nativeDecoder) {
      // Lazy to save memory in case if this is not needed
      helpers.codepairs = new Uint16Array(64 * 64)
      const u16 = helpers.codepairs
      const u8 = new Uint8Array(u16.buffer, u16.byteOffset, u16.byteLength) // write as 1-byte to ignore BE/LE difference
      for (let i = 0; i < 64; i++) {
        const ic = alphabet[i].charCodeAt(0)
        for (let j = 0; j < 64; j++) u8[(i << 7) | (j << 1)] = u8[(j << 7) | ((i << 1) + 1)] = ic
      }
    } else {
      const p = helpers.pairs
      for (let i = 0; i < 64; i++) {
        for (let j = 0; j < 64; j++) p.push(`${alphabet[i]}${alphabet[j]}`)
      }
    }
  }

  const { pairs, codepairs } = helpers

  // Fast path for complete blocks
  // This whole loop can be commented out, the algorithm won't change, it's just an optimization of the next loop
  if (nativeDecoder) {
    const oa = new Uint16Array(fullChunks * 2)
    let j = 0
    for (const last = arr.length - 11; i < last; i += 12, j += 8) {
      const x0 = arr[i]
      const x1 = arr[i + 1]
      const x2 = arr[i + 2]
      const x3 = arr[i + 3]
      const x4 = arr[i + 4]
      const x5 = arr[i + 5]
      const x6 = arr[i + 6]
      const x7 = arr[i + 7]
      const x8 = arr[i + 8]
      const x9 = arr[i + 9]
      const x10 = arr[i + 10]
      const x11 = arr[i + 11]
      oa[j] = codepairs[(x0 << 4) | (x1 >> 4)]
      oa[j + 1] = codepairs[((x1 & 0x0f) << 8) | x2]
      oa[j + 2] = codepairs[(x3 << 4) | (x4 >> 4)]
      oa[j + 3] = codepairs[((x4 & 0x0f) << 8) | x5]
      oa[j + 4] = codepairs[(x6 << 4) | (x7 >> 4)]
      oa[j + 5] = codepairs[((x7 & 0x0f) << 8) | x8]
      oa[j + 6] = codepairs[(x9 << 4) | (x10 >> 4)]
      oa[j + 7] = codepairs[((x10 & 0x0f) << 8) | x11]
    }

    // i < last here is equivalent to i < fullChunksBytes
    for (const last = arr.length - 2; i < last; i += 3, j += 2) {
      const a = arr[i]
      const b = arr[i + 1]
      const c = arr[i + 2]
      oa[j] = codepairs[(a << 4) | (b >> 4)]
      oa[j + 1] = codepairs[((b & 0x0f) << 8) | c]
    }

    o = decodeAscii(oa)
  } else {
    // This can be optimized by ~25% with templates on Hermes, but this codepath is not called on Hermes, it uses btoa
    // Check git history for templates version
    for (; i < fullChunksBytes; i += 3) {
      const a = arr[i]
      const b = arr[i + 1]
      const c = arr[i + 2]
      o += pairs[(a << 4) | (b >> 4)]
      o += pairs[((b & 0x0f) << 8) | c]
    }
  }

  // If we have something left, process it with a full algo
  let carry = 0
  let shift = 2 // First byte needs to be shifted by 2 to get 6 bits
  const length = arr.length
  for (; i < length; i++) {
    const x = arr[i]
    o += alphabet[carry | (x >> shift)] // shift >= 2, so this fits
    if (shift === 6) {
      shift = 0
      o += alphabet[x & 0x3f]
    }

    carry = (x << (6 - shift)) & 0x3f
    shift += 2 // Each byte prints 6 bits and leaves 2 bits
  }

  if (shift !== 2) o += alphabet[carry] // shift 2 means we have no carry left
  if (padding) o += ['', '==', '='][length - fullChunksBytes]

  return o
}

// TODO: can this be optimized? This only affects non-Hermes barebone engines though
const mapSize = nativeEncoder ? 128 : 65_536 // we have to store 64 KiB map or recheck everything if we can't decode to byte array

export function fromBase64(str, isURL) {
  let inputLength = str.length
  while (str[inputLength - 1] === '=') inputLength--
  const paddingLength = str.length - inputLength
  const tailLength = inputLength % 4
  const mainLength = inputLength - tailLength // multiples of 4
  if (tailLength === 1) throw new SyntaxError(E_LENGTH)
  if (paddingLength > 3 || (paddingLength !== 0 && str.length % 4 !== 0)) {
    throw new SyntaxError(E_PADDING)
  }

  const alphabet = isURL ? BASE64URL : BASE64
  const helpers = isURL ? BASE64URL_HELPERS : BASE64_HELPERS

  if (!helpers.fromMap) {
    helpers.fromMap = new Int8Array(mapSize).fill(-1) // no regex input validation here, so we map all other bytes to -1 and recheck sign
    alphabet.forEach((c, i) => (helpers.fromMap[c.charCodeAt(0)] = i))
  }

  const m = helpers.fromMap

  const arr = new Uint8Array(Math.floor((inputLength * 3) / 4))
  let at = 0
  let i = 0

  if (nativeEncoder) {
    const codes = encodeAscii(str, E_CHAR)
    for (; i < mainLength; i += 4) {
      const c0 = codes[i]
      const c1 = codes[i + 1]
      const c2 = codes[i + 2]
      const c3 = codes[i + 3]
      const a = (m[c0] << 18) | (m[c1] << 12) | (m[c2] << 6) | m[c3]
      if (a < 0) throw new SyntaxError(E_CHAR)
      arr[at] = a >> 16
      arr[at + 1] = (a >> 8) & 0xff
      arr[at + 2] = a & 0xff
      at += 3
    }
  } else {
    for (; i < mainLength; i += 4) {
      const c0 = str.charCodeAt(i)
      const c1 = str.charCodeAt(i + 1)
      const c2 = str.charCodeAt(i + 2)
      const c3 = str.charCodeAt(i + 3)
      const a = (m[c0] << 18) | (m[c1] << 12) | (m[c2] << 6) | m[c3]
      if (a < 0) throw new SyntaxError(E_CHAR)
      arr[at] = a >> 16
      arr[at + 1] = (a >> 8) & 0xff
      arr[at + 2] = a & 0xff
      at += 3
    }
  }

  // Can be 0, 2 or 3, verified by padding checks already
  if (tailLength < 2) return arr // 0
  const ab = (m[str.charCodeAt(i++)] << 6) | m[str.charCodeAt(i++)]
  if (ab < 0) throw new SyntaxError(E_CHAR)
  arr[at++] = ab >> 4
  if (tailLength < 3) {
    if (ab & 0xf) throw new SyntaxError(E_LAST)
    return arr // 2
  }

  const c = m[str.charCodeAt(i++)]
  if (c < 0) throw new SyntaxError(E_CHAR)
  arr[at++] = ((ab << 4) & 0xff) | (c >> 2)
  if (c & 0x3) throw new SyntaxError(E_LAST)
  return arr // 3
}
