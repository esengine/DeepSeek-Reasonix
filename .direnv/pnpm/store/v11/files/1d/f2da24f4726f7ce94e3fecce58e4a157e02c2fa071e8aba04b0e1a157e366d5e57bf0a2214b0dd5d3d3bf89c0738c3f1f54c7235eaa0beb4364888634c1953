import { fromBase64url } from '@exodus/bytes/base64.js'
import { utf16toString } from '@exodus/bytes/utf16.js'
import loadEncodings from './multi-byte.encodings.cjs'

export const sizes = {
  jis0208: 11_104,
  jis0212: 7211,
  'euc-kr': 23_750,
  gb18030: 23_940,
  big5: 19_782,
}

// This is huge. It's _much_ smaller than https://npmjs.com/text-encoding though
// Exactly as mapped by the index table
// 0,x - hole of x empty elements
// n,c - continious [c, ...] of length n
// $.. - references to common chunks
// -{x} - same as 1,{x}

// See tests/multi-byte.test.js to verify that this data decodes exactly into the encoding spec tables

let indices
const tables = new Map()
/* eslint-disable @exodus/mutable/no-param-reassign-prop-only */

function loadBase64(str) {
  const x = fromBase64url(str)
  const len = x.length
  const len2 = len >> 1
  const y = new Uint8Array(len)
  let a = -1, b = 0 // prettier-ignore
  for (let i = 0, j = 0; i < len; i += 2, j++) {
    a = (a + x[j] + 1) & 0xff
    b = (b + x[len2 + j]) & 0xff
    y[i] = a
    y[i + 1] = b
  }

  return y
}

function unwrap(res, t, pos) {
  let code = 0
  for (let i = 0; i < t.length; i++) {
    let x = t[i]
    if (typeof x === 'number') {
      if (x === 0) {
        pos += t[++i]
      } else {
        if (x < 0) {
          code -= x
          x = 1
        } else {
          code += t[++i]
        }

        for (let k = 0; k < x; k++, pos++, code++) {
          if (code <= 0xff_ff) {
            res[pos] = code
          } else {
            const c = String.fromCodePoint(code)
            res[pos] = (c.charCodeAt(0) << 16) | c.charCodeAt(1)
          }
        }
      }
    } else if (x[0] === '$' && Object.hasOwn(indices, x)) {
      pos = unwrap(res, indices[x], pos) // self-reference using shared chunks
    } else {
      let last
      // splits by codepoints
      for (const c of utf16toString(loadBase64(x), 'uint8-le')) {
        last = c
        res[pos++] = c.length === 1 ? c.charCodeAt(0) : (c.charCodeAt(0) << 16) | c.charCodeAt(1)
      }

      code = last.codePointAt(0) + 1
    }
  }

  return pos
}

export function getTable(id) {
  const cached = tables.get(id)
  if (cached) return cached

  if (!indices) indices = loadEncodings() // lazy-load
  if (!Object.hasOwn(indices, id)) throw new Error('Unknown encoding')
  if (!indices[id]) throw new Error('Table already used (likely incorrect bundler dedupe)')

  let res
  if (id.endsWith('-ranges')) {
    res = []
    let a = 0, b = 0 // prettier-ignore
    const idx = indices[id]
    while (idx.length > 0) res.push([(a += idx.shift()), (b += idx.shift())]) // destroying, we remove it later anyway
  } else if (id.endsWith('-katakana')) {
    let a = -1
    res = new Uint16Array(indices[id].map((x) => (a += x + 1)))
  } else if (id === 'big5') {
    res = new Uint32Array(sizes[id]) // single or double charcodes
    unwrap(res, indices[id], 0)
    // Pointer code updates are embedded into the table
    // These are skipped in encoder as encoder uses only pointers >= (0xA1 - 0x81) * 157
    res[1133] = 0xca_03_04
    res[1135] = 0xca_03_0c
    res[1164] = 0xea_03_04
    res[1166] = 0xea_03_0c
  } else {
    if (!Object.hasOwn(sizes, id)) throw new Error('Unknown encoding')
    res = new Uint16Array(sizes[id])
    unwrap(res, indices[id], 0)
  }

  indices[id] = null // gc
  tables.set(id, res)
  return res
}
