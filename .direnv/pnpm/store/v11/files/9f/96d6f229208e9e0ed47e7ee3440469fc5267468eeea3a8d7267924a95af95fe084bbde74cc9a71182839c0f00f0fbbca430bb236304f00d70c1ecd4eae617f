import { assertU8, fromUint8, E_STRING } from './fallback/_utils.js'
import { nativeDecoder, nativeEncoder, isHermes } from './fallback/platform.js'
import { encodeAscii, decodeAscii } from './fallback/latin1.js'

const alphabet58 = [...'123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz']
const alphabetXRP = [...'rpshnaf39wBUDNEGHJKLM4PQRST7VWXYZ2bcdeCg65jkm8oFqi1tuvAxyz']
const codes58 = new Uint8Array(alphabet58.map((x) => x.charCodeAt(0)))
const codesXRP = new Uint8Array(alphabetXRP.map((x) => x.charCodeAt(0)))

const _0n = BigInt(0)
const _1n = BigInt(1)
const _8n = BigInt(8)
const _32n = BigInt(32)
const _58n = BigInt(58)
const _0xffffffffn = BigInt(0xff_ff_ff_ff)

let table // 15 * 82, diagonal, <1kb
const fromMaps = new Map()

const E_CHAR = 'Invalid character in base58 input'

const shouldUseBigIntFrom = isHermes // faster only on Hermes, numbers path beats it on normal engines

function toBase58core(arr, alphabet, codes) {
  assertU8(arr)
  const length = arr.length
  if (length === 0) return ''

  const ZERO = alphabet[0]
  let zeros = 0
  while (zeros < length && arr[zeros] === 0) zeros++

  if (length > 60) {
    // Slow path. Can be optimized ~10%, but the main factor is /58n division anyway, so doesn't matter much
    let x = _0n
    for (let i = 0; i < arr.length; i++) x = (x << _8n) | BigInt(arr[i])

    let out = ''
    while (x) {
      const d = x / _58n
      out = alphabet[Number(x - _58n * d)] + out
      x = d
    }

    return ZERO.repeat(zeros) + out
  }

  // We run fast mode operations only on short (<=60 bytes) inputs, via precomputation table
  if (!table) {
    table = []
    let x = _1n
    for (let i = 0; i < 15; i++) {
      // Convert x to base 58 digits
      const in58 = []
      let y = x
      while (y) {
        const d = y / _58n
        in58.push(Number(y - _58n * d))
        y = d
      }

      table.push(new Uint8Array(in58))
      x <<= _32n
    }
  }

  const res = []
  {
    let j = 0
    // We group each 4 bytes into 32-bit chunks
    // Not using u32arr to not deal with remainder + BE/LE differences
    for (let i = length - 1; i >= 0; i -= 4) {
      let c
      if (i > 2) {
        c = (arr[i] | (arr[i - 1] << 8) | (arr[i - 2] << 16) | (arr[i - 3] << 24)) >>> 0
      } else if (i > 1) {
        c = arr[i] | (arr[i - 1] << 8) | (arr[i - 2] << 16)
      } else {
        c = i === 1 ? arr[i] | (arr[i - 1] << 8) : arr[i]
      }

      const row = table[j++]
      if (c === 0) continue
      const olen = res.length
      const nlen = row.length
      let k = 0
      for (; k < olen; k++) res[k] += c * row[k]
      while (k < nlen) res.push(c * row[k++])
    }
  }

  // We can now do a single scan over regular numbers under MAX_SAFE_INTEGER
  // Note: can't use int32 operations on them, as they are outside of 2**32 range
  // This is faster though
  {
    let carry = 0
    let i = 0
    while (i < res.length) {
      const c = res[i] + carry
      carry = Math.floor(c / 58)
      res[i++] = c - carry * 58
    }

    while (carry) {
      const c = carry
      carry = Math.floor(c / 58)
      res.push(c - carry * 58)
    }
  }

  if (nativeDecoder) {
    const oa = new Uint8Array(res.length)
    let j = 0
    for (let i = res.length - 1; i >= 0; i--) oa[j++] = codes[res[i]]
    return ZERO.repeat(zeros) + decodeAscii(oa)
  }

  let out = ''
  for (let i = res.length - 1; i >= 0; i--) out += alphabet[res[i]]
  return ZERO.repeat(zeros) + out
}

function fromBase58core(str, alphabet, codes, format = 'uint8') {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  const length = str.length
  if (length === 0) return fromUint8(new Uint8Array(), format)

  const zeroC = codes[0]
  let zeros = 0
  while (zeros < length && str.charCodeAt(zeros) === zeroC) zeros++

  let fromMap = fromMaps.get(alphabet)
  if (!fromMap) {
    fromMap = new Int8Array(256).fill(-1)
    for (let i = 0; i < 58; i++) fromMap[alphabet[i].charCodeAt(0)] = i
    fromMaps.set(alphabet, fromMap)
  }

  const size = zeros + (((length - zeros + 1) * 3) >> 2) // 3/4 rounded up, larger than ~0.73 coef to fit everything
  const res = new Uint8Array(size)
  let at = size // where is the first significant byte written

  if (shouldUseBigIntFrom) {
    let x = _0n

    // nativeEncoder gives a benefit here
    if (nativeEncoder) {
      const codes = encodeAscii(str, E_CHAR)
      for (let i = zeros; i < length; i++) {
        const c = fromMap[codes[i]]
        if (c < 0) throw new SyntaxError(E_CHAR)
        x = x * _58n + BigInt(c)
      }
    } else {
      for (let i = zeros; i < length; i++) {
        const charCode = str.charCodeAt(i)
        const c = fromMap[charCode]
        if (charCode > 255 || c < 0) throw new SyntaxError(E_CHAR)
        x = x * _58n + BigInt(c)
      }
    }

    while (x) {
      let y = Number(x & _0xffffffffn)
      x >>= _32n
      res[--at] = y & 0xff
      y >>>= 8
      if (!x && !y) break
      res[--at] = y & 0xff
      y >>>= 8
      if (!x && !y) break
      res[--at] = y & 0xff
      y >>>= 8
      if (!x && !y) break
      res[--at] = y & 0xff
    }
  } else {
    for (let i = zeros; i < length; i++) {
      const charCode = str.charCodeAt(i)
      let c = fromMap[charCode]
      if (charCode > 255 || c < 0) throw new SyntaxError(E_CHAR)

      let k = size - 1
      for (;;) {
        if (c === 0 && k < at) break
        c += 58 * res[k]
        res[k] = c & 0xff
        c >>>= 8
        k--
        // unroll a bit
        if (c === 0 && k < at) break
        c += 58 * res[k]
        res[k] = c & 0xff
        c >>>= 8
        k--
        if (c === 0 && k < at) break
        c += 58 * res[k]
        res[k] = c & 0xff
        c >>>= 8
        k--
        if (c === 0 && k < at) break
        c += 58 * res[k]
        res[k] = c & 0xff
        c >>>= 8
        k--
      }

      at = k + 1
      if (c !== 0 || at < zeros) /* c8 ignore next */ throw new Error('Unexpected') // unreachable
    }
  }

  return fromUint8(res.slice(at - zeros), format)
}

export const toBase58 = (arr) => toBase58core(arr, alphabet58, codes58)
export const fromBase58 = (str, format) => fromBase58core(str, alphabet58, codes58, format)
export const toBase58xrp = (arr) => toBase58core(arr, alphabetXRP, codesXRP)
export const fromBase58xrp = (str, format) => fromBase58core(str, alphabetXRP, codesXRP, format)
