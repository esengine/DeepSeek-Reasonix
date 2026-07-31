import { hash } from 'node:crypto'
import { makeBase58check } from './fallback/base58check.js'

const sha256 = (x) => hash('sha256', x, 'buffer')
const hash256 = (x) => sha256(sha256(x))
const {
  encode: toBase58check,
  decode: fromBase58check,
  encodeSync: toBase58checkSync,
  decodeSync: fromBase58checkSync,
} = makeBase58check(hash256, hash256)

export { makeBase58check } from './fallback/base58check.js'
export { toBase58check, fromBase58check, toBase58checkSync, fromBase58checkSync }
