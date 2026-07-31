import { decodeUCS2 } from './latin1.js'
import { assertU8, E_STRING, E_STRICT_UNICODE } from './_utils.js'
import { nativeDecoder, isLE, encodeCharcodes } from './platform.js'

export const E_STRICT = 'Input is not well-formed utf16'
const isWellFormedStr = /* @__PURE__ */ (() => String.prototype.isWellFormed)()
const toWellFormedStr = /* @__PURE__ */ (() => String.prototype.toWellFormed)()

const replacementCodepoint = 0xff_fd
const replacementCodepointSwapped = 0xfd_ff

const to16 = (a) => new Uint16Array(a.buffer, a.byteOffset, a.byteLength / 2) // Requires checked length and alignment!

export function encodeApi(str, loose, format) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  if (format !== 'uint16' && format !== 'uint8-le' && format !== 'uint8-be') {
    throw new TypeError('Unknown format')
  }

  // On v8 and SpiderMonkey, check via isWellFormed is faster than js
  // On JSC, check during loop is faster than isWellFormed
  // If isWellFormed is available, we skip check during decoding and recheck after
  // If isWellFormed is unavailable, we check in js during decoding
  if (!loose && isWellFormedStr && !isWellFormedStr.call(str)) throw new TypeError(E_STRICT_UNICODE)
  const shouldSwap = (isLE && format === 'uint8-be') || (!isLE && format === 'uint8-le')
  const u16 = encode(str, loose, !loose && isWellFormedStr, shouldSwap)

  // Bytes are already swapped and format is already checked, we need to just cast the view
  return format === 'uint16' ? u16 : new Uint8Array(u16.buffer, u16.byteOffset, u16.byteLength)
}

const fatalLE = nativeDecoder ? new TextDecoder('utf-16le', { ignoreBOM: true, fatal: true }) : null
const looseLE = nativeDecoder ? new TextDecoder('utf-16le', { ignoreBOM: true }) : null
const fatalBE = nativeDecoder ? new TextDecoder('utf-16be', { ignoreBOM: true, fatal: true }) : null
const looseBE = nativeDecoder ? new TextDecoder('utf-16be', { ignoreBOM: true }) : null

export function decodeApiDecoders(input, loose, format) {
  if (format === 'uint16') {
    if (!(input instanceof Uint16Array)) throw new TypeError('Expected an Uint16Array')
  } else if (format === 'uint8-le' || format === 'uint8-be') {
    assertU8(input)
    if (input.byteLength % 2 !== 0) throw new TypeError('Expected even number of bytes')
  } else {
    throw new TypeError('Unknown format')
  }

  const le = format === 'uint8-le' || (format === 'uint16' && isLE)
  return (le ? (loose ? looseLE : fatalLE) : loose ? looseBE : fatalBE).decode(input)
}

export function decodeApiJS(input, loose, format) {
  let u16
  switch (format) {
    case 'uint16':
      if (!(input instanceof Uint16Array)) throw new TypeError('Expected an Uint16Array')
      u16 = input
      break
    case 'uint8-le':
      assertU8(input)
      if (input.byteLength % 2 !== 0) throw new TypeError('Expected even number of bytes')
      u16 = to16input(input, true)
      break
    case 'uint8-be':
      assertU8(input)
      if (input.byteLength % 2 !== 0) throw new TypeError('Expected even number of bytes')
      u16 = to16input(input, false)
      break
    default:
      throw new TypeError('Unknown format')
  }

  const str = decode(u16, loose, (!loose && isWellFormedStr) || (loose && toWellFormedStr))
  if (!loose && isWellFormedStr && !isWellFormedStr.call(str)) throw new TypeError(E_STRICT)
  if (loose && toWellFormedStr) return toWellFormedStr.call(str)

  return str
}

export function to16input(u8, le) {
  // Assume even number of bytes
  if (le === isLE) return to16(u8.byteOffset % 2 === 0 ? u8 : Uint8Array.from(u8))
  return to16(swap16(Uint8Array.from(u8)))
}

export const decode = (u16, loose = false, checked = false) => {
  if (checked || isWellFormed(u16)) return decodeUCS2(u16)
  if (!loose) throw new TypeError(E_STRICT)
  return decodeUCS2(toWellFormed(Uint16Array.from(u16))) // cloned for replacement
}

export function encode(str, loose = false, checked = false, swapped = false) {
  const arr = new Uint16Array(str.length)
  if (checked) return swapped ? encodeCheckedSwapped(str, arr) : encodeChecked(str, arr)
  return swapped ? encodeUncheckedSwapped(str, arr, loose) : encodeUnchecked(str, arr, loose)
}

/* eslint-disable @exodus/mutable/no-param-reassign-prop-only */

// Assumes checked length % 2 === 0, otherwise does not swap tail
function swap16(u8) {
  let i = 0
  for (const last3 = u8.length - 3; i < last3; i += 4) {
    const x0 = u8[i]
    const x1 = u8[i + 1]
    const x2 = u8[i + 2]
    const x3 = u8[i + 3]
    u8[i] = x1
    u8[i + 1] = x0
    u8[i + 2] = x3
    u8[i + 3] = x2
  }

  for (const last = u8.length - 1; i < last; i += 2) {
    const x0 = u8[i]
    const x1 = u8[i + 1]
    u8[i] = x1
    u8[i + 1] = x0
  }

  return u8
}

// Splitting paths into small functions helps (at least on SpiderMonkey)

const encodeChecked = (str, arr) => encodeCharcodes(str, arr) // Same as encodeLatin1, but with Uint16Array

function encodeCheckedSwapped(str, arr) {
  // TODO: faster path for Hermes? See encodeCharcodes
  const length = str.length
  for (let i = 0; i < length; i++) {
    const x = str.charCodeAt(i)
    arr[i] = ((x & 0xff) << 8) | (x >> 8)
  }

  return arr
}

// lead: d800 - dbff, trail: dc00 - dfff

function encodeUnchecked(str, arr, loose = false) {
  // TODO: faster path for Hermes? See encodeCharcodes
  const length = str.length
  for (let i = 0; i < length; i++) {
    const code = str.charCodeAt(i)
    arr[i] = code
    if (code >= 0xd8_00 && code < 0xe0_00) {
      // An unexpected trail or a lead at the very end of input
      if (code > 0xdb_ff || i + 1 >= length) {
        if (!loose) throw new TypeError(E_STRICT_UNICODE)
        arr[i] = replacementCodepoint
      } else {
        const next = str.charCodeAt(i + 1) // Process valid pairs immediately
        if (next < 0xdc_00 || next >= 0xe0_00) {
          if (!loose) throw new TypeError(E_STRICT_UNICODE)
          arr[i] = replacementCodepoint
        } else {
          i++ // consume next
          arr[i] = next
        }
      }
    }
  }

  return arr
}

function encodeUncheckedSwapped(str, arr, loose = false) {
  // TODO: faster path for Hermes? See encodeCharcodes
  const length = str.length
  for (let i = 0; i < length; i++) {
    const code = str.charCodeAt(i)
    arr[i] = ((code & 0xff) << 8) | (code >> 8)
    if (code >= 0xd8_00 && code < 0xe0_00) {
      // An unexpected trail or a lead at the very end of input
      if (code > 0xdb_ff || i + 1 >= length) {
        if (!loose) throw new TypeError(E_STRICT_UNICODE)
        arr[i] = replacementCodepointSwapped
      } else {
        const next = str.charCodeAt(i + 1) // Process valid pairs immediately
        if (next < 0xdc_00 || next >= 0xe0_00) {
          if (!loose) throw new TypeError(E_STRICT_UNICODE)
          arr[i] = replacementCodepointSwapped
        } else {
          i++ // consume next
          arr[i] = ((next & 0xff) << 8) | (next >> 8)
        }
      }
    }
  }

  return arr
}

// Only needed on Hermes, everything else has native impl
export function toWellFormed(u16) {
  const length = u16.length
  for (let i = 0; i < length; i++) {
    const code = u16[i]
    if (code >= 0xd8_00 && code < 0xe0_00) {
      // An unexpected trail or a lead at the very end of input
      if (code > 0xdb_ff || i + 1 >= length) {
        u16[i] = replacementCodepoint
      } else {
        const next = u16[i + 1] // Process valid pairs immediately
        if (next < 0xdc_00 || next >= 0xe0_00) {
          u16[i] = replacementCodepoint
        } else {
          i++ // consume next
        }
      }
    }
  }

  return u16
}

// Only needed on Hermes, everything else has native impl
export function isWellFormed(u16) {
  const length = u16.length
  let i = 0

  const m = 0x80_00_80_00
  const l = 0xd8_00
  const h = 0xe0_00

  // Speedup with u32, by skipping to the first surrogate
  // Only implemented for aligned input for now, but almost all input is aligned (pooled Buffer or 0 offset)
  if (length > 32 && u16.byteOffset % 4 === 0) {
    const u32length = (u16.byteLength / 4) | 0
    const u32 = new Uint32Array(u16.buffer, u16.byteOffset, u32length)
    for (const last3 = u32length - 3; ; i += 4) {
      if (i >= last3) break // loop is fast enough for moving this here to be _very_ useful, likely due to array access checks
      const a = u32[i]
      const b = u32[i + 1]
      const c = u32[i + 2]
      const d = u32[i + 3]
      if (a & m || b & m || c & m || d & m) break // bitwise OR does not make this faster on Hermes
    }

    for (; i < u32length; i++) if (u32[i] & m) break
    i *= 2
  }

  // An extra loop gives ~30-40% speedup e.g. on English text without surrogates but with other symbols above 0x80_00
  for (const last3 = length - 3; ; i += 4) {
    if (i >= last3) break
    const a = u16[i]
    const b = u16[i + 1]
    const c = u16[i + 2]
    const d = u16[i + 3]
    if ((a >= l && a < h) || (b >= l && b < h) || (c >= l && c < h) || (d >= l && d < h)) break
  }

  for (; i < length; i++) {
    const code = u16[i]
    if (code >= l && code < h) {
      // An unexpected trail or a lead at the very end of input
      if (code >= 0xdc_00 || i + 1 >= length) return false
      i++ // consume next
      const next = u16[i] // Process valid pairs immediately
      if (next < 0xdc_00 || next >= h) return false
    }
  }

  return true
}
