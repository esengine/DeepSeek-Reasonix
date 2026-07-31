# remark-math

[![Build][build-badge]][build]
[![Coverage][coverage-badge]][coverage]
[![Downloads][downloads-badge]][downloads]
[![Size][size-badge]][size]
[![Sponsors][sponsors-badge]][collective]
[![Backers][backers-badge]][collective]
[![Chat][chat-badge]][chat]

**[remark][]** plugin to support math (`$C_L$`).

## Contents

*   [What is this?](#what-is-this)
*   [When should I use this?](#when-should-i-use-this)
*   [Install](#install)
*   [Use](#use)
*   [API](#api)
    *   [`unified().use(remarkMath[, options])`](#unifieduseremarkmath-options)
    *   [`Options`](#options)
*   [Authoring](#authoring)
*   [HTML](#html)
*   [CSS](#css)
*   [Syntax](#syntax)
*   [Syntax tree](#syntax-tree)
*   [Types](#types)
*   [Compatibility](#compatibility)
*   [Security](#security)
*   [Related](#related)
*   [Contribute](#contribute)
*   [License](#license)

## What is this?

This package is a [unified][] ([remark][]) plugin to add support for math
syntax.
You can use this to add support for parsing and serializing this syntax
extension.

As there is no spec for math in markdown, this extension follows how code
(fenced and text) works in Commonmark, but uses dollars (`$`).

## When should I use this?

This project is useful when you want to support math in markdown.
Extending markdown with a syntax extension makes the markdown less portable.
LaTeX equations are also quite hard.
But this mechanism works well when you want authors, that have some LaTeX
experience, to be able to embed rich diagrams of math in scientific text.

If you *just* want to turn markdown into HTML (with maybe a few extensions such
as math), we recommend [`micromark`][micromark] with
[`micromark-extension-math`][micromark-extension-math] instead.
If you don’t use plugins and want to access the syntax tree, you can use
[`mdast-util-from-markdown`][mdast-util-from-markdown] with
[`mdast-util-math`][mdast-util-math].

This plugins adds [fields on nodes][mdast-util-to-hast-fields] so that the
plugin responsible for turning markdown (mdast) into HTML (hast),
[`remark-rehype`][remark-rehype], will turn text math (inline) into
`<code class="language-math math-inline">…</code>` and flow math (block)
into `<pre><code class="language-math math-display">…</code></pre>`.

## Install

This package is [ESM only][esm].
In Node.js (version 16+), install with [npm][]:

```sh
npm install remark-math
```

In Deno with [`esm.sh`][esmsh]:

```js
import remarkMath from 'https://esm.sh/remark-math@6'
```

In browsers with [`esm.sh`][esmsh]:

```html
<script type="module">
  import remarkMath from 'https://esm.sh/remark-math@6?bundle'
</script>
```

## Use

Say our document `example.md` contains:

```markdown
Lift($$L$$) can be determined by Lift Coefficient ($$C_L$$) like the following
equation.

$$
L = \frac{1}{2} \rho v^2 S C_L
$$
```

…and our module `example.js` contains:

```js
import rehypeKatex from 'rehype-katex'
import rehypeStringify from 'rehype-stringify'
import remarkMath from 'remark-math'
import remarkParse from 'remark-parse'
import remarkRehype from 'remark-rehype'
import {read} from 'to-vfile'
import {unified} from 'unified'

const file = await unified()
  .use(remarkParse)
  .use(remarkMath)
  .use(remarkRehype)
  .use(rehypeKatex)
  .use(rehypeStringify)
  .process(await read('example.md'))

console.log(String(file))
```

…then running `node example.js` yields:

```html
<p>Lift(<code class="language-math math-inline"><span class="katex">…</span></code>) like the following
equation.</p>
<pre><code class="language-math math-display"><span class="katex-display"><span class="katex">…</span></span></code></pre>
```

## API

This package exports no identifiers.
The default export is [`remarkMath`][api-remark-math].

### `unified().use(remarkMath[, options])`

Add support for math.

###### Parameters

*   `options` ([`Options`][api-options], optional)
    — configuration

###### Returns

Nothing (`undefined`).

### `Options`

Configuration (TypeScript type).

###### Fields

*   `singleDollarTextMath` (`boolean`, default: `true`)
    — whether to support text math (inline) with a single dollar.
    Single dollars work in Pandoc and many other places, but often interfere
    with “normal” dollars in text.
    If you turn this off, you can still use two or more dollars for text math.

## Authoring

When authoring markdown with math, keep in mind that math doesn’t work in most
places.
Notably, GitHub currently has a really weird crappy client-side regex-based
thing.
But on your own (math-heavy?) site it can be great!

Instead of a syntax extension to markdown, you can also use fenced code with an
info string of `math`:

````markdown
```math
L = \frac{1}{2} \rho v^2 S C_L
```
````

## HTML

This plugin integrates with [`remark-rehype`][remark-rehype].
When markdown (mdast) is turned into HTML (hast) the math nodes are turned
into `<code class="language-math math-inline">…</code>` and
`<pre><code class="language-math math-display">…</code></pre>` elements.

## CSS

This package does not relate to CSS.
You can choose to render the math with KaTeX, MathJax, or something else, which
might need CSS.

## Syntax

See [*Syntax* in
`micromark-extension-math`](https://github.com/micromark/micromark-extension-math#syntax).

## Syntax tree

See [*Syntax tree* in
`mdast-util-math`](https://github.com/syntax-tree/mdast-util-math#syntax-tree).

## Types

This package is fully typed with [TypeScript][].
It exports the additional type [`Options`][api-options].

If you’re working with the syntax tree, you can register the new node types
with `@types/mdast` by adding a reference:

```js
// Register math nodes in mdast:
/// <reference types="mdast-util-math" />

import {visit} from 'unist-util-visit'

function myRemarkPlugin() {
  /**
   * @param {import('mdast').Root} tree
   *   Tree.
   * @returns {undefined}
   *   Nothing.
   */
  return function (tree) {
    visit(tree, function (node) {
      console.log(node) // `node` can now be one of the math nodes.
    })
  }
}
```

## Compatibility

Projects maintained by the unified collective are compatible with maintained
versions of Node.js.

When we cut a new major release, we drop support for unmaintained versions of
Node.
This means we try to keep the current release line, `remark-math@^6`,
compatible with Node.js 16.

This plugin works with unified version 6+ and remark version 14+.
The previous major (version 4) worked with remark 13.

## Security

Use of `remark-math` does not involve **[rehype][]** ([hast][]) or user
content so there are no openings for [cross-site scripting (XSS)][wiki-xss]
attacks.

## Related

*   [`remark-gfm`](https://github.com/remarkjs/remark-gfm)
    — support GFM (autolink literals, footnotes, strikethrough, tables,
    tasklists)
*   [`remark-frontmatter`](https://github.com/remarkjs/remark-frontmatter)
    — support frontmatter (YAML, TOML, and more)
*   [`remark-directive`](https://github.com/remarkjs/remark-directive)
    — support directives
*   [`remark-mdx`](https://github.com/mdx-js/mdx/tree/main/packages/remark-mdx)
    — support MDX (ESM, JSX, expressions)

## Contribute

See [`contributing.md`][contributing] in [`remarkjs/.github`][health] for ways
to get started.
See [`support.md`][support] for ways to get help.

This project has a [code of conduct][coc].
By interacting with this repository, organization, or community you agree to
abide by its terms.

## License

[MIT][license] © [Junyoung Choi][author]

<!-- Definitions -->

[build-badge]: https://github.com/remarkjs/remark-math/workflows/main/badge.svg

[build]: https://github.com/remarkjs/remark-math/actions

[coverage-badge]: https://img.shields.io/codecov/c/github/remarkjs/remark-math.svg

[coverage]: https://codecov.io/github/remarkjs/remark-math

[downloads-badge]: https://img.shields.io/npm/dm/remark-math.svg

[downloads]: https://www.npmjs.com/package/remark-math

[size-badge]: https://img.shields.io/bundlejs/size/remark-math

[size]: https://bundlejs.com/?q=remark-math

[sponsors-badge]: https://opencollective.com/unified/sponsors/badge.svg

[backers-badge]: https://opencollective.com/unified/backers/badge.svg

[collective]: https://opencollective.com/unified

[chat-badge]: https://img.shields.io/badge/chat-discussions-success.svg

[chat]: https://github.com/remarkjs/remark/discussions

[npm]: https://docs.npmjs.com/cli/install

[esm]: https://gist.github.com/sindresorhus/a39789f98801d908bbc7ff3ecc99d99c

[esmsh]: https://esm.sh

[health]: https://github.com/remarkjs/.github

[contributing]: https://github.com/remarkjs/.github/blob/main/contributing.md

[support]: https://github.com/remarkjs/.github/blob/main/support.md

[coc]: https://github.com/remarkjs/.github/blob/main/code-of-conduct.md

[license]: https://github.com/remarkjs/remark-math/blob/main/license

[author]: https://rokt33r.github.io

[hast]: https://github.com/syntax-tree/hast

[mdast-util-from-markdown]: https://github.com/syntax-tree/mdast-util-from-markdown

[mdast-util-math]: https://github.com/syntax-tree/mdast-util-math

[mdast-util-to-hast-fields]: https://github.com/syntax-tree/mdast-util-to-hast#fields-on-nodes

[micromark]: https://github.com/micromark/micromark

[micromark-extension-math]: https://github.com/micromark/micromark-extension-math

[rehype]: https://github.com/rehypejs/rehype

[remark]: https://github.com/remarkjs/remark

[remark-rehype]: https://github.com/remarkjs/remark-rehype

[typescript]: https://www.typescriptlang.org

[unified]: https://github.com/unifiedjs/unified

[wiki-xss]: https://en.wikipedia.org/wiki/Cross-site_scripting

[api-options]: #options

[api-remark-math]: #unifieduseremarkmath-options
