/// <reference types="mdast-util-math" />
/// <reference types="remark-parse" />
/// <reference types="remark-stringify" />

/**
 * @typedef {import('mdast').Root} Root
 * @typedef {import('mdast-util-math').ToOptions} Options
 * @typedef {import('unified').Processor<Root>} Processor
 */

import {mathFromMarkdown, mathToMarkdown} from 'mdast-util-math'
import {math} from 'micromark-extension-math'

/** @type {Readonly<Options>} */
const emptyOptions = {}

/**
 * Add support for math.
 *
 * @param {Readonly<Options> | null | undefined} [options]
 *   Configuration (optional).
 * @returns {undefined}
 *   Nothing.
 */
export default function remarkMath(options) {
  // @ts-expect-error: TS is wrong about `this`.
  // eslint-disable-next-line unicorn/no-this-assignment
  const self = /** @type {Processor} */ (this)
  const settings = options || emptyOptions
  const data = self.data()

  const micromarkExtensions =
    data.micromarkExtensions || (data.micromarkExtensions = [])
  const fromMarkdownExtensions =
    data.fromMarkdownExtensions || (data.fromMarkdownExtensions = [])
  const toMarkdownExtensions =
    data.toMarkdownExtensions || (data.toMarkdownExtensions = [])

  micromarkExtensions.push(math(settings))
  fromMarkdownExtensions.push(mathFromMarkdown())
  toMarkdownExtensions.push(mathToMarkdown(settings))
}
