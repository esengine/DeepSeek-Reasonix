import { sha256 } from '@noble/hashes/sha2.js'
import { makeBase58check } from './fallback/base58check.js'

// Note: while API is async, we use hashSync for now until we improve webcrypto perf for hash256
// Inputs to base58 are typically very small, and that makes a difference

// Note: using native WebCrypto will have to have account for SharedArrayBuffer

const hash256sync = (x) => sha256(sha256(x))
const hash256 = hash256sync // See note at the top

const b58c = /* @__PURE__ */ makeBase58check(hash256, hash256sync)
export const toBase58check = /* @__PURE__ */ (() => b58c.encode)()
export const fromBase58check = /* @__PURE__ */ (() => b58c.decode)()
export const toBase58checkSync = /* @__PURE__ */ (() => b58c.encodeSync)()
export const fromBase58checkSync = /* @__PURE__ */ (() => b58c.decodeSync)()

export { makeBase58check } from './fallback/base58check.js'
