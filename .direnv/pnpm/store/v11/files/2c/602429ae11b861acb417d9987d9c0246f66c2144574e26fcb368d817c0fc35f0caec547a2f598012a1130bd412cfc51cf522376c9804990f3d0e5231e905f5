import { asciiPrefix, decodeAscii, decodeLatin1 } from './latin1.js'
import encodings from './single-byte.encodings.js'
import { decode2string, nativeDecoder } from './platform.js'

export const E_STRICT = 'Input is not well-formed for this encoding'
const xUserDefined = 'x-user-defined'
const iso8i = 'iso-8859-8-i'

export const assertEncoding = (encoding) => {
  if (Object.hasOwn(encodings, encoding) || encoding === xUserDefined || encoding === iso8i) return
  throw new RangeError('Unsupported encoding')
}

const r = 0xff_fd

export function getEncoding(encoding) {
  assertEncoding(encoding)
  if (encoding === xUserDefined) return Array.from({ length: 128 }, (_, i) => 0xf7_80 + i)
  if (encoding === iso8i) encoding = 'iso-8859-8'
  const enc = encodings[encoding]
  const deltas = enc.flatMap((x) => (Array.isArray(x) ? new Array(x[0]).fill(x[1] ?? 0) : x))
  return deltas.map((x, i) => (x === r ? x : x + 128 + i))
}

const mappers = new Map()
const decoders = new Map()
const encmaps = new Map()

// Used only on Node.js, no reason to optimize for anything else
// E.g. avoiding .from and filling zero-initialized arr manually is faster on Hermes, but we avoid this codepath on Hermes completely
export function encodingMapper(encoding) {
  const cached = mappers.get(encoding)
  if (cached) return cached

  const codes = getEncoding(encoding)
  const incomplete = codes.includes(r)
  let map
  const mapper = (arr, start = 0) => {
    if (!map) {
      map = new Uint16Array(256).map((_, i) => i) // Unicode subset
      map.set(Uint16Array.from(codes), 128)
    }

    const o = Uint16Array.from(start === 0 ? arr : arr.subarray(start)) // copy to modify in-place, also those are 16-bit now
    let i = 0
    for (const end7 = o.length - 7; i < end7; i += 8) {
      o[i] = map[o[i]]
      o[i + 1] = map[o[i + 1]]
      o[i + 2] = map[o[i + 2]]
      o[i + 3] = map[o[i + 3]]
      o[i + 4] = map[o[i + 4]]
      o[i + 5] = map[o[i + 5]]
      o[i + 6] = map[o[i + 6]]
      o[i + 7] = map[o[i + 7]]
    }

    for (const end = o.length; i < end; i++) o[i] = map[o[i]]
    return o
  }

  mappers.set(encoding, { mapper, incomplete })
  return { mapper, incomplete }
}

export function encodingDecoder(encoding) {
  const cached = decoders.get(encoding)
  if (cached) return cached
  const isLatin1 = encoding === 'iso-8859-1'
  if (isLatin1 && !nativeDecoder) return (arr, loose = false) => decodeLatin1(arr) // native decoder is faster for ascii below

  let strings
  const codes = getEncoding(encoding)
  const incomplete = codes.includes(r)
  const decoder = (arr, loose = false) => {
    if (!strings) {
      const allCodes = Array.from({ length: 128 }, (_, i) => i).concat(codes)
      while (allCodes.length < 256) allCodes.push(allCodes.length)
      strings = allCodes.map((c) => String.fromCharCode(c))
    }

    const prefixLen = asciiPrefix(arr)
    if (prefixLen === arr.length) return decodeAscii(arr)
    if (isLatin1) return decodeLatin1(arr) // TODO: check if decodeAscii with subarray is faster for small prefixes too
    const prefix = decodeLatin1(arr, 0, prefixLen) // TODO: check if decodeAscii with subarray is faster for small prefixes too
    const suffix = decode2string(arr, prefix.length, arr.length, strings)
    if (!loose && incomplete && suffix.includes('\uFFFD')) throw new TypeError(E_STRICT)
    return prefix + suffix
  }

  decoders.set(encoding, decoder)
  return decoder
}

export function encodeMap(encoding) {
  const cached = encmaps.get(encoding)
  if (cached) return cached

  const codes = getEncoding(encoding)
  let max = 128
  while (codes.length < 128) codes.push(128 + codes.length)
  for (const code of codes) if (code > max && code !== r) max = code
  const map = new Uint8Array(max + 1) // < 10 KiB for all except macintosh, 63 KiB for macintosh
  for (let i = 0; i < 128; i++) {
    map[i] = i
    if (codes[i] !== r) map[codes[i]] = 128 + i
  }

  encmaps.set(encoding, map)
  return map
}
