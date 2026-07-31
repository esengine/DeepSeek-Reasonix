import { assertU8, E_STRING } from './fallback/_utils.js'
import { nativeEncoder } from './fallback/platform.js'
import { decodeAscii, encodeAscii, encodeLatin1 } from './fallback/latin1.js'

const alphabet = [...'qpzry9x8gf2tvdw0s3jn54khce6mua7l']
const BECH32 = 1
const BECH32M = 0x2b_c8_30_a3

const E_SIZE = 'Input length is out of range'
const E_PREFIX = 'Missing or invalid prefix'
const E_MIXED = 'Mixed-case string'
const E_PADDING = 'Padding is invalid'
const E_CHECKSUM = 'Invalid checksum'
const E_CHARACTER = 'Non-bech32 character'

// nativeEncoder path uses encodeAscii which asserts ascii, otherwise we have 0-255 bytes from encodeLatin1
const c2x = new Int8Array(nativeEncoder ? 128 : 256).fill(-1)
const x2c = new Uint8Array(32)
for (let i = 0; i < alphabet.length; i++) {
  const c = alphabet[i].charCodeAt(0)
  c2x[c] = i
  x2c[i] = c
}

// checksum size is 30 bits, 0x3f_ff_ff_ff
// The good thing about the checksum is that it's linear over every bit
const poly0 = new Uint32Array(32) // just precache all possible ones, it's only 1 KiB
const p = (x) => ((x & 0x1_ff_ff_ff) << 5) ^ poly0[x >> 25]
for (let i = 0; i < 32; i++) {
  poly0[i] =
    (i & 0b0_0001 ? 0x3b_6a_57_b2 : 0) ^
    (i & 0b0_0010 ? 0x26_50_8e_6d : 0) ^
    (i & 0b0_0100 ? 0x1e_a1_19_fa : 0) ^
    (i & 0b0_1000 ? 0x3d_42_33_dd : 0) ^
    (i & 0b1_0000 ? 0x2a_14_62_b3 : 0)
}

// 7 KiB more for faster p6/p8
const poly1 = new Uint32Array(32)
const poly2 = new Uint32Array(32)
const poly3 = new Uint32Array(32)
const poly4 = new Uint32Array(32)
const poly5 = new Uint32Array(32)
const poly6 = new Uint32Array(32)
const poly7 = new Uint32Array(32)
for (let i = 0; i < 32; i++) {
  // poly0[i] === p(p(p(p(p(p(i))))))
  poly1[i] = p(poly0[i]) // aka p(p(p(p(p(p(i << 5))))))
  poly2[i] = p(poly1[i]) // aka p(p(p(p(p(p(i << 10))))))
  poly3[i] = p(poly2[i]) // aka p(p(p(p(p(p(i << 15))))))
  poly4[i] = p(poly3[i]) // aka p(p(p(p(p(p(i << 20))))))
  poly5[i] = p(poly4[i]) // aka p(p(p(p(p(p(i << 25))))))
  poly6[i] = p(poly5[i])
  poly7[i] = p(poly6[i])
}

function p6(x) {
  // Same as: return p(p(p(p(p(p(x))))))
  const x0 = x & 0x1f
  const x1 = (x >> 5) & 0x1f
  const x2 = (x >> 10) & 0x1f
  const x3 = (x >> 15) & 0x1f
  const x4 = (x >> 20) & 0x1f
  const x5 = (x >> 25) & 0x1f
  return poly0[x0] ^ poly1[x1] ^ poly2[x2] ^ poly3[x3] ^ poly4[x4] ^ poly5[x5]
}

function p8(x) {
  // Same as: return p(p(p(p(p(p(p(p(x))))))))
  const x0 = x & 0x1f
  const x1 = (x >> 5) & 0x1f
  const x2 = (x >> 10) & 0x1f
  const x3 = (x >> 15) & 0x1f
  const x4 = (x >> 20) & 0x1f
  const x5 = (x >> 25) & 0x1f
  return poly2[x0] ^ poly3[x1] ^ poly4[x2] ^ poly5[x3] ^ poly6[x4] ^ poly7[x5]
}

// p(p(p(p(p(p(chk) ^ x0) ^ x1) ^ x2) ^ x3) ^ x4) ^ x5 === p6(chk) ^ merge(x0, x1, x2, x3, x4, x5)
const merge = (a, b, c, d, e, f) => f ^ (e << 5) ^ (d << 10) ^ (c << 15) ^ (b << 20) ^ (a << 25)

const prefixCache = new Map() // Cache 10 of them

function pPrefix(prefix) {
  if (prefix === 'bc') return 0x2_31_80_43 // perf
  const cached = prefixCache.get(prefix)
  if (cached !== undefined) return cached

  // bech32_hrp_expand(s): [ord(x) >> 5 for x in s] + [0] + [ord(x) & 31 for x in s]
  // We can do this in a single scan due to linearity, but it's not very beneficial
  let chk = 1 // it starts with one (see def bech32_polymod in BIP_0173)
  const length = prefix.length
  for (let i = 0; i < length; i++) {
    const c = prefix.charCodeAt(i)
    if (c < 33 || c > 126) throw new Error(E_PREFIX) // each character having a value in the range [33-126]
    chk = p(chk) ^ (c >> 5)
  }

  chk = p(chk) // <= for + [0]
  for (let i = 0; i < length; i++) {
    const c = prefix.charCodeAt(i)
    chk = p(chk) ^ (c & 0x1f)
  }

  if (prefixCache.size < 10) prefixCache.set(prefix, chk)
  return chk
}

function toBech32enc(prefix, bytes, limit, encoding) {
  if (typeof prefix !== 'string' || !prefix) throw new TypeError(E_PREFIX)
  if (typeof limit !== 'number') throw new TypeError(E_SIZE)
  assertU8(bytes)
  const bytesLength = bytes.length
  const wordsLength = Math.ceil((bytesLength * 8) / 5)
  if (!(prefix.length + 7 + wordsLength <= limit)) throw new TypeError(E_SIZE)
  prefix = prefix.toLowerCase()
  const out = new Uint8Array(wordsLength + 6)

  let chk = pPrefix(prefix)
  let i = 0, j = 0 // prettier-ignore

  // This loop is just an optimization of the next one
  for (const length4 = bytesLength - 4; i < length4; i += 5, j += 8) {
    const b0 = bytes[i], b1 = bytes[i + 1], b2 = bytes[i + 2], b3 = bytes[i + 3], b4 = bytes[i + 4] // prettier-ignore
    const x0 = b0 >> 3
    const x1 = ((b0 << 2) & 0x1f) | (b1 >> 6)
    const x2 = (b1 >> 1) & 0x1f
    const x3 = ((b1 << 4) & 0x1f) | (b2 >> 4)
    const x4 = ((b2 << 1) & 0x1f) | (b3 >> 7)
    const x5 = (b3 >> 2) & 0x1f
    const x6 = ((b3 << 3) & 0x1f) | (b4 >> 5)
    const x7 = b4 & 0x1f
    chk = merge(x2, x3, x4, x5, x6, x7) ^ poly0[x1] ^ poly1[x0] ^ p8(chk)
    out[j] = x2c[x0]
    out[j + 1] = x2c[x1]
    out[j + 2] = x2c[x2]
    out[j + 3] = x2c[x3]
    out[j + 4] = x2c[x4]
    out[j + 5] = x2c[x5]
    out[j + 6] = x2c[x6]
    out[j + 7] = x2c[x7]
  }

  let value = 0, bits = 0 // prettier-ignore
  for (; i < bytesLength; i++) {
    value = ((value & 0xf) << 8) | bytes[i]
    bits += 3
    const x = (value >> bits) & 0x1f
    chk = p(chk) ^ x
    out[j++] = x2c[x]
    if (bits >= 5) {
      bits -= 5
      const x = (value >> bits) & 0x1f
      chk = p(chk) ^ x
      out[j++] = x2c[x]
    }
  }

  if (bits > 0) {
    const x = (value << (5 - bits)) & 0x1f
    chk = p(chk) ^ x
    out[j++] = x2c[x]
  }

  chk = encoding ^ p6(chk)
  out[j++] = x2c[(chk >> 25) & 0x1f]
  out[j++] = x2c[(chk >> 20) & 0x1f]
  out[j++] = x2c[(chk >> 15) & 0x1f]
  out[j++] = x2c[(chk >> 10) & 0x1f]
  out[j++] = x2c[(chk >> 5) & 0x1f]
  out[j++] = x2c[(chk >> 0) & 0x1f]

  return prefix + '1' + decodeAscii(out) // suboptimal in barebones, but actually ok in Hermes for not to care atm
}

function assertDecodeArgs(str, limit) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  if (typeof limit !== 'number' || str.length < 8 || !(str.length <= limit)) throw new Error(E_SIZE)
}

// this is instant on 8-bit strings
const NON_LATIN = /[^\x00-\xFF]/ // eslint-disable-line no-control-regex

function fromBech32enc(str, limit, encoding) {
  assertDecodeArgs(str, limit)
  const lower = str.toLowerCase()
  if (str !== lower) {
    if (str !== str.toUpperCase()) throw new Error(E_MIXED)
    str = lower
  }

  const split = str.lastIndexOf('1')
  if (split <= 0) throw new Error(E_PREFIX)
  const prefix = str.slice(0, split)
  const charsLength = str.length - split - 1
  const wordsLength = charsLength - 6
  if (wordsLength < 0) throw new Error(E_SIZE)
  const bytesLength = (wordsLength * 5) >> 3
  const slice = str.slice(split + 1)
  if (!nativeEncoder && NON_LATIN.test(slice)) throw new SyntaxError(E_CHARACTER) // otherwise can't use encodeLatin1
  const c = nativeEncoder ? encodeAscii(slice, E_CHARACTER) : encodeLatin1(slice) // suboptimal, but only affects non-Hermes barebones
  const bytes = new Uint8Array(bytesLength)

  let chk = pPrefix(prefix)
  let i = 0, j = 0 // prettier-ignore

  // This loop is just an optimization of the next one
  for (const length7 = wordsLength - 7; i < length7; i += 8, j += 5) {
    const c0 = c[i], c1 = c[i + 1], c2 = c[i + 2], c3 = c[i + 3], c4 = c[i + 4], c5 = c[i + 5], c6 = c[i + 6], c7 = c[i + 7] // prettier-ignore
    const x0 = c2x[c0], x1 = c2x[c1], x2 = c2x[c2], x3 = c2x[c3], x4 = c2x[c4], x5 = c2x[c5], x6 = c2x[c6], x7 = c2x[c7] // prettier-ignore
    if (x0 < 0 || x1 < 0 || x2 < 0 || x3 < 0 || x4 < 0 || x5 < 0 || x6 < 0 || x7 < 0) throw new SyntaxError(E_CHARACTER) // prettier-ignore
    chk = merge(x2, x3, x4, x5, x6, x7) ^ poly0[x1] ^ poly1[x0] ^ p8(chk)
    bytes[j] = (x0 << 3) | (x1 >> 2)
    bytes[j + 1] = (((x1 << 6) | (x2 << 1)) & 0xff) | (x3 >> 4)
    bytes[j + 2] = ((x3 << 4) & 0xff) | (x4 >> 1)
    bytes[j + 3] = ((((x4 << 5) | x5) << 2) & 0xff) | (x6 >> 3)
    bytes[j + 4] = ((x6 << 5) & 0xff) | x7
  }

  let value = 0, bits = 0 // prettier-ignore
  for (; i < wordsLength; i++) {
    const x = c2x[c[i]]
    if (x < 0) throw new SyntaxError(E_CHARACTER)
    chk = p(chk) ^ x
    value = (value << 5) | x
    bits += 5
    if (bits >= 8) {
      bits -= 8
      bytes[j++] = (value >> bits) & 0xff
    }
  }

  if (bits >= 5 || (value << (8 - bits)) & 0xff) throw new Error(E_PADDING)

  // Checksum
  {
    const c0 = c[i], c1 = c[i + 1], c2 = c[i + 2], c3 = c[i + 3], c4 = c[i + 4], c5 = c[i + 5] // prettier-ignore
    const x0 = c2x[c0], x1 = c2x[c1], x2 = c2x[c2], x3 = c2x[c3], x4 = c2x[c4], x5 = c2x[c5] // prettier-ignore
    if (x0 < 0 || x1 < 0 || x2 < 0 || x3 < 0 || x4 < 0 || x5 < 0) throw new SyntaxError(E_CHARACTER)
    if ((merge(x0, x1, x2, x3, x4, x5) ^ p6(chk)) !== encoding) throw new Error(E_CHECKSUM)
  }

  return { prefix, bytes }
}

// This is designed to be a very quick check, skipping all other validation
export function getPrefix(str, limit = 90) {
  assertDecodeArgs(str, limit)
  const split = str.lastIndexOf('1')
  if (split <= 0) throw new Error(E_PREFIX)
  return str.slice(0, split).toLowerCase()
}

export const toBech32 = (prefix, bytes, limit = 90) => toBech32enc(prefix, bytes, limit, BECH32)
export const fromBech32 = (str, limit = 90) => fromBech32enc(str, limit, BECH32)
export const toBech32m = (prefix, bytes, limit = 90) => toBech32enc(prefix, bytes, limit, BECH32M)
export const fromBech32m = (str, limit = 90) => fromBech32enc(str, limit, BECH32M)
