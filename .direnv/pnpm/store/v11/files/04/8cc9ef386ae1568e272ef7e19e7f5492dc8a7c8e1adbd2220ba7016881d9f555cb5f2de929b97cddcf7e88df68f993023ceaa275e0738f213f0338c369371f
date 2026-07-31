export function assertEmptyRest(rest) {
  if (Object.keys(rest).length > 0) throw new TypeError('Unexpected extra options')
}

// eslint-disable-next-line sonarjs/no-nested-template-literals
const makeMessage = (name, extra) => `Expected${name ? ` ${name} to be` : ''} an Uint8Array${extra}`

const TypedArray = Object.getPrototypeOf(Uint8Array)

export function assertTypedArray(arr) {
  if (arr && arr instanceof TypedArray) return
  throw new TypeError('Expected a TypedArray instance')
}

export function assertUint8(arr, options) {
  if (!options) {
    // fast path
    if (arr instanceof Uint8Array) return
    throw new TypeError('Expected an Uint8Array')
  }

  const { name, length, ...rest } = options
  assertEmptyRest(rest)
  if (arr instanceof Uint8Array && (length === undefined || arr.length === length)) return
  throw new TypeError(makeMessage(name, length === undefined ? '' : ` of size ${Number(length)}`))
}
