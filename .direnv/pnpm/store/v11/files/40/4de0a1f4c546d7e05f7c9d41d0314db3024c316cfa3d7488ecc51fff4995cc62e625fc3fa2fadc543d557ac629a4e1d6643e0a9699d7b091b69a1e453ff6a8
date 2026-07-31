/**
 * Same as `@exodus/bytes/encoding.js`, but in browsers instead of polyfilling just uses whatever the
 * browser provides, drastically reducing the bundle size (to less than 2 KiB gzipped).
 *
 * Does not provide `isomorphicDecode` and `isomorphicEncode` exports.
 *
 * ```js
 * import { TextDecoder, TextEncoder } from '@exodus/bytes/encoding-browser.js'
 * import { TextDecoderStream, TextEncoderStream } from '@exodus/bytes/encoding-browser.js' // Requires Streams
 *
 * // Hooks for standards
 * import { getBOMEncoding, legacyHookDecode, labelToName, normalizeEncoding } from '@exodus/bytes/encoding-browser.js'
 * ```
 *
 * Under non-browser engines (Node.js, React Native, etc.) a full polyfill is used as those platforms
 * do not provide sufficiently complete / non-buggy `TextDecoder` APIs.
 *
 * > [!NOTE]
 * > Implementations in browsers [have bugs](https://docs.google.com/spreadsheets/d/1pdEefRG6r9fZy61WHGz0TKSt8cO4ISWqlpBN5KntIvQ/edit),
 * > but they are fixing them and the expected update window is short.\
 * > If you want to circumvent browser bugs, use full `@exodus/bytes/encoding.js` import.
 *
 * @module @exodus/bytes/encoding-browser.js
 */

export {
  TextDecoder,
  TextEncoder,
  TextDecoderStream,
  TextEncoderStream,
  normalizeEncoding,
  getBOMEncoding,
  labelToName,
  legacyHookDecode,
} from './encoding.js'
