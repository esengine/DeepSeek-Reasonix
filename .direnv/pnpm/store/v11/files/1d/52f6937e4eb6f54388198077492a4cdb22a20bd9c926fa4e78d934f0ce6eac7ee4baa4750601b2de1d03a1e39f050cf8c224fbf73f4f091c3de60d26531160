import { getBOMEncoding } from './fallback/encoding.api.js'

// Lite-weight version which re-exports existing implementations on browsers,
// while still being aliased to the full impl in RN and Node.js

// WARNING: Note that browsers have bugs (which hopefully will get fixed soon)

const { TextDecoder, TextEncoder, TextDecoderStream, TextEncoderStream } = globalThis

export { getBOMEncoding } from './fallback/encoding.api.js'
export { TextDecoder, TextEncoder, TextDecoderStream, TextEncoderStream }

export function normalizeEncoding(label) {
  if (label === 'utf-8' || label === 'utf8' || label === 'UTF-8' || label === 'UTF8') return 'utf-8'
  if (label === 'windows-1252' || label === 'ascii' || label === 'latin1') return 'windows-1252'
  if (/[^\w\t\n\f\r .:-]/i.test(label)) return null
  const l = `${label}`.trim().toLowerCase()
  try {
    return new TextDecoder(l).encoding
  } catch {}

  if (l === 'x-user-defined') return l
  if (
    l === 'replacement' ||
    l === 'csiso2022kr' ||
    l === 'hz-gb-2312' ||
    l === 'iso-2022-cn' ||
    l === 'iso-2022-cn-ext' ||
    l === 'iso-2022-kr'
  ) {
    return 'replacement'
  }

  return null
}

export function legacyHookDecode(input, fallbackEncoding = 'utf-8') {
  const enc = getBOMEncoding(input) ?? normalizeEncoding(fallbackEncoding)
  if (enc === 'replacement') return input.byteLength > 0 ? '\uFFFD' : ''
  return new TextDecoder(enc).decode(input)
}

export function labelToName(label) {
  const enc = normalizeEncoding(label)
  if (enc === 'utf-8') return 'UTF-8'
  if (!enc) return enc
  const p = enc.slice(0, 3)
  if (p === 'utf' || p === 'iso' || p === 'koi' || p === 'euc' || p === 'ibm' || p === 'gbk') {
    return enc.toUpperCase()
  }

  if (enc === 'big5') return 'Big5'
  if (enc === 'shift_jis') return 'Shift_JIS'
  return enc
}
