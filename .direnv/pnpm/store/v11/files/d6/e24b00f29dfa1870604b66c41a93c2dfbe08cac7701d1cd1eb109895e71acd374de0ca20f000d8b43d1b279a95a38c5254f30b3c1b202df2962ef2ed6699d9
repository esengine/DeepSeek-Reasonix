import { E_STRING } from './_utils.js'
import { nativeDecoder, nativeEncoder, decode2string } from './platform.js'
import { encodeAscii, decodeAscii } from './latin1.js'

let hexArray // array of 256 bytes converted to two-char hex strings
let hexCodes // hexArray converted to u16 code pairs
let dehexArray
const _00 = 0x30_30 // '00' string in hex, the only allowed char pair to generate 0 byte
const _ff = 0x66_66 // 'ff' string in hex, max allowed char pair (larger than 'FF' string)
const allowed = '0123456789ABCDEFabcdef'

export const E_HEX = 'Input is not a hex string'

// Expects a checked Uint8Array
export function toHex(arr) {
  if (!hexArray) hexArray = Array.from({ length: 256 }, (_, i) => i.toString(16).padStart(2, '0'))
  const length = arr.length // this helps Hermes

  // Only old browsers use this, barebone engines don't have TextDecoder
  // But Hermes can use this when it (hopefully) implements TextDecoder
  if (nativeDecoder) {
    if (!hexCodes) {
      hexCodes = new Uint16Array(256)
      const u8 = new Uint8Array(hexCodes.buffer, hexCodes.byteOffset, hexCodes.byteLength)
      for (let i = 0; i < 256; i++) {
        const pair = hexArray[i]
        u8[2 * i] = pair.charCodeAt(0)
        u8[2 * i + 1] = pair.charCodeAt(1)
      }
    }

    const oa = new Uint16Array(length)
    let i = 0
    for (const last3 = arr.length - 3; ; i += 4) {
      if (i >= last3) break // loop is fast enough for moving this here to be useful on JSC
      const x0 = arr[i]
      const x1 = arr[i + 1]
      const x2 = arr[i + 2]
      const x3 = arr[i + 3]
      oa[i] = hexCodes[x0]
      oa[i + 1] = hexCodes[x1]
      oa[i + 2] = hexCodes[x2]
      oa[i + 3] = hexCodes[x3]
    }

    for (; i < length; i++) oa[i] = hexCodes[arr[i]]
    return decodeAscii(oa)
  }

  return decode2string(arr, 0, length, hexArray)
}

export function fromHex(str) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  if (str.length % 2 !== 0) throw new SyntaxError(E_HEX)

  const length = str.length / 2 // this helps Hermes in loops
  const arr = new Uint8Array(length)

  // Native encoder path is beneficial even for small arrays in Hermes
  if (nativeEncoder) {
    if (!dehexArray) {
      dehexArray = new Uint8Array(_ff + 1) // 26 KiB cache, >2x perf improvement on Hermes
      const u8 = new Uint8Array(2)
      const u16 = new Uint16Array(u8.buffer, u8.byteOffset, 1) // for endianess-agnostic transform
      const map = [...allowed].map((c) => [c.charCodeAt(0), parseInt(c, 16)])
      for (const [ch, vh] of map) {
        u8[0] = ch // first we read high hex char
        for (const [cl, vl] of map) {
          u8[1] = cl // then we read low hex char
          dehexArray[u16[0]] = (vh << 4) | vl
        }
      }
    }

    const codes = encodeAscii(str, E_HEX) // aligned
    const codes16 = new Uint16Array(codes.buffer, codes.byteOffset, codes.byteLength / 2)
    let i = 0
    for (const last3 = length - 3; i < last3; i += 4) {
      const ai = codes16[i]
      const bi = codes16[i + 1]
      const ci = codes16[i + 2]
      const di = codes16[i + 3]
      const a = dehexArray[ai]
      const b = dehexArray[bi]
      const c = dehexArray[ci]
      const d = dehexArray[di]
      if ((!a && ai !== _00) || (!b && bi !== _00) || (!c && ci !== _00) || (!d && di !== _00)) {
        throw new SyntaxError(E_HEX)
      }

      arr[i] = a
      arr[i + 1] = b
      arr[i + 2] = c
      arr[i + 3] = d
    }

    while (i < length) {
      const ai = codes16[i]
      const a = dehexArray[ai]
      if (!a && ai !== _00) throw new SyntaxError(E_HEX)
      arr[i++] = a
    }
  } else {
    if (!dehexArray) {
      // no regex input validation here, so we map all other bytes to -1 and recheck sign
      // non-ASCII chars throw already though, so we should process only 0-127
      dehexArray = new Int8Array(128).fill(-1)
      for (let i = 0; i < 16; i++) {
        const s = i.toString(16)
        dehexArray[s.charCodeAt(0)] = dehexArray[s.toUpperCase().charCodeAt(0)] = i
      }
    }

    let j = 0
    for (let i = 0; i < length; i++) {
      const a = str.charCodeAt(j++)
      const b = str.charCodeAt(j++)
      const res = (dehexArray[a] << 4) | dehexArray[b]
      if (res < 0 || (0x7f | a | b) !== 0x7f) throw new SyntaxError(E_HEX) // 0-127
      arr[i] = res
    }
  }

  return arr
}
