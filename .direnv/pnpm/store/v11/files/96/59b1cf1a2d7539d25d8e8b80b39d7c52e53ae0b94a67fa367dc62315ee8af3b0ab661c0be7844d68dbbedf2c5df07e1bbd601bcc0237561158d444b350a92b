const { Buffer } = globalThis
const haveNativeBuffer = Buffer && !Buffer.TYPED_ARRAY_SUPPORT
export const nativeBuffer = haveNativeBuffer ? Buffer : null
export const isHermes = /* @__PURE__ */ (() => !!globalThis.HermesInternal)()
export const isDeno = /* @__PURE__ */ (() => !!globalThis.Deno)()
export const isLE = /* @__PURE__ */ (() => new Uint8Array(Uint16Array.of(258).buffer)[0] === 2)()

// We consider Node.js TextDecoder/TextEncoder native
// Still needed in platform.native.js as this is re-exported to platform.js
let isNative = (x) => x && (haveNativeBuffer || `${x}`.includes('[native code]'))
if (!haveNativeBuffer && isNative(() => {})) isNative = () => false // e.g. XS, we don't want false positives

export const nativeEncoder = /* @__PURE__ */ (() =>
  isNative(globalThis.TextEncoder) ? new TextEncoder() : null)()
export const nativeDecoder = /* @__PURE__ */ (() =>
  isNative(globalThis.TextDecoder) ? new TextDecoder('utf-8', { ignoreBOM: true }) : null)()

// Actually windows-1252, compatible with ascii and latin1 decoding
// Beware that on non-latin1, i.e. on windows-1252, this is broken in ~all Node.js versions released
// in 2025 due to a regression, so we call it Latin1 as it's usable only for that
export const nativeDecoderLatin1 = /* @__PURE__ */ (() => {
  // Not all barebone engines with TextDecoder support something except utf-8, detect
  if (nativeDecoder) {
    try {
      return new TextDecoder('latin1', { ignoreBOM: true })
    } catch {}
  }

  return null
})()

export function decodePartAddition(a, start, end, m) {
  let o = ''
  let i = start
  for (const last3 = end - 3; i < last3; i += 4) {
    const x0 = a[i]
    const x1 = a[i + 1]
    const x2 = a[i + 2]
    const x3 = a[i + 3]
    o += m[x0]
    o += m[x1]
    o += m[x2]
    o += m[x3]
  }

  while (i < end) o += m[a[i++]]
  return o
}

// Decoding with templates is faster on Hermes
export function decodePartTemplates(a, start, end, m) {
  let o = ''
  let i = start
  for (const last15 = end - 15; i < last15; i += 16) {
    const x0 = a[i]
    const x1 = a[i + 1]
    const x2 = a[i + 2]
    const x3 = a[i + 3]
    const x4 = a[i + 4]
    const x5 = a[i + 5]
    const x6 = a[i + 6]
    const x7 = a[i + 7]
    const x8 = a[i + 8]
    const x9 = a[i + 9]
    const x10 = a[i + 10]
    const x11 = a[i + 11]
    const x12 = a[i + 12]
    const x13 = a[i + 13]
    const x14 = a[i + 14]
    const x15 = a[i + 15]
    o += `${m[x0]}${m[x1]}${m[x2]}${m[x3]}${m[x4]}${m[x5]}${m[x6]}${m[x7]}${m[x8]}${m[x9]}${m[x10]}${m[x11]}${m[x12]}${m[x13]}${m[x14]}${m[x15]}`
  }

  while (i < end) o += m[a[i++]]
  return o
}

const decodePart = isHermes ? decodePartTemplates : decodePartAddition
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

/* eslint-disable @exodus/mutable/no-param-reassign-prop-only */

function encodeCharcodesHermes(str, arr) {
  const length = str.length
  if (length > 64) {
    const at = str.charCodeAt.bind(str) // faster on strings from ~64 chars on Hermes, but can be 10x slower on e.g. JSC
    for (let i = 0; i < length; i++) arr[i] = at(i)
  } else {
    for (let i = 0; i < length; i++) arr[i] = str.charCodeAt(i)
  }

  return arr
}

export function encodeCharcodesPure(str, arr) {
  const length = str.length
  // Can be optimized with unrolling, but this is not used on non-Hermes atm
  for (let i = 0; i < length; i++) arr[i] = str.charCodeAt(i)
  return arr
}

/* eslint-enable @exodus/mutable/no-param-reassign-prop-only */

export const encodeCharcodes = isHermes ? encodeCharcodesHermes : encodeCharcodesPure
