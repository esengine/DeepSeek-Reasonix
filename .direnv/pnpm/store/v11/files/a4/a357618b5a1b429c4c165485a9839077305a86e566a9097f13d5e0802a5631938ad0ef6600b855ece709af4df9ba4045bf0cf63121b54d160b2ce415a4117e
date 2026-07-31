// We can't return native TextDecoder if it's present, as Node.js one is broken on windows-1252 and we fix that
// We are also faster than Node.js built-in on both TextEncoder and TextDecoder

import { utf16toString, utf16toStringLoose } from '@exodus/bytes/utf16.js'
import { utf8fromStringLoose, utf8toString, utf8toStringLoose } from '@exodus/bytes/utf8.js'
import {
  createSinglebyteDecoder,
  latin1toString,
  latin1fromString,
} from '@exodus/bytes/single-byte.js'
import labels from './encoding.labels.js'
import { fromSource, getBOMEncoding } from './encoding.api.js'
import { unfinishedBytes, mergePrefix } from './encoding.util.js'

export { getBOMEncoding } from './encoding.api.js'

export const E_ENCODING = 'Unknown encoding'
const E_MULTI = "import '@exodus/bytes/encoding.js' for legacy multi-byte encodings support"
const E_OPTIONS = 'The "options" argument must be of type object'
const replacementChar = '\uFFFD'
const multibyteSet = new Set(['big5', 'euc-kr', 'euc-jp', 'iso-2022-jp', 'shift_jis', 'gbk', 'gb18030']) // prettier-ignore
let createMultibyteDecoder, multibyteEncoder

let labelsMap
// Warning: unlike whatwg-encoding, returns lowercased labels
// Those are case-insensitive and that's how TextDecoder encoding getter normalizes them
// https://encoding.spec.whatwg.org/#names-and-labels
export function normalizeEncoding(label) {
  // fast path
  if (label === 'utf-8' || label === 'utf8' || label === 'UTF-8' || label === 'UTF8') return 'utf-8'
  if (label === 'windows-1252' || label === 'ascii' || label === 'latin1') return 'windows-1252'
  // full map
  if (/[^\w\t\n\f\r .:-]/i.test(label)) return null // must be ASCII (with ASCII whitespace)
  const low = `${label}`.trim().toLowerCase()
  if (Object.hasOwn(labels, low)) return low
  if (!labelsMap) {
    labelsMap = new Map()
    for (const [name, aliases] of Object.entries(labels)) {
      for (const alias of aliases) labelsMap.set(alias, name)
    }
  }

  const mapped = labelsMap.get(low)
  if (mapped) return mapped
  return null
}

const uppercasePrefixes = new Set(['utf', 'iso', 'koi', 'euc', 'ibm', 'gbk'])

// Unlike normalizeEncoding, case-sensitive
// https://encoding.spec.whatwg.org/#names-and-labels
export function labelToName(label) {
  const enc = normalizeEncoding(label)
  if (enc === 'utf-8') return 'UTF-8' // fast path
  if (!enc) return enc
  if (uppercasePrefixes.has(enc.slice(0, 3))) return enc.toUpperCase()
  if (enc === 'big5') return 'Big5'
  if (enc === 'shift_jis') return 'Shift_JIS'
  return enc
}

export const isMultibyte = (enc) => multibyteSet.has(enc)
export function setMultibyte(createDecoder, createEncoder) {
  createMultibyteDecoder = createDecoder
  multibyteEncoder = createEncoder
}

export function getMultibyteEncoder() {
  if (!multibyteEncoder) throw new Error(E_MULTI)
  return multibyteEncoder
}

const define = (obj, key, value) => Object.defineProperty(obj, key, { value, writable: false })

function isAnyUint8Array(x) {
  if (x instanceof Uint8Array) return true
  if (!x || !ArrayBuffer.isView(x) || x.BYTES_PER_ELEMENT !== 1) return false
  return Object.prototype.toString.call(x) === '[object Uint8Array]'
}

function unicodeDecoder(encoding, loose) {
  if (encoding === 'utf-8') return loose ? utf8toStringLoose : utf8toString // likely
  const form = encoding === 'utf-16le' ? 'uint8-le' : 'uint8-be'
  return loose ? (u) => utf16toStringLoose(u, form) : (u) => utf16toString(u, form)
}

export class TextDecoder {
  #decode
  #unicode
  #multibyte
  #chunk
  #canBOM

  constructor(encoding = 'utf-8', options = {}) {
    if (typeof options !== 'object') throw new TypeError(E_OPTIONS)
    const enc = normalizeEncoding(encoding)
    if (!enc || enc === 'replacement') throw new RangeError(E_ENCODING)
    define(this, 'encoding', enc)
    define(this, 'fatal', !!options.fatal)
    define(this, 'ignoreBOM', !!options.ignoreBOM)
    this.#unicode = enc === 'utf-8' || enc === 'utf-16le' || enc === 'utf-16be'
    this.#multibyte = !this.#unicode && isMultibyte(enc)
    this.#canBOM = this.#unicode && !this.ignoreBOM
  }

  get [Symbol.toStringTag]() {
    return 'TextDecoder'
  }

  decode(input, options = {}) {
    if (typeof options !== 'object') throw new TypeError(E_OPTIONS)
    const stream = !!options.stream
    let u = input === undefined ? new Uint8Array() : fromSource(input)
    const empty = u.length === 0 // also can't be streaming after next line
    if (empty && stream) return '' // no state change

    if (this.#unicode) {
      let prefix
      if (this.#chunk) {
        const merged = mergePrefix(u, this.#chunk, this.encoding)
        if (u.length < 3) {
          u = merged // might be unfinished, but fully consumed old u
        } else {
          prefix = merged // stops at complete chunk
          const add = prefix.length - this.#chunk.length
          if (add > 0) u = u.subarray(add)
        }

        this.#chunk = null
      } else if (empty) {
        this.#canBOM = !this.ignoreBOM // not streaming
        return ''
      }

      // For non-stream utf-8 we don't have to do this as it matches utf8toStringLoose already
      // For non-stream loose utf-16 we still have to do this as this API supports uneven byteLength unlike utf16toStringLoose
      let suffix = ''
      if (stream || (!this.fatal && this.encoding !== 'utf-8')) {
        const trail = unfinishedBytes(u, u.byteLength, this.encoding)
        if (trail > 0) {
          if (stream) {
            this.#chunk = Uint8Array.from(u.subarray(-trail)) // copy
          } else {
            // non-fatal mode as already checked
            suffix = replacementChar
          }

          u = u.subarray(0, -trail)
        }
      }

      let seenBOM = false
      if (this.#canBOM) {
        const bom = this.#findBom(prefix ?? u)
        if (bom) {
          seenBOM = true
          if (prefix) {
            prefix = prefix.subarray(bom)
          } else {
            u = u.subarray(bom)
          }
        }
      } else if (!stream && !this.ignoreBOM) {
        this.#canBOM = true
      }

      if (!this.#decode) this.#decode = unicodeDecoder(this.encoding, !this.fatal)
      try {
        const res = (prefix ? this.#decode(prefix) : '') + this.#decode(u) + suffix
        // "BOM seen" is set on the current decode call only if it did not error, in "serialize I/O queue" after decoding
        if (stream && (seenBOM || res.length > 0)) this.#canBOM = false
        return res
      } catch (err) {
        this.#chunk = null // reset unfinished chunk on errors
        // The correct way per spec seems to be not destroying the decoder state (aka BOM here) in stream mode
        // See also multi-byte.js
        throw err
      }

      // eslint-disable-next-line no-else-return
    } else if (this.#multibyte) {
      if (!createMultibyteDecoder) throw new Error(E_MULTI)
      if (!this.#decode) this.#decode = createMultibyteDecoder(this.encoding, !this.fatal) // can contain state!
      return this.#decode(u, stream)
    } else {
      if (!this.#decode) this.#decode = createSinglebyteDecoder(this.encoding, !this.fatal)
      return this.#decode(u)
    }
  }

  #findBom(u) {
    switch (this.encoding) {
      case 'utf-8':
        return u.byteLength >= 3 && u[0] === 0xef && u[1] === 0xbb && u[2] === 0xbf ? 3 : 0
      case 'utf-16le':
        return u.byteLength >= 2 && u[0] === 0xff && u[1] === 0xfe ? 2 : 0
      case 'utf-16be':
        return u.byteLength >= 2 && u[0] === 0xfe && u[1] === 0xff ? 2 : 0
    }

    /* c8 ignore next */
    throw new Error('Unreachable')
  }
}

export class TextEncoder {
  constructor() {
    define(this, 'encoding', 'utf-8')
  }

  get [Symbol.toStringTag]() {
    return 'TextEncoder'
  }

  encode(str = '') {
    if (typeof str !== 'string') str = `${str}`
    return utf8fromStringLoose(str) // non-pooled
  }

  encodeInto(str, target) {
    if (typeof str !== 'string') str = `${str}`
    if (!isAnyUint8Array(target)) throw new TypeError('Target must be an Uint8Array')
    if (target.buffer.detached) return { read: 0, written: 0 } // Until https://github.com/whatwg/encoding/issues/324 is resolved

    const tlen = target.length
    if (tlen < str.length) str = str.slice(0, tlen)
    let u8 = utf8fromStringLoose(str)
    let read
    if (tlen >= u8.length) {
      read = str.length
    } else if (u8.length === str.length) {
      if (u8.length > tlen) u8 = u8.subarray(0, tlen) // ascii can be truncated
      read = u8.length
    } else {
      u8 = u8.subarray(0, tlen)
      const unfinished = unfinishedBytes(u8, u8.length, 'utf-8')
      if (unfinished > 0) u8 = u8.subarray(0, u8.length - unfinished)

      // We can do this because loose str -> u8 -> str preserves length, unlike loose u8 -> str -> u8
      // Each unpaired surrogate (1 charcode) is replaced with a single charcode
      read = utf8toStringLoose(u8).length // FIXME: Converting back is very inefficient
    }

    try {
      target.set(u8)
    } catch {
      return { read: 0, written: 0 } // see above, likely detached but no .detached property support
    }

    return { read, written: u8.length }
  }
}

const E_NO_STREAMS = 'TransformStream global not present in the environment'

// https://encoding.spec.whatwg.org/#interface-textdecoderstream
export class TextDecoderStream {
  constructor(encoding = 'utf-8', options = {}) {
    if (!globalThis.TransformStream) throw new Error(E_NO_STREAMS)
    const decoder = new TextDecoder(encoding, options)
    const transform = new TransformStream({
      transform: (chunk, controller) => {
        const value = decoder.decode(fromSource(chunk), { stream: true })
        if (value) controller.enqueue(value)
      },
      flush: (controller) => {
        // https://streams.spec.whatwg.org/#dom-transformer-flush
        const value = decoder.decode()
        if (value) controller.enqueue(value)
        // No need to call .terminate() (Node.js is wrong)
      },
    })

    define(this, 'encoding', decoder.encoding)
    define(this, 'fatal', decoder.fatal)
    define(this, 'ignoreBOM', decoder.ignoreBOM)
    define(this, 'readable', transform.readable)
    define(this, 'writable', transform.writable)
  }

  get [Symbol.toStringTag]() {
    return 'TextDecoderStream'
  }
}

// https://encoding.spec.whatwg.org/#interface-textencoderstream
// Only UTF-8 per spec
export class TextEncoderStream {
  constructor() {
    if (!globalThis.TransformStream) throw new Error(E_NO_STREAMS)
    let lead
    const transform = new TransformStream({
      // https://encoding.spec.whatwg.org/#encode-and-enqueue-a-chunk
      // Not identical in code, but reuses loose mode to have identical behavior
      transform: (chunk, controller) => {
        let s = String(chunk) // DOMString, might contain unpaired surrogates
        if (s.length === 0) return
        if (lead) {
          s = lead + s
          lead = null
        }

        const last = s.charCodeAt(s.length - 1) // Can't come from previous lead due to length check
        if ((last & 0xfc_00) === 0xd8_00) {
          lead = s[s.length - 1]
          s = s.slice(0, -1)
        }

        if (s) controller.enqueue(utf8fromStringLoose(s))
      },
      // https://encoding.spec.whatwg.org/#encode-and-flush
      flush: (controller) => {
        if (lead) controller.enqueue(Uint8Array.of(0xef, 0xbf, 0xbd))
      },
    })

    define(this, 'encoding', 'utf-8')
    define(this, 'readable', transform.readable)
    define(this, 'writable', transform.writable)
  }

  get [Symbol.toStringTag]() {
    return 'TextEncoderStream'
  }
}

// https://encoding.spec.whatwg.org/#decode
// Warning: encoding sniffed from BOM takes preference over the supplied one
// Warning: lossy, performs replacement, no option of throwing
// Completely ignores encoding and even skips validation when BOM is found
// Unlike TextDecoder public API, additionally supports 'replacement' encoding
export function legacyHookDecode(input, fallbackEncoding = 'utf-8') {
  let u8 = fromSource(input)
  const bomEncoding = getBOMEncoding(u8)
  if (bomEncoding) u8 = u8.subarray(bomEncoding === 'utf-8' ? 3 : 2)
  const enc = bomEncoding ?? normalizeEncoding(fallbackEncoding) // "the byte order mark is more authoritative than anything else"

  if (enc === 'utf-8') return utf8toStringLoose(u8)
  if (enc === 'utf-16le' || enc === 'utf-16be') {
    let suffix = ''
    if (u8.byteLength % 2 !== 0) {
      suffix = replacementChar
      u8 = u8.subarray(0, -unfinishedBytes(u8, u8.byteLength, enc))
    }

    return utf16toStringLoose(u8, enc === 'utf-16le' ? 'uint8-le' : 'uint8-be') + suffix
  }

  if (!Object.hasOwn(labels, enc)) throw new RangeError(E_ENCODING)

  if (isMultibyte(enc)) {
    if (!createMultibyteDecoder) throw new Error(E_MULTI)
    return createMultibyteDecoder(enc, true)(u8)
  }

  // https://encoding.spec.whatwg.org/#replacement-decoder
  // On non-streaming non-fatal case, it just replaces any non-empty input with a single replacement char
  if (enc === 'replacement') return input.byteLength > 0 ? replacementChar : ''

  return createSinglebyteDecoder(enc, true)(u8)
}

export function isomorphicDecode(input) {
  return latin1toString(fromSource(input))
}

export function isomorphicEncode(str) {
  return latin1fromString(str)
}
