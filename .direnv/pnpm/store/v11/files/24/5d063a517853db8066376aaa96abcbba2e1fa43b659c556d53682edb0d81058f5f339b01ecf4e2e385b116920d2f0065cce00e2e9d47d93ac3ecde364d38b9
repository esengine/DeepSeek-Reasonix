import {
  nativeEncoder,
  nativeDecoder,
  nativeDecoderLatin1,
  nativeBuffer,
  encodeCharcodes,
  isHermes,
  isDeno,
  isLE,
} from './platform.js'

const atob = /* @__PURE__ */ (() => globalThis.atob)()
const web64 = /* @__PURE__ */ (() => Uint8Array.prototype.toBase64)()

// See http://stackoverflow.com/a/22747272/680742, which says that lowest limit is in Chrome, with 0xffff args
// On Hermes, actual max is 0x20_000 minus current stack depth, 1/16 of that should be safe
const maxFunctionArgs = 0x20_00

// toBase64+atob path is faster on everything where fromBase64 is fast
const useLatin1atob = web64 && atob

export function asciiPrefix(arr) {
  let p = 0 // verified ascii bytes
  const length = arr.length
  // Threshold tested on Hermes (worse on <=48, better on >=52)
  // Also on v8 arrs of size <=64 might be on heap and using Uint32Array on them is unoptimal
  if (length > 64) {
    // Speedup with u32
    const u32start = (4 - (arr.byteOffset & 3)) % 4 // offset start by this many bytes for alignment
    for (; p < u32start; p++) if (arr[p] >= 0x80) return p
    const u32length = ((arr.byteLength - u32start) / 4) | 0
    const u32 = new Uint32Array(arr.buffer, arr.byteOffset + u32start, u32length)
    let i = 0
    for (const last3 = u32length - 3; ; p += 16, i += 4) {
      if (i >= last3) break // loop is fast enough for moving this here to be _very_ useful, likely due to array access checks
      const a = u32[i]
      const b = u32[i + 1]
      const c = u32[i + 2]
      const d = u32[i + 3]
      // "(a | b | c | d) & mask" is slower on Hermes though faster on v8
      if (a & 0x80_80_80_80 || b & 0x80_80_80_80 || c & 0x80_80_80_80 || d & 0x80_80_80_80) break
    }

    for (; i < u32length; p += 4, i++) if (u32[i] & 0x80_80_80_80) break
  }

  for (; p < length; p++) if (arr[p] >= 0x80) return p
  return length
}

// Capable of decoding Uint16Array to UTF-16 as well as Uint8Array to Latin-1
export function decodeLatin1(arr, start = 0, stop = arr.length) {
  start |= 0
  stop |= 0
  const total = stop - start
  if (total === 0) return ''

  if (
    useLatin1atob &&
    total >= 256 &&
    total < 1e8 &&
    arr.toBase64 === web64 &&
    arr.BYTES_PER_ELEMENT === 1
  ) {
    const sliced = start === 0 && stop === arr.length ? arr : arr.subarray(start, stop)
    return atob(sliced.toBase64())
  }

  if (total > maxFunctionArgs) {
    let prefix = ''
    for (let i = start; i < stop; ) {
      const i1 = Math.min(stop, i + maxFunctionArgs)
      prefix += String.fromCharCode.apply(String, arr.subarray(i, i1))
      i = i1
    }

    return prefix
  }

  const sliced = start === 0 && stop === arr.length ? arr : arr.subarray(start, stop)
  return String.fromCharCode.apply(String, sliced)
}

// Unchecked for well-formedness, raw. Expects Uint16Array input
export const decodeUCS2 =
  nativeBuffer && isLE && !isDeno
    ? (u16, stop = u16.length) => {
        // TODO: fast path for BE, perhaps faster path for Deno. Note that decoder replaces, this function doesn't
        if (stop > 32) return nativeBuffer.from(u16.buffer, u16.byteOffset, stop * 2).ucs2Slice() // from 64 bytes, below are in heap
        return decodeLatin1(u16, 0, stop)
      }
    : (u16, stop = u16.length) => decodeLatin1(u16, 0, stop)

// Does not check input, uses best available method
// Building an array for this is only faster than proper string concatenation when TextDecoder or native Buffer are available
export const decodeAscii = nativeBuffer
  ? (a) =>
      // Buffer is faster on Node.js (but only for long enough data), if we know that output is ascii
      a.byteLength >= 0x3_00 && !isDeno
        ? nativeBuffer.from(a.buffer, a.byteOffset, a.byteLength).latin1Slice(0, a.byteLength) // .latin1Slice is faster than .asciiSlice
        : nativeDecoder.decode(a) // On Node.js, utf8 decoder is faster than latin1
  : nativeDecoderLatin1
    ? (a) => nativeDecoderLatin1.decode(a) // On browsers (specifically WebKit), latin1 decoder is faster than utf8
    : (a) =>
        decodeLatin1(
          a instanceof Uint8Array ? a : new Uint8Array(a.buffer, a.byteOffset, a.byteLength)
        )

/* eslint-disable @exodus/mutable/no-param-reassign-prop-only */

export function encodeAsciiPrefix(x, s) {
  let i = 0
  for (const len3 = s.length - 3; i < len3; i += 4) {
    const x0 = s.charCodeAt(i), x1 = s.charCodeAt(i + 1), x2 = s.charCodeAt(i + 2), x3 = s.charCodeAt(i + 3) // prettier-ignore
    if ((x0 | x1 | x2 | x3) >= 128) break
    x[i] = x0
    x[i + 1] = x1
    x[i + 2] = x2
    x[i + 3] = x3
  }

  return i
}

/* eslint-enable @exodus/mutable/no-param-reassign-prop-only */

// Warning: can be used only on checked strings, converts strings to 8-bit
export const encodeLatin1 = (str) => encodeCharcodes(str, new Uint8Array(str.length))

// Expects nativeEncoder to be present
const useEncodeInto = /* @__PURE__ */ (() => isHermes && nativeEncoder?.encodeInto)()
export const encodeAscii = useEncodeInto
  ? (str, ERR) => {
      // Much faster in Hermes
      const codes = new Uint8Array(str.length + 4) // overshoot by a full utf8 char
      const info = nativeEncoder.encodeInto(str, codes)
      if (info.read !== str.length || info.written !== str.length) throw new SyntaxError(ERR) // non-ascii
      return codes.subarray(0, str.length)
    }
  : nativeBuffer
    ? (str, ERR) => {
        // TextEncoder is slow on Node.js 24 / 25 (was ok on 22)
        // Node.js Buffer.from always returns 8-byte aligned allocations, this is safe for e.g. conversion to Uint32Array
        const codes = nativeBuffer.from(str, 'utf8') // ascii/latin1 coerces, we need to check
        if (codes.length !== str.length) throw new SyntaxError(ERR) // non-ascii
        return new Uint8Array(codes.buffer, codes.byteOffset, codes.byteLength)
      }
    : (str, ERR) => {
        const codes = nativeEncoder.encode(str)
        if (codes.length !== str.length) throw new SyntaxError(ERR) // non-ascii
        return codes
      }
