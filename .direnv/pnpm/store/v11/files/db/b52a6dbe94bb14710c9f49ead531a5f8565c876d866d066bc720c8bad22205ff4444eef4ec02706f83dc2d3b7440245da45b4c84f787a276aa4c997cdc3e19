# mdast-util-from-markdown

[![Build][badge-build-image]][badge-build-url]
[![Coverage][badge-coverage-image]][badge-coverage-url]
[![Downloads][badge-downloads-image]][badge-downloads-url]
[![Size][badge-size-image]][badge-size-url]

**[mdast][github-mdast]** utility that turns markdown into a syntax tree.

## Contents

* [What is this?](#what-is-this)
* [When should I use this?](#when-should-i-use-this)
* [Install](#install)
* [Use](#use)
* [API](#api)
  * [`fromMarkdown(value[, encoding][, options])`](#frommarkdownvalue-encoding-options)
  * [`CompileContext`](#compilecontext)
  * [`CompileData`](#compiledata)
  * [`Encoding`](#encoding)
  * [`Extension`](#extension)
  * [`Handle`](#handle)
  * [`OnEnterError`](#onentererror)
  * [`OnExitError`](#onexiterror)
  * [`Options`](#options)
  * [`Token`](#token)
  * [`Transform`](#transform)
  * [`Value`](#value)
* [List of extensions](#list-of-extensions)
* [Syntax](#syntax)
* [Syntax tree](#syntax-tree)
* [Types](#types)
* [Compatibility](#compatibility)
* [Security](#security)
* [Related](#related)
* [Contribute](#contribute)
* [License](#license)

## What is this?

This package is a utility that takes markdown input and turns it into an
[mdast][github-mdast] syntax tree.

This utility uses [`micromark`][github-micromark],
which turns markdown into tokens,
and then turns those tokens into nodes.
This package is used inside [`remark-parse`][github-remark-parse],
which focusses on
making it easier to transform content by abstracting these internals away.

## When should I use this?

If you want to handle syntax trees manually, use this.
When you *just* want to turn markdown into HTML,
use [`micromark`][github-micromark] instead.
For an easier time processing content,
use the **[remark][github-remark]** ecosystem instead.

You can combine this package with other packages to add syntax extensions to
markdown.
Notable examples that deeply integrate with this package are
[`mdast-util-mdx`][github-mdast-util-mdx],
[`mdast-util-gfm`][github-mdast-util-gfm],
[`mdast-util-frontmatter`][github-mdast-util-frontmatter],
[`mdast-util-math`][github-mdast-util-math], and
[`mdast-util-directive`][github-mdast-util-directive].

## Install

This package is [ESM only][github-gist-esm].
In Node.js (version 16+), install with [npm][npmjs-install]:

```sh
npm install mdast-util-from-markdown
```

In Deno with [`esm.sh`][esmsh]:

```js
import {fromMarkdown} from 'https://esm.sh/mdast-util-from-markdown@2'
```

In browsers with [`esm.sh`][esmsh]:

```html
<script type="module">
  import {fromMarkdown} from 'https://esm.sh/mdast-util-from-markdown@2?bundle'
</script>
```

## Use

Say we have the following markdown file `example.md`:

```markdown
## Hello, *World*!
```

…and our module `example.js` looks as follows:

```js
import fs from 'node:fs/promises'
import {fromMarkdown} from 'mdast-util-from-markdown'

const doc = await fs.readFile('example.md')
const tree = fromMarkdown(doc)

console.log(tree)
```

…now running `node example.js` yields (positional info removed for brevity):

```js
{
  type: 'root',
  children: [
    {
      type: 'heading',
      depth: 2,
      children: [
        {type: 'text', value: 'Hello, '},
        {type: 'emphasis', children: [{type: 'text', value: 'World'}]},
        {type: 'text', value: '!'}
      ]
    }
  ]
}
```

## API

This package exports the identifier [`fromMarkdown`][api-from-markdown].
There is no default export.

The export map supports the [`development` condition][node-packages-conditions].
Run `node --conditions development example.js` to get instrumented dev code.
Without this condition, production code is loaded.

### `fromMarkdown(value[, encoding][, options])`

Turn markdown into a syntax tree.

###### Overloads

* `(value: Value, encoding: Encoding, options?: Options) => Root`
* `(value: Value, options?: Options) => Root`

###### Parameters

* `value` ([`Value`][api-value])
  — markdown to parse
* `encoding` ([`Encoding`][api-encoding], default: `'utf8'`)
  — [character encoding][node-util-encoding] for when `value` is
  [`Uint8Array`][mozilla-uint8-array]
* `options` ([`Options`][api-options], optional)
  — configuration

###### Returns

mdast tree ([`Root`][github-mdast-root]).

### `CompileContext`

mdast compiler context (TypeScript type).

###### Fields

* `stack` ([`Array<Node>`][github-mdast-nodes])
  — stack of nodes
* `tokenStack` (`Array<[Token, OnEnterError | undefined]>`)
  — stack of tokens
* `data` ([`CompileData`][api-compile-data])
  — info passed around; key/value store
* `buffer` (`() => undefined`)
  — capture some of the output data
* `resume` (`() => string`)
  — stop capturing and access the output data
* `enter` (`(node: Node, token: Token, onError?: OnEnterError) => undefined`)
  — enter a node
* `exit` (`(token: Token, onError?: OnExitError) => undefined`)
  — exit a node
* `sliceSerialize` (`(token: Token, expandTabs?: boolean) => string`)
  — get the string value of a token
* `config` (`Required<Extension>`)
  — configuration

### `CompileData`

Interface of tracked data (TypeScript type).

###### Type

```ts
interface CompileData { /* see code */ }
```

When working on extensions that use more data, extend the corresponding
interface to register their types:

```ts
declare module 'mdast-util-from-markdown' {
  interface CompileData {
    // Register a new field.
    mathFlowInside?: boolean | undefined
  }
}
```

### `Encoding`

Encodings supported by the [`Uint8Array`][mozilla-uint8-array] class
(TypeScript type).

See [`micromark`][github-micromark-api] for more info.

###### Type

```ts
type Encoding = 'utf8' | /* … */
```

### `Extension`

Change how markdown tokens from micromark are turned into mdast (TypeScript
type).

###### Properties

* `canContainEols` (`Array<string>`, optional)
  — token types where line endings are used
* `enter` ([`Record<string, Handle>`][api-handle], optional)
  — opening handles
* `exit` ([`Record<string, Handle>`][api-handle], optional)
  — closing handles
* `transforms` ([`Array<Transform>`][api-transform], optional)
  — tree transforms

### `Handle`

Handle a token (TypeScript type).

###### Parameters

* `this` ([`CompileContext`][api-compile-context])
  — context
* `token` ([`Token`][api-token])
  — current token

###### Returns

Nothing (`undefined`).

### `OnEnterError`

Handle the case where the `right` token is open, but it is closed (by the
`left` token) or because we reached the end of the document (TypeScript type).

###### Parameters

* `this` ([`CompileContext`][api-compile-context])
  — context
* `left` ([`Token`][api-token] or `undefined`)
  — left token
* `right` ([`Token`][api-token])
  — right token

###### Returns

Nothing (`undefined`).

### `OnExitError`

Handle the case where the `right` token is open but it is closed by
exiting the `left` token (TypeScript type).

###### Parameters

* `this` ([`CompileContext`][api-compile-context])
  — context
* `left` ([`Token`][api-token])
  — left token
* `right` ([`Token`][api-token])
  — right token

###### Returns

Nothing (`undefined`).

### `Options`

Configuration (TypeScript type).

###### Properties

* `extensions`
  ([`Array<MicromarkExtension>`][github-micromark-extension], optional)
  — micromark extensions to change how markdown is parsed
* `mdastExtensions`
  ([`Array<Extension | Array<Extension>>`][api-extension],
  optional)
  — extensions for this utility to change how tokens are turned into a tree

### `Token`

Token from micromark (TypeScript type).

###### Type

```ts
type Token = { /* … */ }
```

### `Transform`

Extra transform, to change the AST afterwards (TypeScript type).

###### Parameters

* `tree` ([`Root`][github-mdast-root])
  — tree to transform

###### Returns

New tree ([`Root`][github-mdast-root]) or nothing
(in which case the current tree is used).

### `Value`

Contents of the file (TypeScript type).

See [`micromark`][github-micromark-api] for more info.

###### Type

```ts
type Value = Uint8Array | string
```

## List of extensions

* [`syntax-tree/mdast-util-directive`][github-mdast-util-directive]
  — directives
* [`syntax-tree/mdast-util-frontmatter`][github-mdast-util-frontmatter]
  — frontmatter (YAML, TOML, more)
* [`syntax-tree/mdast-util-gfm`][github-mdast-util-gfm]
  — GFM
* [`syntax-tree/mdast-util-gfm-autolink-literal`](https://github.com/syntax-tree/mdast-util-gfm-autolink-literal)
  — GFM autolink literals
* [`syntax-tree/mdast-util-gfm-footnote`](https://github.com/syntax-tree/mdast-util-gfm-footnote)
  — GFM footnotes
* [`syntax-tree/mdast-util-gfm-strikethrough`](https://github.com/syntax-tree/mdast-util-gfm-strikethrough)
  — GFM strikethrough
* [`syntax-tree/mdast-util-gfm-table`](https://github.com/syntax-tree/mdast-util-gfm-table)
  — GFM tables
* [`syntax-tree/mdast-util-gfm-task-list-item`](https://github.com/syntax-tree/mdast-util-gfm-task-list-item)
  — GFM task list items
* [`syntax-tree/mdast-util-math`][github-mdast-util-math]
  — math
* [`syntax-tree/mdast-util-mdx`][github-mdast-util-mdx]
  — MDX
* [`syntax-tree/mdast-util-mdx-expression`](https://github.com/syntax-tree/mdast-util-mdx-expression)
  — MDX expressions
* [`syntax-tree/mdast-util-mdx-jsx`](https://github.com/syntax-tree/mdast-util-mdx-jsx)
  — MDX JSX
* [`syntax-tree/mdast-util-mdxjs-esm`](https://github.com/syntax-tree/mdast-util-mdxjs-esm)
  — MDX ESM

## Syntax

Markdown is parsed according to CommonMark.
Extensions can add support for other syntax.
If you’re interested in extending markdown,
[more information is available in micromark’s
readme][github-micromark-extension].

## Syntax tree

The syntax tree is [mdast][github-mdast].

## Types

This package is fully typed with [TypeScript][].
It exports the additional types
[`CompileContext`][api-compile-context],
[`CompileData`][api-compile-data],
[`Encoding`][api-encoding],
[`Extension`][api-extension],
[`Handle`][api-handle],
[`OnEnterError`][api-on-enter-error],
[`OnExitError`][api-on-exit-error],
[`Options`][api-options],
[`Token`][api-token],
[`Transform`][api-transform], and
[`Value`][api-value].

## Compatibility

Projects maintained by the unified collective are compatible with maintained
versions of Node.js.

When we cut a new major release, we drop support for unmaintained versions of
Node.
This means we try to keep the current release line,
`mdast-util-from-markdown@^2`, compatible with Node.js 16.

## Security

As markdown is sometimes used for HTML, and improper use of HTML can open you up
to a [cross-site scripting (XSS)][wikipedia-xss] attack, use of `mdast-util-from-markdown`
can also be unsafe.
When going to HTML, use this utility in combination with
[`hast-util-sanitize`][github-hast-util-sanitize] to make the tree safe.

## Related

* [`syntax-tree/mdast-util-to-markdown`](https://github.com/syntax-tree/mdast-util-to-markdown)
  — serialize mdast as markdown
* [`micromark/micromark`][github-micromark]
  — parse markdown
* [`remarkjs/remark`][github-remark]
  — process markdown

## Contribute

See [`contributing.md`][health-contributing]
in
[`syntax-tree/.github`][health]
for ways to get started.
See [`support.md`][health-support] for ways to get help.

This project has a [code of conduct][health-coc].
By interacting with this repository, organization, or community you agree to
abide by its terms.

## License

[MIT][file-license] © [Titus Wormer][wooorm]

<!-- Definitions -->

[api-compile-context]: #compilecontext

[api-compile-data]: #compiledata

[api-encoding]: #encoding

[api-extension]: #extension

[api-from-markdown]: #frommarkdownvalue-encoding-options

[api-handle]: #handle

[api-on-enter-error]: #onentererror

[api-on-exit-error]: #onexiterror

[api-options]: #options

[api-token]: #token

[api-transform]: #transform

[api-value]: #value

[badge-build-image]: https://github.com/syntax-tree/mdast-util-from-markdown/workflows/main/badge.svg

[badge-build-url]: https://github.com/syntax-tree/mdast-util-from-markdown/actions

[badge-coverage-image]: https://img.shields.io/codecov/c/github/syntax-tree/mdast-util-from-markdown.svg

[badge-coverage-url]: https://codecov.io/github/syntax-tree/mdast-util-from-markdown

[badge-downloads-image]: https://img.shields.io/npm/dm/mdast-util-from-markdown.svg

[badge-downloads-url]: https://www.npmjs.com/package/mdast-util-from-markdown

[badge-size-image]: https://img.shields.io/bundlejs/size/mdast-util-from-markdown

[badge-size-url]: https://bundlejs.com/?q=mdast-util-from-markdown

[esmsh]: https://esm.sh

[file-license]: license

[github-gist-esm]: https://gist.github.com/sindresorhus/a39789f98801d908bbc7ff3ecc99d99c

[github-hast-util-sanitize]: https://github.com/syntax-tree/hast-util-sanitize

[github-mdast]: https://github.com/syntax-tree/mdast

[github-mdast-nodes]: https://github.com/syntax-tree/mdast#nodes

[github-mdast-root]: https://github.com/syntax-tree/mdast#root

[github-mdast-util-directive]: https://github.com/syntax-tree/mdast-util-directive

[github-mdast-util-frontmatter]: https://github.com/syntax-tree/mdast-util-frontmatter

[github-mdast-util-gfm]: https://github.com/syntax-tree/mdast-util-gfm

[github-mdast-util-math]: https://github.com/syntax-tree/mdast-util-math

[github-mdast-util-mdx]: https://github.com/syntax-tree/mdast-util-mdx

[github-micromark]: https://github.com/micromark/micromark

[github-micromark-api]: https://github.com/micromark/micromark/tree/main/packages/micromark#micromarkvalue-encoding-options

[github-micromark-extension]: https://github.com/micromark/micromark#extensions

[github-remark]: https://github.com/remarkjs/remark

[github-remark-parse]: https://github.com/remarkjs/remark/tree/main/packages/remark-parse

[health]: https://github.com/syntax-tree/.github

[health-coc]: https://github.com/syntax-tree/.github/blob/main/code-of-conduct.md

[health-contributing]: https://github.com/syntax-tree/.github/blob/main/contributing.md

[health-support]: https://github.com/syntax-tree/.github/blob/main/support.md

[mozilla-uint8-array]: https://developer.mozilla.org/docs/Web/JavaScript/Reference/Global_Objects/Uint8Array

[node-packages-conditions]: https://nodejs.org/api/packages.html#packages_resolving_user_conditions

[node-util-encoding]: https://nodejs.org/api/util.html#whatwg-supported-encodings

[npmjs-install]: https://docs.npmjs.com/cli/install

[typescript]: https://www.typescriptlang.org

[wikipedia-xss]: https://en.wikipedia.org/wiki/Cross-site_scripting

[wooorm]: https://wooorm.com
