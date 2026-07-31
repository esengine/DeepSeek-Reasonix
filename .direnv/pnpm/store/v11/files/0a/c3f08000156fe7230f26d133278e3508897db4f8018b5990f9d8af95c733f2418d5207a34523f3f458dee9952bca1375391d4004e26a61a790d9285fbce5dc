# TypeScript Dedent

[![codecov](https://codecov.io/gh/tamino-martinius/node-ts-dedent/branch/master/graph/badge.svg)](https://codecov.io/gh/tamino-martinius/node-ts-dedent)

TypeScript package which smartly trims and strips indentation from multi-line strings.

## Usage Examples

```js
import { dedent } from 'ts-dedent';

console.log(dedent`A string that gets so long you need to break it over
                    multiple lines. Luckily dedent is here to keep it
                    readable without lots of spaces ending up in the string
                    itself.`);

console.log(dedent`
  A string that gets so long you need to break it over
  multiple lines. Luckily dedent is here to keep it
  readable without lots of spaces ending up in the string
  itself.
`);
```

```txt
A string that gets so long you need to break it over
multiple lines. Luckily dedent is here to keep it
readable without lots of spaces ending up in the string
itself.
```

---

```js
console.log(dedent`
  Leading and trailing lines will be trimmed, so you can write something like
  this and have it work as you expect:

    * how convenient it is
    * that I can use an indented list
        - and still have it do the right thing

  That's all.
`);
```

```txt
Leading and trailing lines will be trimmed, so you can write something like
this and have it work as you expect:

  * how convenient it is
  * that I can use an indented list
    - and still have it do the right thing

That's all.
```

---

```js
console.log(dedent`
  Also works fine

  ${1}. With any kind of
  ${2}. Placeholders
`);
```

```txt
Also works fine

1. With any kind of
2. Placeholders
```

---

```js
console.log(dedent(`
  Wait! I lied. Dedent can also be used as a function.
`);
```

```txt
Wait! I lied. Dedent can also be used as a function.
```

## How it works

`dedent` runs the following steps on the string, in order:

1. **Strip a single leading line break.** If the string starts with a newline
   (the common case when you put your first line of text below the opening
   backtick), that one newline is removed. A second blank line is kept, so you
   can intentionally start the output with an empty line.
2. **Strip a single trailing line break and its indentation.** A trailing
   newline followed only by spaces/tabs is removed. This is what lets you put
   the closing backtick on its own indented line without adding a blank line to
   the result.
3. **Find the common indentation.** For every line that contains actual
   content, `dedent` counts its leading spaces and tabs. The "common
   indentation" is the **smallest** of those counts — i.e. the line with the
   _fewest_ leading whitespace characters. Blank lines are ignored, and a line
   that starts with a non-whitespace character counts as `0` (so if any content
   line has no indentation, nothing is stripped).
4. **Remove the common indentation from every line.** Exactly that many leading
   whitespace characters are removed from the start of each line. Lines that
   were indented further keep the difference, so relative indentation (nested
   lists, code blocks, etc.) is preserved.
5. **Interpolate the placeholders.** `${...}` values are inserted. If a value is
   itself a multi-line string, every line after its first is re-indented to
   match the placeholder's position, so nested/multi-line values stay aligned.

So to answer the common question directly: it removes the indentation of the
line with the **fewest** leading whitespace characters — not the first indented
line — and applies that same removal to every line.

A few details worth knowing:

- Indentation is counted in **characters**, where each tab and each space
  counts as one. Removal also works on raw characters, so mixing tabs and
  spaces in your leading whitespace can give surprising results — pick one and
  be consistent.
- Only **one** leading and **one** trailing line break are trimmed. Additional
  blank lines at the very start or end are preserved.

```js
// Lines indented by 4, 2, and 6 spaces. The minimum is 2, so 2 spaces are
// removed from every line:
console.log(dedent`
    four
  two
      six
`);
```

```txt
  four
two
    six
```

## License

MIT

## Based on

- [dedent](https://www.npmjs.com/package/dedent) by ~dmnd
- [dedent-js](https://www.npmjs.com/package/dedent-js) by ~martin-kolarik

## Changelog

See [history](HISTORY.md) for more details.
