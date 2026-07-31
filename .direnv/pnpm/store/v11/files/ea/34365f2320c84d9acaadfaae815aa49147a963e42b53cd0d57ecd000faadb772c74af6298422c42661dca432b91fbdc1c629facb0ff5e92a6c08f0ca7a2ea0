import { assertU8 } from './_utils.js'
import { nativeEncoder, nativeDecoder, isHermes } from './platform.js'
import { encodeAscii, decodeAscii } from './latin1.js'

// See https://datatracker.ietf.org/doc/html/rfc4648

const BASE32_HELPERS = [{}, {}, {}]
const BASE32_ALPHABETS = [
  [...'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'], // RFC 4648, #6
  [...'0123456789ABCDEFGHIJKLMNOPQRSTUV'], // RFC 4648, #7
  [...'0123456789ABCDEFGHJKMNPQRSTVWXYZ'], // Crockford, base (see extra below in fromMap)
]

export const E_CHAR = 'Invalid character in base32 input'
export const E_PADDING = 'Invalid base32 padding'
export const E_LENGTH = 'Invalid base32 length'
export const E_LAST = 'Invalid last chunk'

const useTemplates = isHermes // Faster on Hermes and JSC, but we use it only on Hermes

// We construct output by concatenating chars, this seems to be fine enough on modern JS engines
export function toBase32(arr, mode, padding) {
  assertU8(arr)
  const fullChunks = Math.floor(arr.length / 5)
  const fullChunksBytes = fullChunks * 5
  let o = ''
  let i = 0

  const alphabet = BASE32_ALPHABETS[mode]
  const helpers = BASE32_HELPERS[mode]
  if (!helpers.pairs) {
    helpers.pairs = []
    if (nativeDecoder) {
      // Lazy to save memory in case if this is not needed
      helpers.codepairs = new Uint16Array(32 * 32)
      const u16 = helpers.codepairs
      const u8 = new Uint8Array(u16.buffer, u16.byteOffset, u16.byteLength) // write as 1-byte to ignore BE/LE difference
      for (let i = 0; i < 32; i++) {
        const ic = alphabet[i].charCodeAt(0)
        for (let j = 0; j < 32; j++) u8[(i << 6) | (j << 1)] = u8[(j << 6) | ((i << 1) + 1)] = ic
      }
    } else {
      const p = helpers.pairs
      for (let i = 0; i < 32; i++) {
        for (let j = 0; j < 32; j++) p.push(`${alphabet[i]}${alphabet[j]}`)
      }
    }
  }

  const { pairs, codepairs } = helpers

  // Fast path for complete blocks
  // This whole loop can be commented out, the algorithm won't change, it's just an optimization of the next loop
  if (nativeDecoder) {
    const oa = new Uint16Array(fullChunks * 4)
    for (let j = 0; i < fullChunksBytes; i += 5) {
      const a = arr[i]
      const b = arr[i + 1]
      const c = arr[i + 2]
      const d = arr[i + 3]
      const e = arr[i + 4]
      const x0 = (a << 2) | (b >> 6) // 8 + 8 - 5 - 5 = 6 left
      const x1 = ((b & 0x3f) << 4) | (c >> 4) // 6 + 8 - 5 - 5 = 4 left
      const x2 = ((c & 0xf) << 6) | (d >> 2) // 4 + 8 - 5 - 5 = 2 left
      const x3 = ((d & 0x3) << 8) | e // 2 + 8 - 5 - 5 = 0 left
      oa[j] = codepairs[x0]
      oa[j + 1] = codepairs[x1]
      oa[j + 2] = codepairs[x2]
      oa[j + 3] = codepairs[x3]
      j += 4
    }

    o = decodeAscii(oa)
  } else if (useTemplates) {
    // Templates are faster only on Hermes and JSC. Browsers have TextDecoder anyway
    for (; i < fullChunksBytes; i += 5) {
      const a = arr[i]
      const b = arr[i + 1]
      const c = arr[i + 2]
      const d = arr[i + 3]
      const e = arr[i + 4]
      const x0 = (a << 2) | (b >> 6) // 8 + 8 - 5 - 5 = 6 left
      const x1 = ((b & 0x3f) << 4) | (c >> 4) // 6 + 8 - 5 - 5 = 4 left
      const x2 = ((c & 0xf) << 6) | (d >> 2) // 4 + 8 - 5 - 5 = 2 left
      const x3 = ((d & 0x3) << 8) | e // 2 + 8 - 5 - 5 = 0 left
      o += `${pairs[x0]}${pairs[x1]}${pairs[x2]}${pairs[x3]}`
    }
  } else {
    for (; i < fullChunksBytes; i += 5) {
      const a = arr[i]
      const b = arr[i + 1]
      const c = arr[i + 2]
      const d = arr[i + 3]
      const e = arr[i + 4]
      const x0 = (a << 2) | (b >> 6) // 8 + 8 - 5 - 5 = 6 left
      const x1 = ((b & 0x3f) << 4) | (c >> 4) // 6 + 8 - 5 - 5 = 4 left
      const x2 = ((c & 0xf) << 6) | (d >> 2) // 4 + 8 - 5 - 5 = 2 left
      const x3 = ((d & 0x3) << 8) | e // 2 + 8 - 5 - 5 = 0 left
      o += pairs[x0]
      o += pairs[x1]
      o += pairs[x2]
      o += pairs[x3]
    }
  }

  // If we have something left, process it with a full algo
  let carry = 0
  let shift = 3 // First byte needs to be shifted by 3 to get 5 bits
  for (; i < arr.length; i++) {
    const x = arr[i]
    o += alphabet[carry | (x >> shift)] // shift >= 3, so this fits
    if (shift >= 5) {
      shift -= 5
      o += alphabet[(x >> shift) & 0x1f]
    }

    carry = (x << (5 - shift)) & 0x1f
    shift += 3 // Each byte prints 5 bits and leaves 3 bits
  }

  if (shift !== 3) o += alphabet[carry] // shift 3 means we have no carry left
  if (padding) o += ['', '======', '====', '===', '='][arr.length - fullChunksBytes]

  return o
}

// TODO: can this be optimized? This only affects non-Hermes barebone engines though
const mapSize = nativeEncoder ? 128 : 65_536 // we have to store 64 KiB map or recheck everything if we can't decode to byte array

export function fromBase32(str, mode) {
  let inputLength = str.length
  while (str[inputLength - 1] === '=') inputLength--
  const paddingLength = str.length - inputLength
  const tailLength = inputLength % 8
  const mainLength = inputLength - tailLength // multiples of 8
  if (![0, 2, 4, 5, 7].includes(tailLength)) throw new SyntaxError(E_LENGTH) // fast verification
  if (paddingLength > 7 || (paddingLength !== 0 && str.length % 8 !== 0)) {
    throw new SyntaxError(E_PADDING)
  }

  const alphabet = BASE32_ALPHABETS[mode]
  const helpers = BASE32_HELPERS[mode]

  if (!helpers.fromMap) {
    helpers.fromMap = new Int8Array(mapSize).fill(-1) // no regex input validation here, so we map all other bytes to -1 and recheck sign
    const m = helpers.fromMap
    alphabet.forEach((c, i) => {
      m[c.charCodeAt(0)] = m[c.toLowerCase().charCodeAt(0)] = i
    })

    if (mode === 2) {
      // Extra Crockford mapping
      m[73] = m[76] = m[105] = m[108] = m[49] // ILil -> 1
      m[79] = m[111] = m[48] // Oo -> 0
    }
  }

  const m = helpers.fromMap

  const arr = new Uint8Array(Math.floor((inputLength * 5) / 8))
  let at = 0
  let i = 0

  if (nativeEncoder) {
    const codes = encodeAscii(str, E_CHAR)
    for (; i < mainLength; i += 8) {
      // each 5 bits, grouped 5 * 4 = 20
      const x0 = codes[i]
      const x1 = codes[i + 1]
      const x2 = codes[i + 2]
      const x3 = codes[i + 3]
      const x4 = codes[i + 4]
      const x5 = codes[i + 5]
      const x6 = codes[i + 6]
      const x7 = codes[i + 7]
      const a = (m[x0] << 15) | (m[x1] << 10) | (m[x2] << 5) | m[x3]
      const b = (m[x4] << 15) | (m[x5] << 10) | (m[x6] << 5) | m[x7]
      if (a < 0 || b < 0) throw new SyntaxError(E_CHAR)
      arr[at] = a >> 12
      arr[at + 1] = (a >> 4) & 0xff
      arr[at + 2] = ((a << 4) & 0xff) | (b >> 16)
      arr[at + 3] = (b >> 8) & 0xff
      arr[at + 4] = b & 0xff
      at += 5
    }
  } else {
    for (; i < mainLength; i += 8) {
      // each 5 bits, grouped 5 * 4 = 20
      const x0 = str.charCodeAt(i)
      const x1 = str.charCodeAt(i + 1)
      const x2 = str.charCodeAt(i + 2)
      const x3 = str.charCodeAt(i + 3)
      const x4 = str.charCodeAt(i + 4)
      const x5 = str.charCodeAt(i + 5)
      const x6 = str.charCodeAt(i + 6)
      const x7 = str.charCodeAt(i + 7)
      const a = (m[x0] << 15) | (m[x1] << 10) | (m[x2] << 5) | m[x3]
      const b = (m[x4] << 15) | (m[x5] << 10) | (m[x6] << 5) | m[x7]
      if (a < 0 || b < 0) throw new SyntaxError(E_CHAR)
      arr[at] = a >> 12
      arr[at + 1] = (a >> 4) & 0xff
      arr[at + 2] = ((a << 4) & 0xff) | (b >> 16)
      arr[at + 3] = (b >> 8) & 0xff
      arr[at + 4] = b & 0xff
      at += 5
    }
  }

  // Last block, valid tailLength: 0 2 4 5 7, checked already
  // We check last chunk to be strict
  if (tailLength < 2) return arr
  const ab = (m[str.charCodeAt(i++)] << 5) | m[str.charCodeAt(i++)]
  if (ab < 0) throw new SyntaxError(E_CHAR)
  arr[at++] = ab >> 2
  if (tailLength < 4) {
    if (ab & 0x3) throw new SyntaxError(E_LAST)
    return arr
  }

  const cd = (m[str.charCodeAt(i++)] << 5) | m[str.charCodeAt(i++)]
  if (cd < 0) throw new SyntaxError(E_CHAR)
  arr[at++] = ((ab << 6) & 0xff) | (cd >> 4)
  if (tailLength < 5) {
    if (cd & 0xf) throw new SyntaxError(E_LAST)
    return arr
  }

  const e = m[str.charCodeAt(i++)]
  if (e < 0) throw new SyntaxError(E_CHAR)
  arr[at++] = ((cd << 4) & 0xff) | (e >> 1) // 4 + 4
  if (tailLength < 7) {
    if (e & 0x1) throw new SyntaxError(E_LAST)
    return arr
  }

  const fg = (m[str.charCodeAt(i++)] << 5) | m[str.charCodeAt(i++)]
  if (fg < 0) throw new SyntaxError(E_CHAR)
  arr[at++] = ((e << 7) & 0xff) | (fg >> 3) // 1 + 5 + 2
  // Can't be 8, so no h
  if (fg & 0x7) throw new SyntaxError(E_LAST)
  return arr
}
