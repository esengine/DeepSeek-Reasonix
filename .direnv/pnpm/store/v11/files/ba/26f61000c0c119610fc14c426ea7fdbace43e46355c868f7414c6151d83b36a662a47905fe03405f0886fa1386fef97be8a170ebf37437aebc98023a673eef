<!--
  Notes for maintaining this document:

  * update the version of the link for `commonmark-html` once in a while
-->

# react-markdown

[![Build][badge-build-image]][badge-build-url]
[![Coverage][badge-coverage-image]][badge-coverage-url]
[![Downloads][badge-downloads-image]][badge-downloads-url]
[![Size][badge-size-image]][badge-size-url]

React component to render markdown.

## Feature highlights

* [x] **[safe][section-security] by default**
  (no `dangerouslySetInnerHTML` or XSS attacks)
* [x] **[components][section-components]**
  (pass your own component to use instead of `<h2>` for `## hi`)
* [x] **[plugins][section-plugins]**
  (many plugins you can pick and choose from)
* [x] **[compliant][section-syntax]**
  (100% to CommonMark, 100% to GFM with a plugin)

## Contents

* [What is this?](#what-is-this)
* [When should I use this?](#when-should-i-use-this)
* [Install](#install)
* [Use](#use)
* [API](#api)
  * [`Markdown`](#markdown)
  * [`MarkdownAsync`](#markdownasync)
  * [`MarkdownHooks`](#markdownhooks)
  * [`defaultUrlTransform(url)`](#defaulturltransformurl)
  * [`AllowElement`](#allowelement)
  * [`Components`](#components)
  * [`ExtraProps`](#extraprops)
  * [`HooksOptions`](#hooksoptions)
  * [`Options`](#options)
  * [`UrlTransform`](#urltransform)
* [Examples](#examples)
  * [Use a plugin](#use-a-plugin)
  * [Use a plugin with options](#use-a-plugin-with-options)
  * [Use custom components (syntax highlight)](#use-custom-components-syntax-highlight)
  * [Use remark and rehype plugins (math)](#use-remark-and-rehype-plugins-math)
* [Plugins](#plugins)
* [Syntax](#syntax)
* [Compatibility](#compatibility)
* [Architecture](#architecture)
* [Appendix A: HTML in markdown](#appendix-a-html-in-markdown)
* [Appendix B: Components](#appendix-b-components)
* [Appendix C: line endings in markdown (and JSX)](#appendix-c-line-endings-in-markdown-and-jsx)
* [Security](#security)
* [Related](#related)
* [Contribute](#contribute)
* [License](#license)

## What is this?

This package is a [React][] component that can be given a string of markdown
that it’ll safely render to React elements.
You can pass plugins to change how markdown is transformed and pass components
that will be used instead of normal HTML elements.

* to learn markdown, see this [cheatsheet and tutorial][commonmark-help]
* to try out `react-markdown`, see [our demo][github-io-react-markdown]

## When should I use this?

There are other ways to use markdown in React out there so why use this one?
The three main reasons are that they often rely on `dangerouslySetInnerHTML`,
have bugs with how they handle markdown, or don’t let you swap elements for
components.
`react-markdown` builds a virtual DOM, so React only replaces what changed,
from a syntax tree.
That’s supported because we use [unified][github-unified],
specifically [remark][github-remark] for markdown and [rehype][github-rehype]
for HTML,
which are popular tools to transform content with plugins.

This package focusses on making it easy for beginners to safely use markdown in
React.
When you’re familiar with unified, you can use a modern hooks based alternative
[`react-remark`][github-react-remark] or [`rehype-react`][github-rehype-react]
manually.
If you instead want to use JavaScript and JSX *inside* markdown files, use
[MDX][github-mdx].

## Install

This package is [ESM only][esm].
In Node.js (version 16+), install with [npm][npm-install]:

```sh
npm install react-markdown
```

In Deno with [`esm.sh`][esmsh]:

```js
import Markdown from 'https://esm.sh/react-markdown@10'
```

In browsers with [`esm.sh`][esmsh]:

```html
<script type="module">
  import Markdown from 'https://esm.sh/react-markdown@10?bundle'
</script>
```

## Use

A basic hello world:

```js
import React from 'react'
import {createRoot} from 'react-dom/client'
import Markdown from 'react-markdown'

const markdown = '# Hi, *Pluto*!'

createRoot(document.body).render(<Markdown>{markdown}</Markdown>)
```

<details>
<summary>Show equivalent JSX</summary>

```js
<h1>
  Hi, <em>Pluto</em>!
</h1>
```

</details>

Here is an example that shows how to use a plugin
([`remark-gfm`][github-remark-gfm],
which adds support for footnotes, strikethrough, tables, tasklists and
URLs directly):

```js
import React from 'react'
import {createRoot} from 'react-dom/client'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

const markdown = `Just a link: www.nasa.gov.`

createRoot(document.body).render(
  <Markdown remarkPlugins={[remarkGfm]}>{markdown}</Markdown>
)
```

<details>
<summary>Show equivalent JSX</summary>

```js
<p>
  Just a link: <a href="http://www.nasa.gov">www.nasa.gov</a>.
</p>
```

</details>

## API

This package exports the identifiers
[`MarkdownAsync`][api-markdown-async],
[`MarkdownHooks`][api-markdown-hooks],
and
[`defaultUrlTransform`][api-default-url-transform].
The default export is [`Markdown`][api-markdown].

It also exports the additional [TypeScript][] types
[`AllowElement`][api-allow-element],
[`Components`][api-components],
[`ExtraProps`][api-extra-props],
[`HooksOptions`][api-hooks-options],
[`Options`][api-options],
and
[`UrlTransform`][api-url-transform].

### `Markdown`

Component to render markdown.

This is a synchronous component.
When using async plugins,
see [`MarkdownAsync`][api-markdown-async] or
[`MarkdownHooks`][api-markdown-hooks].

###### Parameters

* `options` ([`Options`][api-options])
  — props

###### Returns

React element (`ReactElement`).

### `MarkdownAsync`

Component to render markdown with support for async plugins
through async/await.

Components returning promises are supported on the server.
For async support on the client,
see [`MarkdownHooks`][api-markdown-hooks].

###### Parameters

* `options` ([`Options`][api-options])
  — props

###### Returns

Promise to a React element (`Promise<ReactElement>`).

### `MarkdownHooks`

Component to render markdown with support for async plugins through hooks.

This uses `useEffect` and `useState` hooks.
Hooks run on the client and do not immediately render something.
For async support on the server,
see [`MarkdownAsync`][api-markdown-async].

###### Parameters

* `options` ([`Options`][api-options])
  — props

###### Returns

React node (`ReactNode`).

### `defaultUrlTransform(url)`

Make a URL safe.

###### Parameters

* `url` (`string`)
  — URL

###### Returns

Safe URL (`string`).

### `AllowElement`

Filter elements (TypeScript type).

###### Parameters

* `node` ([`Element` from `hast`][github-hast-element])
  — element to check
* `index` (`number | undefined`)
  — index of `element` in `parent`
* `parent` ([`Node` from `hast`][github-hast-nodes])
  — parent of `element`

###### Returns

Whether to allow `element` (`boolean`, optional).

### `Components`

Map tag names to components (TypeScript type).

###### Type

```ts
import type {ExtraProps} from 'react-markdown'
import type {ComponentProps, ElementType} from 'react'

type Components = {
  [Key in Extract<ElementType, string>]?: ElementType<ComponentProps<Key> & ExtraProps>
}
```

### `ExtraProps`

Extra fields we pass to components (TypeScript type).

###### Fields

* `node` ([`Element` from `hast`][github-hast-element], optional)
  — original node

### `HooksOptions`

Configuration for [`MarkdownHooks`][api-markdown-hooks] (TypeScript type);
extends the regular [`Options`][api-options] with a `fallback` prop.

###### Extends

[`Options`][api-options].

###### Fields

* `fallback` (`ReactNode`, optional)
  — content to render while the processor processing the markdown

### `Options`

Configuration (TypeScript type).

###### Fields

* `allowElement` ([`AllowElement`][api-allow-element], optional)
  — filter elements;
  `allowedElements` / `disallowedElements` is used first
* `allowedElements` (`Array<string>`, default: all tag names)
  — tag names to allow;
  cannot combine w/ `disallowedElements`
* `children` (`string`, optional)
  — markdown
* `components` ([`Components`][api-components], optional)
  — map tag names to components
* `disallowedElements` (`Array<string>`, default: `[]`)
  — tag names to disallow;
  cannot combine w/ `allowedElements`
* `rehypePlugins` (`Array<Plugin>`, optional)
  — list of [rehype plugins][github-rehype-plugins] to use
* `remarkPlugins` (`Array<Plugin>`, optional)
  — list of [remark plugins][github-remark-plugins] to use
* `remarkRehypeOptions`
  ([`Options` from `remark-rehype`][github-remark-rehype-options],
  optional)
  — options to pass through to `remark-rehype`
* `skipHtml` (`boolean`, default: `false`)
  — ignore HTML in markdown completely
* `unwrapDisallowed` (`boolean`, default: `false`)
  — extract (unwrap) what’s in disallowed elements;
  normally when say `strong` is not allowed, it and it’s children are dropped,
  with `unwrapDisallowed` the element itself is replaced by its children
* `urlTransform` ([`UrlTransform`][api-url-transform], default:
  [`defaultUrlTransform`][api-default-url-transform])
  — change URLs

### `UrlTransform`

Transform URLs (TypeScript type).

###### Parameters

* `url` (`string`)
  — URL
* `key` (`string`, example: `'href'`)
  — property name
* `node` ([`Element` from `hast`][github-hast-element])
  — element to check

###### Returns

Transformed URL (`string`, optional).

## Examples

### Use a plugin

This example shows how to use a remark plugin.
In this case, [`remark-gfm`][github-remark-gfm],
which adds support for strikethrough, tables, tasklists and URLs directly:

```js
import React from 'react'
import {createRoot} from 'react-dom/client'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

const markdown = `A paragraph with *emphasis* and **strong importance**.

> A block quote with ~strikethrough~ and a URL: https://reactjs.org.

* Lists
* [ ] todo
* [x] done

A table:

| a | b |
| - | - |
`

createRoot(document.body).render(
  <Markdown remarkPlugins={[remarkGfm]}>{markdown}</Markdown>
)
```

<details>
<summary>Show equivalent JSX</summary>

```js
<>
  <p>
    A paragraph with <em>emphasis</em> and <strong>strong importance</strong>.
  </p>
  <blockquote>
    <p>
      A block quote with <del>strikethrough</del> and a URL:{' '}
      <a href="https://reactjs.org">https://reactjs.org</a>.
    </p>
  </blockquote>
  <ul className="contains-task-list">
    <li>Lists</li>
    <li className="task-list-item">
      <input type="checkbox" disabled /> todo
    </li>
    <li className="task-list-item">
      <input type="checkbox" disabled checked /> done
    </li>
  </ul>
  <p>A table:</p>
  <table>
    <thead>
      <tr>
        <th>a</th>
        <th>b</th>
      </tr>
    </thead>
  </table>
</>
```

</details>

### Use a plugin with options

This example shows how to use a plugin and give it options.
To do that, use an array with the plugin at the first place, and the options
second.
[`remark-gfm`][github-remark-gfm] has an option to allow only double tildes for
strikethrough:

```js
import React from 'react'
import {createRoot} from 'react-dom/client'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

const markdown = 'This ~is not~ strikethrough, but ~~this is~~!'

createRoot(document.body).render(
  <Markdown remarkPlugins={[[remarkGfm, {singleTilde: false}]]}>
    {markdown}
  </Markdown>
)
```

<details>
<summary>Show equivalent JSX</summary>

```js
<p>
  This ~is not~ strikethrough, but <del>this is</del>!
</p>
```

</details>

### Use custom components (syntax highlight)

This example shows how you can overwrite the normal handling of an element by
passing a component.
In this case, we apply syntax highlighting with the seriously super amazing
[`react-syntax-highlighter`][github-react-syntax-highlighter] by
[**@conorhastings**][github-conorhastings]:

<!-- To do: currently broken on actual ESM; let’s find an alternative? -->

```js
import React from 'react'
import {createRoot} from 'react-dom/client'
import Markdown from 'react-markdown'
import {Prism as SyntaxHighlighter} from 'react-syntax-highlighter'
import {dark} from 'react-syntax-highlighter/dist/esm/styles/prism'

// Did you know you can use tildes instead of backticks for code in markdown? ✨
const markdown = `Here is some JavaScript code:

~~~js
console.log('It works!')
~~~
`

createRoot(document.body).render(
  <Markdown
    children={markdown}
    components={{
      code(props) {
        const {children, className, node, ...rest} = props
        const match = /language-(\w+)/.exec(className || '')
        return match ? (
          <SyntaxHighlighter
            {...rest}
            PreTag="div"
            children={String(children).replace(/\n$/, '')}
            language={match[1]}
            style={dark}
          />
        ) : (
          <code {...rest} className={className}>
            {children}
          </code>
        )
      }
    }}
  />
)
```

<details>
<summary>Show equivalent JSX</summary>

```js
<>
  <p>Here is some JavaScript code:</p>
  <pre>
    <SyntaxHighlighter language="js" style={dark} PreTag="div" children="console.log('It works!')" />
  </pre>
</>
```

</details>

### Use remark and rehype plugins (math)

This example shows how a syntax extension
(through [`remark-math`][github-remark-math])
is used to support math in markdown, and a transform plugin
([`rehype-katex`][github-rehype-katex]) to render that math.

```js
import React from 'react'
import {createRoot} from 'react-dom/client'
import Markdown from 'react-markdown'
import rehypeKatex from 'rehype-katex'
import remarkMath from 'remark-math'
import 'katex/dist/katex.min.css' // `rehype-katex` does not import the CSS for you

const markdown = `The lift coefficient ($C_L$) is a dimensionless coefficient.`

createRoot(document.body).render(
  <Markdown remarkPlugins={[remarkMath]} rehypePlugins={[rehypeKatex]}>
    {markdown}
  </Markdown>
)
```

<details>
<summary>Show equivalent JSX</summary>

```js
<p>
  The lift coefficient (
  <span className="katex">
    <span className="katex-mathml">
      <math xmlns="http://www.w3.org/1998/Math/MathML">{/* … */}</math>
    </span>
    <span className="katex-html" aria-hidden="true">
      {/* … */}
    </span>
  </span>
  ) is a dimensionless coefficient.
</p>
```

</details>

## Plugins

We use [unified][github-unified],
specifically [remark][github-remark] for markdown and
[rehype][github-rehype] for HTML,
which are tools to transform content with plugins.
Here are three good ways to find plugins:

* [`awesome-remark`][github-awesome-remark] and
  [`awesome-rehype`][github-awesome-rehype]
  — selection of the most awesome projects
* [List of remark plugins][github-remark-plugins] and
  [list of rehype plugins][github-rehype-plugins]
  — list of all plugins
* [`remark-plugin`][github-topic-remark-plugin] and
  [`rehype-plugin`][github-topic-rehype-plugin] topics
  — any tagged repo on GitHub

## Syntax

`react-markdown` follows CommonMark, which standardizes the differences between
markdown implementations, by default.
Some syntax extensions are supported through plugins.

We use [`micromark`][github-micromark] under the hood for our parsing.
See its documentation for more information on markdown, CommonMark, and
extensions.

## Compatibility

Projects maintained by the unified collective are compatible with maintained
versions of Node.js.

When we cut a new major release, we drop support for unmaintained versions of
Node.
This means we try to keep the current release line, `react-markdown@10`,
compatible with Node.js 16.

They work in all modern browsers (essentially: everything not IE 11).
You can use a bundler (such as esbuild, webpack, or Rollup) to use this package
in your project, and use its options (or plugins) to add support for legacy
browsers.

## Architecture

<pre><code>                                                           react-markdown
         +----------------------------------------------------------------------------------------------------------------+
         |                                                                                                                |
         |  +----------+        +----------------+        +---------------+       +----------------+       +------------+ |
         |  |          |        |                |        |               |       |                |       |            | |
<a href="https://commonmark.org">markdown</a>-+->+  <a href="https://github.com/remarkjs/remark">remark</a>  +-<a href="https://github.com/syntax-tree/mdast">mdast</a>->+ <a href="https://github.com/remarkjs/remark/blob/main/doc/plugins.md">remark plugins</a> +-<a href="https://github.com/syntax-tree/mdast">mdast</a>->+ <a href="https://github.com/remarkjs/remark-rehype">remark-rehype</a> +-<a href="https://github.com/syntax-tree/hast">hast</a>->+ <a href="https://github.com/rehypejs/rehype/blob/main/doc/plugins.md">rehype plugins</a> +-<a href="https://github.com/syntax-tree/hast">hast</a>->+ <a href="#appendix-b-components">components</a> +-+->react elements
         |  |          |        |                |        |               |       |                |       |            | |
         |  +----------+        +----------------+        +---------------+       +----------------+       +------------+ |
         |                                                                                                                |
         +----------------------------------------------------------------------------------------------------------------+
</code></pre>

To understand what this project does, it’s important to first understand what
unified does: please read through the [`unifiedjs/unified`][github-unified]
readme
(the part until you hit the API section is required reading).

`react-markdown` is a unified pipeline — wrapped so that most folks don’t need
to directly interact with unified.
The processor goes through these steps:

* parse markdown to mdast (markdown syntax tree)
* transform through remark (markdown ecosystem)
* transform mdast to hast (HTML syntax tree)
* transform through rehype (HTML ecosystem)
* render hast to React with components

## Appendix A: HTML in markdown

`react-markdown` typically escapes HTML (or ignores it, with `skipHtml`)
because it is dangerous and defeats the purpose of this library.

However, if you are in a trusted environment (you trust the markdown), and
can spare the bundle size (±60kb minzipped), then you can use
[`rehype-raw`][github-rehype-raw]:

```js
import React from 'react'
import {createRoot} from 'react-dom/client'
import Markdown from 'react-markdown'
import rehypeRaw from 'rehype-raw'

const markdown = `<div class="note">

Some *emphasis* and <strong>strong</strong>!

</div>`

createRoot(document.body).render(
  <Markdown rehypePlugins={[rehypeRaw]}>{markdown}</Markdown>
)
```

<details>
<summary>Show equivalent JSX</summary>

```js
<div className="note">
  <p>
    Some <em>emphasis</em> and <strong>strong</strong>!
  </p>
</div>
```

</details>

**Note**: HTML in markdown is still bound by how [HTML works in
CommonMark][commonmark-html].
Make sure to use blank lines around block-level HTML that again contains
markdown!

## Appendix B: Components

You can also change the things that come from markdown:

```js
<Markdown
  components={{
    // Map `h1` (`# heading`) to use `h2`s.
    h1: 'h2',
    // Rewrite `em`s (`*like so*`) to `i` with a red foreground color.
    em(props) {
      const {node, ...rest} = props
      return <i style={{color: 'red'}} {...rest} />
    }
  }}
/>
```

The keys in components are HTML equivalents for the things you write with
markdown (such as `h1` for `# heading`).
Normally, in markdown, those are: `a`, `blockquote`, `br`, `code`, `em`, `h1`,
`h2`, `h3`, `h4`, `h5`, `h6`, `hr`, `img`, `li`, `ol`, `p`, `pre`, `strong`, and
`ul`.
With [`remark-gfm`][github-remark-gfm],
you can also use `del`, `input`, `table`, `tbody`, `td`, `th`, `thead`, and `tr`.
Other remark or rehype plugins that add support for new constructs will also
work with `react-markdown`.

The props that are passed are what you probably would expect: an `a` (link) will
get `href` (and `title`) props, and `img` (image) an `src`, `alt` and `title`,
etc.

Every component will receive a `node`.
This is the original [`Element` from `hast`][github-hast-element] element being
turned into a React element.

## Appendix C: line endings in markdown (and JSX)

You might have trouble with how line endings work in markdown and JSX.
We recommend the following, which solves all line ending problems:

```js
// If you write actual markdown in your code, put your markdown in a variable;
// **do not indent markdown**:
const markdown = `
# This is perfect!
`

// Pass the value as an expression as an only child:
const result = <Markdown>{markdown}</Markdown>
```

👆 That works.
Read on for what doesn’t and why that is.

You might try to write markdown directly in your JSX and find that it **does
not** work:

```js
<Markdown>
  # Hi

  This is **not** a paragraph.
</Markdown>
```

The is because in JSX the whitespace (including line endings) is collapsed to
a single space.
So the above example is equivalent to:

```js
<Markdown> # Hi This is **not** a paragraph. </Markdown>
```

Instead, to pass markdown to `Markdown`, you can use an expression:
with a template literal:

```js
<Markdown>{`
# Hi

This is a paragraph.
`}</Markdown>
```

Template literals have another potential problem, because they keep whitespace
(including indentation) inside them.
That means that the following **does not** turn into a heading:

```js
<Markdown>{`
    # This is **not** a heading, it’s an indented code block
`}</Markdown>
```

## Security

Use of `react-markdown` is secure by default.
Overwriting `urlTransform` to something insecure will open you up to XSS
vectors.
Furthermore, the `remarkPlugins`, `rehypePlugins`, and `components` you use may
be insecure.

To make sure the content is completely safe, even after what plugins do,
use [`rehype-sanitize`][github-rehype-sanitize].
It lets you define your own schema of what is and isn’t allowed.

## Related

* [`MDX`][github-mdx]
  — JSX *in* markdown
* [`remark-gfm`][github-remark-gfm]
  — add support for GitHub flavored markdown support
* [`react-remark`][github-react-remark]
  — hook based alternative
* [`rehype-react`][github-rehype-react]
  — turn HTML into React elements

## Contribute

See [`contributing.md`][health-contributing] in [`remarkjs/.github`][health]
for ways to get started.
See [`support.md`][health-support] for ways to get help.

This project has a [code of conduct][health-coc].
By interacting with this repository, organization, or community you agree to
abide by its terms.

## License

[MIT][file-license] © [Espen Hovlandsdal][author]

[api-allow-element]: #allowelement

[api-components]: #components

[api-default-url-transform]: #defaulturltransformurl

[api-extra-props]: #extraprops

[api-hooks-options]: #hooksoptions

[api-markdown]: #markdown

[api-markdown-async]: #markdownasync

[api-markdown-hooks]: #markdownhooks

[api-options]: #options

[api-url-transform]: #urltransform

[author]: https://espen.codes/

[badge-build-image]: https://github.com/remarkjs/react-markdown/workflows/main/badge.svg

[badge-build-url]: https://github.com/remarkjs/react-markdown/actions

[badge-coverage-image]: https://img.shields.io/codecov/c/github/remarkjs/react-markdown.svg

[badge-coverage-url]: https://codecov.io/github/remarkjs/react-markdown

[badge-downloads-image]: https://img.shields.io/npm/dm/react-markdown.svg

[badge-downloads-url]: https://www.npmjs.com/package/react-markdown

[badge-size-image]: https://img.shields.io/bundlejs/size/react-markdown

[badge-size-url]: https://bundlejs.com/?q=react-markdown

[commonmark-help]: https://commonmark.org/help/

[commonmark-html]: https://spec.commonmark.org/0.31.2/#html-blocks

[esm]: https://gist.github.com/sindresorhus/a39789f98801d908bbc7ff3ecc99d99c

[esmsh]: https://esm.sh

[file-license]: license

[github-awesome-rehype]: https://github.com/rehypejs/awesome-rehype

[github-awesome-remark]: https://github.com/remarkjs/awesome-remark

[github-conorhastings]: https://github.com/conorhastings

[github-hast-element]: https://github.com/syntax-tree/hast#element

[github-hast-nodes]: https://github.com/syntax-tree/hast#nodes

[github-io-react-markdown]: https://remarkjs.github.io/react-markdown/

[github-mdx]: https://github.com/mdx-js/mdx/

[github-micromark]: https://github.com/micromark/micromark

[github-react-remark]: https://github.com/remarkjs/react-remark

[github-react-syntax-highlighter]: https://github.com/react-syntax-highlighter/react-syntax-highlighter

[github-rehype]: https://github.com/rehypejs/rehype

[github-rehype-katex]: https://github.com/remarkjs/remark-math/tree/main/packages/rehype-katex

[github-rehype-plugins]: https://github.com/rehypejs/rehype/blob/main/doc/plugins.md#list-of-plugins

[github-rehype-raw]: https://github.com/rehypejs/rehype-raw

[github-rehype-react]: https://github.com/rehypejs/rehype-react

[github-rehype-sanitize]: https://github.com/rehypejs/rehype-sanitize

[github-remark]: https://github.com/remarkjs/remark

[github-remark-gfm]: https://github.com/remarkjs/remark-gfm

[github-remark-math]: https://github.com/remarkjs/remark-math

[github-remark-plugins]: https://github.com/remarkjs/remark/blob/main/doc/plugins.md#list-of-plugins

[github-remark-rehype-options]: https://github.com/remarkjs/remark-rehype#options

[github-topic-rehype-plugin]: https://github.com/topics/rehype-plugin

[github-topic-remark-plugin]: https://github.com/topics/remark-plugin

[github-unified]: https://github.com/unifiedjs/unified

[health]: https://github.com/remarkjs/.github

[health-coc]: https://github.com/remarkjs/.github/blob/main/code-of-conduct.md

[health-contributing]: https://github.com/remarkjs/.github/blob/main/contributing.md

[health-support]: https://github.com/remarkjs/.github/blob/main/support.md

[npm-install]: https://docs.npmjs.com/cli/install

[react]: http://reactjs.org

[section-components]: #appendix-b-components

[section-plugins]: #plugins

[section-security]: #security

[section-syntax]: #syntax

[typescript]: https://www.typescriptlang.org
