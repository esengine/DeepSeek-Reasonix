import { toBase58checkSync, fromBase58checkSync } from '@exodus/bytes/base58check.js'
import { assertUint8 } from './assert.js'

// Mostly matches npmjs.com/wif, but with extra checks + using our base58check
// Also no inconsistent behavior on Buffer/Uint8Array input

function from(arr, expectedVersion) {
  assertUint8(arr)
  if (arr.length !== 33 && arr.length !== 34) throw new Error('Invalid WIF length')
  const version = arr[0]
  if (expectedVersion !== undefined && version !== expectedVersion) {
    throw new Error('Invalid network version')
  }

  // Makes a copy, regardless of input being a Buffer or a Uint8Array (unlike .slice)
  const privateKey = Uint8Array.from(arr.subarray(1, 33))
  if (arr.length === 33) return { version, privateKey, compressed: false }
  if (arr[33] !== 1) throw new Error('Invalid compression flag')
  return { version, privateKey, compressed: true }
}

function to({ version: v, privateKey, compressed }) {
  if (!Number.isSafeInteger(v) || v < 0 || v > 0xff) throw new Error('Missing or invalid version')
  assertUint8(privateKey, { length: 32, name: 'privateKey' })
  const out = new Uint8Array(compressed ? 34 : 33)
  out[0] = v
  out.set(privateKey, 1)
  if (compressed) out[33] = 1
  return out
}

// Async performance is worse here, so expose the same internal methods as sync for now
// ./base58check is sync internally anyway for now, so doesn't matter until that is changed

export const fromWifStringSync = (string, version) => from(fromBase58checkSync(string), version)
// export const fromWifString = async (string, version) => from(await fromBase58check(string), version)
export const fromWifString = async (string, version) => from(fromBase58checkSync(string), version)

export const toWifStringSync = (wif) => toBase58checkSync(to(wif))
// export const toWifString = async (wif) => toBase58check(to(wif))
export const toWifString = async (wif) => toBase58checkSync(to(wif))
