// Get a number of last bytes in an Uint8Array `u` ending at `len` that don't
// form a codepoint yet, but can be a part of a single codepoint on more data
export function unfinishedBytes(u, len, enc) {
  switch (enc) {
    case 'utf-8': {
      // 0-3
      let p = 0
      while (p < 2 && p < len && (u[len - p - 1] & 0xc0) === 0x80) p++ // go back 0-2 trailing bytes
      if (p === len) return 0 // no space for lead
      const l = u[len - p - 1]
      if (l < 0xc2 || l > 0xf4) return 0 // not a lead
      if (p === 0) return 1 // nothing to recheck, we have only lead, return it. 2-byte must return here
      if (l < 0xe0 || (l < 0xf0 && p >= 2)) return 0 // 2-byte, or 3-byte or less and we already have 2 trailing
      const lower = l === 0xf0 ? 0x90 : l === 0xe0 ? 0xa0 : 0x80
      const upper = l === 0xf4 ? 0x8f : l === 0xed ? 0x9f : 0xbf
      const n = u[len - p]
      return n >= lower && n <= upper ? p + 1 : 0
    }

    case 'utf-16le':
    case 'utf-16be': {
      // 0-3
      const p = len % 2 // uneven byte length adds 1
      if (len < 2) return p
      const l = len - p - 1
      const last = enc === 'utf-16le' ? (u[l] << 8) ^ u[l - 1] : (u[l - 1] << 8) ^ u[l]
      return last >= 0xd8_00 && last < 0xdc_00 ? p + 2 : p // lone lead adds 2
    }
  }

  throw new Error('Unsupported encoding')
}

// Merge prefix `chunk` with `u` and return new combined prefix
// For u.length < 3, fully consumes u and can return unfinished data,
// otherwise returns a prefix with no unfinished bytes
export function mergePrefix(u, chunk, enc) {
  if (u.length === 0) return chunk
  const cl = chunk.length
  if (u.length < 3) {
    // No reason to bruteforce offsets, also it's possible this doesn't yet end the sequence
    const a = new Uint8Array(cl + u.length)
    a.set(chunk)
    a.set(u, cl)
    return a
  }

  // Slice off a small portion of u into prefix chunk so we can decode them separately without extending array size
  const t = new Uint8Array(cl + 3) // We have 1-3 bytes and need 1-3 more bytes
  t.set(chunk)
  t.set(u.subarray(0, 3), cl)

  // Stop at the first offset where unfinished bytes reaches 0 or fits into u
  // If that doesn't happen (u too short), just concat chunk and u completely (above)
  for (let i = 1; i <= 3; i++) {
    const unfinished = unfinishedBytes(t, cl + i, enc) // 0-3
    if (unfinished <= i) {
      // Always reachable at 3, but we still need 'unfinished' value for it
      const add = i - unfinished // 0-3
      return add > 0 ? t.subarray(0, cl + add) : chunk
    }
  }

  // Unreachable
}
