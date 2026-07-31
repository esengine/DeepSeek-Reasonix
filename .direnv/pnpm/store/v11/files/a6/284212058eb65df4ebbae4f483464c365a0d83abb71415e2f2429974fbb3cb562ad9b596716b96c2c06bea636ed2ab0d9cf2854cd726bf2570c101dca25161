import { decodeAscii, encodeLatin1 } from './latin1.js'
import { decode2string } from './platform.js'

const ERR = 'percentEncodeSet must be a string of unique increasing codepoints in range 0x20 - 0x7e'
const percentMap = new Map()
let hex, base

export function percentEncoder(set, spaceAsPlus = false) {
  if (typeof set !== 'string' || /[^\x20-\x7E]/.test(set)) throw new TypeError(ERR)
  if (typeof spaceAsPlus !== 'boolean') throw new TypeError('spaceAsPlus must be boolean')
  const id = set + +spaceAsPlus
  const cached = percentMap.get(id)
  if (cached) return cached

  const n = encodeLatin1(set).sort() // string checked above to be ascii
  if (decodeAscii(n) !== set || new Set(n).size !== n.length) throw new TypeError(ERR)

  if (!base) {
    hex = Array.from({ length: 256 }, (_, i) => `%${i.toString(16).padStart(2, '0').toUpperCase()}`)
    base = hex.map((h, i) => (i < 0x20 || i > 0x7e ? h : String.fromCharCode(i)))
  }

  const map = base.slice() // copy
  for (const c of n) map[c] = hex[c]
  if (spaceAsPlus) map[0x20] = '+' // overrides whatever percentEncodeSet thinks about it

  // Input is not typechecked, for internal use only
  const percentEncode = (u8, start = 0, end = u8.length) => decode2string(u8, start, end, map)
  percentMap.set(id, percentEncode)
  return percentEncode
}
