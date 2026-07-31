import { utf8fromStringLoose } from '@exodus/bytes/utf8.js'
import { createSinglebyteEncoder } from '@exodus/bytes/single-byte.js'
import {
  isMultibyte,
  getMultibyteEncoder,
  normalizeEncoding,
  E_ENCODING,
} from './fallback/encoding.js'
import { percentEncoder } from './fallback/percent.js'
import { encodeMap } from './fallback/single-byte.js'
import { E_STRING } from './fallback/_utils.js'

// https://url.spec.whatwg.org/#string-percent-encode-after-encoding
// Codepoints below 0x20, 0x7F specifically, and above 0x7F (non-ASCII) are always encoded
// > A C0 control is a code point in the range U+0000 NULL to U+001F INFORMATION SEPARATOR ONE, inclusive.
// > The C0 control percent-encode set are the C0 controls and all code points greater than U+007E (~).
export function percentEncodeAfterEncoding(encoding, input, percentEncodeSet, spaceAsPlus = false) {
  const enc = normalizeEncoding(encoding)
  // Ref: https://encoding.spec.whatwg.org/#get-an-encoder
  if (!enc || enc === 'replacement' || enc === 'utf-16le' || enc === 'utf-16be') {
    throw new RangeError(E_ENCODING)
  }

  const percent = percentEncoder(percentEncodeSet, spaceAsPlus)
  if (enc === 'utf-8') return percent(utf8fromStringLoose(input))

  const multi = isMultibyte(enc)
  const encoder = multi ? getMultibyteEncoder() : createSinglebyteEncoder
  const fatal = encoder(enc)
  try {
    return percent(fatal(input))
  } catch {}

  let res = ''
  let last = 0
  if (multi) {
    const rep = enc === 'gb18030' ? percent(fatal('\uFFFD')) : `%26%23${0xff_fd}%3B` // only gb18030 can encode it
    const escaping = encoder(enc, (cp, u, i) => {
      res += percent(u, last, i)
      res += cp >= 0xd8_00 && cp < 0xe0_00 ? rep : `%26%23${cp}%3B` // &#cp;
      last = i
      return 0 // no bytes emitted
    })

    const u = escaping(input) // has side effects on res
    res += percent(u, last)
  } else {
    if (typeof input !== 'string') throw new TypeError(E_STRING) // all other paths have their own validation
    const m = encodeMap(enc)
    const len = input.length
    const u = new Uint8Array(len)
    for (let i = 0; i < len; i++) {
      const x = input.charCodeAt(i)
      const b = m[x]
      if (!b && x) {
        let cp = x
        const i0 = i
        if (x >= 0xd8_00 && x < 0xe0_00) {
          cp = 0xff_fd
          if (x < 0xdc_00 && i + 1 < len) {
            const x1 = input.charCodeAt(i + 1)
            if (x1 >= 0xdc_00 && x1 < 0xe0_00) {
              cp = 0x1_00_00 + ((x1 & 0x3_ff) | ((x & 0x3_ff) << 10))
              i++
            }
          }
        }

        res += `${percent(u, last, i0)}%26%23${cp}%3B` // &#cp;
        last = i + 1 // skip current
      } else {
        u[i] = b
      }
    }

    res += percent(u, last)
  }

  return res
}
