/**
 * @import {ElementContent, Root} from 'hast'
 * @import {KatexOptions} from 'katex'
 * @import {VFile} from 'vfile'
 */

/**
 * @typedef {Omit<KatexOptions, 'displayMode' | 'throwOnError'>} Options
 */

import {fromHtmlIsomorphic} from 'hast-util-from-html-isomorphic'
import {toText} from 'hast-util-to-text'
import katex from 'katex'
import {SKIP, visitParents} from 'unist-util-visit-parents'

/** @type {Readonly<Options>} */
const emptyOptions = {}
/** @type {ReadonlyArray<unknown>} */
const emptyClasses = []

/**
 * Render elements with a `language-math` (or `math-display`, `math-inline`)
 * class with KaTeX.
 *
 * @param {Readonly<Options> | null | undefined} [options]
 *   Configuration (optional).
 * @returns
 *   Transform.
 */
export default function rehypeKatex(options) {
  const settings = options || emptyOptions

  /**
   * Transform.
   *
   * @param {Root} tree
   *   Tree.
   * @param {VFile} file
   *   File.
   * @returns {undefined}
   *   Nothing.
   */
  return function (tree, file) {
    visitParents(tree, 'element', function (element, parents) {
      const classes = Array.isArray(element.properties.className)
        ? element.properties.className
        : emptyClasses
      // This class can be generated from markdown with ` ```math `.
      const languageMath = classes.includes('language-math')
      // This class is used by `remark-math` for flow math (block, `$$\nmath\n$$`).
      const mathDisplay = classes.includes('math-display')
      // This class is used by `remark-math` for text math (inline, `$math$`).
      const mathInline = classes.includes('math-inline')
      let displayMode = mathDisplay

      // Any class is fine.
      if (!languageMath && !mathDisplay && !mathInline) {
        return
      }

      let parent = parents[parents.length - 1]
      let scope = element

      // If this was generated with ` ```math `, replace the `<pre>` and use
      // display.
      if (
        element.tagName === 'code' &&
        languageMath &&
        parent &&
        parent.type === 'element' &&
        parent.tagName === 'pre'
      ) {
        scope = parent
        parent = parents[parents.length - 2]
        displayMode = true
      }

      /* c8 ignore next -- verbose to test. */
      if (!parent) return

      const value = toText(scope, {whitespace: 'pre'})

      /** @type {Array<ElementContent> | string | undefined} */
      let result

      try {
        result = katex.renderToString(value, {
          ...settings,
          displayMode,
          throwOnError: true
        })
      } catch (error) {
        const cause = /** @type {Error} */ (error)
        const ruleId = cause.name.toLowerCase()

        file.message('Could not render math with KaTeX', {
          ancestors: [...parents, element],
          cause,
          place: element.position,
          ruleId,
          source: 'rehype-katex'
        })

        // KaTeX *should* handle `ParseError` itself, but not others.
        // it doesn’t always:
        // <https://github.com/remarkjs/react-markdown/issues/853>
        try {
          result = katex.renderToString(value, {
            ...settings,
            displayMode,
            strict: 'ignore',
            throwOnError: false
          })
        } catch {
          // Generate similar markup if this is an other error.
          // See: <https://github.com/KaTeX/KaTeX/blob/5dc7af0/docs/error.md>.
          result = [
            {
              type: 'element',
              tagName: 'span',
              properties: {
                className: ['katex-error'],
                style: 'color:' + (settings.errorColor || '#cc0000'),
                title: String(error)
              },
              children: [{type: 'text', value}]
            }
          ]
        }
      }

      if (typeof result === 'string') {
        const root = fromHtmlIsomorphic(result, {fragment: true})
        // Cast as we don’t expect `doctypes` in KaTeX result.
        result = /** @type {Array<ElementContent>} */ (root.children)
      }

      const index = parent.children.indexOf(scope)
      parent.children.splice(index, 1, ...result)
      return SKIP
    })
  }
}
