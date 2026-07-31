import { decodePartAddition as decodePart } from './platform.native.js'

export { isLE, encodeCharcodesPure as encodeCharcodes } from './platform.native.js'

export const nativeBuffer = null
export const isHermes = false
export const isDeno = false
export const nativeEncoder = /* @__PURE__ */ (() => new TextEncoder())()
export const nativeDecoder = /* @__PURE__ */ (() => new TextDecoder('utf-8', { ignoreBOM: true }))()
export const nativeDecoderLatin1 = /* @__PURE__ */ (() =>
  new TextDecoder('latin1', { ignoreBOM: true }))()

export function decode2string(arr, start, end, m) {
  if (end - start > 30_000) {
    // Limit concatenation to avoid excessive GC
    // Thresholds checked on Hermes for toHex
    const concat = []
    for (let i = start; i < end; ) {
      const step = i + 500
      const iNext = step > end ? end : step
      concat.push(decodePart(arr, i, iNext, m))
      i = iNext
    }

    const res = concat.join('')
    concat.length = 0
    return res
  }

  return decodePart(arr, start, end, m)
}
