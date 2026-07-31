import { E_STRING } from './_utils.js'
import { nativeEncoder } from './platform.js'
import { asciiPrefix, decodeAscii, decodeLatin1, decodeUCS2 } from './latin1.js'
import { getTable } from './multi-byte.table.js'

export const E_STRICT = 'Input is not well-formed for this encoding'

/* Decoders */

// If the decoder is not cleared properly, state can be preserved between non-streaming calls!
// See comment about fatal stream

// All except iso-2022-jp are ASCII supersets
// When adding something that is not an ASCII superset, ajust the ASCII fast path
const mappers = {
  // https://encoding.spec.whatwg.org/#euc-kr-decoder
  'euc-kr': (err) => {
    const euc = getTable('euc-kr')
    let lead = 0
    let oi = 0
    let o16

    const decodeLead = (b) => {
      if (b < 0x41 || b > 0xfe) {
        lead = 0
        o16[oi++] = err()
        if (b < 128) o16[oi++] = b
      } else {
        const p = euc[(lead - 0x81) * 190 + b - 0x41]
        lead = 0
        if (p) {
          o16[oi++] = p
        } else {
          o16[oi++] = err()
          if (b < 128) o16[oi++] = b
        }
      }
    }

    const decode = (arr, start, end, stream) => {
      let i = start
      o16 = new Uint16Array(end - start + (lead ? 1 : 0)) // there are pairs but they consume more than one byte
      oi = 0

      // Fast path
      if (!lead) {
        for (const last1 = end - 1; i < last1; ) {
          const l = arr[i]
          if (l < 128) {
            o16[oi++] = l
            i++
          } else {
            if (l === 0x80 || l === 0xff) break
            const b = arr[i + 1]
            if (b < 0x41 || b === 0xff) break
            const p = euc[(l - 0x81) * 190 + b - 0x41]
            if (!p) break
            o16[oi++] = p
            i += 2
          }
        }
      }

      if (lead && i < end) decodeLead(arr[i++])
      while (i < end) {
        const b = arr[i++]
        if (b < 128) {
          o16[oi++] = b
        } else if (b === 0x80 || b === 0xff) {
          o16[oi++] = err()
        } else {
          lead = b
          if (i < end) decodeLead(arr[i++])
        }
      }

      if (lead && !stream) {
        lead = 0
        o16[oi++] = err()
      }

      const res = decodeUCS2(o16, oi)
      o16 = null
      return res
    }

    return { decode, isAscii: () => lead === 0 }
  },
  // https://encoding.spec.whatwg.org/#euc-jp-decoder
  'euc-jp': (err) => {
    const jis0208 = getTable('jis0208')
    const jis0212 = getTable('jis0212')
    let j12 = false
    let lead = 0
    let oi = 0
    let o16

    const decodeLead = (b) => {
      if (lead === 0x8e && b >= 0xa1 && b <= 0xdf) {
        lead = 0
        o16[oi++] = 0xfe_c0 + b
      } else if (lead === 0x8f && b >= 0xa1 && b <= 0xfe) {
        j12 = true
        lead = b
      } else {
        let cp
        if (lead >= 0xa1 && lead <= 0xfe && b >= 0xa1 && b <= 0xfe) {
          cp = (j12 ? jis0212 : jis0208)[(lead - 0xa1) * 94 + b - 0xa1]
        }

        lead = 0
        j12 = false
        if (cp) {
          o16[oi++] = cp
        } else {
          o16[oi++] = err()
          if (b < 128) o16[oi++] = b
        }
      }
    }

    const decode = (arr, start, end, stream) => {
      let i = start
      o16 = new Uint16Array(end - start + (lead ? 1 : 0))
      oi = 0

      // Fast path, non-j12
      // lead = 0 means j12 = 0
      if (!lead) {
        for (const last1 = end - 1; i < last1; ) {
          const l = arr[i]
          if (l < 128) {
            o16[oi++] = l
            i++
          } else {
            const b = arr[i + 1]
            if (l === 0x8e && b >= 0xa1 && b <= 0xdf) {
              o16[oi++] = 0xfe_c0 + b
              i += 2
            } else {
              if (l < 0xa1 || l === 0xff || b < 0xa1 || b === 0xff) break
              const cp = jis0208[(l - 0xa1) * 94 + b - 0xa1]
              if (!cp) break
              o16[oi++] = cp
              i += 2
            }
          }
        }
      }

      if (lead && i < end) decodeLead(arr[i++])
      if (lead && i < end) decodeLead(arr[i++]) // could be two leads, but no more
      while (i < end) {
        const b = arr[i++]
        if (b < 128) {
          o16[oi++] = b
        } else if ((b < 0xa1 && b !== 0x8e && b !== 0x8f) || b === 0xff) {
          o16[oi++] = err()
        } else {
          lead = b
          if (i < end) decodeLead(arr[i++])
          if (lead && i < end) decodeLead(arr[i++]) // could be two leads
        }
      }

      if (lead && !stream) {
        lead = 0
        j12 = false // can be true only when lead is non-zero
        o16[oi++] = err()
      }

      const res = decodeUCS2(o16, oi)
      o16 = null
      return res
    }

    return { decode, isAscii: () => lead === 0 } // j12 can be true only when lead is non-zero
  },
  // https://encoding.spec.whatwg.org/#iso-2022-jp-decoder
  'iso-2022-jp': (err) => {
    const jis0208 = getTable('jis0208')
    let dState = 1
    let oState = 1
    let lead = 0 // 0 or 0x21-0x7e
    let out = false

    const bytes = (pushback, b) => {
      if (dState < 5 && b === 0x1b) {
        dState = 6 // escape start
        return
      }

      switch (dState) {
        case 1:
        case 2:
          // ASCII, Roman (common)
          out = false
          if (dState === 2) {
            if (b === 0x5c) return 0xa5
            if (b === 0x7e) return 0x20_3e
          }

          if (b <= 0x7f && b !== 0x0e && b !== 0x0f) return b
          return err()
        case 3:
          // Katakana
          out = false
          if (b >= 0x21 && b <= 0x5f) return 0xff_40 + b
          return err()
        case 4:
          // Leading byte
          out = false
          if (b < 0x21 || b > 0x7e) return err()
          lead = b
          dState = 5
          return
        case 5:
          // Trailing byte
          out = false
          if (b === 0x1b) {
            dState = 6 // escape start
            return err()
          }

          dState = 4
          if (b >= 0x21 && b <= 0x7e) {
            const cp = jis0208[(lead - 0x21) * 94 + b - 0x21]
            if (cp) return cp
          }

          return err()
        case 6:
          // Escape start
          if (b === 0x24 || b === 0x28) {
            lead = b
            dState = 7
            return
          }

          out = false
          dState = oState
          pushback.push(b)
          return err()
        case 7: {
          // Escape
          const l = lead
          lead = 0
          let s
          if (l === 0x28) {
            // eslint-disable-next-line unicorn/prefer-switch
            if (b === 0x42) {
              s = 1
            } else if (b === 0x4a) {
              s = 2
            } else if (b === 0x49) {
              s = 3
            }
          } else if (l === 0x24 && (b === 0x40 || b === 0x42)) {
            s = 4
          }

          if (s) {
            dState = oState = s
            const output = out
            out = true
            return output ? err() : undefined
          }

          out = false
          dState = oState
          pushback.push(b, l)
          return err()
        }
      }
    }

    const eof = (pushback) => {
      if (dState < 5) return null
      out = false
      switch (dState) {
        case 5:
          dState = 4
          return err()
        case 6:
          dState = oState
          return err()
        case 7: {
          dState = oState
          pushback.push(lead)
          lead = 0
          return err()
        }
      }
    }

    const decode = (arr, start, end, stream) => {
      const o16 = new Uint16Array(end - start + 2) // err in eof + lead from state
      let oi = 0
      let i = start
      const pushback = [] // local and auto-cleared

      // First, dump everything until EOF
      // Same as the full loop, but without EOF handling
      while (i < end || pushback.length > 0) {
        const c = bytes(pushback, pushback.length > 0 ? pushback.pop() : arr[i++])
        if (c !== undefined) o16[oi++] = c // 16-bit
      }

      // Then, dump EOF. This needs the same loop as the characters can be pushed back
      if (!stream) {
        while (i <= end || pushback.length > 0) {
          if (i < end || pushback.length > 0) {
            const c = bytes(pushback, pushback.length > 0 ? pushback.pop() : arr[i++])
            if (c !== undefined) o16[oi++] = c // 16-bit
          } else {
            const c = eof(pushback)
            if (c === null) break // clean exit
            o16[oi++] = c
          }
        }
      }

      // Chrome and WebKit fail on this, we don't: completely destroy the old decoder state when finished streaming
      // > If this’s do not flush is false, then set this’s decoder to a new instance of this’s encoding’s decoder,
      // > Set this’s do not flush to options["stream"]
      if (!stream) {
        dState = oState = 1
        lead = 0
        out = false
      }

      return decodeUCS2(o16, oi)
    }

    return { decode, isAscii: () => false }
  },
  // https://encoding.spec.whatwg.org/#shift_jis-decoder
  shift_jis: (err) => {
    const jis0208 = getTable('jis0208')
    let lead = 0
    let oi = 0
    let o16

    const decodeLead = (b) => {
      const l = lead
      lead = 0
      if (b >= 0x40 && b <= 0xfc && b !== 0x7f) {
        const p = (l - (l < 0xa0 ? 0x81 : 0xc1)) * 188 + b - (b < 0x7f ? 0x40 : 0x41)
        if (p >= 8836 && p <= 10_715) {
          o16[oi++] = 0xe0_00 - 8836 + p
          return
        }

        const cp = jis0208[p]
        if (cp) {
          o16[oi++] = cp
          return
        }
      }

      o16[oi++] = err()
      if (b < 128) o16[oi++] = b
    }

    const decode = (arr, start, end, stream) => {
      o16 = new Uint16Array(end - start + (lead ? 1 : 0))
      oi = 0
      let i = start

      // Fast path
      if (!lead) {
        for (const last1 = end - 1; i < last1; ) {
          const l = arr[i]
          if (l <= 0x80) {
            o16[oi++] = l
            i++
          } else if (l >= 0xa1 && l <= 0xdf) {
            o16[oi++] = 0xfe_c0 + l
            i++
          } else {
            if (l === 0xa0 || l > 0xfc) break
            const b = arr[i + 1]
            if (b < 0x40 || b > 0xfc || b === 0x7f) break
            const p = (l - (l < 0xa0 ? 0x81 : 0xc1)) * 188 + b - (b < 0x7f ? 0x40 : 0x41)
            if (p >= 8836 && p <= 10_715) {
              o16[oi++] = 0xe0_00 - 8836 + p
              i += 2
            } else {
              const cp = jis0208[p]
              if (!cp) break
              o16[oi++] = cp
              i += 2
            }
          }
        }
      }

      if (lead && i < end) decodeLead(arr[i++])
      while (i < end) {
        const b = arr[i++]
        if (b <= 0x80) {
          o16[oi++] = b // 0x80 is allowed
        } else if (b >= 0xa1 && b <= 0xdf) {
          o16[oi++] = 0xfe_c0 + b
        } else if (b === 0xa0 || b > 0xfc) {
          o16[oi++] = err()
        } else {
          lead = b
          if (i < end) decodeLead(arr[i++])
        }
      }

      if (lead && !stream) {
        lead = 0
        o16[oi++] = err()
      }

      const res = decodeUCS2(o16, oi)
      o16 = null
      return res
    }

    return { decode, isAscii: () => lead === 0 }
  },
  // https://encoding.spec.whatwg.org/#gbk-decoder
  gbk: (err) => mappers.gb18030(err), // 10.1.1. GBK’s decoder is gb18030’s decoder
  // https://encoding.spec.whatwg.org/#gb18030-decoder
  gb18030: (err) => {
    const gb18030 = getTable('gb18030')
    const gb18030r = getTable('gb18030-ranges')
    let g1 = 0, g2 = 0, g3 = 0 // prettier-ignore
    const index = (p) => {
      if ((p > 39_419 && p < 189_000) || p > 1_237_575) return
      if (p === 7457) return 0xe7_c7
      let a = 0, b = 0 // prettier-ignore
      for (const [c, d] of gb18030r) {
        if (c > p) break
        a = c
        b = d
      }

      return b + p - a
    }

    // g1 is 0 or 0x81-0xfe
    // g2 is 0 or 0x30-0x39
    // g3 is 0 or 0x81-0xfe

    const decode = (arr, start, end, stream) => {
      const o16 = new Uint16Array(end - start + (g1 ? 3 : 0)) // even with pushback it's at most 1 char per byte
      let oi = 0
      let i = start
      const pushback = [] // local and auto-cleared

      // Fast path for 2-byte only
      // pushback is always empty ad start, and g1 = 0 means g2 = g3 = 0
      if (g1 === 0) {
        for (const last1 = end - 1; i < last1; ) {
          const b = arr[i]
          if (b < 128) {
            o16[oi++] = b
            i++
          } else if (b === 0x80) {
            o16[oi++] = 0x20_ac
            i++
          } else {
            if (b === 0xff) break
            const n = arr[i + 1]
            let cp
            if (n < 0x7f) {
              if (n < 0x40) break
              cp = gb18030[(b - 0x81) * 190 + n - 0x40]
            } else {
              if (n === 0xff || n === 0x7f) break
              cp = gb18030[(b - 0x81) * 190 + n - 0x41]
            }

            if (!cp) break
            o16[oi++] = cp // 16-bit
            i += 2
          }
        }
      }

      // First, dump everything until EOF
      // Same as the full loop, but without EOF handling
      while (i < end || pushback.length > 0) {
        const b = pushback.length > 0 ? pushback.pop() : arr[i++]
        if (g1) {
          // g2 can be set only when g1 is set, g3 can be set only when g2 is set
          // hence, 3 checks for g3 is faster than 3 checks for g1
          if (g2) {
            if (g3) {
              if (b <= 0x39 && b >= 0x30) {
                const p = index(
                  (g1 - 0x81) * 12_600 + (g2 - 0x30) * 1260 + (g3 - 0x81) * 10 + b - 0x30
                )
                g1 = g2 = g3 = 0
                if (p === undefined) {
                  o16[oi++] = err()
                } else if (p <= 0xff_ff) {
                  o16[oi++] = p // Can validly return replacement
                } else {
                  const d = p - 0x1_00_00
                  o16[oi++] = 0xd8_00 | (d >> 10)
                  o16[oi++] = 0xdc_00 | (d & 0x3_ff)
                }
              } else {
                pushback.push(b, g3, g2)
                g1 = g2 = g3 = 0
                o16[oi++] = err()
              }
            } else if (b >= 0x81 && b <= 0xfe) {
              g3 = b
            } else {
              pushback.push(b, g2)
              g1 = g2 = 0
              o16[oi++] = err()
            }
          } else if (b <= 0x39 && b >= 0x30) {
            g2 = b
          } else {
            let cp
            if (b >= 0x40 && b <= 0xfe && b !== 0x7f) {
              cp = gb18030[(g1 - 0x81) * 190 + b - (b < 0x7f ? 0x40 : 0x41)]
            }

            g1 = 0
            if (cp) {
              o16[oi++] = cp // 16-bit
            } else {
              o16[oi++] = err()
              if (b < 128) o16[oi++] = b // can be processed immediately
            }
          }
        } else if (b < 128) {
          o16[oi++] = b
        } else if (b === 0x80) {
          o16[oi++] = 0x20_ac
        } else if (b === 0xff) {
          o16[oi++] = err()
        } else {
          g1 = b
        }
      }

      // if g1 = 0 then g2 = g3 = 0
      if (g1 && !stream) {
        g1 = g2 = g3 = 0
        o16[oi++] = err()
      }

      return decodeUCS2(o16, oi)
    }

    return { decode, isAscii: () => g1 === 0 } // if g1 = 0 then g2 = g3 = 0
  },
  // https://encoding.spec.whatwg.org/#big5
  big5: (err) => {
    // The only decoder which returns multiple codepoints per byte, also has non-charcode codepoints
    // We store that as strings
    const big5 = getTable('big5')
    let lead = 0
    let oi = 0
    let o16

    const decodeLead = (b) => {
      if (b < 0x40 || (b > 0x7e && b < 0xa1) || b === 0xff) {
        lead = 0
        o16[oi++] = err()
        if (b < 128) o16[oi++] = b
      } else {
        const p = big5[(lead - 0x81) * 157 + b - (b < 0x7f ? 0x40 : 0x62)]
        lead = 0
        if (p > 0x1_00_00) {
          o16[oi++] = p >> 16
          o16[oi++] = p & 0xff_ff
        } else if (p) {
          o16[oi++] = p
        } else {
          o16[oi++] = err()
          if (b < 128) o16[oi++] = b
        }
      }
    }

    // eslint-disable-next-line sonarjs/no-identical-functions
    const decode = (arr, start, end, stream) => {
      let i = start
      o16 = new Uint16Array(end - start + (lead ? 1 : 0)) // there are pairs but they consume more than one byte
      oi = 0

      // Fast path
      if (!lead) {
        for (const last1 = end - 1; i < last1; ) {
          const l = arr[i]
          if (l < 128) {
            o16[oi++] = l
            i++
          } else {
            if (l === 0x80 || l === 0xff) break
            const b = arr[i + 1]
            if (b < 0x40 || (b > 0x7e && b < 0xa1) || b === 0xff) break
            const p = big5[(l - 0x81) * 157 + b - (b < 0x7f ? 0x40 : 0x62)]
            if (p > 0x1_00_00) {
              o16[oi++] = p >> 16
              o16[oi++] = p & 0xff_ff
            } else {
              if (!p) break
              o16[oi++] = p
            }

            i += 2
          }
        }
      }

      if (lead && i < end) decodeLead(arr[i++])
      while (i < end) {
        const b = arr[i++]
        if (b < 128) {
          o16[oi++] = b
        } else if (b === 0x80 || b === 0xff) {
          o16[oi++] = err()
        } else {
          lead = b
          if (i < end) decodeLead(arr[i++])
        }
      }

      if (lead && !stream) {
        lead = 0
        o16[oi++] = err()
      }

      const res = decodeUCS2(o16, oi)
      o16 = null
      return res
    }

    return { decode, isAscii: () => lead === 0 }
  },
}

export const isAsciiSuperset = (enc) => enc !== 'iso-2022-jp' // all others are ASCII supersets and can use fast path

export function multibyteDecoder(enc, loose = false) {
  if (typeof loose !== 'boolean') throw new TypeError('loose option should be boolean')
  if (!Object.hasOwn(mappers, enc)) throw new RangeError('Unsupported encoding')

  // Input is assumed to be typechecked already
  let mapper
  const asciiSuperset = isAsciiSuperset(enc)
  let streaming // because onErr is cached in mapper
  const onErr = loose
    ? () => 0xff_fd
    : () => {
        // The correct way per spec seems to be not destoying the decoder state in stream mode, even when fatal
        // Decoders big5, euc-jp, euc-kr, shift_jis, gb18030 / gbk - all clear state before throwing unless EOF, so not affected
        // iso-2022-jp is the only tricky one one where this !stream check matters in non-stream mode
        if (!streaming) mapper = null // destroy state, effectively the same as 'do not flush' = false, but early
        throw new TypeError(E_STRICT)
      }

  return (arr, stream = false) => {
    let res = ''
    if (asciiSuperset && (!mapper || mapper.isAscii?.())) {
      const prefixLen = asciiPrefix(arr)
      if (prefixLen === arr.length) return decodeAscii(arr) // ascii
      res = decodeLatin1(arr, 0, prefixLen) // TODO: check if decodeAscii with subarray is faster for small prefixes too
    }

    streaming = stream // affects onErr
    if (!mapper) mapper = mappers[enc](onErr)
    return res + mapper.decode(arr, res.length, arr.length, stream)
  }
}

/* Encoders */

const maps = new Map()
const e7 = [[148, 236], [149, 237], [150, 243]] // prettier-ignore
const e8 = [[30, 89], [38, 97], [43, 102], [44, 103], [50, 109], [67, 126], [84, 144], [100, 160]] // prettier-ignore
const preencoders = {
  __proto__: null,
  big5: (p) => ((((p / 157) | 0) + 0x81) << 8) | ((p % 157 < 0x3f ? 0x40 : 0x62) + (p % 157)),
  shift_jis: (p) => {
    const l = (p / 188) | 0
    const t = p % 188
    return ((l + (l < 0x1f ? 0x81 : 0xc1)) << 8) | ((t < 0x3f ? 0x40 : 0x41) + t)
  },
  'iso-2022-jp': (p) => ((((p / 94) | 0) + 0x21) << 8) | ((p % 94) + 0x21),
  'euc-jp': (p) => ((((p / 94) | 0) + 0xa1) << 8) | ((p % 94) + 0xa1),
  'euc-kr': (p) => ((((p / 190) | 0) + 0x81) << 8) | ((p % 190) + 0x41),
  gb18030: (p) => ((((p / 190) | 0) + 0x81) << 8) | ((p % 190 < 0x3f ? 0x40 : 0x41) + (p % 190)),
}

preencoders.gbk = preencoders.gb18030

// We accept that encoders use non-trivial amount of mem, for perf
// most are are 128 KiB mem, big5 is 380 KiB, lazy-loaded at first use
function getMap(id, size, ascii) {
  const cached = maps.get(id)
  if (cached) return cached
  let tname = id
  const sjis = id === 'shift_jis'
  const iso2022jp = id === 'iso-2022-jp'
  if (iso2022jp) tname = 'jis0208'
  if (id === 'gbk') tname = 'gb18030'
  if (id === 'euc-jp' || sjis) tname = 'jis0208'
  const table = getTable(tname)
  const map = new Uint16Array(size)
  const enc = preencoders[id] || ((p) => p + 1)
  for (let i = 0; i < table.length; i++) {
    const c = table[i]
    if (!c) continue
    if (id === 'big5') {
      if (i < 5024) continue // this also skips multi-codepoint strings
      // In big5, all return first entries except for these
      if (
        map[c] &&
        c !== 0x25_50 &&
        c !== 0x25_5e &&
        c !== 0x25_61 &&
        c !== 0x25_6a &&
        c !== 0x53_41 &&
        c !== 0x53_45
      ) {
        continue
      }
    } else {
      if (sjis && i >= 8272 && i <= 8835) continue
      if (map[c]) continue
    }

    if (c > 0xff_ff) {
      // always a single codepoint here
      const s = String.fromCharCode(c >> 16, c & 0xff_ff)
      map[s.codePointAt(0)] = enc(i)
    } else {
      map[c] = enc(i)
    }
  }

  if (ascii) for (let i = 0; i < 0x80; i++) map[i] = i
  if (sjis || id === 'euc-jp') {
    if (sjis) map[0x80] = 0x80
    const d = sjis ? 0xfe_c0 : 0x70_c0
    for (let i = 0xff_61; i <= 0xff_9f; i++) map[i] = i - d
    map[0x22_12] = map[0xff_0d]
    map[0xa5] = 0x5c
    map[0x20_3e] = 0x7e
  } else if (tname === 'gb18030') {
    if (id === 'gbk') map[0x20_ac] = 0x80
    for (let i = 0xe7_8d; i <= 0xe7_93; i++) map[i] = i - 0x40_b4
    for (const [a, b] of e7) map[0xe7_00 | a] = 0xa6_00 | b
    for (const [a, b] of e8) map[0xe8_00 | a] = 0xfe_00 | b
  }

  maps.set(id, map)
  return map
}

const NON_LATIN = /[^\x00-\xFF]/ // eslint-disable-line no-control-regex
let gb18030r, katakana

export function multibyteEncoder(enc, onError) {
  if (!Object.hasOwn(mappers, enc)) throw new RangeError('Unsupported encoding')
  const size = enc === 'big5' ? 0x2_f8_a7 : 0x1_00_00 // for big5, max codepoint in table + 1
  const iso2022jp = enc === 'iso-2022-jp'
  const gb18030 = enc === 'gb18030'
  const ascii = isAsciiSuperset(enc)
  const width = iso2022jp ? 5 : gb18030 ? 4 : 2
  const tailsize = iso2022jp ? 3 : 0
  const map = getMap(enc, size, ascii)
  if (gb18030 && !gb18030r) gb18030r = getTable('gb18030-ranges')
  if (iso2022jp && !katakana) katakana = getTable('iso-2022-jp-katakana')
  return (str) => {
    if (typeof str !== 'string') throw new TypeError(E_STRING)
    if (ascii && nativeEncoder && !NON_LATIN.test(str)) {
      const u8 = nativeEncoder.encode(str)
      if (u8.length === str.length) return u8
    }

    const length = str.length
    const u8 = new Uint8Array(length * width + tailsize)
    let i = 0

    if (ascii) {
      while (i < length) {
        const x = str.charCodeAt(i)
        if (x >= 128) break
        u8[i++] = x
      }
    }

    // eslint-disable-next-line unicorn/consistent-function-scoping
    const err = (code) => {
      if (onError) return onError(code, u8, i)
      throw new TypeError(E_STRICT)
    }

    if (!map || map.length < size) /* c8 ignore next */ throw new Error('Unreachable') // Important for perf

    if (iso2022jp) {
      let state = 0 // 0 = ASCII, 1 = Roman, 2 = jis0208
      const restore = () => {
        state = 0
        u8[i++] = 0x1b
        u8[i++] = 0x28
        u8[i++] = 0x42
      }

      for (let j = 0; j < length; j++) {
        let x = str.charCodeAt(j)
        if (x >= 0xd8_00 && x < 0xe0_00) {
          if (state === 2) restore()
          if (x >= 0xdc_00 || j + 1 === length) {
            i += err(x) // lone
          } else {
            const x1 = str.charCodeAt(j + 1)
            if (x1 < 0xdc_00 || x1 >= 0xe0_00) {
              i += err(x) // lone
            } else {
              j++ // consume x1
              i += err(0x1_00_00 + ((x1 & 0x3_ff) | ((x & 0x3_ff) << 10)))
            }
          }
        } else if (x < 0x80) {
          if (state === 2 || (state === 1 && (x === 0x5c || x === 0x7e))) restore()
          if (x === 0xe || x === 0xf || x === 0x1b) {
            i += err(0xff_fd) // 12.2.2. step 3: This returns U+FFFD rather than codePoint to prevent attacks
          } else {
            u8[i++] = x
          }
        } else if (x === 0xa5 || x === 0x20_3e) {
          if (state !== 1) {
            state = 1
            u8[i++] = 0x1b
            u8[i++] = 0x28
            u8[i++] = 0x4a
          }

          u8[i++] = x === 0xa5 ? 0x5c : 0x7e
        } else {
          if (x === 0x22_12) x = 0xff_0d
          if (x >= 0xff_61 && x <= 0xff_9f) x = katakana[x - 0xff_61]
          const e = map[x]
          if (e) {
            if (state !== 2) {
              state = 2
              u8[i++] = 0x1b
              u8[i++] = 0x24
              u8[i++] = 0x42
            }

            u8[i++] = e >> 8
            u8[i++] = e & 0xff
          } else {
            if (state === 2) restore()
            i += err(x)
          }
        }
      }

      if (state) restore()
    } else if (gb18030) {
      // Deduping this branch hurts other encoders perf
      const encode = (cp) => {
        let a = 0, b = 0 // prettier-ignore
        for (const [c, d] of gb18030r) {
          if (d > cp) break
          a = c
          b = d
        }

        let rp = cp === 0xe7_c7 ? 7457 : a + cp - b
        u8[i++] = 0x81 + ((rp / 12_600) | 0)
        rp %= 12_600
        u8[i++] = 0x30 + ((rp / 1260) | 0)
        rp %= 1260
        u8[i++] = 0x81 + ((rp / 10) | 0)
        u8[i++] = 0x30 + (rp % 10)
      }

      for (let j = i; j < length; j++) {
        const x = str.charCodeAt(j)
        if (x >= 0xd8_00 && x < 0xe0_00) {
          if (x >= 0xdc_00 || j + 1 === length) {
            i += err(x) // lone
          } else {
            const x1 = str.charCodeAt(j + 1)
            if (x1 < 0xdc_00 || x1 >= 0xe0_00) {
              i += err(x) // lone
            } else {
              j++ // consume x1
              encode(0x1_00_00 + ((x1 & 0x3_ff) | ((x & 0x3_ff) << 10)))
            }
          }
        } else {
          const e = map[x]
          if (e & 0xff_00) {
            u8[i++] = e >> 8
            u8[i++] = e & 0xff
          } else if (e || x === 0) {
            u8[i++] = e
          } else if (x === 0xe5_e5) {
            i += err(x)
          } else {
            encode(x)
          }
        }
      }
    } else {
      const long =
        enc === 'big5'
          ? (x) => {
              const e = map[x]
              if (e & 0xff_00) {
                u8[i++] = e >> 8
                u8[i++] = e & 0xff
              } else if (e || x === 0) {
                u8[i++] = e
              } else {
                i += err(x)
              }
            }
          : (x) => {
              i += err(x)
            }

      for (let j = i; j < length; j++) {
        const x = str.charCodeAt(j)
        if (x >= 0xd8_00 && x < 0xe0_00) {
          if (x >= 0xdc_00 || j + 1 === length) {
            i += err(x) // lone
          } else {
            const x1 = str.charCodeAt(j + 1)
            if (x1 < 0xdc_00 || x1 >= 0xe0_00) {
              i += err(x) // lone
            } else {
              j++ // consume x1
              long(0x1_00_00 + ((x1 & 0x3_ff) | ((x & 0x3_ff) << 10)))
            }
          }
        } else {
          const e = map[x]
          if (e & 0xff_00) {
            u8[i++] = e >> 8
            u8[i++] = e & 0xff
          } else if (e || x === 0) {
            u8[i++] = e
          } else {
            i += err(x)
          }
        }
      }
    }

    return i === u8.length ? u8 : u8.slice(0, i)
  }
}
